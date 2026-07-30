package mailer

import (
	"context"

	"github.com/jarviisha/darkvoid/pkg/logger"
)

// NopMailer is a no-op mailer that logs emails instead of sending them.
// Used in development and testing.
type NopMailer struct{}

// Send logs the email details without actually sending.
// It returns an empty message id — nothing was sent, so there is nothing to
// correlate a delivery report against.
func (m *NopMailer) Send(ctx context.Context, msg *Message) (string, error) {
	logger.Info(ctx, "nop mailer: email not sent (dev mode)",
		"to", msg.To,
		"subject", msg.Subject,
	)
	return "", nil
}
