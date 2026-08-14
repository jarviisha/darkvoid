package service

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/feature/feed"
	feedentity "github.com/jarviisha/darkvoid/internal/feature/feed/entity"
	"github.com/jarviisha/darkvoid/pkg/errors"
	"github.com/jarviisha/darkvoid/pkg/logger"
)

// discoveryReader owns the public discovery stream and the fallback handoff
// used after mixed sources are exhausted.
type discoveryReader struct {
	postReader feed.PostReader
	ranker     feed.Ranker
	enricher   *feedEnricher
}

func discoverHandoff(cursor *feed.FeedCursor) *feed.DiscoverCursor {
	position := cursor.FollowingPosition()
	if position == nil {
		return nil
	}
	return &feed.DiscoverCursor{CreatedAt: position.CreatedAt, PostID: position.PostID}
}

func (r *discoveryReader) fallback(ctx context.Context, userID uuid.UUID, cursor *feed.DiscoverCursor) ([]*feedentity.FeedItem, *feed.FeedCursor, error) {
	posts, err := r.postReader.GetDiscoverWithCursor(ctx, cursor, pageSize+1, nil)
	if err != nil {
		logger.LogError(ctx, err, "failed to get discover fallback", "user_id", userID)
		return nil, nil, errors.NewInternalError(err)
	}
	hasMore := len(posts) > pageSize
	if hasMore {
		posts = posts[:pageSize]
	}

	scores, rankErr := r.ranker.RankPosts(ctx, posts, map[string]bool{}, time.Now().UTC())
	if rankErr != nil {
		logger.LogError(ctx, rankErr, "ranker failed in discover fallback", "user_id", userID)
		scores = make(map[string]float64)
	}
	items := make([]*feedentity.FeedItem, 0, len(posts))
	for _, post := range posts {
		items = append(items, &feedentity.FeedItem{
			Post:   post,
			Score:  scores[post.ID.String()],
			Source: feedentity.SourceDiscover,
		})
	}
	r.enricher.liked(ctx, userID, items)
	r.enricher.followingItems(ctx, userID, items)

	var next *feed.FeedCursor
	if hasMore && len(posts) > 0 {
		last := posts[len(posts)-1]
		timestamp := last.CreatedAt.UnixNano()
		next = &feed.FeedCursor{
			TimelineUser:      userID.String(),
			DiscoverCreatedAt: &timestamp,
			DiscoverPostID:    last.ID.String(),
		}
	}
	return items, next, nil
}

func (r *discoveryReader) get(ctx context.Context, viewerID *uuid.UUID, cursor *feed.DiscoverCursor, limit int32) ([]*feedentity.Post, *feed.DiscoverCursor, error) {
	const defaultLimit = 20
	if limit <= 0 {
		limit = defaultLimit
	}
	posts, err := r.postReader.GetDiscoverWithCursor(ctx, cursor, limit+1, viewerID)
	if err != nil {
		logger.LogError(ctx, err, "failed to get discover feed")
		return nil, nil, err
	}

	var next *feed.DiscoverCursor
	if len(posts) > int(limit) {
		last := posts[limit-1]
		next = &feed.DiscoverCursor{CreatedAt: last.CreatedAt, PostID: last.ID.String()}
		posts = posts[:limit]
	}
	if viewerID != nil {
		r.enricher.followingPosts(ctx, *viewerID, posts)
	}
	return posts, next, nil
}
