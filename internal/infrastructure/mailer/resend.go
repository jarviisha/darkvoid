package mailer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/pkg/logger"
)

const (
	// resendAPIBaseURL is the production API root. Overridden in tests.
	resendAPIBaseURL = "https://api.resend.com"

	// resendTimeout bounds a single attempt. SendPasswordReset runs inside the
	// request path and propagates its error to the handler, so an unbounded
	// client here would hang a user request on a provider stall.
	resendTimeout = 10 * time.Second

	// resendMaxRetries is how many times a retryable failure is re-attempted.
	// Two keeps the worst case (3 attempts plus backoff) inside the 30s budget
	// the post-register goroutines allow, and inside a request timeout.
	resendMaxRetries = 2

	// resendBaseBackoff is the delay before the first retry; it doubles after.
	resendBaseBackoff = 500 * time.Millisecond

	// resendMaxRetryAfter caps how long a Retry-After header can park us. The
	// header is the provider's, not ours, and honouring an arbitrarily large
	// value would hold a request open past any timeout we set.
	resendMaxRetryAfter = 5 * time.Second
)

// ResendMailer sends email through Resend's HTTP API.
//
// This talks to the REST surface directly rather than through resend-go: the API
// is one POST, and owning the call means the timeout, retry policy and logging
// match the rest of this codebase instead of the SDK's defaults.
//
// Safe for concurrent use.
type ResendMailer struct {
	apiKey string
	from   string
	// baseURL and baseBackoff are set once at construction and only overridden
	// by tests, which is why neither is a config knob.
	baseURL     string
	baseBackoff time.Duration
	http        *http.Client
}

// NewResendMailer creates a Resend mailer from the given config.
// It fails when the API key is missing — see New.
func NewResendMailer(cfg Config) (*ResendMailer, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, errors.New("mailer: RESEND_API_KEY is required when MAILER_PROVIDER=resend")
	}
	if strings.TrimSpace(cfg.From) == "" {
		return nil, errors.New("mailer: MAILER_FROM is required when MAILER_PROVIDER=resend")
	}

	return &ResendMailer{
		apiKey:      cfg.APIKey,
		from:        cfg.From,
		baseURL:     resendAPIBaseURL,
		baseBackoff: resendBaseBackoff,
		http:        &http.Client{Timeout: resendTimeout},
	}, nil
}

// resendRequest is the POST /emails payload.
type resendRequest struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	HTML    string   `json:"html,omitempty"`
	Text    string   `json:"text,omitempty"`
}

// resendResponse is the success body: the id assigned to the queued email.
type resendResponse struct {
	ID string `json:"id"`
}

// resendError is the failure body. Resend also repeats the status in it, which
// we ignore in favour of the real HTTP status.
type resendError struct {
	Name    string `json:"name"`
	Message string `json:"message"`
}

// Send posts the message to Resend and returns the id it assigns.
//
// Retries cover rate limiting (429), server faults (5xx) and transport errors.
// A 4xx other than 429 is permanent — a rejected sender domain or a malformed
// payload does not become valid on the second try — so it returns immediately.
func (m *ResendMailer) Send(ctx context.Context, msg *Message) (string, error) {
	body, err := json.Marshal(resendRequest{
		From:    m.from,
		To:      msg.To,
		Subject: msg.Subject,
		HTML:    msg.HTML,
		Text:    msg.Text,
	})
	if err != nil {
		return "", fmt.Errorf("marshal resend request: %w", err)
	}

	// One key for the whole call, reused by every retry: that is the point of it.
	// A fresh key per attempt would let a retried-but-actually-delivered send go
	// out twice.
	idempotencyKey := uuid.NewString()

	var lastErr error
	for attempt := 0; attempt <= resendMaxRetries; attempt++ {
		if attempt > 0 {
			wait := m.baseBackoff << (attempt - 1)
			if retryAfter := retryAfterFrom(lastErr); retryAfter > 0 {
				wait = retryAfter
			}
			if err := sleepCtx(ctx, wait); err != nil {
				return "", fmt.Errorf("send email to %s: %w", strings.Join(msg.To, ","), err)
			}
		}

		id, err := m.postEmail(ctx, body, idempotencyKey)
		if err == nil {
			logger.Info(ctx, "email sent", "to", msg.To, "subject", msg.Subject, "provider_message_id", id)
			return id, nil
		}

		lastErr = err
		var retryable *retryableError
		if !errors.As(err, &retryable) {
			logger.Error(ctx, "failed to send email", "to", msg.To, "subject", msg.Subject, "error", err)
			return "", fmt.Errorf("send email to %s: %w", strings.Join(msg.To, ","), err)
		}

		// Only when another attempt actually follows — logging "retrying" on the
		// last failure would claim a retry that never happens.
		if attempt < resendMaxRetries {
			logger.Warn(ctx, "retrying email send",
				"to", msg.To, "subject", msg.Subject, "attempt", attempt+1, "error", err)
		}
	}

	logger.Error(ctx, "failed to send email after retries",
		"to", msg.To, "subject", msg.Subject, "attempts", resendMaxRetries+1, "error", lastErr)
	return "", fmt.Errorf("send email to %s: %w", strings.Join(msg.To, ","), lastErr)
}

// postEmail performs a single attempt. Retryable failures are wrapped in
// retryableError; everything else is returned as-is and ends the loop.
func (m *ResendMailer) postEmail(ctx context.Context, body []byte, idempotencyKey string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.baseURL+"/emails", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build resend request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+m.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", idempotencyKey)

	resp, err := m.http.Do(req)
	if err != nil {
		// A cancelled or expired context is the caller giving up, not a fault
		// worth retrying against.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", fmt.Errorf("resend request: %w", ctxErr)
		}
		return "", &retryableError{err: fmt.Errorf("resend request: %w", err)}
	}
	defer func() { _ = resp.Body.Close() }()

	// Bounded read: an error body is small, and a runaway response should not be
	// buffered into memory.
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return "", &retryableError{err: fmt.Errorf("read resend response: %w", err)}
	}

	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
		var parsed resendResponse
		if err := json.Unmarshal(respBody, &parsed); err != nil {
			return "", fmt.Errorf("decode resend response: %w", err)
		}
		if parsed.ID == "" {
			return "", errors.New("resend accepted the email but returned no id")
		}
		return parsed.ID, nil
	}

	apiErr := fmt.Errorf("resend api: %s", describeResendError(resp.StatusCode, respBody))

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError {
		return "", &retryableError{err: apiErr, retryAfter: parseRetryAfter(resp.Header.Get("Retry-After"))}
	}
	return "", apiErr
}

// describeResendError renders a status and body into one log-friendly string,
// preferring Resend's own error name and message when the body parses.
func describeResendError(status int, body []byte) string {
	var parsed resendError
	if err := json.Unmarshal(body, &parsed); err == nil && parsed.Message != "" {
		if parsed.Name != "" {
			return fmt.Sprintf("status %d: %s: %s", status, parsed.Name, parsed.Message)
		}
		return fmt.Sprintf("status %d: %s", status, parsed.Message)
	}
	return fmt.Sprintf("status %d: %s", status, strings.TrimSpace(string(body)))
}

// retryableError marks a failure worth another attempt, optionally carrying the
// provider's own requested delay.
type retryableError struct {
	err        error
	retryAfter time.Duration
}

func (e *retryableError) Error() string { return e.err.Error() }
func (e *retryableError) Unwrap() error { return e.err }

// retryAfterFrom extracts a Retry-After delay from a previous attempt's error.
func retryAfterFrom(err error) time.Duration {
	var retryable *retryableError
	if errors.As(err, &retryable) {
		return retryable.retryAfter
	}
	return 0
}

// parseRetryAfter reads the delay-seconds form of Retry-After, clamped to
// resendMaxRetryAfter. The HTTP-date form is ignored — Resend sends seconds, and
// a date is not worth a second parse path.
func parseRetryAfter(header string) time.Duration {
	if header == "" {
		return 0
	}
	seconds, err := strconv.Atoi(strings.TrimSpace(header))
	if err != nil || seconds <= 0 {
		return 0
	}
	wait := time.Duration(seconds) * time.Second
	if wait > resendMaxRetryAfter {
		return resendMaxRetryAfter
	}
	return wait
}

// sleepCtx waits for d, or returns early if ctx is done.
func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
