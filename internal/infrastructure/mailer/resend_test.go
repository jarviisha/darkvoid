package mailer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// newTestResendMailer points a ResendMailer at a stub server and shrinks the
// backoff so the retry paths cost microseconds instead of seconds.
func newTestResendMailer(t *testing.T, handler http.HandlerFunc) *ResendMailer {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	m, err := NewResendMailer(Config{
		Provider: "resend",
		APIKey:   "re_test_key",
		From:     "DarkVoid <noreply@darkvoid.test>",
	})
	if err != nil {
		t.Fatalf("NewResendMailer: unexpected error: %v", err)
	}
	m.baseURL = srv.URL
	m.baseBackoff = time.Millisecond

	return m
}

func testMessage() *Message {
	return &Message{
		To:      []string{"user@example.com"},
		Subject: "Verify your email - DarkVoid",
		HTML:    "<p>hello</p>",
		Text:    "hello",
	}
}

func TestNewResendMailer_MissingAPIKey(t *testing.T) {
	_, err := NewResendMailer(Config{Provider: "resend", From: "noreply@darkvoid.test"})
	if err == nil {
		t.Fatal("expected an error when RESEND_API_KEY is empty, got nil")
	}
	if !strings.Contains(err.Error(), "RESEND_API_KEY") {
		t.Errorf("error should name the missing variable, got: %v", err)
	}
}

func TestNewResendMailer_MissingFrom(t *testing.T) {
	_, err := NewResendMailer(Config{Provider: "resend", APIKey: "re_test_key"})
	if err == nil {
		t.Fatal("expected an error when MAILER_FROM is empty, got nil")
	}
}

func TestNew_ResendWithoutAPIKeyFailsInsteadOfFallingBackToNop(t *testing.T) {
	m, err := New(Config{Provider: "resend"})
	if err == nil {
		t.Fatalf("expected New to fail, got mailer %T", m)
	}
	if m != nil {
		t.Errorf("expected a nil mailer alongside the error, got %T", m)
	}
}

func TestResendSend_Success(t *testing.T) {
	var (
		gotAuth        string
		gotContentType string
		gotIdempotency string
		gotPath        string
		gotBody        resendRequest
	)

	m := newTestResendMailer(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		gotIdempotency = r.Header.Get("Idempotency-Key")
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"4ef9a417-02e9-4d39-ad75-9611e0fcc33c"}`))
	})

	id, err := m.Send(context.Background(), testMessage())
	if err != nil {
		t.Fatalf("Send: unexpected error: %v", err)
	}
	if id != "4ef9a417-02e9-4d39-ad75-9611e0fcc33c" {
		t.Errorf("id = %q, want the id from the response body", id)
	}

	if gotPath != "/emails" {
		t.Errorf("path = %q, want /emails", gotPath)
	}
	if gotAuth != "Bearer re_test_key" {
		t.Errorf("Authorization = %q, want bearer token", gotAuth)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}
	if gotIdempotency == "" {
		t.Error("Idempotency-Key header is missing")
	}

	if gotBody.From != "DarkVoid <noreply@darkvoid.test>" {
		t.Errorf("from = %q, want the configured MAILER_FROM", gotBody.From)
	}
	if len(gotBody.To) != 1 || gotBody.To[0] != "user@example.com" {
		t.Errorf("to = %v, want [user@example.com]", gotBody.To)
	}
	if gotBody.Subject != "Verify your email - DarkVoid" {
		t.Errorf("subject = %q", gotBody.Subject)
	}
	if gotBody.HTML != "<p>hello</p>" || gotBody.Text != "hello" {
		t.Errorf("html = %q, text = %q", gotBody.HTML, gotBody.Text)
	}
}

func TestResendSend_AcceptedWithoutIDIsAnError(t *testing.T) {
	m := newTestResendMailer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	})

	if _, err := m.Send(context.Background(), testMessage()); err == nil {
		t.Fatal("expected an error when the response carries no id, got nil")
	}
}

func TestResendSend_ValidationErrorIsNotRetried(t *testing.T) {
	var attempts int
	m := newTestResendMailer(t, func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"statusCode":422,"name":"validation_error","message":"The darkvoid.test domain is not verified"}`))
	})

	_, err := m.Send(context.Background(), testMessage())
	if err == nil {
		t.Fatal("expected an error for a 422 response, got nil")
	}
	if attempts != 1 {
		t.Errorf("attempts = %d, want 1 — a 422 will not become valid on a retry", attempts)
	}
	if !strings.Contains(err.Error(), "domain is not verified") {
		t.Errorf("error should carry the provider's message, got: %v", err)
	}
}

func TestResendSend_ForbiddenIsNotRetried(t *testing.T) {
	var attempts int
	m := newTestResendMailer(t, func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"name":"invalid_api_key","message":"API key is invalid"}`))
	})

	if _, err := m.Send(context.Background(), testMessage()); err == nil {
		t.Fatal("expected an error for a 403 response, got nil")
	}
	if attempts != 1 {
		t.Errorf("attempts = %d, want 1", attempts)
	}
}

func TestResendSend_RateLimitedThenSucceeds(t *testing.T) {
	var (
		mu             sync.Mutex
		attempts       int
		idempotencyKey []string
	)

	m := newTestResendMailer(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		attempts++
		current := attempts
		idempotencyKey = append(idempotencyKey, r.Header.Get("Idempotency-Key"))
		mu.Unlock()

		if current == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"name":"rate_limit_exceeded","message":"Too many requests"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"retried-id"}`))
	})

	id, err := m.Send(context.Background(), testMessage())
	if err != nil {
		t.Fatalf("Send: unexpected error after a retryable 429: %v", err)
	}
	if id != "retried-id" {
		t.Errorf("id = %q, want retried-id", id)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	if idempotencyKey[0] != idempotencyKey[1] {
		t.Errorf("Idempotency-Key changed between attempts (%q then %q) — a delivered-but-timed-out send would go out twice",
			idempotencyKey[0], idempotencyKey[1])
	}
}

func TestResendSend_ServerErrorExhaustsRetries(t *testing.T) {
	var attempts int
	m := newTestResendMailer(t, func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"name":"internal_server_error","message":"boom"}`))
	})

	_, err := m.Send(context.Background(), testMessage())
	if err == nil {
		t.Fatal("expected an error once retries are exhausted, got nil")
	}
	if attempts != resendMaxRetries+1 {
		t.Errorf("attempts = %d, want %d", attempts, resendMaxRetries+1)
	}
}

func TestResendSend_CancelledContextStopsImmediately(t *testing.T) {
	var attempts int
	m := newTestResendMailer(t, func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"never-read"}`))
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := m.Send(ctx, testMessage()); err == nil {
		t.Fatal("expected an error for a cancelled context, got nil")
	}
	if attempts != 0 {
		t.Errorf("attempts = %d, want 0 — a cancelled caller should not reach the provider", attempts)
	}
}

func TestParseRetryAfter(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   time.Duration
	}{
		{"empty", "", 0},
		{"seconds", "2", 2 * time.Second},
		{"padded", " 3 ", 3 * time.Second},
		{"clamped to the ceiling", "600", resendMaxRetryAfter},
		{"zero", "0", 0},
		{"negative", "-5", 0},
		{"http date form is ignored", "Wed, 21 Oct 2026 07:28:00 GMT", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseRetryAfter(tt.header); got != tt.want {
				t.Errorf("parseRetryAfter(%q) = %v, want %v", tt.header, got, tt.want)
			}
		})
	}
}
