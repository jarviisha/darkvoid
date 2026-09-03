// Package middleware provides authentication, authorization, trusted-proxy IP
// resolution, rate limiting, and browser security middleware used by the
// application router. Bearer credentials are accepted only from the
// Authorization header. Browser policies distinguish machine API responses,
// Swagger UI pages, and locally served user uploads.
package middleware
