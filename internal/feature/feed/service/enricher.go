package service

import (
	"context"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/feature/feed"
	feedentity "github.com/jarviisha/darkvoid/internal/feature/feed/entity"
	"github.com/jarviisha/darkvoid/pkg/logger"
)

// feedEnricher owns viewer-specific response flags. Enrichment is best-effort:
// source membership and ordering are preserved when a lookup fails.
type feedEnricher struct {
	likeReader feed.LikeReader
	following  *followingResolver
}

func (e *feedEnricher) liked(ctx context.Context, userID uuid.UUID, items []*feedentity.FeedItem) {
	if len(items) == 0 {
		return
	}
	postIDs := make([]uuid.UUID, len(items))
	for i, item := range items {
		postIDs[i] = item.Post.ID
	}
	likedIDs, err := e.likeReader.GetLikedPostIDs(ctx, userID, postIDs)
	if err != nil {
		logger.LogError(ctx, err, "failed to enrich is_liked for feed", "user_id", userID)
		return
	}
	likedSet := make(map[uuid.UUID]bool, len(likedIDs))
	for _, id := range likedIDs {
		likedSet[id] = true
	}
	for _, item := range items {
		item.Post.IsLiked = likedSet[item.Post.ID]
	}
}

func enrichFollowing(items []*feedentity.FeedItem, followingSet map[uuid.UUID]bool) {
	for _, item := range items {
		item.Post.IsFollowingAuthor = followingSet[item.Post.AuthorID]
	}
}

func (e *feedEnricher) followingItems(ctx context.Context, userID uuid.UUID, items []*feedentity.FeedItem) {
	if len(items) == 0 {
		return
	}
	ids, err := e.following.get(ctx, userID)
	if err != nil {
		logger.LogError(ctx, err, "failed to resolve following IDs for is_following_author", "user_id", userID)
		return
	}
	followingSet := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		followingSet[id] = true
	}
	enrichFollowing(items, followingSet)
}

func (e *feedEnricher) followingPosts(ctx context.Context, userID uuid.UUID, posts []*feedentity.Post) {
	if len(posts) == 0 {
		return
	}
	ids, err := e.following.get(ctx, userID)
	if err != nil {
		logger.LogError(ctx, err, "failed to resolve following IDs for is_following_author", "user_id", userID)
		return
	}
	followingSet := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		followingSet[id] = true
	}
	for _, post := range posts {
		post.IsFollowingAuthor = followingSet[post.AuthorID]
	}
}
