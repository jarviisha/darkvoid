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
package mailer
