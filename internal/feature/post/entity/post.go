package entity

import (
	"time"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/author"
)

type Visibility string

const (
	VisibilityPublic    Visibility = "public"
	VisibilityFollowers Visibility = "followers"
	VisibilityPrivate   Visibility = "private"
)

// Author is resolved by the user context's AuthorDirectory.
type Author = author.Author

type Post struct {
	ID         uuid.UUID
	AuthorID   uuid.UUID
	Content    string
	Visibility Visibility
	CreatedAt  time.Time
	UpdatedAt  time.Time
	DeletedAt  *time.Time

	// Denormalized counters (stored in posts table, kept in sync by DB triggers)
	LikeCount    int64
	CommentCount int64

	// Enriched fields (not stored in posts table)
	Media             []*PostMedia
	IsLiked           bool
	IsFollowingAuthor bool
	Author            *Author
	Tags              []string
	Mentions          []*MentionedUser
}

type PostMedia struct {
	ID        uuid.UUID
	PostID    uuid.UUID
	MediaKey  string
	MediaType string
	Position  int32
	CreatedAt time.Time
}

// MentionedUser holds minimal info for a user mentioned in a post.
type MentionedUser struct {
	ID          uuid.UUID
	Username    string
	DisplayName string
}
