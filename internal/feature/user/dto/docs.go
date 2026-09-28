// Package dto defines request and response payloads for user and auth operations.
//
// `binding` tags feed only the generated OpenAPI schema; nothing binds on them,
// and the service layer's validation is what enforces each rule. A field the
// service rejects when empty carries `binding:"required,min=1"`, and
// service.TestRequestSchema_RequiredMatchesServiceValidation fails when the
// tag and the service disagree for any request it lists — add new request
// types there.
//
// Fields that set a password carry the password rule's bounds instead,
// `required,min=8,max=72`, pinned by TestRequestSchema_PasswordBoundsMatchRules.
// The schema counts both in characters while the server's 72 is bytes, so for
// non-Latin text the schema is the looser of the two, never the stricter.
package dto
