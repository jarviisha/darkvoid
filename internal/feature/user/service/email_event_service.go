package service

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/feature/user/entity"
	"github.com/jarviisha/darkvoid/internal/infrastructure/mailer"
	"github.com/jarviisha/darkvoid/pkg/logger"
)

// emailDeliveryRepo defines the repository operations EmailEventService needs.
type emailDeliveryRepo interface {
	CreateDelivery(ctx context.Context, userID uuid.UUID, providerMessageID, recipient string, kind entity.EmailKind) (*entity.EmailDelivery, error)
	GetDeliveryByProviderMessageID(ctx context.Context, providerMessageID string) (*entity.EmailDelivery, error)
	ApplyEvent(ctx context.Context, providerMessageID string, status entity.EmailDeliveryStatus, occurredAt time.Time) (bool, error)
	Suppress(ctx context.Context, email string, reason entity.SuppressionReason, detail string) error
	IsSuppressed(ctx context.Context, email string) (bool, error)
	Unsuppress(ctx context.Context, email string) (bool, error)
	ListSuppressions(ctx context.Context, limit int32) ([]*entity.EmailSuppression, error)
}

// EmailEventService records outgoing account mail and applies the delivery
// reports the provider sends back.
type EmailEventService struct {
	repo emailDeliveryRepo
	// now is injectable so a webhook carrying no usable timestamp can be tested.
	now func() time.Time
}

// NewEmailEventService creates a new EmailEventService.
func NewEmailEventService(repo emailDeliveryRepo) *EmailEventService {
	return &EmailEventService{repo: repo, now: time.Now}
}

// RecordSend logs an accepted send so a later delivery report can be attributed
// to a user. Errors are returned for the caller to log — a send that already left
// must not be reported as failed because bookkeeping did.
func (s *EmailEventService) RecordSend(
	ctx context.Context,
	userID uuid.UUID,
	providerMessageID, recipient string,
	kind entity.EmailKind,
) error {
	if providerMessageID == "" {
		// The nop mailer has no id, and there is nothing a webhook could ever
		// match against — recording it would only add unmatched rows.
		return nil
	}

	_, err := s.repo.CreateDelivery(ctx, userID, providerMessageID, recipient, kind)
	return err
}

// IsSuppressed reports whether an address must not be mailed.
// This is the mailer.SuppressionChecker implementation.
func (s *EmailEventService) IsSuppressed(ctx context.Context, email string) (bool, error) {
	return s.repo.IsSuppressed(ctx, email)
}

// Unsuppress lifts a suppression and reports whether one existed.
func (s *EmailEventService) Unsuppress(ctx context.Context, email string) (bool, error) {
	return s.repo.Unsuppress(ctx, email)
}

// ListSuppressions returns the most recently suppressed addresses.
func (s *EmailEventService) ListSuppressions(ctx context.Context, limit int32) ([]*entity.EmailSuppression, error) {
	return s.repo.ListSuppressions(ctx, limit)
}

// HandleResendEvent applies one verified webhook event.
//
// Returning an error tells the caller to answer 5xx, which makes the provider
// retry. That is only wanted for faults on our side: an event we do not model,
// or one for a send we never recorded, is answered 2xx so the provider stops
// re-sending something we will never accept.
func (s *EmailEventService) HandleResendEvent(ctx context.Context, event *mailer.WebhookEvent) error {
	status, ok := statusForEventType(event.Type)
	if !ok {
		logger.Info(ctx, "ignoring unmodelled email event", "type", event.Type, "message_id", event.MessageID)
		return nil
	}

	occurredAt := event.OccurredAt
	if occurredAt.IsZero() {
		// Ordering degrades to arrival order for this one event. Better than
		// dropping it: a bounce we cannot timestamp is still a bounce.
		occurredAt = s.now()
		logger.Warn(ctx, "email event carried no usable timestamp, using arrival time",
			"type", event.Type, "message_id", event.MessageID)
	}

	applied, err := s.repo.ApplyEvent(ctx, event.MessageID, status, occurredAt)
	if err != nil {
		return err
	}
	if !applied {
		// Either the send predates the delivery log, or a newer event already
		// landed. Neither is retryable.
		logger.Info(ctx, "email event did not apply to any send",
			"type", event.Type, "message_id", event.MessageID)
	}

	reason, suppress := suppressionReasonFor(event)
	if !suppress {
		return nil
	}

	recipients := s.recipientsFor(ctx, event)
	if len(recipients) == 0 {
		logger.Warn(ctx, "cannot suppress: event names no recipient and the send is unknown",
			"type", event.Type, "message_id", event.MessageID)
		return nil
	}

	for _, recipient := range recipients {
		if err := s.repo.Suppress(ctx, recipient, reason, event.Detail); err != nil {
			return err
		}
		logger.Warn(ctx, "suppressed email address",
			"email", recipient, "reason", reason, "detail", event.Detail)
	}

	return nil
}

// recipientsFor prefers the addresses named in the event and falls back to the
// one recorded at send time, which is all a payload without a "to" gives us.
func (s *EmailEventService) recipientsFor(ctx context.Context, event *mailer.WebhookEvent) []string {
	recipients := make([]string, 0, len(event.Recipients))
	for _, r := range event.Recipients {
		if trimmed := strings.TrimSpace(r); trimmed != "" {
			recipients = append(recipients, trimmed)
		}
	}
	if len(recipients) > 0 {
		return recipients
	}

	delivery, err := s.repo.GetDeliveryByProviderMessageID(ctx, event.MessageID)
	if err != nil || delivery == nil {
		return nil
	}
	return []string{delivery.Recipient}
}

// statusForEventType maps a provider event to a delivery status. Unknown types
// report false: Resend adds event types over time and we only model the ones
// that change what we know about a send.
func statusForEventType(eventType string) (entity.EmailDeliveryStatus, bool) {
	switch eventType {
	case mailer.EventEmailSent:
		return entity.EmailDeliverySent, true
	case mailer.EventEmailDelivered:
		return entity.EmailDeliveryDelivered, true
	case mailer.EventEmailDelayed:
		return entity.EmailDeliveryDelayed, true
	case mailer.EventEmailBounced:
		return entity.EmailDeliveryBounced, true
	case mailer.EventEmailComplained:
		return entity.EmailDeliveryComplained, true
	default:
		return "", false
	}
}

// suppressionReasonFor decides whether an event should stop us mailing an address.
//
// A complaint always does — someone pressed "this is spam". A bounce only does
// when it is permanent: "Transient" covers a full mailbox or a greylisting server,
// and suppressing on that would lock a real user out of password resets over a
// problem that fixes itself.
func suppressionReasonFor(event *mailer.WebhookEvent) (entity.SuppressionReason, bool) {
	switch event.Type {
	case mailer.EventEmailComplained:
		return entity.SuppressionComplained, true
	case mailer.EventEmailBounced:
		if strings.EqualFold(event.BounceType, "Transient") {
			return "", false
		}
		return entity.SuppressionBounced, true
	default:
		return "", false
	}
}
