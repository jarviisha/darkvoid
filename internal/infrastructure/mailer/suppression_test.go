package mailer

import (
	"context"
	"errors"
	"testing"
)

// recordingMailer is the inner Mailer a gate forwards to.
type recordingMailer struct {
	sends [][]string
	err   error
}

func (m *recordingMailer) Send(_ context.Context, msg *Message) (string, error) {
	m.sends = append(m.sends, append([]string(nil), msg.To...))
	return "inner-id", m.err
}

// checkerFunc adapts a function to SuppressionChecker.
type checkerFunc func(ctx context.Context, email string) (bool, error)

func (f checkerFunc) IsSuppressed(ctx context.Context, email string) (bool, error) {
	return f(ctx, email)
}

func suppressList(addresses ...string) checkerFunc {
	blocked := make(map[string]bool, len(addresses))
	for _, a := range addresses {
		blocked[a] = true
	}
	return func(_ context.Context, email string) (bool, error) {
		return blocked[email], nil
	}
}

func TestSuppressionGate_PassesThroughWithoutAChecker(t *testing.T) {
	inner := &recordingMailer{}
	gate := NewSuppressionGate(inner)

	id, err := gate.Send(context.Background(), &Message{To: []string{"user@example.com"}})
	if err != nil {
		t.Fatalf("Send: unexpected error: %v", err)
	}
	if id != "inner-id" {
		t.Errorf("id = %q, want the inner mailer's id", id)
	}
	if len(inner.sends) != 1 {
		t.Fatalf("inner send count = %d, want 1", len(inner.sends))
	}
}

func TestSuppressionGate_ForwardsAllowedRecipients(t *testing.T) {
	inner := &recordingMailer{}
	gate := NewSuppressionGate(inner)
	gate.WithChecker(suppressList("blocked@example.com"))

	if _, err := gate.Send(context.Background(), &Message{To: []string{"ok@example.com"}}); err != nil {
		t.Fatalf("Send: unexpected error: %v", err)
	}
	if len(inner.sends) != 1 || inner.sends[0][0] != "ok@example.com" {
		t.Errorf("inner sends = %v, want one send to ok@example.com", inner.sends)
	}
}

func TestSuppressionGate_AllRecipientsSuppressed(t *testing.T) {
	inner := &recordingMailer{}
	gate := NewSuppressionGate(inner)
	gate.WithChecker(suppressList("blocked@example.com"))

	id, err := gate.Send(context.Background(), &Message{To: []string{"blocked@example.com"}})
	if !errors.Is(err, ErrSuppressed) {
		t.Fatalf("err = %v, want ErrSuppressed", err)
	}
	if id != "" {
		t.Errorf("id = %q, want empty — nothing was sent", id)
	}
	if len(inner.sends) != 0 {
		t.Errorf("inner was called %d times, want 0", len(inner.sends))
	}
}

func TestSuppressionGate_DropsOnlyTheSuppressedRecipients(t *testing.T) {
	inner := &recordingMailer{}
	gate := NewSuppressionGate(inner)
	gate.WithChecker(suppressList("blocked@example.com"))

	msg := &Message{To: []string{"ok@example.com", "blocked@example.com", "also-ok@example.com"}}
	if _, err := gate.Send(context.Background(), msg); err != nil {
		t.Fatalf("Send: unexpected error: %v", err)
	}

	if len(inner.sends) != 1 {
		t.Fatalf("inner send count = %d, want 1", len(inner.sends))
	}
	got := inner.sends[0]
	if len(got) != 2 || got[0] != "ok@example.com" || got[1] != "also-ok@example.com" {
		t.Errorf("forwarded recipients = %v, want the two allowed addresses", got)
	}

	// The caller's message must come back untouched.
	if len(msg.To) != 3 {
		t.Errorf("caller's msg.To = %v, want the original three recipients", msg.To)
	}
}

func TestSuppressionGate_CheckerErrorFailsOpen(t *testing.T) {
	inner := &recordingMailer{}
	gate := NewSuppressionGate(inner)
	gate.WithChecker(checkerFunc(func(_ context.Context, _ string) (bool, error) {
		return false, errors.New("database is down")
	}))

	if _, err := gate.Send(context.Background(), &Message{To: []string{"user@example.com"}}); err != nil {
		t.Fatalf("a failed suppression check must not block the send, got: %v", err)
	}
	if len(inner.sends) != 1 {
		t.Errorf("inner send count = %d, want 1 — failing closed would block password resets", len(inner.sends))
	}
}

func TestSuppressionGate_PropagatesInnerError(t *testing.T) {
	sendErr := errors.New("provider rejected the message")
	inner := &recordingMailer{err: sendErr}
	gate := NewSuppressionGate(inner)
	gate.WithChecker(suppressList())

	if _, err := gate.Send(context.Background(), &Message{To: []string{"user@example.com"}}); !errors.Is(err, sendErr) {
		t.Fatalf("err = %v, want the inner mailer's error", err)
	}
}
