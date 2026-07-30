// Package mailer provides mail delivery abstractions, templates, and implementations.
//
// Mailer is the seam: New selects one of three backends from MAILER_PROVIDER —
// resend (HTTP API), smtp, or nop (logs only, the default). Send returns the
// provider's message id so a delivery or bounce report can be traced back to a
// send; nothing persists it yet, so it currently only reaches the logs.
//
// The Resend implementation calls the REST API directly instead of using
// resend-go — the API is a single POST, and owning the call keeps the timeout,
// retry policy and logging consistent with the rest of the codebase. SMTP has no
// server-assigned id, so it generates its own Message-ID header and returns that.
//
// Two pieces sit around the Mailer rather than inside one:
//
//   - SuppressionGate decorates any Mailer and drops recipients that have bounced
//     or complained. A decorator, not a check per call site, so a flow added later
//     cannot forget it. The suppression source is injected after construction —
//     the mailer is built before the context that owns the table.
//   - ResendWebhookVerifier and ParseResendWebhook handle inbound delivery
//     reports: Svix HMAC verification plus payload normalisation into a
//     WebhookEvent, so consumers never parse Resend's shape themselves.
package mailer
