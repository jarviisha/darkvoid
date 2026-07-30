package mailer

import "context"

// Mailer sends emails. Implementations include Resend, SMTP and a no-op stub
// for testing/development.
//
// Send returns the provider's message id alongside the error. It is the only
// handle a later delivery or bounce report can be matched back to a send, so
// callers log it even though nothing stores it yet. Implementations that have no
// such id return an empty string.
type Mailer interface {
	Send(ctx context.Context, msg *Message) (string, error)
}

// Message represents an email to be sent.
type Message struct {
	To      []string
	Subject string
	HTML    string
	Text    string // plain text fallback
}

// Config holds mailer configuration.
type Config struct {
	// Provider selects the mailer backend: "resend", "smtp" or "nop"
	Provider string

	// SMTP settings
	Host     string
	Port     int
	Username string
	Password string

	// APIKey is the Resend API key. Only read when Provider is "resend".
	APIKey string

	From string

	// BaseURL is the application URL used to build links in emails (e.g. verification links).
	BaseURL string
}

// New creates a Mailer based on the provider specified in cfg.
// Unknown providers fall back to NopMailer.
//
// A provider that is named but unusable — "resend" with no API key — is an
// error, not a fallback: silently degrading to nop would mean verification and
// password-reset mail vanishing into the log with the process reporting a
// healthy start.
func New(cfg Config) (Mailer, error) {
	switch cfg.Provider {
	case "resend":
		// Returned explicitly rather than forwarded: `return NewResendMailer(cfg)`
		// would hand back a non-nil Mailer wrapping a nil *ResendMailer, so a
		// caller checking the interface for nil would see a usable mailer.
		m, err := NewResendMailer(cfg)
		if err != nil {
			return nil, err
		}
		return m, nil
	case "smtp":
		return NewSMTPMailer(cfg), nil
	default:
		return &NopMailer{}, nil
	}
}
