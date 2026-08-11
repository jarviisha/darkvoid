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
	"golang.org/x/sync/singleflight"
)

const scoreEpsilon = 1e-9

const (
	pageSize           = 20
	fetchMultiplier    = 3
	trendingFetchLimit = 100
	// sharedRebuildTimeout bounds the single-flight rebuilds (trending fetch,
	// timeline refresh-on-miss). They run detached from the leader's request
	// context — a leader disconnecting must not fail every waiter — so they
	// need their own deadline or a hung provider call would pin the flight
	// open indefinitely.
	sharedRebuildTimeout = 10 * time.Second
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
	// settings carries the timeline rollout gates. Nil means "no gates" — the
	// pre-rollout behaviour the tests that predate this rely on — which is why the
	// checks below test the pointer rather than calling Get and reading a default
	// that would gate them off.
	settings *feed.Settings
	// flight collapses concurrent expensive rebuilds: the global trending
	// fetch (whose cache expiry is a synchronized miss across every in-flight
	// page-1 request) and per-user timeline refresh-on-miss. Per instance
	// only — siblings may still rebuild once each, which is the cheap 90%.
	flight singleflight.Group
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

// WithSettings attaches the live settings holder that gates prepared timeline
// reads. Once attached, every read consults the current snapshot, so an operator
// raising the rollout percent takes effect on the next request rather than the
// next restart.
func (s *FeedService) WithSettings(settings *feed.Settings) {
	s.settings = settings
}

// GetFeed returns the cursor-paginated mixed feed for userID.
func (s *FeedService) GetFeed(ctx context.Context, userID uuid.UUID, cursor *feed.FeedCursor) ([]*feedentity.FeedItem, *feed.FeedCursor, error) {
	if cursor != nil {
		if err := cursor.ValidateForUser(userID); err != nil {
			return nil, nil, errors.NewBadRequestError("invalid cursor")
		}
	}
	// Once the feed has handed off to the discover stream it stays there:
	// the mixed sources are already exhausted for this scroll, so re-collecting
	// them would only re-serve posts the earlier pages showed.
	if pos := cursor.DiscoverPosition(); pos != nil {
		return s.discoverFallback(ctx, userID, pos)
	}
	// The prepared timeline serves fresh reads and its own continuations. A
	// cursor holding mixed-path state (following/trending/recommendation) means
	// this scroll started on the mixed path — switching to the timeline
	// mid-scroll would restart from its top and duplicate what the mixed pages
	// already served.
	if s.timelineReadAllowed(userID) && (cursor == nil || cursor.TimelinePosition() != nil) {
		items, next, err := s.getFeedFromTimeline(ctx, userID, cursor)
		if err == nil && len(items) > 0 {
			feed.CountTimelineHit()
			logger.Info(ctx, "timeline feed hit", "user_id", userID, "items", len(items))
			return items, next, nil
		}
		if err == nil && cursor != nil && cursor.TimelinePosition() != nil {
			// A timeline continuation stays in the timeline family. If all
			// remaining entries became stale, end this scroll instead of silently
			// switching cursor families and re-serving public posts from the top.
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

	candidates, recWindow, trendingCollected, followingFetched, err := s.collectMixedCandidates(ctx, userID, authorIDs, cursor)
	if err != nil {
		return nil, nil, err
	}
	candidates = filterEligibleCandidates(userID, followingSet, collapseCandidates(candidates))
	recWindow.validOffsets = recommendationCandidateOffsets(candidates)
	if len(candidates) == 0 && !followingFetched {
		feed.CountFallback()
		logger.Info(ctx, "feed fallback entered", "user_id", userID)
		// Hand off at the following position when there is one: the discover
		// stream then continues chronologically below the last following post
		// served, instead of restarting from the newest public post.
		return s.discoverFallback(ctx, userID, discoverHandoff(cursor))
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
	return page, nextMixedCursor(userID, page, cursor, recWindow, trendingCollected), nil
}

// discoverHandoff converts a following continuation into a discover one. The
// two cursors share (created_at, post_id) semantics, which is what makes the
// hand-off seamless: discover resumes exactly where following ran dry.
func discoverHandoff(cursor *feed.FeedCursor) *feed.DiscoverCursor {
	pos := cursor.FollowingPosition()
	if pos == nil {
		return nil
	}
	return &feed.DiscoverCursor{CreatedAt: pos.CreatedAt, PostID: pos.PostID}
}

func (s *FeedService) timelineReadAllowed(userID uuid.UUID) bool {
	if s.timelineStore == nil {
		return false
	}
	if s.settings == nil {
		return true
	}
	rs := s.settings.Get()
	return rs.TimelineEnabled && inRollout(userID, rs.TimelineRolloutPercent)
}

func (s *FeedService) refreshOnMissAllowed() bool {
	if s.settings == nil {
		return true
	}
	return s.settings.Get().TimelineRefreshOnMiss
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
		position = cursor.TimelinePosition()
	}
	page, err := s.timelineStore.ReadPage(ctx, userID, position, pageSize*fetchMultiplier)
	if err != nil {
		return nil, nil, err
	}
	if position == nil && (page == nil || len(page.Entries) == 0) && s.refresher != nil && s.refreshOnMissAllowed() {
		feed.CountLazyRefresh()
		logger.Info(ctx, "timeline refresh on miss started", "user_id", userID)
		if refreshErr := s.refreshTimelineShared(ctx, userID); refreshErr != nil {
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
	entryOrder := make(map[uuid.UUID]int, len(page.Entries))
	for i, entry := range page.Entries {
		ids = append(ids, entry.PostID)
		entryByPost[entry.PostID] = entry
		entryOrder[entry.PostID] = i
	}
	posts, err := s.postReader.GetPostsByIDs(ctx, ids)
	if err != nil {
		return nil, nil, err
	}

	// The timeline is already ranked (materialized packed scores) — serve it
	// in ZSET order, no realtime ranking. GetPostsByIDs gives no ordering
	// guarantee, so the hydrated posts are reordered explicitly.
	sort.Slice(posts, func(i, j int) bool {
		return entryOrder[posts[i].ID] < entryOrder[posts[j].ID]
	})
	items := make([]*feedentity.FeedItem, 0, pageSize+1)
	for _, p := range posts {
		if !isEligibleTimelinePost(userID, followingSet, p) {
			continue
		}
		items = append(items, &feedentity.FeedItem{
			Post:   p,
			Score:  feed.UnpackTimelineRank(entryByPost[p.ID].Score),
			Source: feedentity.SourceFollowing,
		})
		if len(items) == pageSize+1 {
			break
		}
	}
	// Stale = entries examined but not served. On a full page only entries up
	// to the last kept one were examined; counting the whole read window would
	// report healthy beyond-the-page entries as stale.
	considered := len(page.Entries)
	if len(items) == pageSize+1 {
		considered = entryOrder[items[len(items)-1].Post.ID] + 1
	}
	feed.CountStaleFiltered(considered - len(items))
	hasMore := page.HasMore || len(items) > pageSize
	if len(items) > pageSize {
		items = items[:pageSize]
	}
	s.enrichIsLiked(ctx, userID, items)
	s.enrichIsFollowingAuthor(items, followingSet)

	if len(items) == 0 {
		return nil, nil, nil
	}
	lastEntry := entryByPost[items[len(items)-1].Post.ID]
	next := (*feed.FeedCursor)(nil)
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

// refreshTimelineShared collapses concurrent on-miss rebuilds of one user's
// timeline. A rebuild reads the whole following set and re-ranks up to
// timeline_max_items posts, so N parallel requests from one cold user — a
// refresh-spamming client, or the first page after a Redis flush — must not
// run it N times. Detached from the leader's cancellation and bounded by its
// own deadline, for the same reasons as the trending rebuild.
func (s *FeedService) refreshTimelineShared(ctx context.Context, userID uuid.UUID) error {
	_, err, _ := s.flight.Do("timeline-refresh:"+userID.String(), func() (any, error) {
		refreshCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), sharedRebuildTimeout)
		defer cancel()
		return nil, s.refresher.RefreshTimeline(refreshCtx, userID)
	})
	return err
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

func recommendationSeenSet(cursor *feed.FeedCursor) map[int]bool {
	seen := make(map[int]bool)
	if cursor == nil {
		return seen
	}
	for _, offset := range cursor.RecommendationSeen {
		seen[offset] = true
	}
	return seen
}

func recommendationCandidateOffsets(candidates []feedCandidate) map[int]bool {
	offsets := make(map[int]bool)
	for _, candidate := range candidates {
		if candidate.recommendationOffset != nil {
			offsets[*candidate.recommendationOffset] = true
		}
	}
	return offsets
}

// feedCandidate is a post being considered for inclusion in the feed.
// recommendationScore/recommendationRank carry the upstream Codohue values for
// observability and downstream re-ranking — they are NOT the post's final
// position in the returned page (that is decided after sortFeedItems).
type feedCandidate struct {
	post                 *feedentity.Post
	source               feedentity.Source
	sourceRank           int
	providerScore        *float64
	providerRank         *int
	recommendationScore  *float64
	recommendationRank   *int
	recommendationOffset *int
}

type recommendationWindow struct {
	start        int
	end          int
	total        int
	validOffsets map[int]bool
}

func (s *FeedService) collectMixedCandidates(ctx context.Context, userID uuid.UUID, authorIDs []uuid.UUID, cursor *feed.FeedCursor) ([]feedCandidate, recommendationWindow, bool, bool, error) {
	recommendationOffset := recommendationOffset(cursor)
	recWindow := recommendationWindow{start: recommendationOffset, end: recommendationOffset}
	candidates := make([]feedCandidate, 0, pageSize*fetchMultiplier)

	followingPosts, err := s.postReader.GetFollowingPostsWithCursor(ctx, authorIDs, userID, cursor.FollowingPosition(), pageSize*fetchMultiplier)
	if err != nil {
		logger.LogError(ctx, err, "failed to get following posts", "user_id", userID)
		return nil, recWindow, false, false, errors.NewInternalError(err)
	}
	for i, p := range followingPosts {
		candidates = append(candidates, feedCandidate{post: p, source: feedentity.SourceFollowing, sourceRank: i + 1})
	}

	if s.recommender != nil {
		recPage, recErr := s.recommender.GetRecommendations(ctx, userID.String(), pageSize, recommendationOffset)
		if recErr != nil {
			logger.LogError(ctx, recErr, "codohue recommendations failed, skipping", "user_id", userID)
		} else if recPage != nil {
			recWindow.start = recPage.Offset
			recWindow.end = recPage.Offset + len(recPage.Items)
			recWindow.total = recPage.Total
			recCandidates, loadErr := s.loadRecommendationCandidates(ctx, recPage.Items, recPage.Offset, cursor)
			if loadErr != nil {
				logger.LogError(ctx, loadErr, "failed to load recommendation candidates", "user_id", userID)
			}
			candidates = append(candidates, recCandidates...)
		}
	}

	trendingCollected := false
	if cursor == nil || cursor.TrendingPosition() != nil {
		trendingPosts, err := s.getTrending(ctx)
		if err != nil {
			logger.LogError(ctx, err, "failed to get trending posts, skipping", "user_id", userID)
		}
		trendingPosts = applyTrendingCursor(trendingPosts, cursor.TrendingPosition(), pageSize)
		for i, p := range trendingPosts {
			candidates = append(candidates, feedCandidate{post: p, source: feedentity.SourceTrending, sourceRank: i + 1})
		}
		trendingCollected = len(trendingPosts) > 0
	}

	return candidates, recWindow, trendingCollected, len(followingPosts) > 0, nil
}

func (s *FeedService) loadRecommendationCandidates(ctx context.Context, items []feed.RecommendedItem, baseOffset int, cursor *feed.FeedCursor) ([]feedCandidate, error) {
	ids := make([]uuid.UUID, 0, len(items))
	meta := make(map[uuid.UUID]feed.RecommendedItem, len(items))
	offsets := make(map[uuid.UUID]int, len(items))
	seen := recommendationSeenSet(cursor)
	for i, item := range items {
		offset := baseOffset + i
		if seen[offset] {
			continue
		}
		if item.Score < 0 {
			continue
		}
		id, err := uuid.Parse(item.ObjectID)
		if err != nil {
			continue
		}
		ids = append(ids, id)
		meta[id] = item
		offsets[id] = offset
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
		offset := offsets[p.ID]
		result = append(result, feedCandidate{
			post:                 p,
			source:               feedentity.SourceRecommendation,
			sourceRank:           item.Rank,
			providerScore:        &score,
			providerRank:         &rank,
			recommendationScore:  &score,
			recommendationRank:   &rank,
			recommendationOffset: &offset,
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
			existing.recommendationOffset = c.recommendationOffset
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
		case feedentity.SourceRecommendation:
			// A user's own post must never come back to them labeled
			// "recommended". Codohue is expected to exclude authored objects
			// upstream, but the guarantee belongs on this side of the trust
			// boundary too — a provider config change must not be able to put
			// a user's post in their own suggestions. (Own posts remain fine
			// as trending: the badge claims popularity, not discovery.)
			if candidate.post.Visibility == "public" && candidate.post.AuthorID != userID {
				filtered = append(filtered, candidate)
			}
		case feedentity.SourceTrending, feedentity.SourceDiscover:
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

func nextMixedCursor(userID uuid.UUID, page []*feedentity.FeedItem, incoming *feed.FeedCursor, recWindow recommendationWindow, trendingCollected bool) *feed.FeedCursor {
	next := &feed.FeedCursor{TimelineUser: userID.String()}
	seen := recommendationSeenSet(incoming)
	for _, item := range page {
		if item.RecommendationOffset != nil {
			seen[*item.RecommendationOffset] = true
		}
	}
	recOffset := recWindow.start
	for recOffset < recWindow.end && (seen[recOffset] || !recWindow.validOffsets[recOffset]) {
		delete(seen, recOffset)
		recOffset++
	}
	if recWindow.total > 0 && recOffset < recWindow.total {
		next.RecommendationOffset = recOffset
		for offset := range seen {
			if offset >= recOffset {
				next.RecommendationSeen = append(next.RecommendationSeen, offset)
			}
		}
		sort.Ints(next.RecommendationSeen)
	}
	// The trending position advances to the lowest-trend-score item served on
	// this page — the page is blend-ordered, so the last trending item in page
	// order is not necessarily the lowest, and any other boundary re-serves the
	// shown items scoring below it. When no trending item made the page the
	// incoming position carries over, and on a first page whose trending
	// candidates were all outranked, a start-of-list sentinel keeps the source
	// alive — a cursor without a trending position stops injecting trending
	// for the rest of the scroll.
	if lowest := lowestTrendingShown(page); lowest != nil {
		score := trendScoreFromPost(lowest)
		next.TrendingScore = &score
		next.TrendingPostID = lowest.ID.String()
	} else if incoming.TrendingPosition() != nil {
		next.TrendingScore = incoming.TrendingScore
		next.TrendingPostID = incoming.TrendingPostID
	} else if trendingCollected {
		score := math.MaxFloat64
		next.TrendingScore = &score
		next.TrendingPostID = uuid.Max.String()
	}
	// The following position advances to the oldest following post served on
	// this page — the page is score-ordered, so "oldest shown" rather than
	// "last shown" is what keeps the DB scan monotone. Fetched-but-unshown
	// posts older than it get refetched next page and keep their chance; when
	// no following post made the page, the incoming position carries over
	// unchanged so nothing already served is refetched from the top.
	if oldest := oldestFollowingShown(page); oldest != nil {
		ts := oldest.CreatedAt.UnixNano()
		next.FollowingCreatedAt = &ts
		next.FollowingPostID = oldest.ID.String()
	} else if incoming != nil && incoming.FollowingCreatedAt != nil {
		next.FollowingCreatedAt = incoming.FollowingCreatedAt
		next.FollowingPostID = incoming.FollowingPostID
	}
	if !next.HasContinuation() {
		return nil
	}
	return next
}

// lowestTrendingShown returns the trending-source post on the page with the
// smallest (trend score, id) — the continuation point matching
// isAfterTrendCursor's (score DESC, id DESC) ordering.
func lowestTrendingShown(page []*feedentity.FeedItem) *feedentity.Post {
	var lowest *feedentity.Post
	for _, item := range page {
		if item.Source != feedentity.SourceTrending || item.Post == nil {
			continue
		}
		p := item.Post
		if lowest == nil {
			lowest = p
			continue
		}
		ps, ls := trendScoreFromPost(p), trendScoreFromPost(lowest)
		if ps < ls || (ps == ls && p.ID.String() < lowest.ID.String()) {
			lowest = p
		}
	}
	return lowest
}

// oldestFollowingShown returns the following-source post on the page with the
// smallest (created_at, id) — the continuation point for the next DB scan,
// matching GetFollowingPostsWithCursor's (created_at, id) < (ts, id) ordering.
func oldestFollowingShown(page []*feedentity.FeedItem) *feedentity.Post {
	var oldest *feedentity.Post
	for _, item := range page {
		if item.Source != feedentity.SourceFollowing || item.Post == nil {
			continue
		}
		p := item.Post
		if oldest == nil ||
			p.CreatedAt.Before(oldest.CreatedAt) ||
			(p.CreatedAt.Equal(oldest.CreatedAt) && p.ID.String() < oldest.ID.String()) {
			oldest = p
		}
	}
	return oldest
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
			Post:                 c.post,
			Score:                score,
			Source:               c.source,
			RecommendationScore:  c.recommendationScore,
			RecommendationRank:   c.recommendationRank,
			RecommendationOffset: c.recommendationOffset,
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
	// Fetch one extra to detect whether a next page exists.
	posts, err := s.postReader.GetDiscoverWithCursor(ctx, cursor, pageSize+1, nil)
	if err != nil {
		logger.LogError(ctx, err, "failed to get discover fallback", "user_id", userID)
		return nil, nil, errors.NewInternalError(err)
	}
	hasMore := len(posts) > pageSize
	if hasMore {
		posts = posts[:pageSize]
	}

	now := time.Now().UTC()
	scores, rankErr := s.ranker.RankPosts(ctx, posts, map[string]bool{}, now)
	if rankErr != nil {
		logger.LogError(ctx, rankErr, "ranker failed in discover fallback", "user_id", userID)
		scores = make(map[string]float64)
	}
	// Items keep DB (created_at, id) order — the order the cursor paginates in.
	// Scores are attached for observability only; re-sorting by them here would
	// desync the served order from the continuation point.
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

	var next *feed.FeedCursor
	if hasMore && len(posts) > 0 {
		last := posts[len(posts)-1]
		ts := last.CreatedAt.UnixNano()
		next = &feed.FeedCursor{
			TimelineUser:      userID.String(),
			DiscoverCreatedAt: &ts,
			DiscoverPostID:    last.ID.String(),
		}
	}
	return items, next, nil
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

	// One flight rebuilds, the rest share the result: the trending key is
	// global with one hard TTL, so its expiry is a synchronized miss across
	// every in-flight page-1 request on this instance, each otherwise paying
	// the provider fetch plus the DB aggregate. The rebuild detaches from the
	// leader's cancellation (with its own deadline) so a leader disconnecting
	// mid-fetch does not fail every waiter.
	posts, err, _ := s.flight.Do("trending", func() (any, error) {
		rebuildCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), sharedRebuildTimeout)
		defer cancel()
		return s.rebuildTrending(rebuildCtx)
	})
	if err != nil {
		return nil, err
	}
	shared, _ := posts.([]*feedentity.Post)
	return copyPosts(shared), nil
}

// copyPosts returns per-caller copies of a shared post slice. Every caller of
// a collapsed rebuild receives the same underlying posts, but downstream
// enrichment writes viewer-specific fields (IsLiked, IsFollowingAuthor) onto
// them — served shared, one viewer's flags would race into another's response.
// A shallow copy is enough: enrichment only writes scalars, and Media/Author
// are read-only past this point. The cache-hit path needs none of this, since
// each request unmarshals its own instance from Redis.
func copyPosts(shared []*feedentity.Post) []*feedentity.Post {
	out := make([]*feedentity.Post, len(shared))
	for i, p := range shared {
		if p == nil {
			continue
		}
		cp := *p
		out[i] = &cp
	}
	return out
}

func (s *FeedService) rebuildTrending(ctx context.Context) ([]*feedentity.Post, error) {
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

func applyTrendingCursor(posts []*feedentity.Post, cursor *feed.TrendPosition, limit int) []*feedentity.Post {
	filtered := make([]*feedentity.Post, 0, len(posts))
	for _, p := range posts {
		if p == nil {
			continue
		}
		if cursor != nil && !isAfterTrendCursor(p, cursor) {
			continue
		}
		filtered = append(filtered, p)
	}
	if len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered
}

func isAfterTrendCursor(p *feedentity.Post, cursor *feed.TrendPosition) bool {
	score := trendScoreFromPost(p)
	return score < cursor.Score || (score == cursor.Score && p.ID.String() < cursor.PostID)
}

func trendScoreFromPost(p *feedentity.Post) float64 {
	if p == nil {
		return math.MaxFloat64
	}
	return float64(p.LikeCount)
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
