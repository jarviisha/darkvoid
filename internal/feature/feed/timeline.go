package feed

import (
	"context"
	"math"
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
	// AddPost inserts one entry only if absent (ZADD NX): it never updates the
	// score of an existing member, so a fan-out event that lands after a
	// background re-rank cannot clobber the refreshed score.
	AddPost(ctx context.Context, userID uuid.UUID, entry TimelineEntry) error
	// SetPostsBatch upserts entries, overwriting scores of existing members.
	// It is the write path for background ranking (refresher / re-rank jobs).
	SetPostsBatch(ctx context.Context, userID uuid.UUID, entries []TimelineEntry) error
	ReadPage(ctx context.Context, userID uuid.UUID, after *TimelinePosition, limit int) (*TimelinePage, error)
	Trim(ctx context.Context, userID uuid.UUID) error
	RemovePostBestEffort(ctx context.Context, userID uuid.UUID, postID uuid.UUID) error
}

// Packed timeline score layout: rank bucket (0.001 resolution) in bits 32–51,
// createdAt seconds since timelineEpoch in bits 0–31. The maximum packed value
// (~4.5e15) stays below 2^53, so Redis float64 ZSET scores hold it exactly.
const (
	timelineEpoch     int64 = 1577836800 // 2020-01-01T00:00:00Z; 32 bits of seconds last until ~2156
	timelineRankMax   int64 = 1<<20 - 1
	timelineRankScale       = 1000
)

// PackTimelineScore encodes a rank score and creation time into one sortable
// int64. Rank orders first (0.001 resolution); createdAt breaks ties inside a
// rank bucket so equal-rank posts (e.g. fresh fan-out writes) stay
// newest-first. Out-of-range inputs are clamped, never rejected.
func PackTimelineScore(rankScore float64, createdAt time.Time) int64 {
	bucket := int64(math.Round(rankScore * timelineRankScale))
	if bucket < 0 {
		bucket = 0
	}
	if bucket > timelineRankMax {
		bucket = timelineRankMax
	}
	ts := createdAt.UTC().Unix() - timelineEpoch
	if ts < 0 {
		ts = 0
	}
	if ts > math.MaxUint32 {
		ts = math.MaxUint32
	}
	return bucket<<32 | ts
}

// UnpackTimelineRank returns the rank-score component of a packed timeline
// score, for display and observability only. The createdAt component is lossy
// by design and must not be reconstructed.
func UnpackTimelineRank(score int64) float64 {
	if score < 0 {
		return 0
	}
	return float64(score>>32) / timelineRankScale
}

// TimelineScoreFromTime converts a post creation time to a Redis-safe
// microsecond score. Legacy timestamp-only scoring, superseded by
// PackTimelineScore; kept only while writer call sites migrate and deleted at
// the end of the materialized-ranking change.
func TimelineScoreFromTime(t time.Time) int64 {
	return t.UTC().UnixMicro()
}
