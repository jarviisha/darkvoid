package service

import (
	"context"
	"math"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/feature/feed"
	feedcache "github.com/jarviisha/darkvoid/internal/feature/feed/cache"
	feedentity "github.com/jarviisha/darkvoid/internal/feature/feed/entity"
	"github.com/jarviisha/darkvoid/pkg/logger"
	"golang.org/x/sync/singleflight"
)

// trendingSource owns cache lookup, provider fallback, hydration, and
// single-flight rebuilding for the global trending stream.
type trendingSource struct {
	postReader feed.PostReader
	cache      feedcache.FeedCache
	fetcher    feed.TrendingFetcher
	flight     singleflight.Group
}

func (s *trendingSource) get(ctx context.Context) ([]*feedentity.Post, error) {
	if cached, err := s.cache.GetTrending(ctx); err != nil {
		logger.LogError(ctx, err, "trending cache read error, falling through to source")
	} else if cached != nil {
		return cached, nil
	}

	posts, err, _ := s.flight.Do("trending", func() (any, error) {
		rebuildCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), sharedRebuildTimeout)
		defer cancel()
		return s.rebuild(rebuildCtx)
	})
	if err != nil {
		return nil, err
	}
	shared, _ := posts.([]*feedentity.Post)
	return copyPosts(shared), nil
}

func (s *trendingSource) rebuild(ctx context.Context) ([]*feedentity.Post, error) {
	var posts []*feedentity.Post
	if s.fetcher != nil {
		page, err := s.fetcher.GetTrending(ctx, trendingFetchLimit, 0)
		if err != nil {
			logger.LogError(ctx, err, "codohue trending fetch failed, falling back to local DB trending")
		} else if page != nil && len(page.Items) > 0 {
			postIDs := make([]uuid.UUID, 0, len(page.Items))
			for _, item := range page.Items {
				postID, parseErr := uuid.Parse(item.ObjectID)
				if parseErr != nil {
					continue
				}
				postIDs = append(postIDs, postID)
			}
			if len(postIDs) > 0 {
				fetched, fetchErr := s.postReader.GetPostsByIDs(ctx, postIDs)
				if fetchErr != nil {
					logger.LogError(ctx, fetchErr, "failed to resolve codohue trending posts, falling back to local DB")
				} else {
					posts = filterPublicPosts(fetched)
				}
			}
		}
	}

	if len(posts) == 0 {
		var err error
		posts, err = s.postReader.GetTrendingPosts(ctx, trendingFetchLimit)
		if err != nil {
			return nil, err
		}
	}
	if err := s.cache.SetTrending(ctx, posts); err != nil {
		logger.LogError(ctx, err, "trending cache write error")
	}
	return posts, nil
}

// copyPosts isolates viewer-specific scalar enrichment between callers that
// shared a single-flight result.
func copyPosts(shared []*feedentity.Post) []*feedentity.Post {
	out := make([]*feedentity.Post, len(shared))
	for i, post := range shared {
		if post == nil {
			continue
		}
		cloned := *post
		out[i] = &cloned
	}
	return out
}

func filterPublicPosts(posts []*feedentity.Post) []*feedentity.Post {
	filtered := posts[:0]
	for _, post := range posts {
		if post.Visibility == "public" {
			filtered = append(filtered, post)
		}
	}
	return filtered
}

func applyTrendingCursor(posts []*feedentity.Post, cursor *feed.TrendPosition, limit int) []*feedentity.Post {
	filtered := make([]*feedentity.Post, 0, len(posts))
	for _, post := range posts {
		if post == nil || (cursor != nil && !isAfterTrendCursor(post, cursor)) {
			continue
		}
		filtered = append(filtered, post)
	}
	if len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered
}

func isAfterTrendCursor(post *feedentity.Post, cursor *feed.TrendPosition) bool {
	score := trendScoreFromPost(post)
	return score < cursor.Score || (score == cursor.Score && post.ID.String() < cursor.PostID)
}

func trendScoreFromPost(post *feedentity.Post) float64 {
	if post == nil {
		return math.MaxFloat64
	}
	return float64(post.LikeCount)
}
