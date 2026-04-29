package service

import (
	"context"
	stderrs "errors"
	"math"
	"sort"
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
)

// FeedService handles feed business logic.
type FeedService struct {
	postReader      feed.PostReader
	followReader    feed.FollowReader
	likeReader      feed.LikeReader
	ranker          feed.Ranker
	cache           feedcache.FeedCache
	sessionCache    feedcache.FeedSessionCache
	recommender     feed.Recommender     // optional: nil = no CF augmentation
	trendingFetcher feed.TrendingFetcher // optional: nil = use local DB trending
}

// NewFeedService creates a new FeedService.
func NewFeedService(postReader feed.PostReader, followReader feed.FollowReader, likeReader feed.LikeReader, ranker feed.Ranker, cache feedcache.FeedCache, sessionCache feedcache.FeedSessionCache) *FeedService {
	if sessionCache == nil {
		sessionCache = feedcache.NewNopFeedSessionCache()
	}
	return &FeedService{
		postReader:   postReader,
		followReader: followReader,
		likeReader:   likeReader,
		ranker:       ranker,
		cache:        cache,
		sessionCache: sessionCache,
	}
}

// WithRecommender attaches a Codohue recommender for CF-based feed augmentation. Called at wire-up time.
func (s *FeedService) WithRecommender(r feed.Recommender) {
	s.recommender = r
}

// WithTrendingFetcher attaches a Codohue trending fetcher. When set, page-1 trending is sourced
// from Codohue (GET /v1/trending/{ns}) instead of the local DB. Called at wire-up time.
func (s *FeedService) WithTrendingFetcher(f feed.TrendingFetcher) {
	s.trendingFetcher = f
}

// GetFeed returns the cursor-paginated mixed feed for userID.
func (s *FeedService) GetFeed(ctx context.Context, userID uuid.UUID, cursor *feed.FeedCursor) ([]*feedentity.FeedItem, *feed.FeedCursor, error) {
	now := time.Now().UTC()
	state := feed.NewFeedPageState(now)
	if cursor != nil && cursor.State != nil {
		state = cursor.State
	}
	if state.SessionID != "" {
		cached, cacheErr := s.sessionCache.GetFeedState(ctx, state.SessionID)
		switch {
		case cacheErr == nil && cached != nil:
			cached.SessionID = state.SessionID
			state = cached
		case cacheErr != nil && !stderrs.Is(cacheErr, feedcache.ErrFeedSessionMiss):
			logger.LogError(ctx, cacheErr, "feed session cache read failed", "session_id", state.SessionID)
		}
	}
	if err := state.Validate(now); err != nil {
		return nil, nil, errors.NewBadRequestError("invalid cursor")
	}
	if state.SessionID == "" {
		state.SessionID = uuid.NewString()
	}

	cachedIDs, err := s.getFollowingIDs(ctx, userID)
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

	if state.Mode == feed.PhaseDiscoverFallback {
		return s.discoverFallbackWithState(ctx, userID, state)
	}

	candidates, followingFetched, err := s.collectMixedCandidates(ctx, userID, authorIDs, state)
	if err != nil {
		return nil, nil, err
	}
	if len(candidates) == 0 && !followingFetched {
		state.Mode = feed.PhaseDiscoverFallback
		return s.discoverFallbackWithState(ctx, userID, state)
	}

	items := s.rankCandidates(ctx, candidates, followingSet, now)
	s.sortFeedItems(items)

	page := items
	if len(page) > pageSize {
		page = page[:pageSize]
	}

	returnedIDs := make(map[string]bool, len(page))
	seenIDs := make([]string, 0, len(page))
	for _, item := range page {
		id := item.Post.ID.String()
		returnedIDs[id] = true
		seenIDs = append(seenIDs, id)
	}
	state.AddSeen(seenIDs...)

	pending := make([]feed.FeedCandidate, 0, len(items)-len(page))
	for _, item := range items {
		id := item.Post.ID.String()
		if returnedIDs[id] {
			continue
		}
		pending = append(pending, feedCandidateFromItem(item))
	}
	state.PendingItems = pending
	state.TrimPending()

	s.enrichIsLiked(ctx, userID, page)
	s.enrichIsFollowingAuthor(page, followingSet)

	if len(page) == 0 {
		s.deleteFeedSession(ctx, state)
		return nil, nil, nil
	}
	if len(state.PendingItems) == 0 && !s.mayHaveMoreMixed(state, followingFetched) {
		s.deleteFeedSession(ctx, state)
		return page, nil, nil
	}
	s.storeFeedSession(ctx, state)
	return page, &feed.FeedCursor{State: state}, nil
}

type feedCandidate struct {
	post                *feedentity.Post
	source              feedentity.Source
	sourceRank          int
	providerScore       *float64
	providerRank        *int
	recommendationScore *float64
	recommendationRank  *int
}

func (s *FeedService) collectMixedCandidates(ctx context.Context, userID uuid.UUID, authorIDs []uuid.UUID, state *feed.FeedPageState) ([]feedCandidate, bool, error) {
	seen := state.SeenSet()
	candidates := make([]feedCandidate, 0, len(state.PendingItems)+pageSize*fetchMultiplier)

	if len(state.PendingItems) > 0 {
		pending := state.PendingItems
		state.PendingItems = nil
		loaded, err := s.loadCandidatesByID(ctx, pending)
		if err != nil {
			logger.LogError(ctx, err, "failed to load pending feed candidates", "user_id", userID)
		} else {
			candidates = append(candidates, loaded...)
		}
	}

	followingPosts, err := s.postReader.GetFollowingPostsWithCursor(ctx, authorIDs, state.FollowingCursor, pageSize*fetchMultiplier)
	if err != nil {
		logger.LogError(ctx, err, "failed to get following posts", "user_id", userID)
		return nil, false, errors.NewInternalError(err)
	}
	if len(followingPosts) > 0 {
		last := followingPosts[len(followingPosts)-1]
		state.FollowingCursor = &feed.FollowingCursor{
			Mode:      feed.ModeFollowing,
			CreatedAt: last.CreatedAt,
			PostID:    last.ID.String(),
		}
	}
	for i, p := range followingPosts {
		if seen[p.ID.String()] {
			continue
		}
		candidates = append(candidates, feedCandidate{post: p, source: feedentity.SourceFollowing, sourceRank: i + 1})
	}

	if s.recommender != nil {
		recPage, recErr := s.recommender.GetRecommendations(ctx, userID.String(), pageSize, state.RecommendationOffset)
		if recErr != nil {
			logger.LogError(ctx, recErr, "codohue recommendations failed, skipping", "user_id", userID)
		} else if recPage != nil {
			state.RecommendationOffset = recPage.Offset + len(recPage.Items)
			state.RecommendationTotal = recPage.Total
			recCandidates, loadErr := s.loadRecommendationCandidates(ctx, recPage.Items, seen)
			if loadErr != nil {
				logger.LogError(ctx, loadErr, "failed to load recommendation candidates", "user_id", userID)
			}
			candidates = append(candidates, recCandidates...)
		}
	}

	if state.TrendingOffset == 0 {
		trendingPosts, err := s.getTrending(ctx)
		if err != nil {
			logger.LogError(ctx, err, "failed to get trending posts, skipping", "user_id", userID)
		}
		for i, p := range trendingPosts {
			if seen[p.ID.String()] {
				continue
			}
			candidates = append(candidates, feedCandidate{post: p, source: feedentity.SourceTrending, sourceRank: i + 1})
		}
		state.TrendingOffset = len(trendingPosts)
		state.TrendingTotal = len(trendingPosts)
	}

	return collapseCandidates(candidates), len(followingPosts) > 0, nil
}

func (s *FeedService) loadCandidatesByID(ctx context.Context, pending []feed.FeedCandidate) ([]feedCandidate, error) {
	ids := make([]uuid.UUID, 0, len(pending))
	meta := make(map[uuid.UUID]feed.FeedCandidate, len(pending))
	for _, item := range pending {
		id, err := uuid.Parse(item.PostID)
		if err != nil {
			continue
		}
		ids = append(ids, id)
		meta[id] = item
	}
	posts, err := s.postReader.GetPostsByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	result := make([]feedCandidate, 0, len(posts))
	for _, p := range posts {
		item := meta[p.ID]
		result = append(result, feedCandidate{
			post:                p,
			source:              feedentity.Source(item.Source),
			sourceRank:          item.SourceRank,
			providerScore:       item.ProviderScore,
			providerRank:        item.ProviderRank,
			recommendationScore: item.ProviderScore,
			recommendationRank:  item.ProviderRank,
		})
	}
	return result, nil
}

func (s *FeedService) loadRecommendationCandidates(ctx context.Context, items []feed.RecommendedItem, seen map[string]bool) ([]feedCandidate, error) {
	ids := make([]uuid.UUID, 0, len(items))
	meta := make(map[uuid.UUID]feed.RecommendedItem, len(items))
	for _, item := range items {
		if seen[item.ObjectID] || item.Score < 0 {
			continue
		}
		id, err := uuid.Parse(item.ObjectID)
		if err != nil {
			continue
		}
		ids = append(ids, id)
		meta[id] = item
	}
	posts, err := s.postReader.GetPostsByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	result := make([]feedCandidate, 0, len(posts))
	for _, p := range posts {
		if p.Visibility != "public" {
			continue
		}
		item := meta[p.ID]
		score := item.Score
		rank := item.Rank
		result = append(result, feedCandidate{
			post:                p,
			source:              feedentity.SourceRecommendation,
			sourceRank:          item.Rank,
			providerScore:       &score,
			providerRank:        &rank,
			recommendationScore: &score,
			recommendationRank:  &rank,
		})
	}
	return result, nil
}

func collapseCandidates(candidates []feedCandidate) []feedCandidate {
	byID := make(map[uuid.UUID]feedCandidate, len(candidates))
	for _, c := range candidates {
		if c.post == nil {
			continue
		}
		existing, ok := byID[c.post.ID]
		if !ok || sourcePriority(c.source) > sourcePriority(existing.source) {
			byID[c.post.ID] = c
			continue
		}
		if ok && c.providerScore != nil && existing.providerScore == nil {
			existing.providerScore = c.providerScore
			existing.providerRank = c.providerRank
			existing.recommendationScore = c.recommendationScore
			existing.recommendationRank = c.recommendationRank
			byID[c.post.ID] = existing
		}
	}
	result := make([]feedCandidate, 0, len(byID))
	for _, c := range byID {
		result = append(result, c)
	}
	return result
}

func sourcePriority(source feedentity.Source) int {
	switch source {
	case feedentity.SourceFollowing:
		return 4
	case feedentity.SourceRecommendation:
		return 3
	case feedentity.SourceTrending:
		return 2
	default:
		return 1
	}
}

func (s *FeedService) rankCandidates(ctx context.Context, candidates []feedCandidate, followingSet map[uuid.UUID]bool, now time.Time) []*feedentity.FeedItem {
	posts := make([]*feedentity.Post, len(candidates))
	for i, c := range candidates {
		posts[i] = c.post
	}
	followingStrSet := make(map[string]bool, len(followingSet))
	for id := range followingSet {
		followingStrSet[id.String()] = true
	}
	scores, err := s.ranker.RankPosts(ctx, posts, followingStrSet, now)
	if err != nil {
		logger.LogError(ctx, err, "ranker failed, falling back to chronological order")
		scores = make(map[string]float64)
	}
	items := make([]*feedentity.FeedItem, 0, len(candidates))
	for _, c := range candidates {
		score := scores[c.post.ID.String()]
		if c.providerScore != nil {
			score += (*c.providerScore) * 20
		}
		if c.providerRank != nil && *c.providerRank > 0 {
			score += 5 / float64(*c.providerRank)
		}
		items = append(items, &feedentity.FeedItem{
			Post:                c.post,
			Score:               score,
			Source:              c.source,
			RecommendationScore: c.recommendationScore,
			RecommendationRank:  c.recommendationRank,
		})
	}
	return items
}

func (s *FeedService) sortFeedItems(items []*feedentity.FeedItem) {
	sort.Slice(items, func(i, j int) bool {
		if math.Abs(items[i].Score-items[j].Score) > scoreEpsilon {
			return items[i].Score > items[j].Score
		}
		if !items[i].Post.CreatedAt.Equal(items[j].Post.CreatedAt) {
			return items[i].Post.CreatedAt.After(items[j].Post.CreatedAt)
		}
		return items[i].Post.ID.String() > items[j].Post.ID.String()
	})
}

func feedCandidateFromItem(item *feedentity.FeedItem) feed.FeedCandidate {
	return feed.FeedCandidate{
		PostID:        item.Post.ID.String(),
		Source:        string(item.Source),
		ProviderScore: item.RecommendationScore,
		ProviderRank:  item.RecommendationRank,
		CreatedAt:     item.Post.CreatedAt,
	}
}

func (s *FeedService) mayHaveMoreMixed(state *feed.FeedPageState, followingFetched bool) bool {
	return followingFetched ||
		len(state.PendingItems) > 0 ||
		(state.RecommendationTotal > 0 && state.RecommendationOffset < state.RecommendationTotal)
}

func (s *FeedService) storeFeedSession(ctx context.Context, state *feed.FeedPageState) {
	if state == nil || state.SessionID == "" {
		return
	}
	if err := s.sessionCache.SetFeedState(ctx, state.SessionID, state); err != nil {
		logger.LogError(ctx, err, "feed session cache write failed", "session_id", state.SessionID)
	}
}

func (s *FeedService) deleteFeedSession(ctx context.Context, state *feed.FeedPageState) {
	if state == nil || state.SessionID == "" {
		return
	}
	if err := s.sessionCache.DeleteFeedState(ctx, state.SessionID); err != nil {
		logger.LogError(ctx, err, "feed session cache delete failed", "session_id", state.SessionID)
	}
}

// getFollowingIDs returns following IDs from cache, falling back to followReader on miss.
func (s *FeedService) getFollowingIDs(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error) {
	if ids, err := s.cache.GetFollowingIDs(ctx, userID); err != nil {
		logger.LogError(ctx, err, "following IDs cache read error, falling through to DB", "user_id", userID)
	} else if ids != nil {
		return ids, nil
	}

	ids, err := s.followReader.GetFollowingIDs(ctx, userID)
	if err != nil {
		logger.LogError(ctx, err, "failed to resolve following IDs", "user_id", userID)
		return nil, errors.NewInternalError(err)
	}

	if err := s.cache.SetFollowingIDs(ctx, userID, ids); err != nil {
		logger.LogError(ctx, err, "following IDs cache write error", "user_id", userID)
	}
	return ids, nil
}

func (s *FeedService) discoverFallbackWithState(ctx context.Context, userID uuid.UUID, state *feed.FeedPageState) ([]*feedentity.FeedItem, *feed.FeedCursor, error) {
	fetchLimit := pageSize + 1 + len(state.SeenPostIDs)
	if fetchLimit > 100 {
		fetchLimit = 100
	}
	posts, err := s.postReader.GetDiscoverWithCursor(ctx, state.DiscoveryCursor, int32(fetchLimit), nil) //nolint:gosec // capped to 100 above
	if err != nil {
		logger.LogError(ctx, err, "failed to get discover fallback", "user_id", userID)
		return nil, nil, errors.NewInternalError(err)
	}

	seen := state.SeenSet()
	filtered := make([]*feedentity.Post, 0, pageSize+1)
	for _, p := range posts {
		if seen[p.ID.String()] {
			state.DiscoveryCursor = &feed.DiscoverCursor{CreatedAt: p.CreatedAt, PostID: p.ID.String()}
			continue
		}
		filtered = append(filtered, p)
		state.DiscoveryCursor = &feed.DiscoverCursor{CreatedAt: p.CreatedAt, PostID: p.ID.String()}
		if len(filtered) > pageSize {
			break
		}
	}

	hasMore := len(filtered) > pageSize || len(posts) == fetchLimit
	if len(filtered) > pageSize {
		filtered = filtered[:pageSize]
	}

	now := time.Now()
	scores, rankErr := s.ranker.RankPosts(ctx, filtered, map[string]bool{}, now)
	if rankErr != nil {
		logger.LogError(ctx, rankErr, "ranker failed in discover fallback", "user_id", userID)
		scores = make(map[string]float64)
	}
	items := make([]*feedentity.FeedItem, 0, len(filtered))
	seenIDs := make([]string, 0, len(filtered))
	for _, p := range filtered {
		items = append(items, &feedentity.FeedItem{
			Post:   p,
			Score:  scores[p.ID.String()],
			Source: feedentity.SourceDiscover,
		})
		seenIDs = append(seenIDs, p.ID.String())
	}
	state.AddSeen(seenIDs...)

	s.enrichIsLiked(ctx, userID, items)
	s.enrichIsFollowingAuthorFromDB(ctx, userID, items)
	if len(items) == 0 || !hasMore {
		s.deleteFeedSession(ctx, state)
		return items, nil, nil
	}
	s.storeFeedSession(ctx, state)
	return items, &feed.FeedCursor{State: state}, nil
}

// enrichIsLiked batch-fetches like status for the viewer and sets Post.IsLiked.
// Best-effort: on error, items are returned as-is (is_liked stays false).
func (s *FeedService) enrichIsLiked(ctx context.Context, userID uuid.UUID, items []*feedentity.FeedItem) {
	if len(items) == 0 {
		return
	}
	postIDs := make([]uuid.UUID, len(items))
	for i, item := range items {
		postIDs[i] = item.Post.ID
	}
	likedIDs, err := s.likeReader.GetLikedPostIDs(ctx, userID, postIDs)
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

// getTrending returns trending posts.
// When a TrendingFetcher is configured (Codohue), it fetches trending IDs from Codohue and resolves
// the posts from DB. Falls back to local DB trending on any Codohue error.
// Results are cached in Redis regardless of source.
func (s *FeedService) getTrending(ctx context.Context) ([]*feedentity.Post, error) {
	if cached, err := s.cache.GetTrending(ctx); err != nil {
		logger.LogError(ctx, err, "trending cache read error, falling through to source")
	} else if cached != nil {
		return cached, nil
	}

	var posts []*feedentity.Post

	if s.trendingFetcher != nil {
		page, err := s.trendingFetcher.GetTrending(ctx, trendingFetchLimit, 0)
		if err != nil {
			logger.LogError(ctx, err, "codohue trending fetch failed, falling back to local DB trending")
		} else if page != nil && len(page.Items) > 0 {
			postUUIDs := make([]uuid.UUID, 0, len(page.Items))
			for _, item := range page.Items {
				uid, err := uuid.Parse(item.ObjectID)
				if err != nil {
					continue
				}
				postUUIDs = append(postUUIDs, uid)
			}
			if len(postUUIDs) > 0 {
				fetched, err := s.postReader.GetPostsByIDs(ctx, postUUIDs)
				if err != nil {
					logger.LogError(ctx, err, "failed to resolve codohue trending posts, falling back to local DB")
				} else {
					posts = filterPublicPosts(fetched)
				}
			}
		}
	}

	// Fall back to local DB trending when Codohue is unavailable or returned no results.
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

func filterPublicPosts(posts []*feedentity.Post) []*feedentity.Post {
	filtered := posts[:0]
	for _, p := range posts {
		if p.Visibility == "public" {
			filtered = append(filtered, p)
		}
	}
	return filtered
}

// GetDiscover returns the cursor-paginated public discovery feed.
// viewerID is optional — when provided, is_liked is populated.
func (s *FeedService) GetDiscover(ctx context.Context, viewerID *uuid.UUID, cursor *feed.DiscoverCursor, limit int32) ([]*feedentity.Post, *feed.DiscoverCursor, error) {
	const defaultLimit = 20
	if limit <= 0 {
		limit = defaultLimit
	}
	// Fetch one extra to detect if there's a next page
	posts, err := s.postReader.GetDiscoverWithCursor(ctx, cursor, limit+1, viewerID)
	if err != nil {
		logger.LogError(ctx, err, "failed to get discover feed")
		return nil, nil, err
	}

	var nextCursor *feed.DiscoverCursor
	if len(posts) > int(limit) {
		last := posts[limit-1]
		nextCursor = &feed.DiscoverCursor{
			CreatedAt: last.CreatedAt,
			PostID:    last.ID.String(),
		}
		posts = posts[:limit]
	}

	if viewerID != nil {
		s.enrichPostsIsFollowingAuthor(ctx, *viewerID, posts)
	}
	return posts, nextCursor, nil
}

// enrichIsFollowingAuthor sets IsFollowingAuthor on feed items using a precomputed following set.
func (s *FeedService) enrichIsFollowingAuthor(items []*feedentity.FeedItem, followingSet map[uuid.UUID]bool) {
	for _, item := range items {
		item.Post.IsFollowingAuthor = followingSet[item.Post.AuthorID]
	}
}

// enrichIsFollowingAuthorFromDB resolves following IDs (cached) and sets IsFollowingAuthor on feed items.
// Used in discoverFallback where no precomputed following set is available.
func (s *FeedService) enrichIsFollowingAuthorFromDB(ctx context.Context, userID uuid.UUID, items []*feedentity.FeedItem) {
	if len(items) == 0 {
		return
	}
	ids, err := s.getFollowingIDs(ctx, userID)
	if err != nil {
		logger.LogError(ctx, err, "failed to resolve following IDs for is_following_author", "user_id", userID)
		return
	}
	followingSet := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		followingSet[id] = true
	}
	s.enrichIsFollowingAuthor(items, followingSet)
}

// enrichPostsIsFollowingAuthor resolves following IDs (cached) and sets IsFollowingAuthor on posts.
// Used in GetDiscover where items are []*Post, not []*FeedItem.
func (s *FeedService) enrichPostsIsFollowingAuthor(ctx context.Context, userID uuid.UUID, posts []*feedentity.Post) {
	if len(posts) == 0 {
		return
	}
	ids, err := s.getFollowingIDs(ctx, userID)
	if err != nil {
		logger.LogError(ctx, err, "failed to resolve following IDs for is_following_author", "user_id", userID)
		return
	}
	followingSet := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		followingSet[id] = true
	}
	for _, p := range posts {
		p.IsFollowingAuthor = followingSet[p.AuthorID]
	}
}
