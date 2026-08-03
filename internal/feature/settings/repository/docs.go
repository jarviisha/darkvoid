// Package repository is the only place that knows the settings schema.
//
// It translates between the sqlc row types and the entity types, so the service
// above it works in time.Duration and *uuid.UUID rather than in seconds and
// pgtype values, and never sees a column width. The narrowing conversions on the
// write path are safe because the service validates the update first; the column
// CHECKs are the backstop if that ever stops being true.
package repository
