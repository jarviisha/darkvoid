package feed

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// TimelineEntry is one prepared primary feed item in a user's timeline.
type TimelineEntry struct {
	PostID uuid.UUID
	Score  int64
}

// TimelinePosition identifies a continuation point in a prepared timeline.
type TimelinePosition struct {
	Score  int64
	PostID string
}

// TimelinePage is one page of prepared timeline entries plus the last position read.
type TimelinePage struct {
	Entries []TimelineEntry
	Last    *TimelinePosition
}

// TimelineStore stores and reads prepared per-user feed timelines.
type TimelineStore interface {
	AddPost(ctx context.Context, userID uuid.UUID, entry TimelineEntry) error
	AddPostsBatch(ctx context.Context, userID uuid.UUID, entries []TimelineEntry) error
	ReadPage(ctx context.Context, userID uuid.UUID, after *TimelinePosition, limit int) (*TimelinePage, error)
	Trim(ctx context.Context, userID uuid.UUID) error
	RemovePostBestEffort(ctx context.Context, userID uuid.UUID, postID uuid.UUID) error
}

// TimelineScoreFromTime converts a post creation time to a Redis-safe microsecond score.
func TimelineScoreFromTime(t time.Time) int64 {
	return t.UTC().UnixMicro()
}
