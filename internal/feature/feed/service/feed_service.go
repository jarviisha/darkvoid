package service

import (
	"context"
	"hash/crc32"
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
	timelineStore   feed.TimelineStore
	refresher       feed.TimelineRefresher
	recommender     feed.Recommender     // optional: nil = no CF augmentation
	trendingFetcher feed.TrendingFetcher // optional: nil = use local DB trending
	timelineOptions timelineOptions
}

type timelineOptions struct {
	configured     bool
	servingEnabled bool
	rolloutPercent int
	refreshOnMiss  bool
}

// NewFeedService creates a new FeedService.
func NewFeedService(postReader feed.PostReader, followReader feed.FollowReader, likeReader feed.LikeReader, ranker feed.Ranker, cache feedcache.FeedCache) *FeedService {
	return &FeedService{
		postReader:   postReader,
		followReader: followReader,
		likeReader:   likeReader,
		ranker:       ranker,
		cache:        cache,
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

// WithTimelineStore attaches a prepared timeline store for timeline-first reads.
func (s *FeedService) WithTimelineStore(store feed.TimelineStore) {
	s.timelineStore = store
}

// WithTimelineRefresher attaches a lazy refresher for missing prepared timelines.
func (s *FeedService) WithTimelineRefresher(refresher feed.TimelineRefresher) {
	s.refresher = refresher
}

// WithTimelineOptions configures rollout gates for prepared timeline reads.
func (s *FeedService) WithTimelineOptions(servingEnabled bool, rolloutPercent int, refreshOnMiss bool) {
	s.timelineOptions = timelineOptions{
		configured:     true,
		servingEnabled: servingEnabled,
		rolloutPercent: normalizeRolloutPercent(rolloutPercent),
		refreshOnMiss:  refreshOnMiss,
	}
}

// GetFeed returns the cursor-paginated mixed feed for userID.
func (s *FeedService) GetFeed(ctx context.Context, userID uuid.UUID, cursor *feed.FeedCursor) ([]*feedentity.FeedItem, *feed.FeedCursor, error) {
	if cursor != nil {
		if err := cursor.Validate(); err != nil {
			return nil, nil, errors.NewBadRequestError("invalid cursor")
		}
	}
	if s.timelineReadAllowed(userID) {
		items, next, err := s.getFeedFromTimeline(ctx, userID, cursor)
		if err == nil && len(items) > 0 {
			feed.CountTimelineHit()
			logger.Info(ctx, "timeline feed hit", "user_id", userID, "items", len(items))
			return items, next, nil
		}
		if err != nil {
			feed.CountTimelineReadError()
			logger.LogError(ctx, err, "timeline feed read failed, falling back", "user_id", userID)
		} else {
			feed.CountTimelineMiss()
			logger.Info(ctx, "timeline feed miss", "user_id", userID)
		}
	}

	if cursor != nil && cursor.FallbackCursor != nil {
		feed.CountFallback()
		logger.Info(ctx, "feed fallback continuation", "user_id", userID)
		return s.discoverFallback(ctx, userID, cursor.FallbackCursor)
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

	candidates, recOffset, recTotal, followingFetched, err := s.collectMixedCandidates(ctx, userID, authorIDs, cursor)
	if err != nil {
		return nil, nil, err
	}
	candidates = filterEligibleCandidates(userID, followingSet, collapseCandidates(candidates))
	if len(candidates) == 0 && !followingFetched {
		feed.CountFallback()
		logger.Info(ctx, "feed fallback entered", "user_id", userID)
		return s.discoverFallback(ctx, userID, nil)
	}

	now := time.Now().UTC()
	items := s.rankCandidates(ctx, candidates, followingSet, now)
	s.sortFeedItems(items)

	page := items
	if len(page) > pageSize {
		page = page[:pageSize]
	}

	s.enrichIsLiked(ctx, userID, page)
	s.enrichIsFollowingAuthor(page, followingSet)

	if len(page) == 0 {
		return nil, nil, nil
	}
	if recTotal == 0 || recOffset >= recTotal {
		return page, nil, nil
	}
	return page, &feed.FeedCursor{
		Version:              feed.FeedCursorVersion,
		RecommendationOffset: recOffset,
		IssuedAt:             time.Now().UTC(),
	}, nil
}

func (s *FeedService) timelineReadAllowed(userID uuid.UUID) bool {
	if s.timelineStore == nil {
		return false
	}
	if !s.timelineOptions.configured {
		return true
	}
	return s.timelineOptions.servingEnabled && inRollout(userID, s.timelineOptions.rolloutPercent)
}

func (s *FeedService) refreshOnMissAllowed() bool {
	if !s.timelineOptions.configured {
		return true
	}
	return s.timelineOptions.refreshOnMiss
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

func (s *FeedService) getFeedFromTimeline(ctx context.Context, userID uuid.UUID, cursor *feed.FeedCursor) ([]*feedentity.FeedItem, *feed.FeedCursor, error) {
	position := (*feed.TimelinePosition)(nil)
	if cursor != nil {
		position = cursor.Timeline
	}
	page, err := s.timelineStore.ReadPage(ctx, userID, position, pageSize*fetchMultiplier)
	if err != nil {
		return nil, nil, err
	}
	if (page == nil || len(page.Entries) == 0) && s.refresher != nil && s.refreshOnMissAllowed() {
		feed.CountLazyRefresh()
		logger.Info(ctx, "timeline refresh on miss started", "user_id", userID)
		if refreshErr := s.refresher.RefreshTimeline(ctx, userID); refreshErr != nil {
			logger.LogError(ctx, refreshErr, "timeline refresh on miss failed", "user_id", userID)
		} else {
			logger.Info(ctx, "timeline refresh on miss completed", "user_id", userID)
			page, err = s.timelineStore.ReadPage(ctx, userID, position, pageSize*fetchMultiplier)
			if err != nil {
				return nil, nil, err
			}
		}
	}
	if page == nil || len(page.Entries) == 0 {
		return nil, nil, nil
	}

	cachedIDs, err := s.getFollowingIDs(ctx, userID)
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
	for _, entry := range page.Entries {
		ids = append(ids, entry.PostID)
		entryByPost[entry.PostID] = entry
	}
	posts, err := s.postReader.GetPostsByIDs(ctx, ids)
	if err != nil {
		return nil, nil, err
	}

	now := time.Now().UTC()
	ranked := s.rankCandidates(ctx, postsToTimelineCandidates(posts), followingSet, now)
	items := make([]*feedentity.FeedItem, 0, pageSize)
	for _, item := range ranked {
		if !isEligibleTimelinePost(userID, followingSet, item.Post) {
			continue
		}
		item.Source = feedentity.SourceFollowing
		items = append(items, item)
		if len(items) == pageSize {
			break
		}
	}
	feed.CountStaleFiltered(len(page.Entries) - len(items))
	s.enrichIsLiked(ctx, userID, items)
	s.enrichIsFollowingAuthor(items, followingSet)

	if len(items) == 0 {
		return nil, nil, nil
	}
	lastEntry := entryByPost[items[len(items)-1].Post.ID]
	next := (*feed.FeedCursor)(nil)
	if len(items) == pageSize || len(page.Entries) == pageSize*fetchMultiplier {
		next = &feed.FeedCursor{
			Version: feed.FeedCursorVersion,
			Timeline: &feed.TimelinePosition{
				Score:  lastEntry.Score,
				PostID: lastEntry.PostID.String(),
			},
			RecommendationOffset: recommendationOffset(cursor),
			IssuedAt:             time.Now().UTC(),
		}
	}
	return items, next, nil
}

func postsToTimelineCandidates(posts []*feedentity.Post) []feedCandidate {
	candidates := make([]feedCandidate, 0, len(posts))
	for i, p := range posts {
		candidates = append(candidates, feedCandidate{post: p, source: feedentity.SourceFollowing, sourceRank: i + 1})
	}
	return candidates
}

func isEligibleTimelinePost(userID uuid.UUID, followingSet map[uuid.UUID]bool, p *feedentity.Post) bool {
	if p == nil {
		return false
	}
	if p.AuthorID != userID && !followingSet[p.AuthorID] {
		return false
	}
	switch p.Visibility {
	case "public", "followers":
		return true
	case "private":
		return p.AuthorID == userID
	default:
		return false
	}
}

func recommendationOffset(cursor *feed.FeedCursor) int {
	if cursor == nil {
		return 0
	}
	return cursor.RecommendationOffset
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

func (s *FeedService) collectMixedCandidates(ctx context.Context, userID uuid.UUID, authorIDs []uuid.UUID, cursor *feed.FeedCursor) ([]feedCandidate, int, int, bool, error) {
	recommendationOffset := recommendationOffset(cursor)
	candidates := make([]feedCandidate, 0, pageSize*fetchMultiplier)

	followingPosts, err := s.postReader.GetFollowingPostsWithCursor(ctx, authorIDs, nil, pageSize*fetchMultiplier)
	if err != nil {
		logger.LogError(ctx, err, "failed to get following posts", "user_id", userID)
		return nil, recommendationOffset, 0, false, errors.NewInternalError(err)
	}
	for i, p := range followingPosts {
		candidates = append(candidates, feedCandidate{post: p, source: feedentity.SourceFollowing, sourceRank: i + 1})
	}

	recommendationTotal := 0
	if s.recommender != nil {
		recPage, recErr := s.recommender.GetRecommendations(ctx, userID.String(), pageSize, recommendationOffset)
		if recErr != nil {
			logger.LogError(ctx, recErr, "codohue recommendations failed, skipping", "user_id", userID)
		} else if recPage != nil {
			recommendationOffset = recPage.Offset + len(recPage.Items)
			recommendationTotal = recPage.Total
			recCandidates, loadErr := s.loadRecommendationCandidates(ctx, recPage.Items)
			if loadErr != nil {
				logger.LogError(ctx, loadErr, "failed to load recommendation candidates", "user_id", userID)
			}
			candidates = append(candidates, recCandidates...)
		}
	}

	if cursor == nil || cursor.TrendingCursor == "" {
		trendingPosts, err := s.getTrending(ctx)
		if err != nil {
			logger.LogError(ctx, err, "failed to get trending posts, skipping", "user_id", userID)
		}
		if len(trendingPosts) > pageSize {
			trendingPosts = trendingPosts[:pageSize]
		}
		for i, p := range trendingPosts {
			candidates = append(candidates, feedCandidate{post: p, source: feedentity.SourceTrending, sourceRank: i + 1})
		}
	}

	return candidates, recommendationOffset, recommendationTotal, len(followingPosts) > 0, nil
}

func (s *FeedService) loadRecommendationCandidates(ctx context.Context, items []feed.RecommendedItem) ([]feedCandidate, error) {
	ids := make([]uuid.UUID, 0, len(items))
	meta := make(map[uuid.UUID]feed.RecommendedItem, len(items))
	for _, item := range items {
		if item.Score < 0 {
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

func filterEligibleCandidates(userID uuid.UUID, followingSet map[uuid.UUID]bool, candidates []feedCandidate) []feedCandidate {
	filtered := candidates[:0]
	for _, candidate := range candidates {
		if candidate.post == nil {
			continue
		}
		switch candidate.source {
		case feedentity.SourceFollowing:
			if isEligibleTimelinePost(userID, followingSet, candidate.post) {
				filtered = append(filtered, candidate)
			}
		case feedentity.SourceRecommendation, feedentity.SourceTrending, feedentity.SourceDiscover:
			if candidate.post.Visibility == "public" {
				filtered = append(filtered, candidate)
			}
		default:
			if candidate.post.Visibility == "public" {
				filtered = append(filtered, candidate)
			}
		}
	}
	return filtered
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

func (s *FeedService) discoverFallback(ctx context.Context, userID uuid.UUID, cursor *feed.DiscoverCursor) ([]*feedentity.FeedItem, *feed.FeedCursor, error) {
	posts, err := s.postReader.GetDiscoverWithCursor(ctx, cursor, pageSize+1, nil)
	if err != nil {
		logger.LogError(ctx, err, "failed to get discover fallback", "user_id", userID)
		return nil, nil, errors.NewInternalError(err)
	}

	hasMore := len(posts) > pageSize
	if hasMore {
		posts = posts[:pageSize]
	}

	now := time.Now()
	scores, rankErr := s.ranker.RankPosts(ctx, posts, map[string]bool{}, now)
	if rankErr != nil {
		logger.LogError(ctx, rankErr, "ranker failed in discover fallback", "user_id", userID)
		scores = make(map[string]float64)
	}
	items := make([]*feedentity.FeedItem, 0, len(posts))
	for _, p := range posts {
		items = append(items, &feedentity.FeedItem{
			Post:   p,
			Score:  scores[p.ID.String()],
			Source: feedentity.SourceDiscover,
		})
	}

	s.enrichIsLiked(ctx, userID, items)
	s.enrichIsFollowingAuthorFromDB(ctx, userID, items)
	if len(items) == 0 || !hasMore {
		return items, nil, nil
	}
	last := items[len(items)-1].Post
	return items, &feed.FeedCursor{
		Version:        feed.FeedCursorVersion,
		FallbackCursor: &feed.DiscoverCursor{CreatedAt: last.CreatedAt, PostID: last.ID.String()},
		IssuedAt:       time.Now().UTC(),
	}, nil
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
