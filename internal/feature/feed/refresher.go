package feed

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// TimelineRefresher warms or rebuilds a user's prepared feed timeline.
type TimelineRefresher interface {
	RefreshTimeline(ctx context.Context, userID uuid.UUID) error
}

// PreparedTimelineRefresher refreshes a prepared timeline from current follows
// and recent posts, materializing rank scores so the read path never ranks.
// This is the background re-rank write path: it overwrites fan-out write-time
// scores via SetPostsBatch.
type PreparedTimelineRefresher struct {
	postReader   PostReader
	followReader FollowReader
	timeline     TimelineStore
	ranker       Ranker
	maxItems     int
}

func NewPreparedTimelineRefresher(postReader PostReader, followReader FollowReader, timeline TimelineStore, ranker Ranker, maxItems int) *PreparedTimelineRefresher {
	if maxItems <= 0 {
		maxItems = 1000
	}
	return &PreparedTimelineRefresher{
		postReader:   postReader,
		followReader: followReader,
		timeline:     timeline,
		ranker:       ranker,
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

	followingSet := make(map[string]bool, len(authorIDs))
	for _, id := range authorIDs {
		followingSet[id.String()] = true
	}
	scores := map[string]float64{}
	if r.ranker != nil {
		scores, err = r.ranker.RankPosts(ctx, posts, followingSet, time.Now().UTC())
		if err != nil {
			return err
		}
	}

	entries := make([]TimelineEntry, 0, len(posts))
	for _, p := range posts {
		entries = append(entries, TimelineEntry{
			PostID: p.ID,
			Score:  PackTimelineScore(scores[p.ID.String()], p.CreatedAt),
		})
	}
	return r.timeline.SetPostsBatch(ctx, userID, entries)
}
