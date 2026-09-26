// Package dto defines request and response payloads for user and auth operations.
//
// `binding:"required,min=1"` only marks a field required, and non-empty, in the
// generated OpenAPI schema; nothing binds on it. The service layer's validation
// enforces it, and service.TestRequestSchema_RequiredMatchesServiceValidation
// fails when the two disagree for any request it lists — add new request types
// there.
package dto
