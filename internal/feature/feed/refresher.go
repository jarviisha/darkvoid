package feed

import (
	"context"

	"github.com/google/uuid"
)

// TimelineRefresher warms or rebuilds a user's prepared feed timeline.
type TimelineRefresher interface {
	RefreshTimeline(ctx context.Context, userID uuid.UUID) error
}

// PreparedTimelineRefresher refreshes a prepared timeline from current follows and recent posts.
type PreparedTimelineRefresher struct {
	postReader   PostReader
	followReader FollowReader
	timeline     TimelineStore
	maxItems     int
}

func NewPreparedTimelineRefresher(postReader PostReader, followReader FollowReader, timeline TimelineStore, maxItems int) *PreparedTimelineRefresher {
	if maxItems <= 0 {
		maxItems = 1000
	}
	return &PreparedTimelineRefresher{
		postReader:   postReader,
		followReader: followReader,
		timeline:     timeline,
		maxItems:     maxItems,
	}
}

func (r *PreparedTimelineRefresher) RefreshTimeline(ctx context.Context, userID uuid.UUID) error {
	if r == nil || r.timeline == nil {
		return nil
	}
	return r.refreshOne(ctx, userID)
}

// WarmTimelines refreshes prepared timelines for a bounded list of users.
func (r *PreparedTimelineRefresher) WarmTimelines(ctx context.Context, userIDs []uuid.UUID) error {
	if r == nil || r.timeline == nil {
		return nil
	}
	for _, userID := range userIDs {
		if err := r.refreshOne(ctx, userID); err != nil {
			return err
		}
	}
	return nil
}

func (r *PreparedTimelineRefresher) refreshOne(ctx context.Context, userID uuid.UUID) error {
	authorIDs, err := r.followReader.GetFollowingIDs(ctx, userID)
	if err != nil {
		return err
	}
	authorIDs = append(authorIDs, userID)
	posts, err := r.postReader.GetFollowingPostsWithCursor(ctx, authorIDs, nil, int32(r.maxItems)) //nolint:gosec // maxItems is configuration-validated.
	if err != nil {
		return err
	}
	entries := make([]TimelineEntry, 0, len(posts))
	for _, p := range posts {
		entries = append(entries, TimelineEntry{
			PostID: p.ID,
			Score:  TimelineScoreFromTime(p.CreatedAt),
		})
	}
	return r.timeline.AddPostsBatch(ctx, userID, entries)
}
