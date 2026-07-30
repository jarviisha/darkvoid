package service

import (
	"context"
	stderrors "errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/feature/user/entity"
	"github.com/jarviisha/darkvoid/internal/infrastructure/mailer"
)

type suppressCall struct {
	email  string
	reason entity.SuppressionReason
	detail string
}

type applyCall struct {
	messageID  string
	status     entity.EmailDeliveryStatus
	occurredAt time.Time
}

type mockEmailDeliveryRepo struct {
	created       []recordedSend
	applied       []applyCall
	suppressed    []suppressCall
	applyResult   bool
	applyErr      error
	suppressErr   error
	createErr     error
	lookup        *entity.EmailDelivery
	lookupErr     error
	isSuppressed  bool
	suppressedErr error
}

func (m *mockEmailDeliveryRepo) CreateDelivery(_ context.Context, userID uuid.UUID, providerMessageID, recipient string, kind entity.EmailKind) (*entity.EmailDelivery, error) {
	m.created = append(m.created, recordedSend{userID: userID, messageID: providerMessageID, recipient: recipient, kind: kind})
	if m.createErr != nil {
		return nil, m.createErr
	}
	return &entity.EmailDelivery{UserID: userID, ProviderMessageID: providerMessageID, Recipient: recipient, Kind: kind}, nil
}

func (m *mockEmailDeliveryRepo) GetDeliveryByProviderMessageID(_ context.Context, _ string) (*entity.EmailDelivery, error) {
	if m.lookupErr != nil {
		return nil, m.lookupErr
	}
	return m.lookup, nil
}

func (m *mockEmailDeliveryRepo) ApplyEvent(_ context.Context, providerMessageID string, status entity.EmailDeliveryStatus, occurredAt time.Time) (bool, error) {
	m.applied = append(m.applied, applyCall{messageID: providerMessageID, status: status, occurredAt: occurredAt})
	return m.applyResult, m.applyErr
}

func (m *mockEmailDeliveryRepo) Suppress(_ context.Context, email string, reason entity.SuppressionReason, detail string) error {
	m.suppressed = append(m.suppressed, suppressCall{email: email, reason: reason, detail: detail})
	return m.suppressErr
}

func (m *mockEmailDeliveryRepo) IsSuppressed(_ context.Context, _ string) (bool, error) {
	return m.isSuppressed, m.suppressedErr
}

func (m *mockEmailDeliveryRepo) Unsuppress(_ context.Context, _ string) (bool, error) {
	return true, nil
}

func (m *mockEmailDeliveryRepo) ListSuppressions(_ context.Context, _ int32) ([]*entity.EmailSuppression, error) {
	return nil, nil
}

func newEmailEventServiceForTest(repo emailDeliveryRepo) *EmailEventService {
	svc := NewEmailEventService(repo)
	svc.now = func() time.Time { return time.Unix(1_800_000_000, 0) }
	return svc
}

func TestRecordSend_SkipsEmptyMessageID(t *testing.T) {
	repo := &mockEmailDeliveryRepo{applyResult: true}
	svc := newEmailEventServiceForTest(repo)

	if err := svc.RecordSend(context.Background(), uuid.New(), "", "user@example.com", entity.EmailKindWelcome); err != nil {
		t.Fatalf("RecordSend: unexpected error: %v", err)
	}
	if len(repo.created) != 0 {
		t.Errorf("recorded %d deliveries, want 0 — the nop mailer has no id for a webhook to match", len(repo.created))
	}
}

func TestRecordSend_StoresTheSend(t *testing.T) {
	repo := &mockEmailDeliveryRepo{}
	svc := newEmailEventServiceForTest(repo)
	userID := uuid.New()

	if err := svc.RecordSend(context.Background(), userID, "msg-1", "user@example.com", entity.EmailKindVerifyEmail); err != nil {
		t.Fatalf("RecordSend: unexpected error: %v", err)
	}
	if len(repo.created) != 1 {
		t.Fatalf("recorded %d deliveries, want 1", len(repo.created))
	}
	got := repo.created[0]
	if got.userID != userID || got.messageID != "msg-1" || got.kind != entity.EmailKindVerifyEmail {
		t.Errorf("recorded %+v, want the send's user, id and kind", got)
	}
}

func TestHandleResendEvent_DeliveredUpdatesStatusWithoutSuppressing(t *testing.T) {
	repo := &mockEmailDeliveryRepo{applyResult: true}
	svc := newEmailEventServiceForTest(repo)
	occurredAt := time.Unix(1_799_999_000, 0).UTC()

	err := svc.HandleResendEvent(context.Background(), &mailer.WebhookEvent{
		Type:       mailer.EventEmailDelivered,
		MessageID:  "msg-1",
		Recipients: []string{"user@example.com"},
		OccurredAt: occurredAt,
	})
	if err != nil {
		t.Fatalf("HandleResendEvent: unexpected error: %v", err)
	}

	if len(repo.applied) != 1 {
		t.Fatalf("applied %d events, want 1", len(repo.applied))
	}
	if repo.applied[0].status != entity.EmailDeliveryDelivered {
		t.Errorf("status = %q, want delivered", repo.applied[0].status)
	}
	if !repo.applied[0].occurredAt.Equal(occurredAt) {
		t.Errorf("occurredAt = %v, want the event's own timestamp %v", repo.applied[0].occurredAt, occurredAt)
	}
	if len(repo.suppressed) != 0 {
		t.Errorf("suppressed %v, want nothing for a delivered event", repo.suppressed)
	}
}

func TestHandleResendEvent_PermanentBounceSuppresses(t *testing.T) {
	repo := &mockEmailDeliveryRepo{applyResult: true}
	svc := newEmailEventServiceForTest(repo)

	err := svc.HandleResendEvent(context.Background(), &mailer.WebhookEvent{
		Type:       mailer.EventEmailBounced,
		MessageID:  "msg-1",
		Recipients: []string{"gone@example.com"},
		OccurredAt: time.Unix(1_799_999_000, 0),
		BounceType: "Permanent",
		Detail:     "Permanent General The recipient does not exist",
	})
	if err != nil {
		t.Fatalf("HandleResendEvent: unexpected error: %v", err)
	}

	if len(repo.suppressed) != 1 {
		t.Fatalf("suppressed %d addresses, want 1", len(repo.suppressed))
	}
	got := repo.suppressed[0]
	if got.email != "gone@example.com" || got.reason != entity.SuppressionBounced {
		t.Errorf("suppressed %+v, want gone@example.com as bounced", got)
	}
	if got.detail == "" {
		t.Error("detail is empty, want the provider's bounce message on the record")
	}
}

func TestHandleResendEvent_TransientBounceDoesNotSuppress(t *testing.T) {
	repo := &mockEmailDeliveryRepo{applyResult: true}
	svc := newEmailEventServiceForTest(repo)

	err := svc.HandleResendEvent(context.Background(), &mailer.WebhookEvent{
		Type:       mailer.EventEmailBounced,
		MessageID:  "msg-1",
		Recipients: []string{"full-mailbox@example.com"},
		OccurredAt: time.Unix(1_799_999_000, 0),
		BounceType: "Transient",
	})
	if err != nil {
		t.Fatalf("HandleResendEvent: unexpected error: %v", err)
	}

	if len(repo.applied) != 1 || repo.applied[0].status != entity.EmailDeliveryBounced {
		t.Errorf("applied = %+v, want the bounce still recorded", repo.applied)
	}
	if len(repo.suppressed) != 0 {
		t.Errorf("suppressed %v, want nothing — a full mailbox fixes itself and would otherwise lock the user out of password resets", repo.suppressed)
	}
}

func TestHandleResendEvent_ComplaintAlwaysSuppresses(t *testing.T) {
	repo := &mockEmailDeliveryRepo{applyResult: true}
	svc := newEmailEventServiceForTest(repo)

	err := svc.HandleResendEvent(context.Background(), &mailer.WebhookEvent{
		Type:       mailer.EventEmailComplained,
		MessageID:  "msg-1",
		Recipients: []string{"annoyed@example.com"},
		OccurredAt: time.Unix(1_799_999_000, 0),
	})
	if err != nil {
		t.Fatalf("HandleResendEvent: unexpected error: %v", err)
	}

	if len(repo.suppressed) != 1 || repo.suppressed[0].reason != entity.SuppressionComplained {
		t.Fatalf("suppressed = %+v, want one complained entry", repo.suppressed)
	}
}

func TestHandleResendEvent_FallsBackToTheRecordedRecipient(t *testing.T) {
	repo := &mockEmailDeliveryRepo{
		applyResult: true,
		lookup:      &entity.EmailDelivery{ProviderMessageID: "msg-1", Recipient: "recorded@example.com"},
	}
	svc := newEmailEventServiceForTest(repo)

	err := svc.HandleResendEvent(context.Background(), &mailer.WebhookEvent{
		Type:       mailer.EventEmailBounced,
		MessageID:  "msg-1",
		OccurredAt: time.Unix(1_799_999_000, 0),
		BounceType: "Permanent",
	})
	if err != nil {
		t.Fatalf("HandleResendEvent: unexpected error: %v", err)
	}

	if len(repo.suppressed) != 1 || repo.suppressed[0].email != "recorded@example.com" {
		t.Fatalf("suppressed = %+v, want the address recorded at send time", repo.suppressed)
	}
}

func TestHandleResendEvent_UnknownRecipientIsNotAnError(t *testing.T) {
	repo := &mockEmailDeliveryRepo{applyResult: false, lookupErr: stderrors.New("not found")}
	svc := newEmailEventServiceForTest(repo)

	err := svc.HandleResendEvent(context.Background(), &mailer.WebhookEvent{
		Type:       mailer.EventEmailBounced,
		MessageID:  "unknown",
		OccurredAt: time.Unix(1_799_999_000, 0),
		BounceType: "Permanent",
	})
	if err != nil {
		t.Fatalf("an event for an unrecorded send must not be retried, got: %v", err)
	}
	if len(repo.suppressed) != 0 {
		t.Errorf("suppressed %v, want nothing when no address is known", repo.suppressed)
	}
}

func TestHandleResendEvent_UnmodelledTypeIsIgnored(t *testing.T) {
	repo := &mockEmailDeliveryRepo{applyResult: true}
	svc := newEmailEventServiceForTest(repo)

	err := svc.HandleResendEvent(context.Background(), &mailer.WebhookEvent{
		Type:      "email.opened",
		MessageID: "msg-1",
	})
	if err != nil {
		t.Fatalf("an unmodelled event must be accepted, got: %v", err)
	}
	if len(repo.applied) != 0 {
		t.Errorf("applied %+v, want nothing for an event we do not model", repo.applied)
	}
}

func TestHandleResendEvent_MissingTimestampUsesArrivalTime(t *testing.T) {
	repo := &mockEmailDeliveryRepo{applyResult: true}
	svc := newEmailEventServiceForTest(repo)

	err := svc.HandleResendEvent(context.Background(), &mailer.WebhookEvent{
		Type:      mailer.EventEmailDelivered,
		MessageID: "msg-1",
	})
	if err != nil {
		t.Fatalf("HandleResendEvent: unexpected error: %v", err)
	}
	if len(repo.applied) != 1 {
		t.Fatalf("applied %d events, want 1 — a timestampless event is still an event", len(repo.applied))
	}
	if !repo.applied[0].occurredAt.Equal(time.Unix(1_800_000_000, 0)) {
		t.Errorf("occurredAt = %v, want the injected arrival time", repo.applied[0].occurredAt)
	}
}

func TestHandleResendEvent_RepositoryFailureIsRetryable(t *testing.T) {
	repo := &mockEmailDeliveryRepo{applyErr: stderrors.New("database is down")}
	svc := newEmailEventServiceForTest(repo)

	err := svc.HandleResendEvent(context.Background(), &mailer.WebhookEvent{
		Type:       mailer.EventEmailBounced,
		MessageID:  "msg-1",
		OccurredAt: time.Unix(1_799_999_000, 0),
		BounceType: "Permanent",
	})
	if err == nil {
		t.Fatal("expected an error so the caller answers 5xx and the provider retries")
	}
}

func TestHandleResendEvent_SuppressFailureIsRetryable(t *testing.T) {
	repo := &mockEmailDeliveryRepo{applyResult: true, suppressErr: stderrors.New("database is down")}
	svc := newEmailEventServiceForTest(repo)

	err := svc.HandleResendEvent(context.Background(), &mailer.WebhookEvent{
		Type:       mailer.EventEmailComplained,
		MessageID:  "msg-1",
		Recipients: []string{"annoyed@example.com"},
		OccurredAt: time.Unix(1_799_999_000, 0),
	})
	if err == nil {
		t.Fatal("expected an error: a suppression lost to a database blip is one we never make")
	}
}
