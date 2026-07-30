package entity

import (
	"time"

	"github.com/google/uuid"
)

// EmailKind identifies which account-mail flow produced a send.
type EmailKind string

const (
	EmailKindWelcome       EmailKind = "welcome"
	EmailKindVerifyEmail   EmailKind = "verify_email"
	EmailKindResetPassword EmailKind = "reset_password"
)

// EmailDeliveryStatus is the last outcome the provider reported for a send.
// "sent" is what we record ourselves at hand-off; everything else arrives by
// webhook.
type EmailDeliveryStatus string

const (
	EmailDeliverySent       EmailDeliveryStatus = "sent"
	EmailDeliveryDelivered  EmailDeliveryStatus = "delivered"
	EmailDeliveryDelayed    EmailDeliveryStatus = "delivery_delayed"
	EmailDeliveryBounced    EmailDeliveryStatus = "bounced"
	EmailDeliveryComplained EmailDeliveryStatus = "complained"
)

// EmailDelivery is one send, keyed by the provider's message id so a later
// delivery report can be attributed back to a user.
type EmailDelivery struct {
	ID                uuid.UUID
	UserID            uuid.UUID
	ProviderMessageID string
	Recipient         string
	Kind              EmailKind
	Status            EmailDeliveryStatus
	// LastEventAt is when the last applied provider event occurred, per the event
	// payload — nil until the first webhook lands.
	LastEventAt *time.Time
	CreatedAt   time.Time
}

// SuppressionReason records why an address stopped being mailable.
type SuppressionReason string

const (
	SuppressionBounced    SuppressionReason = "bounced"
	SuppressionComplained SuppressionReason = "complained"
)

// EmailSuppression is an address we must not send to. Sending to a known-dead
// mailbox or to someone who marked us as spam costs sender reputation, which is
// shared across every other email the app sends.
type EmailSuppression struct {
	Email     string
	Reason    SuppressionReason
	Detail    string
	CreatedAt time.Time
}
