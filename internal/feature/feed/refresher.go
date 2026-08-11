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
//
// The item count is read from settings per refresh, so it agrees with what the
// timeline store trims to. Capturing it here instead would let the two drift
// apart on an operator's edit — the refresher would write more entries than the
// store keeps, or fewer than a page needs, and neither shows up as an error.
type PreparedTimelineRefresher struct {
	postReader   PostReader
	followReader FollowReader
	timeline     TimelineStore
	ranker       Ranker
	settings     *Settings
}

func NewPreparedTimelineRefresher(postReader PostReader, followReader FollowReader, timeline TimelineStore, ranker Ranker, settings *Settings) *PreparedTimelineRefresher {
	return &PreparedTimelineRefresher{
		postReader:   postReader,
		followReader: followReader,
		timeline:     timeline,
		ranker:       ranker,
		settings:     settings,
	}
}

// maxItems is the current per-timeline entry count. A non-positive stored value
// falls back to the default rather than to zero, which would refresh every
// timeline into an empty one and report success.
func (r *PreparedTimelineRefresher) maxItems() int {
	if n := r.settings.Get().TimelineMaxItems; n > 0 {
		return n
	}
	return DefaultRuntimeSettings().TimelineMaxItems
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
	refreshStartedAt := time.Now().UTC()
	authorIDs, err := r.followReader.GetFollowingIDs(ctx, userID)
	if err != nil {
		return err
	}
	authorIDs = append(authorIDs, userID)
	posts, err := r.postReader.GetFollowingPostsWithCursor(ctx, authorIDs, userID, nil, int32(r.maxItems())) //nolint:gosec // bounded to 1..10000 by the settings.feed CHECK and entity.Validate.
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
	return r.timeline.ReplacePosts(ctx, userID, entries, refreshStartedAt)
}
