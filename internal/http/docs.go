// Package httputil provides shared HTTP context, request decoding, and response
// helpers that validate JSON payloads before committing headers.
//
// Decoding is strict by default: DecodeJSON rejects a field the destination does
// not declare, which turns a misspelled one into a 400 rather than an edit that
// silently does nothing. DecodeJSONLenient drops only that rule, for the handful
// of endpoints whose GET and PUT share a path and whose response is far wider
// than their update.
package httputil
