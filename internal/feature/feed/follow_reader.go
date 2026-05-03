package feed

import (
	"context"

	"github.com/google/uuid"
)

// FollowReader resolves the list of user IDs that a given user follows.
// Implemented at the app layer to avoid cross-context imports.
type FollowReader interface {
	GetFollowingIDs(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error)
}

// FollowerReader resolves the list of users who follow a target user.
type FollowerReader interface {
	GetFollowerIDs(ctx context.Context, targetID uuid.UUID) ([]uuid.UUID, error)
}

// FollowGraphReader combines following and follower lookups.
type FollowGraphReader interface {
	FollowReader
	FollowerReader
}
