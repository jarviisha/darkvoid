package service

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/feature/feed"
	feedcache "github.com/jarviisha/darkvoid/internal/feature/feed/cache"
	feedentity "github.com/jarviisha/darkvoid/internal/feature/feed/entity"
	"github.com/jarviisha/darkvoid/pkg/errors"
	"github.com/jarviisha/darkvoid/pkg/logger"
)

const scoreEpsilon = 1e-9

const (
	pageSize           = 20
	fetchMultiplier    = 3
	trendingFetchLimit = 100
	// sharedRebuildTimeout bounds detached single-flight rebuilds so a hung
	// provider cannot pin timeline or trending work open indefinitely.
	sharedRebuildTimeout = 10 * time.Second
)

// FeedService coordinates the timeline, mixed-source, and discovery feed
// components. Source retrieval, blending, enrichment, and cursor mechanics are
// implemented by focused collaborators in this package.
type FeedService struct {
	following *followingResolver
	timeline  *timelineReader
	mixed     *mixedFeedBuilder
	discovery *discoveryReader
	enricher  *feedEnricher
}

// NewFeedService creates a new FeedService.
func NewFeedService(postReader feed.PostReader, followReader feed.FollowReader, likeReader feed.LikeReader, ranker feed.Ranker, cache feedcache.FeedCache) *FeedService {
	following := &followingResolver{reader: followReader, cache: cache}
	enricher := &feedEnricher{likeReader: likeReader, following: following}
	trending := &trendingSource{postReader: postReader, cache: cache}

	return &FeedService{
		following: following,
		timeline: &timelineReader{
			postReader: postReader,
			following:  following,
			enricher:   enricher,
		},
		mixed: &mixedFeedBuilder{
			postReader: postReader,
			ranker:     ranker,
			trending:   trending,
		},
		discovery: &discoveryReader{
			postReader: postReader,
			ranker:     ranker,
			enricher:   enricher,
		},
		enricher: enricher,
	}
}

// WithRecommender attaches a Codohue recommender for mixed-feed augmentation.
func (s *FeedService) WithRecommender(recommender feed.Recommender) {
	s.mixed.recommender = recommender
}

// WithTrendingFetcher attaches a Codohue trending source.
func (s *FeedService) WithTrendingFetcher(fetcher feed.TrendingFetcher) {
	s.mixed.trending.fetcher = fetcher
}

// WithTimelineStore attaches a prepared timeline store for timeline-first reads.
func (s *FeedService) WithTimelineStore(store feed.TimelineStore) {
	s.timeline.store = store
}

// WithTimelineRefresher attaches a lazy refresher for missing prepared timelines.
func (s *FeedService) WithTimelineRefresher(refresher feed.TimelineRefresher) {
	s.timeline.refresher = refresher
}

// WithSettings attaches live rollout settings consulted on every timeline read.
func (s *FeedService) WithSettings(settings *feed.Settings) {
	s.timeline.settings = settings
}

// GetFeed returns the cursor-paginated mixed feed for userID.
func (s *FeedService) GetFeed(ctx context.Context, userID uuid.UUID, cursor *feed.FeedCursor) ([]*feedentity.FeedItem, *feed.FeedCursor, error) {
	if cursor != nil {
		if err := cursor.ValidateForUser(userID); err != nil {
			return nil, nil, errors.NewBadRequestError("invalid cursor")
		}
	}

	if position := cursor.DiscoverPosition(); position != nil {
		return s.discovery.fallback(ctx, userID, position)
	}

	if s.timeline.readAllowed(userID) && (cursor == nil || cursor.TimelinePosition() != nil) {
		items, next, err := s.timeline.read(ctx, userID, cursor)
		if err == nil && len(items) > 0 {
			feed.CountTimelineHit()
			logger.Info(ctx, "timeline feed hit", "user_id", userID, "items", len(items))
			return items, next, nil
		}
		if err == nil && cursor != nil && cursor.TimelinePosition() != nil {
			return nil, nil, nil
		}
		if err != nil {
			feed.CountTimelineReadError()
			logger.LogError(ctx, err, "timeline feed read failed, falling back", "user_id", userID)
		} else {
			feed.CountTimelineMiss()
			logger.Info(ctx, "timeline feed miss", "user_id", userID)
		}
	}

	cachedIDs, err := s.following.get(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	authorIDs := make([]uuid.UUID, len(cachedIDs)+1)
	copy(authorIDs, cachedIDs)
	authorIDs[len(cachedIDs)] = userID

	followingSet := make(map[uuid.UUID]bool, len(authorIDs))
	for _, id := range authorIDs {
		followingSet[id] = true
	}

	candidates, recWindow, trendingCollected, followingFetched, err := s.mixed.collect(ctx, userID, authorIDs, cursor)
	if err != nil {
		return nil, nil, err
	}
	candidates = filterEligibleCandidates(userID, followingSet, collapseCandidates(candidates))
	recWindow.validOffsets = recommendationCandidateOffsets(candidates)
	if len(candidates) == 0 && !followingFetched {
		feed.CountFallback()
		logger.Info(ctx, "feed fallback entered", "user_id", userID)
		return s.discovery.fallback(ctx, userID, discoverHandoff(cursor))
	}

	items := s.mixed.rank(ctx, candidates, followingSet, time.Now().UTC())
	s.mixed.sort(items)
	page := items
	if len(page) > pageSize {
		page = page[:pageSize]
	}

	s.enricher.liked(ctx, userID, page)
	enrichFollowing(page, followingSet)
	if len(page) == 0 {
		return nil, nil, nil
	}
	return page, nextMixedCursor(userID, page, cursor, recWindow, trendingCollected), nil
}

// GetDiscover returns the cursor-paginated public discovery feed.
func (s *FeedService) GetDiscover(ctx context.Context, viewerID *uuid.UUID, cursor *feed.DiscoverCursor, limit int32) ([]*feedentity.Post, *feed.DiscoverCursor, error) {
	return s.discovery.get(ctx, viewerID, cursor, limit)
}
