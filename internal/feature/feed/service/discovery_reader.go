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

func seenPostSet(seen []feed.SeenPost) map[uuid.UUID]bool {
	set := make(map[uuid.UUID]bool, len(seen))
	for _, post := range seen {
		id, err := uuid.Parse(post.PostID)
		if err != nil {
			continue
		}
		set[id] = true
	}
	return set
}

func discoverHandoff(cursor *feed.FeedCursor) *feed.DiscoverCursor {
	position := cursor.FollowingPosition()
	if position == nil {
		return nil
	}
	return &feed.DiscoverCursor{CreatedAt: position.CreatedAt, PostID: position.PostID}
}

// hasMore reports whether the discover stream still holds a post below cursor.
// It is the only source the mixed path cannot answer for from what it already
// fetched, and it is asked once per scroll — only on a page where every other
// source came up dry — so the extra row costs nothing on the pages that page.
func (r *discoveryReader) hasMore(ctx context.Context, userID uuid.UUID, cursor *feed.DiscoverCursor, seen []feed.SeenPost) bool {
	// The probe has to apply the same filter the page would: asking for one row
	// and finding an already-served post answers "more" for a stream that in fact
	// has nothing left, which puts back the empty trailing page this removes.
	skip := seenPostSet(seen)
	//nolint:gosec // bounded by MaxDiscoverSeen, enforced on decode
	posts, err := r.postReader.GetDiscoverWithCursor(ctx, cursor, int32(1+len(skip)), nil)
	if err != nil {
		// Fail towards paginating: a spurious empty page ends the scroll for the
		// client, where a spurious cursor costs it one more request.
		logger.LogError(ctx, err, "discover exhaustion probe failed, keeping the cursor", "user_id", userID)
		return true
	}
	for _, post := range posts {
		if post != nil && !skip[post.ID] {
			return true
		}
	}
	return false
}

func (r *discoveryReader) fallback(ctx context.Context, userID uuid.UUID, cursor *feed.DiscoverCursor, seen []feed.SeenPost) ([]*feedentity.FeedItem, *feed.FeedCursor, error) {
	skip := seenPostSet(seen)
	// Over-fetch by the number of rows the seen filter may remove, so a page
	// thinned by already-served posts still fills.
	//nolint:gosec // bounded by pageSize plus MaxDiscoverSeen, enforced on decode
	limit := int32(pageSize + 1 + len(skip))
	posts, err := r.postReader.GetDiscoverWithCursor(ctx, cursor, limit, nil)
	if err != nil {
		logger.LogError(ctx, err, "failed to get discover fallback", "user_id", userID)
		return nil, nil, errors.NewInternalError(err)
	}

	// A seen post drops out of the page, and out of the carried list once the
	// cursor has actually passed it. Those are not the same moment: the fetch
	// reaches past the end of the page, and an id found among the rows the page
	// truncates away is still ahead of the cursor this page will emit. Pruning on
	// sight would drop it here and serve it on the next page.
	passedAfter := make(map[uuid.UUID]int, len(skip))
	kept := make([]*feedentity.Post, 0, len(posts))
	for _, post := range posts {
		if post == nil {
			continue
		}
		if skip[post.ID] {
			passedAfter[post.ID] = len(kept)
			continue
		}
		kept = append(kept, post)
	}
	posts = kept

	hasMore := len(posts) > pageSize
	if hasMore {
		posts = posts[:pageSize]
	}

	remaining := make([]string, 0, len(seen))
	for _, post := range seen {
		id, err := uuid.Parse(post.PostID)
		if err != nil {
			continue
		}
		// Fewer kept rows ahead of it than the page serves means the cursor, which
		// anchors on the last row served, has moved past it.
		if served, found := passedAfter[id]; found && served < len(posts) {
			continue
		}
		remaining = append(remaining, post.Encode())
	}
	if len(remaining) == 0 {
		remaining = nil
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
			DiscoverSeen:      remaining,
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
