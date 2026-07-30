package mailer

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Webhook event types Resend sends. Anything not listed here is ignored by the
// consumer rather than rejected — Resend adds event types over time, and a 4xx
// on an unknown one would make it retry something we will never accept.
const (
	EventEmailSent       = "email.sent"
	EventEmailDelivered  = "email.delivered"
	EventEmailDelayed    = "email.delivery_delayed"
	EventEmailBounced    = "email.bounced"
	EventEmailComplained = "email.complained"
)

// webhookTolerance is how far a webhook's timestamp may be from our clock.
// Bounding it is what makes the signature check replay-resistant: a captured
// request stays validly signed forever, so the timestamp is the only thing that
// expires it.
const webhookTolerance = 5 * time.Minute

// ErrInvalidSignature reports a webhook that failed verification. Callers map it
// to 401 — the signature is the only authentication this endpoint has.
var ErrInvalidSignature = errors.New("mailer: webhook signature verification failed")

// ResendWebhookVerifier verifies the Svix signature Resend puts on webhooks.
//
// Resend delivers webhooks through Svix, so the scheme is Svix's: HMAC-SHA256
// over "{svix-id}.{svix-timestamp}.{body}" keyed by the endpoint secret. This is
// ~50 lines, which is why it is here rather than pulling in the svix SDK for one
// function.
type ResendWebhookVerifier struct {
	secret []byte
	// now is injectable so the timestamp tolerance can be tested without waiting.
	// Set once at construction and never written again.
	now func() time.Time
}

// NewResendWebhookVerifier creates a verifier from a Resend endpoint secret
// (the "whsec_..." value from the webhook's settings page).
func NewResendWebhookVerifier(secret string) (*ResendWebhookVerifier, error) {
	raw := strings.TrimPrefix(strings.TrimSpace(secret), "whsec_")
	if raw == "" {
		return nil, errors.New("mailer: webhook secret is empty")
	}

	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("mailer: webhook secret is not valid base64: %w", err)
	}

	return &ResendWebhookVerifier{secret: key, now: time.Now}, nil
}

// Verify checks the Svix headers against the raw request body.
//
// The body must be the exact bytes received: the signature covers them verbatim,
// so anything that re-encodes the JSON first will fail to verify.
func (v *ResendWebhookVerifier) Verify(header http.Header, body []byte) error {
	id := header.Get("svix-id")
	timestamp := header.Get("svix-timestamp")
	signatures := header.Get("svix-signature")
	if id == "" || timestamp == "" || signatures == "" {
		return fmt.Errorf("%w: missing svix headers", ErrInvalidSignature)
	}

	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return fmt.Errorf("%w: malformed svix-timestamp", ErrInvalidSignature)
	}
	drift := v.now().Sub(time.Unix(seconds, 0))
	if drift < -webhookTolerance || drift > webhookTolerance {
		return fmt.Errorf("%w: timestamp is outside the %s tolerance", ErrInvalidSignature, webhookTolerance)
	}

	mac := hmac.New(sha256.New, v.secret)
	mac.Write([]byte(id + "." + timestamp + "."))
	mac.Write(body)
	expected := mac.Sum(nil)

	// The header carries a space-separated list, and during a secret rotation it
	// holds one entry per active secret — so a non-matching entry is normal and
	// only the whole list failing is an error.
	for _, entry := range strings.Split(signatures, " ") {
		version, encoded, ok := strings.Cut(entry, ",")
		if !ok || version != "v1" {
			continue
		}
		got, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			continue
		}
		if hmac.Equal(got, expected) {
			return nil
		}
	}

	return fmt.Errorf("%w: no v1 signature matched", ErrInvalidSignature)
}

// WebhookEvent is a provider delivery report, normalised so consumers do not
// parse Resend's payload shape themselves.
type WebhookEvent struct {
	// Type is the provider's event name, e.g. "email.bounced".
	Type string
	// MessageID matches the id Send returned for the original send.
	MessageID string
	// Recipients is who the event concerns. May be empty — the consumer then
	// falls back to the recipient recorded at send time.
	Recipients []string
	// OccurredAt is when the provider says the event happened. Zero when the
	// payload carried no parseable timestamp.
	OccurredAt time.Time
	// BounceType is Resend's classification, "Permanent" or "Transient". Only set
	// on bounces, and load-bearing: a transient bounce (a full mailbox) must not
	// suppress an address forever.
	BounceType string
	// Detail is a human-readable reason, for logs and the suppression record.
	Detail string
}

// resendWebhookPayload mirrors the JSON Resend posts.
type resendWebhookPayload struct {
	Type      string `json:"type"`
	CreatedAt string `json:"created_at"`
	Data      struct {
		EmailID string   `json:"email_id"`
		To      []string `json:"to"`
		Subject string   `json:"subject"`
		Bounce  *struct {
			Type    string `json:"type"`
			SubType string `json:"subType"`
			Message string `json:"message"`
		} `json:"bounce"`
	} `json:"data"`
}

// ParseResendWebhook decodes a verified webhook body into a WebhookEvent.
func ParseResendWebhook(body []byte) (*WebhookEvent, error) {
	var payload resendWebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("mailer: decode webhook payload: %w", err)
	}
	if payload.Type == "" {
		return nil, errors.New("mailer: webhook payload has no event type")
	}
	if payload.Data.EmailID == "" {
		return nil, errors.New("mailer: webhook payload has no email_id")
	}

	event := &WebhookEvent{
		Type:       payload.Type,
		MessageID:  payload.Data.EmailID,
		Recipients: payload.Data.To,
		OccurredAt: parseWebhookTime(payload.CreatedAt),
	}

	if payload.Data.Bounce != nil {
		event.BounceType = payload.Data.Bounce.Type
		event.Detail = strings.TrimSpace(strings.Join([]string{
			payload.Data.Bounce.Type,
			payload.Data.Bounce.SubType,
			payload.Data.Bounce.Message,
		}, " "))
	}

	return event, nil
}

// parseWebhookTime accepts the formats Resend has used for created_at, and
// returns the zero time when none match.
//
// It does not fail the whole event on an unparseable timestamp: the timestamp
// only orders events against each other, and losing that ordering for one event
// is much cheaper than rejecting a bounce we would otherwise act on.
func parseWebhookTime(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}

	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999-07",
		"2006-01-02 15:04:05.999999Z07:00",
		"2006-01-02 15:04:05",
	} {
		if t, err := time.Parse(layout, value); err == nil {
			return t
		}
	}
	return time.Time{}
}
