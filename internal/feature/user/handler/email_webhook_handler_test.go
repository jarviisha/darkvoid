package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jarviisha/darkvoid/internal/infrastructure/mailer"
)

// stubVerifier accepts or rejects without doing real crypto — the signature
// scheme itself is covered in the mailer package.
type stubVerifier struct {
	err    error
	bodies [][]byte
}

func (v *stubVerifier) Verify(_ http.Header, body []byte) error {
	v.bodies = append(v.bodies, body)
	return v.err
}

type stubEventApplier struct {
	events []*mailer.WebhookEvent
	err    error
}

func (a *stubEventApplier) HandleResendEvent(_ context.Context, event *mailer.WebhookEvent) error {
	a.events = append(a.events, event)
	return a.err
}

const bouncePayload = `{"type":"email.bounced","created_at":"2026-07-30T03:15:00.000Z","data":{"email_id":"msg-1","to":["gone@example.com"],"bounce":{"type":"Permanent","message":"no such user"}}}`

func postWebhook(t *testing.T, h *EmailWebhookHandler, body string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/webhooks/resend", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.HandleResend(rec, req)
	return rec
}

func TestHandleResend_Success(t *testing.T) {
	verifier := &stubVerifier{}
	events := &stubEventApplier{}
	h := NewEmailWebhookHandler(verifier, events)

	rec := postWebhook(t, h, bouncePayload)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", rec.Code, rec.Body.String())
	}
	if len(events.events) != 1 {
		t.Fatalf("applied %d events, want 1", len(events.events))
	}
	if events.events[0].MessageID != "msg-1" || events.events[0].BounceType != "Permanent" {
		t.Errorf("event = %+v, want the parsed payload", events.events[0])
	}
	if len(verifier.bodies) != 1 || string(verifier.bodies[0]) != bouncePayload {
		t.Error("verifier did not see the exact request bytes — the signature covers them verbatim")
	}
}

func TestHandleResend_InvalidSignatureIsUnauthorized(t *testing.T) {
	events := &stubEventApplier{}
	h := NewEmailWebhookHandler(&stubVerifier{err: mailer.ErrInvalidSignature}, events)

	rec := postWebhook(t, h, bouncePayload)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if len(events.events) != 0 {
		t.Error("an unverified webhook must not reach the event service")
	}
}

func TestHandleResend_MalformedPayloadIsBadRequest(t *testing.T) {
	events := &stubEventApplier{}
	h := NewEmailWebhookHandler(&stubVerifier{}, events)

	rec := postWebhook(t, h, `{"type":"email.bounced","data":{}}`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 — a signed but unusable payload will fail every retry too", rec.Code)
	}
	if len(events.events) != 0 {
		t.Error("a payload that did not parse must not reach the event service")
	}
}

func TestHandleResend_ApplyFailureIsServerErrorSoTheProviderRetries(t *testing.T) {
	events := &stubEventApplier{err: errors.New("database is down")}
	h := NewEmailWebhookHandler(&stubVerifier{}, events)

	rec := postWebhook(t, h, bouncePayload)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}
