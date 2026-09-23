package httputil

import (
	"context"

	"github.com/google/uuid"
)

// Context keys for storing values in HTTP request context
type contextKey string

// ContextKeyUserID stores the authenticated user ID.
const ContextKeyUserID contextKey = "user_id"

// GetUserID extracts user ID from context
func GetUserID(ctx context.Context) *uuid.UUID {
	if val := ctx.Value(ContextKeyUserID); val != nil {
		if id, ok := val.(uuid.UUID); ok {
			return &id
		}
	}
	return nil
}

// WithUserID adds user ID to context
func WithUserID(ctx context.Context, userID uuid.UUID) context.Context {
	return context.WithValue(ctx, ContextKeyUserID, userID)
}
