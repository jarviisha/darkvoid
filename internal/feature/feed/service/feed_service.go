package service

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/feature/feed"
	feedcache "github.com/jarviisha/darkvoid/internal/feature/feed/cache"
	feedentity "github.com/jarviisha/darkvoid/internal/feature/feed/entity"
	"github.com/jarviisha/darkvoid/pkg/deps"
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

// FeedDeps carries the dependencies FeedService cannot work without.
//
// The timeline store, its refresher and the settings are required even while the
// timeline read is switched off: whether it is on is a runtime setting, so the
// service has to be able to serve it the moment an operator flips it.
type FeedDeps struct {
	Posts     feed.PostReader
	Follows   feed.FollowReader
	Likes     feed.LikeReader
	Ranker    feed.Ranker
	Cache     feedcache.FeedCache
	Timeline  feed.TimelineStore
	Refresher feed.TimelineRefresher
	Settings  *feed.Settings
}

func (d FeedDeps) validate() error {
	return deps.Missing(map[string]any{
		"Posts":     d.Posts,
		"Follows":   d.Follows,
		"Likes":     d.Likes,
		"Ranker":    d.Ranker,
		"Cache":     d.Cache,
		"Timeline":  d.Timeline,
		"Refresher": d.Refresher,
		"Settings":  d.Settings,
	})
}

// FeedServiceOption configures the dependencies FeedService can run without.
type FeedServiceOption func(*FeedService)

// WithRecommender attaches a Codohue recommender for mixed-feed augmentation.
// Absent whenever CODOHUE_ENABLED is unset, which is why it is an option rather
// than a FeedDeps field.
func WithRecommender(recommender feed.Recommender) FeedServiceOption {
	return func(s *FeedService) { s.mixed.recommender = recommender }
}

// WithTrendingFetcher attaches a Codohue trending source. Optional for the same
// reason as WithRecommender.
func WithTrendingFetcher(fetcher feed.TrendingFetcher) FeedServiceOption {
	return func(s *FeedService) { s.mixed.trending.fetcher = fetcher }
}

// NewFeedService creates a new FeedService.
func NewFeedService(d FeedDeps, opts ...FeedServiceOption) (*FeedService, error) {
	if err := d.validate(); err != nil {
		return nil, err
	}
	following := &followingResolver{reader: d.Follows, cache: d.Cache}
	enricher := &feedEnricher{likeReader: d.Likes, following: following}
	trending := &trendingSource{postReader: d.Posts, cache: d.Cache}

	s := &FeedService{
		following: following,
		timeline: &timelineReader{
			postReader: d.Posts,
			following:  following,
			enricher:   enricher,
			store:      d.Timeline,
			refresher:  d.Refresher,
			settings:   d.Settings,
		},
		mixed: &mixedFeedBuilder{
			postReader: d.Posts,
			ranker:     d.Ranker,
			trending:   trending,
			settings:   d.Settings,
		},
		discovery: &discoveryReader{
			postReader: d.Posts,
			ranker:     d.Ranker,
			enricher:   enricher,
		},
		enricher: enricher,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s, nil
}

// GetFeed returns the cursor-paginated mixed feed for userID.
func (s *FeedService) GetFeed(ctx context.Context, userID uuid.UUID, cursor *feed.FeedCursor) ([]*feedentity.FeedItem, *feed.FeedCursor, error) {
	if cursor != nil {
		if err := cursor.ValidateForUser(userID); err != nil {
			return nil, nil, errors.NewBadRequestError("invalid cursor")
		}
	}

	if position := cursor.DiscoverPosition(); position != nil {
		return s.discovery.fallback(ctx, userID, position, cursor.DiscoverSeenPosts())
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

	sources, err := s.mixed.collect(ctx, userID, authorIDs, cursor)
	if err != nil {
		return nil, nil, err
	}
	candidates := filterEligibleCandidates(userID, followingSet, collapseCandidates(sources.candidates))
	sources.recWindow.validOffsets = recommendationCandidateOffsets(candidates)
	if len(candidates) == 0 && sources.followingCount == 0 {
		feed.CountFallback()
		logger.Info(ctx, "feed fallback entered", "user_id", userID)
		return s.discovery.fallback(ctx, userID, discoverHandoff(cursor), cursor.DiscoverSeenPosts())
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
	transition := nextMixedCursor(userID, page, cursor, sources)
	if transition.cursor != nil && transition.sourcesDry &&
		!s.discovery.hasMore(ctx, userID, discoverHandoff(transition.cursor), transition.cursor.DiscoverSeenPosts()) {
		return page, nil, nil
	}
	return page, transition.cursor, nil
}

// GetDiscover returns the cursor-paginated public discovery feed.
func (s *FeedService) GetDiscover(ctx context.Context, viewerID *uuid.UUID, cursor *feed.DiscoverCursor, limit int32) ([]*feedentity.Post, *feed.DiscoverCursor, error) {
	return s.discovery.get(ctx, viewerID, cursor, limit)
}
