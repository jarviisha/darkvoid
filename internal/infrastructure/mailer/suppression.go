package mailer

import (
	"context"
	"errors"

	"github.com/jarviisha/darkvoid/pkg/logger"
)

// ErrSuppressed reports that every recipient of a message is on the suppression
// list, so nothing was sent.
var ErrSuppressed = errors.New("mailer: all recipients are suppressed")

// SuppressionChecker reports whether an address must not be mailed.
// Implemented outside this package — the mailer knows nothing about the database.
type SuppressionChecker interface {
	IsSuppressed(ctx context.Context, email string) (bool, error)
}

// SuppressionGate wraps a Mailer and drops recipients that have bounced or
// complained.
//
// A decorator rather than a check at each call site: there are three account-mail
// flows today and the fourth would be the one that forgets. Wrapping the Mailer
// means every present and future send is filtered by construction.
type SuppressionGate struct {
	inner Mailer
	// checker is injected after construction — the mailer is built during
	// infrastructure setup, before the user context that owns the suppression
	// table exists. Written once during setup and only read afterwards, the same
	// as the other deferred wiring in internal/app.
	checker SuppressionChecker
}

// NewSuppressionGate wraps inner. Until WithChecker is called the gate passes
// everything through, which is also the steady state for the nop provider.
func NewSuppressionGate(inner Mailer) *SuppressionGate {
	return &SuppressionGate{inner: inner}
}

// WithChecker injects the suppression source. Call during setup, before serving.
func (g *SuppressionGate) WithChecker(checker SuppressionChecker) {
	g.checker = checker
}

// Send drops suppressed recipients and forwards the rest.
//
// A checker error is treated as "not suppressed": the suppression list is a
// reputation guard, and failing closed on a database blip would block password
// resets — a worse outcome than one email to a dead mailbox.
func (g *SuppressionGate) Send(ctx context.Context, msg *Message) (string, error) {
	if g.checker == nil || len(msg.To) == 0 {
		return g.inner.Send(ctx, msg)
	}

	allowed := make([]string, 0, len(msg.To))
	var dropped []string
	for _, recipient := range msg.To {
		suppressed, err := g.checker.IsSuppressed(ctx, recipient)
		if err != nil {
			logger.LogError(ctx, err, "suppression check failed, sending anyway", "recipient", recipient)
			allowed = append(allowed, recipient)
			continue
		}
		if suppressed {
			dropped = append(dropped, recipient)
			continue
		}
		allowed = append(allowed, recipient)
	}

	if len(allowed) == 0 {
		logger.Warn(ctx, "email not sent, all recipients suppressed",
			"recipients", dropped, "subject", msg.Subject)
		return "", ErrSuppressed
	}

	if len(dropped) > 0 {
		logger.Warn(ctx, "dropped suppressed recipients from email",
			"dropped", dropped, "subject", msg.Subject)
		// Copied rather than mutating the caller's message: callers build one
		// Message and are entitled to still recognise it afterwards.
		filtered := *msg
		filtered.To = allowed
		msg = &filtered
	}

	return g.inner.Send(ctx, msg)
}
