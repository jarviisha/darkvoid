package mailer

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Built at runtime rather than pasted as a literal so it cannot read as a real
// leaked credential.
var testWebhookSecret = "whsec_" + base64.StdEncoding.EncodeToString([]byte("test-secret-key-material"))

// testWebhookID is the svix-id every signed test request carries.
const testWebhookID = "msg_1"

// signWebhook builds the headers Svix would send for body at the given time.
func signWebhook(t *testing.T, secret string, at time.Time, body []byte) http.Header {
	t.Helper()

	key, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(secret, "whsec_"))
	if err != nil {
		t.Fatalf("decode test secret: %v", err)
	}

	timestamp := strconv.FormatInt(at.Unix(), 10)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(testWebhookID + "." + timestamp + "."))
	mac.Write(body)

	header := http.Header{}
	header.Set("svix-id", testWebhookID)
	header.Set("svix-timestamp", timestamp)
	header.Set("svix-signature", "v1,"+base64.StdEncoding.EncodeToString(mac.Sum(nil)))
	return header
}

func newTestVerifier(t *testing.T, at time.Time) *ResendWebhookVerifier {
	t.Helper()

	v, err := NewResendWebhookVerifier(testWebhookSecret)
	if err != nil {
		t.Fatalf("NewResendWebhookVerifier: %v", err)
	}
	v.now = func() time.Time { return at }
	return v
}

func TestNewResendWebhookVerifier_RejectsUnusableSecrets(t *testing.T) {
	for _, secret := range []string{"", "whsec_", "   ", "whsec_not!base64!"} {
		if _, err := NewResendWebhookVerifier(secret); err == nil {
			t.Errorf("NewResendWebhookVerifier(%q) = nil error, want a failure", secret)
		}
	}
}

func TestNewResendWebhookVerifier_AcceptsSecretWithoutPrefix(t *testing.T) {
	if _, err := NewResendWebhookVerifier("dGVzdC1zZWNyZXQta2V5LW1hdGVyaWFs"); err != nil {
		t.Errorf("a secret pasted without the whsec_ prefix should still work, got: %v", err)
	}
}

func TestWebhookVerify_ValidSignature(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	body := []byte(`{"type":"email.delivered","data":{"email_id":"abc"}}`)

	v := newTestVerifier(t, now)
	if err := v.Verify(signWebhook(t, testWebhookSecret, now, body), body); err != nil {
		t.Fatalf("Verify: unexpected error: %v", err)
	}
}

func TestWebhookVerify_AcceptsOneOfSeveralSignatures(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	body := []byte(`{"type":"email.delivered","data":{"email_id":"abc"}}`)

	header := signWebhook(t, testWebhookSecret, now, body)
	// During a secret rotation the header carries one entry per active secret, and
	// only ours will match.
	header.Set("svix-signature", "v1,"+base64.StdEncoding.EncodeToString([]byte("from-the-old-secret"))+" "+header.Get("svix-signature"))

	if err := newTestVerifier(t, now).Verify(header, body); err != nil {
		t.Fatalf("Verify: unexpected error with a rotated-secret header: %v", err)
	}
}

func TestWebhookVerify_TamperedBody(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	body := []byte(`{"type":"email.delivered","data":{"email_id":"abc"}}`)
	header := signWebhook(t, testWebhookSecret, now, body)

	err := newTestVerifier(t, now).Verify(header, []byte(`{"type":"email.bounced","data":{"email_id":"abc"}}`))
	if !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("err = %v, want ErrInvalidSignature", err)
	}
}

func TestWebhookVerify_WrongSecret(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	body := []byte(`{"type":"email.delivered","data":{"email_id":"abc"}}`)
	header := signWebhook(t, "whsec_YW5vdGhlci1zZWNyZXQ=", now, body)

	if err := newTestVerifier(t, now).Verify(header, body); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("err = %v, want ErrInvalidSignature", err)
	}
}

func TestWebhookVerify_MissingHeaders(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	body := []byte(`{}`)
	v := newTestVerifier(t, now)

	for _, drop := range []string{"svix-id", "svix-timestamp", "svix-signature"} {
		header := signWebhook(t, testWebhookSecret, now, body)
		header.Del(drop)
		if err := v.Verify(header, body); !errors.Is(err, ErrInvalidSignature) {
			t.Errorf("without %s: err = %v, want ErrInvalidSignature", drop, err)
		}
	}
}

func TestWebhookVerify_StaleTimestampIsRejected(t *testing.T) {
	signedAt := time.Unix(1_800_000_000, 0)
	body := []byte(`{"type":"email.delivered","data":{"email_id":"abc"}}`)
	header := signWebhook(t, testWebhookSecret, signedAt, body)

	// A captured request stays validly signed forever, so the timestamp is the
	// only thing that expires a replay.
	v := newTestVerifier(t, signedAt.Add(webhookTolerance+time.Minute))
	if err := v.Verify(header, body); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("err = %v, want ErrInvalidSignature for a replayed request", err)
	}

	// Clock skew in the other direction is rejected too.
	v = newTestVerifier(t, signedAt.Add(-(webhookTolerance + time.Minute)))
	if err := v.Verify(header, body); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("err = %v, want ErrInvalidSignature for a future timestamp", err)
	}

	// Just inside the window still verifies.
	v = newTestVerifier(t, signedAt.Add(webhookTolerance-time.Second))
	if err := v.Verify(header, body); err != nil {
		t.Fatalf("a timestamp inside the tolerance should verify, got: %v", err)
	}
}

func TestWebhookVerify_MalformedTimestamp(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	body := []byte(`{}`)
	header := signWebhook(t, testWebhookSecret, now, body)
	header.Set("svix-timestamp", "not-a-number")

	if err := newTestVerifier(t, now).Verify(header, body); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("err = %v, want ErrInvalidSignature", err)
	}
}

func TestParseResendWebhook_Bounce(t *testing.T) {
	body := []byte(`{
		"type": "email.bounced",
		"created_at": "2026-07-30T03:15:00.123Z",
		"data": {
			"email_id": "4ef9a417-02e9-4d39-ad75-9611e0fcc33c",
			"to": ["user@example.com"],
			"subject": "Verify your email - DarkVoid",
			"bounce": {"type": "Permanent", "subType": "General", "message": "The recipient does not exist"}
		}
	}`)

	event, err := ParseResendWebhook(body)
	if err != nil {
		t.Fatalf("ParseResendWebhook: %v", err)
	}

	if event.Type != EventEmailBounced {
		t.Errorf("Type = %q, want %q", event.Type, EventEmailBounced)
	}
	if event.MessageID != "4ef9a417-02e9-4d39-ad75-9611e0fcc33c" {
		t.Errorf("MessageID = %q", event.MessageID)
	}
	if len(event.Recipients) != 1 || event.Recipients[0] != "user@example.com" {
		t.Errorf("Recipients = %v", event.Recipients)
	}
	if event.BounceType != "Permanent" {
		t.Errorf("BounceType = %q, want Permanent — this decides whether we suppress", event.BounceType)
	}
	if !strings.Contains(event.Detail, "does not exist") {
		t.Errorf("Detail = %q, want the provider's message", event.Detail)
	}
	if event.OccurredAt.IsZero() {
		t.Error("OccurredAt is zero, want the parsed created_at")
	}
}

func TestParseResendWebhook_DeliveredWithoutBounceBlock(t *testing.T) {
	body := []byte(`{"type":"email.delivered","created_at":"2026-07-30 03:15:00.123456+00","data":{"email_id":"abc","to":["user@example.com"]}}`)

	event, err := ParseResendWebhook(body)
	if err != nil {
		t.Fatalf("ParseResendWebhook: %v", err)
	}
	if event.BounceType != "" || event.Detail != "" {
		t.Errorf("BounceType = %q, Detail = %q, want both empty", event.BounceType, event.Detail)
	}
	if event.OccurredAt.IsZero() {
		t.Error("OccurredAt is zero — the space-separated timestamp form should still parse")
	}
}

func TestParseResendWebhook_UnparseableTimestampIsNotFatal(t *testing.T) {
	body := []byte(`{"type":"email.delivered","created_at":"whenever","data":{"email_id":"abc"}}`)

	event, err := ParseResendWebhook(body)
	if err != nil {
		t.Fatalf("a bad timestamp must not fail the event: %v", err)
	}
	if !event.OccurredAt.IsZero() {
		t.Errorf("OccurredAt = %v, want the zero time so the consumer can substitute its clock", event.OccurredAt)
	}
}

func TestParseResendWebhook_Rejects(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"not json", `not json at all`},
		{"no event type", `{"data":{"email_id":"abc"}}`},
		{"no email id", `{"type":"email.delivered","data":{}}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ParseResendWebhook([]byte(tt.body)); err == nil {
				t.Error("expected an error, got nil")
			}
		})
	}
}
