package service

import (
	"context"
	"hash/crc32"
	"sort"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/feature/feed"
	feedentity "github.com/jarviisha/darkvoid/internal/feature/feed/entity"
	"github.com/jarviisha/darkvoid/pkg/logger"
	"golang.org/x/sync/singleflight"
)

// timelineReader owns rollout gates, lazy refresh, hydration, visibility
// filtering, and continuation for materialized timelines.
type timelineReader struct {
	postReader feed.PostReader
	following  *followingResolver
	enricher   *feedEnricher
	store      feed.TimelineStore
	refresher  feed.TimelineRefresher
	settings   *feed.Settings
	flight     singleflight.Group
}

func (r *timelineReader) readAllowed(userID uuid.UUID) bool {
	if r.store == nil {
		return false
	}
	if r.settings == nil {
		return true
	}
	settings := r.settings.Get()
	return settings.TimelineEnabled && inRollout(userID, settings.TimelineRolloutPercent)
}

func (r *timelineReader) refreshAllowed() bool {
	if r.settings == nil {
		return true
	}
	return r.settings.Get().TimelineRefreshOnMiss
}

func normalizeRolloutPercent(percent int) int {
	switch {
	case percent < 0:
		return 0
	case percent > 100:
		return 100
	default:
		return percent
	}
}

func inRollout(userID uuid.UUID, percent int) bool {
	percent = normalizeRolloutPercent(percent)
	if percent == 0 {
		return false
	}
	if percent == 100 {
		return true
	}
	bucket := int(crc32.ChecksumIEEE([]byte(userID.String())) % 100)
	return bucket < percent
}

func (r *timelineReader) read(ctx context.Context, userID uuid.UUID, cursor *feed.FeedCursor) ([]*feedentity.FeedItem, *feed.FeedCursor, error) {
	var position *feed.TimelinePosition
	if cursor != nil {
		position = cursor.TimelinePosition()
	}
	page, err := r.store.ReadPage(ctx, userID, position, pageSize*fetchMultiplier)
	if err != nil {
		return nil, nil, err
	}
	if position == nil && (page == nil || len(page.Entries) == 0) && r.refresher != nil && r.refreshAllowed() {
		feed.CountLazyRefresh()
		logger.Info(ctx, "timeline refresh on miss started", "user_id", userID)
		if refreshErr := r.refreshShared(ctx, userID); refreshErr != nil {
			logger.LogError(ctx, refreshErr, "timeline refresh on miss failed", "user_id", userID)
		} else {
			logger.Info(ctx, "timeline refresh on miss completed", "user_id", userID)
			page, err = r.store.ReadPage(ctx, userID, position, pageSize*fetchMultiplier)
			if err != nil {
				return nil, nil, err
			}
		}
	}
	if page == nil || len(page.Entries) == 0 {
		return nil, nil, nil
	}

	cachedIDs, err := r.following.get(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	followingSet := make(map[uuid.UUID]bool, len(cachedIDs)+1)
	for _, id := range cachedIDs {
		followingSet[id] = true
	}
	followingSet[userID] = true

	ids := make([]uuid.UUID, 0, len(page.Entries))
	entryByPost := make(map[uuid.UUID]feed.TimelineEntry, len(page.Entries))
	entryOrder := make(map[uuid.UUID]int, len(page.Entries))
	for i, entry := range page.Entries {
		ids = append(ids, entry.PostID)
		entryByPost[entry.PostID] = entry
		entryOrder[entry.PostID] = i
	}
	posts, err := r.postReader.GetPostsByIDs(ctx, ids)
	if err != nil {
		return nil, nil, err
	}

	// Materialized entries are already ranked. Hydration has no ordering
	// guarantee, so restore the stored order instead of ranking again.
	sort.Slice(posts, func(i, j int) bool {
		return entryOrder[posts[i].ID] < entryOrder[posts[j].ID]
	})
	items := make([]*feedentity.FeedItem, 0, pageSize+1)
	for _, post := range posts {
		if !isEligibleTimelinePost(userID, followingSet, post) {
			continue
		}
		items = append(items, &feedentity.FeedItem{
			Post:   post,
			Score:  feed.UnpackTimelineRank(entryByPost[post.ID].Score),
			Source: feedentity.SourceFollowing,
		})
		if len(items) == pageSize+1 {
			break
		}
	}

	considered := len(page.Entries)
	if len(items) == pageSize+1 {
		considered = entryOrder[items[len(items)-1].Post.ID] + 1
	}
	feed.CountStaleFiltered(considered - len(items))
	hasMore := page.HasMore || len(items) > pageSize
	if len(items) > pageSize {
		items = items[:pageSize]
	}
	r.enricher.liked(ctx, userID, items)
	enrichFollowing(items, followingSet)
	if len(items) == 0 {
		return nil, nil, nil
	}

	lastEntry := entryByPost[items[len(items)-1].Post.ID]
	var next *feed.FeedCursor
	if hasMore {
		score := lastEntry.Score
		next = &feed.FeedCursor{
			TimelineScore:        &score,
			TimelinePostID:       lastEntry.PostID.String(),
			TimelineUser:         userID.String(),
			RecommendationOffset: recommendationOffset(cursor),
		}
		if !next.HasContinuation() {
			next = nil
		}
	}
	return items, next, nil
}

func (r *timelineReader) refreshShared(ctx context.Context, userID uuid.UUID) error {
	_, err, _ := r.flight.Do("timeline-refresh:"+userID.String(), func() (any, error) {
		refreshCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), sharedRebuildTimeout)
		defer cancel()
		return nil, r.refresher.RefreshTimeline(refreshCtx, userID)
	})
	return err
}

func isEligibleTimelinePost(userID uuid.UUID, followingSet map[uuid.UUID]bool, post *feedentity.Post) bool {
	if post == nil {
		return false
	}
	if post.AuthorID != userID && !followingSet[post.AuthorID] {
		return false
	}
	switch post.Visibility {
	case "public", "followers":
		return true
	case "private":
		return post.AuthorID == userID
	default:
		return false
	}
}
