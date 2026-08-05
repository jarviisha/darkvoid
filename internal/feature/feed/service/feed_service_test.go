package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/feature/feed"
	feedcache "github.com/jarviisha/darkvoid/internal/feature/feed/cache"
	feedentity "github.com/jarviisha/darkvoid/internal/feature/feed/entity"
)

type mockPostReader struct {
	following   []*feedentity.Post
	trending    []*feedentity.Post
	discover    []*feedentity.Post
	byID        map[uuid.UUID]*feedentity.Post
	byIDErr     error
	trendingErr error
	// returnOrder, when set, forces GetPostsByIDs to return posts in this
	// order instead of the requested ids order — GetPostsByIDs gives no
	// ordering guarantee in production (WHERE id = ANY), and tests use this to
	// prove the service does not depend on hydrate order.
	returnOrder []uuid.UUID
	// trendingCalls counts GetTrendingPosts calls (atomically) and
	// trendingGate, when set, blocks each call until closed — used by the
	// single-flight collapse tests.
	trendingCalls atomic.Int32
	trendingGate  chan struct{}
}

func (m *mockPostReader) GetFollowingPostsWithCursor(_ context.Context, _ []uuid.UUID, cursor *feed.FollowingCursor, limit int32) ([]*feedentity.Post, error) {
	return applyFollowingCursor(m.following, cursor, int(limit)), nil
}

func (m *mockPostReader) GetTrendingPosts(_ context.Context, limit int32) ([]*feedentity.Post, error) {
	m.trendingCalls.Add(1)
	if m.trendingGate != nil {
		<-m.trendingGate
	}
	if m.trendingErr != nil {
		return nil, m.trendingErr
	}
	return takePosts(m.trending, int(limit)), nil
}

func (m *mockPostReader) GetDiscoverWithCursor(_ context.Context, cursor *feed.DiscoverCursor, limit int32, _ *uuid.UUID) ([]*feedentity.Post, error) {
	return applyDiscoverCursor(m.discover, cursor, int(limit)), nil
}

func (m *mockPostReader) GetPostsByIDs(_ context.Context, ids []uuid.UUID) ([]*feedentity.Post, error) {
	if m.byIDErr != nil {
		return nil, m.byIDErr
	}
	order := ids
	if len(m.returnOrder) > 0 {
		requested := make(map[uuid.UUID]bool, len(ids))
		for _, id := range ids {
			requested[id] = true
		}
		order = make([]uuid.UUID, 0, len(m.returnOrder))
		for _, id := range m.returnOrder {
			if requested[id] {
				order = append(order, id)
			}
		}
	}
	result := make([]*feedentity.Post, 0, len(order))
	for _, id := range order {
		if p, ok := m.byID[id]; ok {
			result = append(result, p)
		}
	}
	return result, nil
}

type mockFollowReader struct {
	ids []uuid.UUID
}

func (m *mockFollowReader) GetFollowingIDs(_ context.Context, _ uuid.UUID) ([]uuid.UUID, error) {
	return m.ids, nil
}

type mockLikeReader struct{}

func (m *mockLikeReader) GetLikedPostIDs(_ context.Context, _ uuid.UUID, _ []uuid.UUID) ([]uuid.UUID, error) {
	return nil, nil
}

type mockTimelineStore struct {
	pages       []*feed.TimelinePage
	readCount   int
	addedBatch  []feed.TimelineEntry
	addedUserID uuid.UUID
}

func (m *mockTimelineStore) AddPost(_ context.Context, userID uuid.UUID, entry feed.TimelineEntry) error {
	m.addedUserID = userID
	m.addedBatch = append(m.addedBatch, entry)
	return nil
}

func (m *mockTimelineStore) SetPostsBatch(_ context.Context, userID uuid.UUID, entries []feed.TimelineEntry) error {
	m.addedUserID = userID
	m.addedBatch = append(m.addedBatch, entries...)
	return nil
}

func (m *mockTimelineStore) ReadPage(_ context.Context, _ uuid.UUID, _ *feed.TimelinePosition, _ int) (*feed.TimelinePage, error) {
	if m.readCount >= len(m.pages) {
		m.readCount++
		return &feed.TimelinePage{}, nil
	}
	page := m.pages[m.readCount]
	m.readCount++
	return page, nil
}

func (m *mockTimelineStore) Trim(_ context.Context, _ uuid.UUID) error { return nil }

func (m *mockTimelineStore) RemovePostBestEffort(_ context.Context, _ uuid.UUID, _ uuid.UUID) error {
	return nil
}

type mockTimelineRefresher struct {
	calls int
	err   error
}

func (m *mockTimelineRefresher) RefreshTimeline(_ context.Context, _ uuid.UUID) error {
	m.calls++
	return m.err
}

type mockRanker struct {
	scores map[uuid.UUID]float64
}

func (m *mockRanker) RankPosts(_ context.Context, posts []*feedentity.Post, _ map[string]bool, _ time.Time) (map[string]float64, error) {
	result := make(map[string]float64, len(posts))
	for _, p := range posts {
		result[p.ID.String()] = m.scores[p.ID]
	}
	return result, nil
}

type mockRecommender struct {
	items []feed.RecommendedItem
	err   error
}

func (m *mockRecommender) GetRecommendations(_ context.Context, _ string, limit int, offset int) (*feed.RecommendationPage, error) {
	if m.err != nil {
		return nil, m.err
	}
	if offset >= len(m.items) {
		return &feed.RecommendationPage{Items: nil, Limit: limit, Offset: offset, Total: len(m.items)}, nil
	}
	end := offset + limit
	if end > len(m.items) {
		end = len(m.items)
	}
	return &feed.RecommendationPage{Items: m.items[offset:end], Limit: limit, Offset: offset, Total: len(m.items)}, nil
}

// timelineSettings builds the snapshot the rollout gates read from. The service
// consults it per request, so these are the settings in force for the call under
// test rather than the ones it was constructed with.
func timelineSettings(enabled bool, rolloutPercent int, refreshOnMiss bool) *feed.Settings {
	rs := feed.DefaultRuntimeSettings()
	rs.TimelineEnabled = enabled
	rs.TimelineRolloutPercent = rolloutPercent
	rs.TimelineRefreshOnMiss = refreshOnMiss
	return feed.NewSettings(rs)
}

func newTestService(posts *mockPostReader, ranker feed.Ranker) *FeedService {
	return NewFeedService(
		posts,
		&mockFollowReader{ids: []uuid.UUID{uuid.New()}},
		&mockLikeReader{},
		ranker,
		feedcache.NewNopFeedCache(),
	)
}

func TestGetFeed_MixedFallbackDoesNotEmitSessionCursor(t *testing.T) {
	now := time.Now().UTC()
	reader := &mockPostReader{byID: map[uuid.UUID]*feedentity.Post{}}
	scores := map[uuid.UUID]float64{}

	for i := 0; i < 5; i++ {
		p := testPost(now.Add(-time.Duration(i) * time.Minute))
		reader.following = append(reader.following, p)
		reader.byID[p.ID] = p
		scores[p.ID] = float64(i)
	}
	for i := 0; i < 5; i++ {
		p := testPost(now.Add(-time.Duration(i+40) * time.Minute))
		reader.trending = append(reader.trending, p)
		reader.byID[p.ID] = p
		scores[p.ID] = 500 + float64(i)
	}

	svc := newTestService(reader, &mockRanker{scores: scores})
	userID := uuid.New()
	page1, cursor, err := svc.GetFeed(context.Background(), userID, nil)
	if err != nil {
		t.Fatalf("GetFeed page1: %v", err)
	}
	if len(page1) == 0 {
		t.Fatal("expected mixed fallback items")
	}
	// Trending items were served, so the cursor carries a plain trending
	// position — continuation state stays flat fields, never session-backed.
	if cursor == nil || cursor.TrendingScore == nil {
		t.Fatalf("cursor = %+v, want stateless trending continuation", cursor)
	}
	page2, next, err := svc.GetFeed(context.Background(), userID, cursor)
	if err != nil {
		t.Fatalf("GetFeed page2: %v", err)
	}
	if len(page2) != 0 || next != nil {
		t.Fatalf("page2 len/cursor = %d/%+v, want exhausted scroll", len(page2), next)
	}
}

// TestGetFeed_TimelineOrderCharacterization pins the served order of a
// timeline page for a fixed fixture (Constitution II: characterize before
// refactoring). Entries are stored rank-descending and hydration returns them
// in the same order — the stable production case — so this test must keep
// passing UNMODIFIED through the materialized-ranking refactor: before it, the
// order comes from realtime ranking; after it, from the ZSET entry order. The
// fixture aligns both.
func TestGetFeed_TimelineOrderCharacterization(t *testing.T) {
	now := time.Now().UTC()
	userID := uuid.New()
	reader := &mockPostReader{byID: map[uuid.UUID]*feedentity.Post{}}
	scores := map[uuid.UUID]float64{}

	ages := []time.Duration{time.Hour, 2 * time.Hour, 3 * time.Hour, 10 * time.Hour}
	ranks := []float64{63, 38, 21, 10.5}
	likes := []int64{100, 10, 3, 0}
	entries := make([]feed.TimelineEntry, 0, len(ages))
	ordered := make([]uuid.UUID, 0, len(ages))
	for i := range ages {
		p := testPost(now.Add(-ages[i]))
		p.AuthorID = userID
		p.LikeCount = likes[i]
		reader.byID[p.ID] = p
		scores[p.ID] = ranks[i]
		entries = append(entries, feed.TimelineEntry{PostID: p.ID, Score: int64(1000 - i)})
		ordered = append(ordered, p.ID)
	}
	store := &mockTimelineStore{pages: []*feed.TimelinePage{{Entries: entries}}}

	svc := newTestService(reader, &mockRanker{scores: scores})
	svc.WithTimelineStore(store)
	page, cursor, err := svc.GetFeed(context.Background(), userID, nil)
	if err != nil {
		t.Fatalf("GetFeed: %v", err)
	}
	if len(page) != len(ordered) {
		t.Fatalf("page len = %d, want %d", len(page), len(ordered))
	}
	for i, item := range page {
		if item.Post.ID != ordered[i] {
			t.Fatalf("position %d = %s, want %s", i, item.Post.ID, ordered[i])
		}
		if item.Source != feedentity.SourceFollowing {
			t.Fatalf("position %d source = %s, want following", i, item.Source)
		}
	}
	if cursor != nil {
		t.Fatalf("cursor = %+v, want nil for exhausted timeline page", cursor)
	}
}

// TestGetFeed_TimelineServesStoredOrder is the US2 behavior test: the page
// must follow the materialized ZSET order even when (a) realtime re-ranking
// would order differently and (b) hydration returns posts shuffled
// (GetPostsByIDs gives no ordering guarantee). Served scores must be the
// unpacked materialized ranks, not realtime ones.
func TestGetFeed_TimelineServesStoredOrder(t *testing.T) {
	now := time.Now().UTC()
	userID := uuid.New()
	reader := &mockPostReader{byID: map[uuid.UUID]*feedentity.Post{}}

	// Stored order deliberately CONTRADICTS the realtime rank order: the
	// first entry gets the LOWER realtime score.
	first := testPost(now.Add(-2 * time.Hour))
	first.AuthorID = userID
	second := testPost(now.Add(-time.Hour))
	second.AuthorID = userID
	reader.byID[first.ID] = first
	reader.byID[second.ID] = second
	realtimeScores := map[uuid.UUID]float64{first.ID: 1, second.ID: 100}
	entries := []feed.TimelineEntry{
		{PostID: first.ID, Score: feed.PackTimelineScore(50, first.CreatedAt)},
		{PostID: second.ID, Score: feed.PackTimelineScore(40, second.CreatedAt)},
	}
	// Hydration returns posts reversed relative to the entries.
	reader.returnOrder = []uuid.UUID{second.ID, first.ID}
	store := &mockTimelineStore{pages: []*feed.TimelinePage{{Entries: entries}}}

	svc := newTestService(reader, &mockRanker{scores: realtimeScores})
	svc.WithTimelineStore(store)
	page, _, err := svc.GetFeed(context.Background(), userID, nil)
	if err != nil {
		t.Fatalf("GetFeed: %v", err)
	}
	if len(page) != 2 || page[0].Post.ID != first.ID || page[1].Post.ID != second.ID {
		t.Fatalf("page order = %v, want stored ZSET order [%s %s]", pageIDs(page), first.ID, second.ID)
	}
	if page[0].Score != 50 || page[1].Score != 40 {
		t.Fatalf("served scores = %v/%v, want unpacked materialized ranks 50/40", page[0].Score, page[1].Score)
	}
}

func pageIDs(page []*feedentity.FeedItem) []uuid.UUID {
	ids := make([]uuid.UUID, len(page))
	for i, item := range page {
		ids[i] = item.Post.ID
	}
	return ids
}

func TestGetFeed_TimelineFirstOrderingAndCursor(t *testing.T) {
	now := time.Now().UTC()
	userID := uuid.New()
	reader := &mockPostReader{byID: map[uuid.UUID]*feedentity.Post{}}
	store := &mockTimelineStore{}
	scores := map[uuid.UUID]float64{}

	entries := make([]feed.TimelineEntry, 0, pageSize+1)
	for i := 0; i < pageSize+1; i++ {
		p := testPost(now.Add(-time.Duration(i) * time.Minute))
		p.AuthorID = userID
		reader.byID[p.ID] = p
		scores[p.ID] = float64(100 - i)
		entries = append(entries, feed.TimelineEntry{PostID: p.ID, Score: feed.PackTimelineScore(30, p.CreatedAt)})
	}
	store.pages = []*feed.TimelinePage{{Entries: entries}}

	svc := newTestService(reader, &mockRanker{scores: scores})
	svc.WithTimelineStore(store)
	page, cursor, err := svc.GetFeed(context.Background(), userID, nil)
	if err != nil {
		t.Fatalf("GetFeed: %v", err)
	}
	if len(page) != pageSize {
		t.Fatalf("page len = %d, want %d", len(page), pageSize)
	}
	if page[0].Post.ID != entries[0].PostID {
		t.Fatalf("first post = %s, want newest timeline post %s", page[0].Post.ID, entries[0].PostID)
	}
	if cursor == nil || cursor.TimelineScore == nil || cursor.TimelinePostID != page[len(page)-1].Post.ID.String() || cursor.TimelineUser != userID.String() {
		t.Fatalf("next cursor mismatch: %+v", cursor)
	}
}

func TestGetFeed_TimelinePaginationNoDuplicates(t *testing.T) {
	now := time.Now().UTC()
	userID := uuid.New()
	reader := &mockPostReader{byID: map[uuid.UUID]*feedentity.Post{}}
	allEntries := make([]feed.TimelineEntry, 0, pageSize+5)
	for i := 0; i < pageSize+5; i++ {
		p := testPost(now.Add(-time.Duration(i) * time.Minute))
		p.AuthorID = userID
		reader.byID[p.ID] = p
		allEntries = append(allEntries, feed.TimelineEntry{PostID: p.ID, Score: feed.PackTimelineScore(30, p.CreatedAt)})
	}
	store := &mockTimelineStore{pages: []*feed.TimelinePage{
		{Entries: allEntries[:pageSize]},
		{Entries: allEntries[pageSize:]},
	}}

	svc := newTestService(reader, &mockRanker{scores: map[uuid.UUID]float64{}})
	svc.WithTimelineStore(store)
	page1, cursor, err := svc.GetFeed(context.Background(), userID, nil)
	if err != nil {
		t.Fatalf("GetFeed page1: %v", err)
	}
	if cursor == nil {
		t.Fatal("expected next cursor")
	}
	page2, _, err := svc.GetFeed(context.Background(), userID, cursor)
	if err != nil {
		t.Fatalf("GetFeed page2: %v", err)
	}

	seen := make(map[uuid.UUID]bool)
	for _, item := range append(page1, page2...) {
		if seen[item.Post.ID] {
			t.Fatalf("duplicate timeline post returned: %s", item.Post.ID)
		}
		seen[item.Post.ID] = true
	}
}

func TestGetFeed_TimelineFiltersStaleVisibilityAndFollowState(t *testing.T) {
	now := time.Now().UTC()
	userID := uuid.New()
	followedAuthor := uuid.New()
	unfollowedAuthor := uuid.New()
	visible := testPost(now)
	visible.AuthorID = followedAuthor
	visible.Visibility = "public"
	private := testPost(now.Add(-time.Minute))
	private.AuthorID = followedAuthor
	private.Visibility = "private"
	unfollowed := testPost(now.Add(-2 * time.Minute))
	unfollowed.AuthorID = unfollowedAuthor
	unfollowed.Visibility = "public"

	reader := &mockPostReader{
		byID: map[uuid.UUID]*feedentity.Post{
			visible.ID:    visible,
			private.ID:    private,
			unfollowed.ID: unfollowed,
		},
	}
	store := &mockTimelineStore{pages: []*feed.TimelinePage{{Entries: []feed.TimelineEntry{
		{PostID: visible.ID, Score: feed.PackTimelineScore(30, visible.CreatedAt)},
		{PostID: private.ID, Score: feed.PackTimelineScore(30, private.CreatedAt)},
		{PostID: unfollowed.ID, Score: feed.PackTimelineScore(30, unfollowed.CreatedAt)},
	}}}}

	svc := NewFeedService(
		reader,
		&mockFollowReader{ids: []uuid.UUID{followedAuthor}},
		&mockLikeReader{},
		&mockRanker{scores: map[uuid.UUID]float64{}},
		feedcache.NewNopFeedCache(),
	)
	svc.WithTimelineStore(store)
	page, _, err := svc.GetFeed(context.Background(), userID, nil)
	if err != nil {
		t.Fatalf("GetFeed: %v", err)
	}
	if len(page) != 1 || page[0].Post.ID != visible.ID {
		t.Fatalf("expected only visible followed post, got %+v", page)
	}
}

func TestGetFeed_TimelineRefreshesOnMiss(t *testing.T) {
	now := time.Now().UTC()
	userID := uuid.New()
	post := testPost(now)
	post.AuthorID = userID
	reader := &mockPostReader{byID: map[uuid.UUID]*feedentity.Post{post.ID: post}}
	store := &mockTimelineStore{pages: []*feed.TimelinePage{
		{},
		{Entries: []feed.TimelineEntry{{PostID: post.ID, Score: feed.PackTimelineScore(30, post.CreatedAt)}}},
	}}
	refresher := &mockTimelineRefresher{}

	svc := newTestService(reader, &mockRanker{scores: map[uuid.UUID]float64{}})
	svc.WithTimelineStore(store)
	svc.WithTimelineRefresher(refresher)
	page, _, err := svc.GetFeed(context.Background(), userID, nil)
	if err != nil {
		t.Fatalf("GetFeed: %v", err)
	}
	if refresher.calls != 1 {
		t.Fatalf("refresh calls = %d, want 1", refresher.calls)
	}
	if len(page) != 1 || page[0].Post.ID != post.ID {
		t.Fatalf("expected refreshed timeline item, got %+v", page)
	}
}

func TestGetFeed_TimelineRolloutGateDisablesPreparedTimelineRead(t *testing.T) {
	now := time.Now().UTC()
	userID := uuid.New()
	timelinePost := testPost(now)
	timelinePost.AuthorID = userID
	fallbackPost := testPost(now.Add(-time.Minute))
	reader := &mockPostReader{
		discover: []*feedentity.Post{fallbackPost},
		byID: map[uuid.UUID]*feedentity.Post{
			timelinePost.ID: timelinePost,
			fallbackPost.ID: fallbackPost,
		},
	}
	store := &mockTimelineStore{pages: []*feed.TimelinePage{{Entries: []feed.TimelineEntry{
		{PostID: timelinePost.ID, Score: feed.PackTimelineScore(30, timelinePost.CreatedAt)},
	}}}}

	svc := newTestService(reader, &mockRanker{scores: map[uuid.UUID]float64{fallbackPost.ID: 1}})
	svc.WithTimelineStore(store)
	svc.WithSettings(timelineSettings(false, 100, true))

	page, _, err := svc.GetFeed(context.Background(), userID, nil)
	if err != nil {
		t.Fatalf("GetFeed: %v", err)
	}
	if store.readCount != 0 {
		t.Fatalf("timeline read count = %d, want 0 when serving disabled", store.readCount)
	}
	if len(page) != 1 || page[0].Post.ID != fallbackPost.ID {
		t.Fatalf("expected fallback item, got %+v", page)
	}
}

func TestGetFeed_TimelineRefreshOnMissGate(t *testing.T) {
	userID := uuid.New()
	reader := &mockPostReader{byID: map[uuid.UUID]*feedentity.Post{}}
	store := &mockTimelineStore{pages: []*feed.TimelinePage{{}}}
	refresher := &mockTimelineRefresher{}

	svc := newTestService(reader, &mockRanker{scores: map[uuid.UUID]float64{}})
	svc.WithTimelineStore(store)
	svc.WithTimelineRefresher(refresher)
	svc.WithSettings(timelineSettings(true, 100, false))

	if _, _, err := svc.GetFeed(context.Background(), userID, nil); err != nil {
		t.Fatalf("GetFeed: %v", err)
	}
	if refresher.calls != 0 {
		t.Fatalf("refresh calls = %d, want 0 when refresh-on-miss disabled", refresher.calls)
	}
}

func TestInRolloutDeterministicBoundaries(t *testing.T) {
	userID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	if inRollout(userID, 0) {
		t.Fatal("0 percent rollout should exclude user")
	}
	if !inRollout(userID, 100) {
		t.Fatal("100 percent rollout should include user")
	}
	first := inRollout(userID, 50)
	second := inRollout(userID, 50)
	if first != second {
		t.Fatal("rollout eligibility should be deterministic")
	}
	if inRollout(userID, -1) {
		t.Fatal("negative rollout should clamp to 0")
	}
	if !inRollout(userID, 101) {
		t.Fatal("rollout over 100 should clamp to 100")
	}
}

func TestGetFeed_RecommendationScoreAndRankAffectOrdering(t *testing.T) {
	now := time.Now().UTC()
	reader := &mockPostReader{byID: map[uuid.UUID]*feedentity.Post{}}
	lowRank := testPost(now)
	highRank := testPost(now.Add(-time.Minute))
	reader.following = []*feedentity.Post{testPost(now.Add(-2 * time.Minute))}
	reader.byID[lowRank.ID] = lowRank
	reader.byID[highRank.ID] = highRank
	reader.byID[reader.following[0].ID] = reader.following[0]

	svc := newTestService(reader, &mockRanker{scores: map[uuid.UUID]float64{}})
	svc.WithRecommender(&mockRecommender{items: []feed.RecommendedItem{
		{ObjectID: lowRank.ID.String(), Score: 0.1, Rank: 2},
		{ObjectID: highRank.ID.String(), Score: 0.9, Rank: 1},
	}})

	page, _, err := svc.GetFeed(context.Background(), uuid.New(), nil)
	if err != nil {
		t.Fatalf("GetFeed: %v", err)
	}
	if len(page) < 2 {
		t.Fatalf("expected at least two items, got %d", len(page))
	}
	if page[0].Post.ID != highRank.ID {
		t.Fatalf("expected higher recommendation score first, got %s", page[0].Post.ID)
	}
	if page[0].RecommendationScore == nil || *page[0].RecommendationScore != 0.9 {
		t.Fatalf("recommendation score not preserved: %+v", page[0].RecommendationScore)
	}
}

func TestGetFeed_SupplementalMergeCollapsesDuplicatesFiltersVisibilityAndBounds(t *testing.T) {
	now := time.Now().UTC()
	userID := uuid.New()
	followedAuthor := uuid.New()
	reader := &mockPostReader{byID: map[uuid.UUID]*feedentity.Post{}}

	duplicate := testPost(now)
	duplicate.AuthorID = followedAuthor
	privateRecommendation := testPost(now.Add(-time.Minute))
	privateRecommendation.Visibility = "private"
	reader.following = []*feedentity.Post{duplicate}
	reader.byID[duplicate.ID] = duplicate
	reader.byID[privateRecommendation.ID] = privateRecommendation

	for i := 0; i < pageSize+5; i++ {
		p := testPost(now.Add(-time.Duration(i+2) * time.Minute))
		if i == 0 {
			p = duplicate
		}
		reader.trending = append(reader.trending, p)
		reader.byID[p.ID] = p
	}

	svc := NewFeedService(
		reader,
		&mockFollowReader{ids: []uuid.UUID{followedAuthor}},
		&mockLikeReader{},
		&mockRanker{scores: map[uuid.UUID]float64{}},
		feedcache.NewNopFeedCache(),
	)
	svc.WithRecommender(&mockRecommender{items: []feed.RecommendedItem{
		{ObjectID: duplicate.ID.String(), Score: 1, Rank: 1},
		{ObjectID: privateRecommendation.ID.String(), Score: 1, Rank: 2},
	}})

	page, _, err := svc.GetFeed(context.Background(), userID, nil)
	if err != nil {
		t.Fatalf("GetFeed: %v", err)
	}
	if len(page) > pageSize {
		t.Fatalf("page len = %d, want <= %d", len(page), pageSize)
	}
	seen := make(map[uuid.UUID]bool, len(page))
	for _, item := range page {
		if seen[item.Post.ID] {
			t.Fatalf("duplicate post returned: %s", item.Post.ID)
		}
		seen[item.Post.ID] = true
		if item.Post.ID == privateRecommendation.ID {
			t.Fatal("private recommendation was returned")
		}
	}
	if !seen[duplicate.ID] {
		t.Fatal("expected duplicate candidate to survive as one feed item")
	}
}

func TestGetFeed_SupplementalProviderFailuresReturnValidFeed(t *testing.T) {
	now := time.Now().UTC()
	userID := uuid.New()
	followedAuthor := uuid.New()
	local := testPost(now)
	local.AuthorID = followedAuthor
	reader := &mockPostReader{
		following:   []*feedentity.Post{local},
		byID:        map[uuid.UUID]*feedentity.Post{local.ID: local},
		trendingErr: errors.New("trending down"),
		byIDErr:     nil,
		trending:    []*feedentity.Post{testPost(now.Add(-time.Minute))},
	}

	svc := NewFeedService(
		reader,
		&mockFollowReader{ids: []uuid.UUID{followedAuthor}},
		&mockLikeReader{},
		&mockRanker{scores: map[uuid.UUID]float64{local.ID: 10}},
		feedcache.NewNopFeedCache(),
	)
	svc.WithRecommender(&mockRecommender{err: errors.New("recommendations down")})

	page, cursor, err := svc.GetFeed(context.Background(), userID, nil)
	if err != nil {
		t.Fatalf("GetFeed: %v", err)
	}
	if len(page) != 1 || page[0].Post.ID != local.ID {
		t.Fatalf("expected local feed item despite supplemental failures, got %+v", page)
	}
	// The page served a following post, so the cursor must carry the following
	// continuation — provider failures do not end the scroll.
	if cursor == nil || cursor.FollowingCreatedAt == nil || cursor.FollowingPostID != local.ID.String() {
		t.Fatalf("cursor = %+v, want following continuation at the served post", cursor)
	}

	// The continuation drains cleanly: no more following posts and no discover
	// content behind them means an empty final page with no cursor.
	page2, next, err := svc.GetFeed(context.Background(), userID, cursor)
	if err != nil {
		t.Fatalf("GetFeed page2: %v", err)
	}
	if len(page2) != 0 || next != nil {
		t.Fatalf("page2 len/cursor = %d/%+v, want exhausted scroll", len(page2), next)
	}
}

func TestGetFeed_DiscoverFallbackPaginatesWithoutDuplicates(t *testing.T) {
	now := time.Now().UTC()
	userID := uuid.New()
	reader := &mockPostReader{byID: map[uuid.UUID]*feedentity.Post{}}
	posts := make([]*feedentity.Post, 0, pageSize+2)
	for i := 0; i < pageSize+2; i++ {
		p := testPost(now.Add(-time.Duration(i) * time.Minute))
		posts = append(posts, p)
		reader.byID[p.ID] = p
	}
	reader.discover = posts

	svc := newTestService(reader, &mockRanker{scores: map[uuid.UUID]float64{}})
	page1, cursor, err := svc.GetFeed(context.Background(), userID, nil)
	if err != nil {
		t.Fatalf("GetFeed page1: %v", err)
	}
	if len(page1) != pageSize || cursor == nil || cursor.DiscoverCreatedAt == nil {
		t.Fatalf("page1 len/cursor = %d/%+v, want full page with discover continuation", len(page1), cursor)
	}

	page2, next, err := svc.GetFeed(context.Background(), userID, cursor)
	if err != nil {
		t.Fatalf("GetFeed page2: %v", err)
	}
	if len(page2) != 2 {
		t.Fatalf("page2 len = %d, want 2", len(page2))
	}
	if next != nil {
		t.Fatalf("next cursor = %+v, want exhausted discover", next)
	}
	seen := make(map[uuid.UUID]bool)
	for _, item := range append(page1, page2...) {
		if item.Source != feedentity.SourceDiscover {
			t.Fatalf("source = %s, want discover", item.Source)
		}
		if seen[item.Post.ID] {
			t.Fatalf("duplicate discover post returned: %s", item.Post.ID)
		}
		seen[item.Post.ID] = true
	}
	if len(seen) != pageSize+2 {
		t.Fatalf("total unique posts = %d, want %d", len(seen), pageSize+2)
	}
}

func TestGetFeed_EmptyMixedCandidatesEnterDiscoverFallback(t *testing.T) {
	now := time.Now().UTC()
	fallback := testPost(now)
	reader := &mockPostReader{
		discover: []*feedentity.Post{fallback},
		byID:     map[uuid.UUID]*feedentity.Post{fallback.ID: fallback},
	}

	svc := newTestService(reader, &mockRanker{scores: map[uuid.UUID]float64{fallback.ID: 7}})
	page, cursor, err := svc.GetFeed(context.Background(), uuid.New(), nil)
	if err != nil {
		t.Fatalf("GetFeed: %v", err)
	}
	if cursor != nil {
		t.Fatalf("cursor = %+v, want nil for exhausted fallback", cursor)
	}
	if len(page) != 1 || page[0].Post.ID != fallback.ID || page[0].Source != feedentity.SourceDiscover {
		t.Fatalf("expected discover fallback item, got %+v", page)
	}
}

func TestGetFeed_V2CursorDoesNotRequireSessionState(t *testing.T) {
	now := time.Now().UTC()
	userID := uuid.New()
	followedAuthor := uuid.New()
	local := testPost(now)
	local.AuthorID = followedAuthor
	reader := &mockPostReader{
		following: []*feedentity.Post{local},
		byID:      map[uuid.UUID]*feedentity.Post{local.ID: local},
	}

	svc := NewFeedService(
		reader,
		&mockFollowReader{ids: []uuid.UUID{followedAuthor}},
		&mockLikeReader{},
		&mockRanker{scores: map[uuid.UUID]float64{local.ID: 10}},
		feedcache.NewNopFeedCache(),
	)
	page, _, err := svc.GetFeed(context.Background(), userID, &feed.FeedCursor{TimelineUser: userID.String()})
	if err != nil {
		t.Fatalf("GetFeed with no-version cursor: %v", err)
	}
	if len(page) != 1 || page[0].Post.ID != local.ID {
		t.Fatalf("expected local item with no-version cursor, got %+v", page)
	}
}

func TestGetFeed_InvalidCursorReturnsError(t *testing.T) {
	reader := &mockPostReader{byID: map[uuid.UUID]*feedentity.Post{}}
	svc := newTestService(reader, &mockRanker{scores: map[uuid.UUID]float64{}})

	if _, _, err := svc.GetFeed(context.Background(), uuid.New(), &feed.FeedCursor{RecommendationOffset: -1}); err == nil {
		t.Fatal("expected invalid cursor error")
	}
}

func TestGetFeed_RecommendationOffsetContinuation(t *testing.T) {
	now := time.Now().UTC()
	reader := &mockPostReader{byID: map[uuid.UUID]*feedentity.Post{}}
	recs := make([]feed.RecommendedItem, 0, 25)
	for i := 0; i < 25; i++ {
		p := testPost(now.Add(-time.Duration(i) * time.Minute))
		reader.byID[p.ID] = p
		recs = append(recs, feed.RecommendedItem{ObjectID: p.ID.String(), Score: 1, Rank: i + 1})
	}

	svc := newTestService(reader, &mockRanker{scores: map[uuid.UUID]float64{}})
	svc.WithRecommender(&mockRecommender{items: recs})
	userID := uuid.New()

	page1, cursor, err := svc.GetFeed(context.Background(), userID, nil)
	if err != nil {
		t.Fatalf("GetFeed page1: %v", err)
	}
	if len(page1) != pageSize || cursor == nil {
		t.Fatalf("page1 len/cursor = %d/%v", len(page1), cursor)
	}
	if cursor.RecommendationOffset != pageSize {
		t.Fatalf("recommendation offset = %d, want %d", cursor.RecommendationOffset, pageSize)
	}
	if cursor.TimelineUser == "" {
		t.Fatalf("timeline user missing from cursor: %+v", cursor)
	}

	page2, _, err := svc.GetFeed(context.Background(), userID, cursor)
	if err != nil {
		t.Fatalf("GetFeed page2: %v", err)
	}
	if len(page2) != 5 {
		t.Fatalf("page2 len = %d, want 5", len(page2))
	}
}

func TestGetFeed_TrendingContinuationNoDuplicates(t *testing.T) {
	now := time.Now().UTC()
	userID := uuid.New()
	reader := &mockPostReader{byID: map[uuid.UUID]*feedentity.Post{}}
	scores := map[uuid.UUID]float64{}
	for i := 0; i < pageSize+5; i++ {
		p := testPost(now.Add(-time.Duration(i) * time.Minute))
		p.LikeCount = int64(pageSize + 5 - i)
		reader.trending = append(reader.trending, p)
		reader.byID[p.ID] = p
		scores[p.ID] = float64(p.LikeCount)
	}

	svc := newTestService(reader, &mockRanker{scores: scores})
	page1, cursor, err := svc.GetFeed(context.Background(), userID, nil)
	if err != nil {
		t.Fatalf("GetFeed page1: %v", err)
	}
	if len(page1) != pageSize || cursor == nil || cursor.TrendingScore == nil || cursor.TrendingPostID == "" {
		t.Fatalf("page1 len/cursor = %d/%+v, want trending continuation", len(page1), cursor)
	}
	page2, next, err := svc.GetFeed(context.Background(), userID, cursor)
	if err != nil {
		t.Fatalf("GetFeed page2: %v", err)
	}
	if len(page2) != 5 {
		t.Fatalf("page2 len = %d, want 5", len(page2))
	}
	seen := make(map[uuid.UUID]bool)
	for _, item := range append(page1, page2...) {
		if seen[item.Post.ID] {
			t.Fatalf("duplicate trending post returned: %s", item.Post.ID)
		}
		seen[item.Post.ID] = true
	}

	// Page 2 still served trending, so its position survives; the next request
	// finds nothing behind it and ends the scroll cleanly.
	if next == nil || next.TrendingScore == nil {
		t.Fatalf("next cursor = %+v, want carried trending continuation", next)
	}
	page3, tail, err := svc.GetFeed(context.Background(), userID, next)
	if err != nil {
		t.Fatalf("GetFeed page3: %v", err)
	}
	if len(page3) != 0 || tail != nil {
		t.Fatalf("page3 len/cursor = %d/%+v, want exhausted scroll", len(page3), tail)
	}
}

// TestGetFeed_TrendingCursorUsesLowestShownScore pins the trending boundary to
// the lowest trend score served, not the last trending item in blend order.
// With the old boundary, B (5 likes) sat above A and below C in blend order,
// the cursor took C's 7 likes, and B reappeared on page 2.
func TestGetFeed_TrendingCursorUsesLowestShownScore(t *testing.T) {
	now := time.Now().UTC()
	userID := uuid.New()
	reader := &mockPostReader{byID: map[uuid.UUID]*feedentity.Post{}}
	scores := map[uuid.UUID]float64{}

	likes := []int64{10, 5, 7}
	blend := []float64{100, 50, 10}
	posts := make([]*feedentity.Post, 3)
	for i := range posts {
		p := testPost(now.Add(-time.Duration(i) * time.Minute))
		p.LikeCount = likes[i]
		posts[i] = p
		reader.trending = append(reader.trending, p)
		reader.byID[p.ID] = p
		scores[p.ID] = blend[i]
	}

	svc := newTestService(reader, &mockRanker{scores: scores})
	page1, cursor, err := svc.GetFeed(context.Background(), userID, nil)
	if err != nil {
		t.Fatalf("GetFeed page1: %v", err)
	}
	if len(page1) != 3 {
		t.Fatalf("page1 len = %d, want 3", len(page1))
	}
	if cursor == nil || cursor.TrendingScore == nil || *cursor.TrendingScore != 5 {
		t.Fatalf("cursor = %+v, want trending boundary at the lowest shown score 5", cursor)
	}

	page2, _, err := svc.GetFeed(context.Background(), userID, cursor)
	if err != nil {
		t.Fatalf("GetFeed page2: %v", err)
	}
	for _, item := range page2 {
		for _, p := range posts {
			if item.Post.ID == p.ID {
				t.Fatalf("trending post re-served on page 2: %s", p.ID)
			}
		}
	}
}

// TestGetFeed_TrendingSurvivesPageWithNoTrendingShown pins the start-of-list
// sentinel: when every trending candidate is outranked off page 1, the cursor
// must still carry a trending position, or the source is dropped for the rest
// of the scroll.
func TestGetFeed_TrendingSurvivesPageWithNoTrendingShown(t *testing.T) {
	now := time.Now().UTC()
	userID := uuid.New()
	followedAuthor := uuid.New()
	reader := &mockPostReader{byID: map[uuid.UUID]*feedentity.Post{}}
	scores := map[uuid.UUID]float64{}

	total := pageSize + 10
	for i := 0; i < total; i++ {
		p := testPost(now.Add(-time.Duration(i) * time.Minute))
		p.AuthorID = followedAuthor
		reader.following = append(reader.following, p)
		reader.byID[p.ID] = p
		scores[p.ID] = float64(1000 - i)
	}
	trendingIDs := make(map[uuid.UUID]bool, 3)
	for i := 0; i < 3; i++ {
		p := testPost(now.Add(-time.Duration(i+200) * time.Minute))
		p.LikeCount = int64(i + 1)
		reader.trending = append(reader.trending, p)
		reader.byID[p.ID] = p
		trendingIDs[p.ID] = true
		scores[p.ID] = float64(i + 1)
	}

	svc := NewFeedService(
		reader,
		&mockFollowReader{ids: []uuid.UUID{followedAuthor}},
		&mockLikeReader{},
		&mockRanker{scores: scores},
		feedcache.NewNopFeedCache(),
	)

	page1, cursor, err := svc.GetFeed(context.Background(), userID, nil)
	if err != nil {
		t.Fatalf("GetFeed page1: %v", err)
	}
	for _, item := range page1 {
		if trendingIDs[item.Post.ID] {
			t.Fatalf("fixture broken: trending post made page 1")
		}
	}
	if cursor == nil || cursor.TrendingScore == nil {
		t.Fatalf("cursor = %+v, want sentinel trending position despite none shown", cursor)
	}

	page2, _, err := svc.GetFeed(context.Background(), userID, cursor)
	if err != nil {
		t.Fatalf("GetFeed page2: %v", err)
	}
	served := 0
	for _, item := range page2 {
		if trendingIDs[item.Post.ID] {
			served++
		}
	}
	if served != len(trendingIDs) {
		t.Fatalf("trending posts on page 2 = %d, want %d", served, len(trendingIDs))
	}
}

func TestGetFeed_FollowingContinuationNoDuplicates(t *testing.T) {
	now := time.Now().UTC()
	userID := uuid.New()
	followedAuthor := uuid.New()
	reader := &mockPostReader{byID: map[uuid.UUID]*feedentity.Post{}}
	scores := map[uuid.UUID]float64{}
	total := pageSize + 10
	for i := 0; i < total; i++ {
		p := testPost(now.Add(-time.Duration(i) * time.Minute))
		p.AuthorID = followedAuthor
		reader.following = append(reader.following, p)
		reader.byID[p.ID] = p
		// Newest scores highest so the served order matches DB order and the
		// page boundary is deterministic.
		scores[p.ID] = float64(total - i)
	}

	svc := NewFeedService(
		reader,
		&mockFollowReader{ids: []uuid.UUID{followedAuthor}},
		&mockLikeReader{},
		&mockRanker{scores: scores},
		feedcache.NewNopFeedCache(),
	)

	page1, cursor, err := svc.GetFeed(context.Background(), userID, nil)
	if err != nil {
		t.Fatalf("GetFeed page1: %v", err)
	}
	if len(page1) != pageSize || cursor == nil || cursor.FollowingCreatedAt == nil {
		t.Fatalf("page1 len/cursor = %d/%+v, want full page with following continuation", len(page1), cursor)
	}

	page2, next, err := svc.GetFeed(context.Background(), userID, cursor)
	if err != nil {
		t.Fatalf("GetFeed page2: %v", err)
	}
	if len(page2) != total-pageSize {
		t.Fatalf("page2 len = %d, want %d", len(page2), total-pageSize)
	}
	seen := make(map[uuid.UUID]bool)
	for _, item := range append(page1, page2...) {
		if seen[item.Post.ID] {
			t.Fatalf("duplicate following post returned: %s", item.Post.ID)
		}
		seen[item.Post.ID] = true
	}
	if len(seen) != total {
		t.Fatalf("total unique posts = %d, want %d", len(seen), total)
	}

	// Page 2 still served following posts, so the continuation survives; the
	// next request finds nothing behind it and ends the scroll cleanly.
	if next == nil || next.FollowingCreatedAt == nil {
		t.Fatalf("next cursor = %+v, want carried following continuation", next)
	}
	page3, tail, err := svc.GetFeed(context.Background(), userID, next)
	if err != nil {
		t.Fatalf("GetFeed page3: %v", err)
	}
	if len(page3) != 0 || tail != nil {
		t.Fatalf("page3 len/cursor = %d/%+v, want exhausted scroll", len(page3), tail)
	}
}

func TestGetFeed_FollowingExhaustionHandsOffToDiscoverBelowLastServed(t *testing.T) {
	now := time.Now().UTC()
	userID := uuid.New()
	followedAuthor := uuid.New()
	reader := &mockPostReader{byID: map[uuid.UUID]*feedentity.Post{}}
	scores := map[uuid.UUID]float64{}

	followingPosts := make([]*feedentity.Post, 0, 5)
	for i := 0; i < 5; i++ {
		p := testPost(now.Add(-time.Duration(i+1) * time.Minute))
		p.AuthorID = followedAuthor
		followingPosts = append(followingPosts, p)
		reader.following = append(reader.following, p)
		reader.byID[p.ID] = p
		scores[p.ID] = float64(10 - i)
	}
	// The discover stream contains the same following posts (public posts show
	// up there too) plus strictly older public posts.
	reader.discover = append(reader.discover, followingPosts...)
	olderPublic := make([]*feedentity.Post, 0, 10)
	for i := 0; i < 10; i++ {
		p := testPost(now.Add(-time.Duration(i+60) * time.Minute))
		olderPublic = append(olderPublic, p)
		reader.discover = append(reader.discover, p)
		reader.byID[p.ID] = p
	}

	svc := NewFeedService(
		reader,
		&mockFollowReader{ids: []uuid.UUID{followedAuthor}},
		&mockLikeReader{},
		&mockRanker{scores: scores},
		feedcache.NewNopFeedCache(),
	)

	page1, cursor, err := svc.GetFeed(context.Background(), userID, nil)
	if err != nil {
		t.Fatalf("GetFeed page1: %v", err)
	}
	if len(page1) != 5 || cursor == nil || cursor.FollowingCreatedAt == nil {
		t.Fatalf("page1 len/cursor = %d/%+v, want following page with continuation", len(page1), cursor)
	}

	// Following is exhausted; the feed hands off to discover below the last
	// served post, so page 1's posts are not re-served from the top.
	page2, next, err := svc.GetFeed(context.Background(), userID, cursor)
	if err != nil {
		t.Fatalf("GetFeed page2: %v", err)
	}
	if len(page2) != len(olderPublic) {
		t.Fatalf("page2 len = %d, want %d", len(page2), len(olderPublic))
	}
	page1IDs := make(map[uuid.UUID]bool, len(page1))
	for _, item := range page1 {
		page1IDs[item.Post.ID] = true
	}
	for _, item := range page2 {
		if item.Source != feedentity.SourceDiscover {
			t.Fatalf("page2 source = %s, want discover", item.Source)
		}
		if page1IDs[item.Post.ID] {
			t.Fatalf("post re-served after discover hand-off: %s", item.Post.ID)
		}
	}
	if next != nil {
		t.Fatalf("next cursor = %+v, want exhausted discover", next)
	}
}

// emptyTimelineStore always reads back an empty page. Stateless on purpose:
// the concurrency tests hit it from many goroutines.
type emptyTimelineStore struct{}

func (emptyTimelineStore) AddPost(_ context.Context, _ uuid.UUID, _ feed.TimelineEntry) error {
	return nil
}
func (emptyTimelineStore) SetPostsBatch(_ context.Context, _ uuid.UUID, _ []feed.TimelineEntry) error {
	return nil
}
func (emptyTimelineStore) ReadPage(_ context.Context, _ uuid.UUID, _ *feed.TimelinePosition, _ int) (*feed.TimelinePage, error) {
	return &feed.TimelinePage{}, nil
}
func (emptyTimelineStore) Trim(_ context.Context, _ uuid.UUID) error { return nil }
func (emptyTimelineStore) RemovePostBestEffort(_ context.Context, _ uuid.UUID, _ uuid.UUID) error {
	return nil
}

// gatedRefresher counts RefreshTimeline calls atomically and blocks each one
// until the gate closes.
type gatedRefresher struct {
	calls atomic.Int32
	gate  chan struct{}
}

func (r *gatedRefresher) RefreshTimeline(_ context.Context, _ uuid.UUID) error {
	r.calls.Add(1)
	if r.gate != nil {
		<-r.gate
	}
	return nil
}

// waitUntil polls cond until it holds or the deadline passes.
func waitUntil(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not reached within deadline")
		}
		time.Sleep(time.Millisecond)
	}
}

// TestGetFeed_ConcurrentTrendingMissesCollapseToOneRebuild pins the
// single-flight around the trending source: the cache key is global with one
// hard TTL, so its expiry is a synchronized miss across every in-flight
// page-1 request — without collapsing, each would run the provider fetch and
// the DB aggregate.
func TestGetFeed_ConcurrentTrendingMissesCollapseToOneRebuild(t *testing.T) {
	now := time.Now().UTC()
	reader := &mockPostReader{byID: map[uuid.UUID]*feedentity.Post{}, trendingGate: make(chan struct{})}
	for i := 0; i < 3; i++ {
		p := testPost(now.Add(-time.Duration(i) * time.Minute))
		reader.trending = append(reader.trending, p)
		reader.byID[p.ID] = p
	}
	svc := newTestService(reader, &mockRanker{scores: map[uuid.UUID]float64{}})

	const callers = 8
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, _ = svc.GetFeed(context.Background(), uuid.New(), nil)
		}()
	}
	// Let the leader enter the fetch, give the others time to queue up behind
	// the flight, then release everyone at once.
	waitUntil(t, func() bool { return reader.trendingCalls.Load() >= 1 })
	time.Sleep(50 * time.Millisecond)
	close(reader.trendingGate)
	wg.Wait()

	if got := reader.trendingCalls.Load(); got != 1 {
		t.Fatalf("trending fetches = %d, want 1 shared rebuild", got)
	}
}

// TestGetFeed_ConcurrentTimelineMissesCollapseToOneRefresh pins the per-user
// single-flight around refresh-on-miss: a rebuild reads the whole following
// set and re-ranks up to timeline_max_items posts, so parallel requests from
// one cold user must share one rebuild instead of running one each.
func TestGetFeed_ConcurrentTimelineMissesCollapseToOneRefresh(t *testing.T) {
	userID := uuid.New()
	reader := &mockPostReader{byID: map[uuid.UUID]*feedentity.Post{}}
	svc := newTestService(reader, &mockRanker{scores: map[uuid.UUID]float64{}})
	svc.WithTimelineStore(emptyTimelineStore{})
	refresher := &gatedRefresher{gate: make(chan struct{})}
	svc.WithTimelineRefresher(refresher)
	svc.WithSettings(timelineSettings(true, 100, true))

	const callers = 8
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, _ = svc.GetFeed(context.Background(), userID, nil)
		}()
	}
	waitUntil(t, func() bool { return refresher.calls.Load() >= 1 })
	time.Sleep(50 * time.Millisecond)
	close(refresher.gate)
	wg.Wait()

	if got := refresher.calls.Load(); got != 1 {
		t.Fatalf("timeline refreshes = %d, want 1 shared rebuild", got)
	}
}

func TestGetFeed_InvalidCursorRejectedBeforeSourceReads(t *testing.T) {
	reader := &mockPostReader{byID: map[uuid.UUID]*feedentity.Post{}}
	store := &mockTimelineStore{pages: []*feed.TimelinePage{{}}}
	svc := newTestService(reader, &mockRanker{scores: map[uuid.UUID]float64{}})
	svc.WithTimelineStore(store)

	_, _, err := svc.GetFeed(context.Background(), uuid.New(), &feed.FeedCursor{TimelineUser: uuid.NewString()})
	if err == nil {
		t.Fatal("expected invalid cursor error")
	}
	if store.readCount != 0 {
		t.Fatalf("timeline read count = %d, want 0", store.readCount)
	}
}

func TestGetFeed_InvalidProviderItemsAreFiltered(t *testing.T) {
	now := time.Now().UTC()
	reader := &mockPostReader{byID: map[uuid.UUID]*feedentity.Post{}}
	valid := testPost(now)
	reader.byID[valid.ID] = valid

	svc := newTestService(reader, &mockRanker{scores: map[uuid.UUID]float64{}})
	svc.WithRecommender(&mockRecommender{items: []feed.RecommendedItem{
		{ObjectID: "not-a-uuid", Score: 1, Rank: 1},
		{ObjectID: uuid.New().String(), Score: 1, Rank: 2},
		{ObjectID: valid.ID.String(), Score: 1, Rank: 3},
	}})

	page, _, err := svc.GetFeed(context.Background(), uuid.New(), nil)
	if err != nil {
		t.Fatalf("GetFeed: %v", err)
	}
	if len(page) != 1 || page[0].Post.ID != valid.ID {
		t.Fatalf("expected only valid resolved provider item, got %+v", page)
	}
}

// TestGetFeed_OwnPostsNotServedAsRecommendations pins the app-side authored
// exclusion: even if the provider returns the viewer's own post, it must not
// reach them labeled "recommended".
func TestGetFeed_OwnPostsNotServedAsRecommendations(t *testing.T) {
	now := time.Now().UTC()
	userID := uuid.New()
	own := testPost(now)
	own.AuthorID = userID
	other := testPost(now.Add(-time.Minute))
	reader := &mockPostReader{byID: map[uuid.UUID]*feedentity.Post{own.ID: own, other.ID: other}}

	svc := newTestService(reader, &mockRanker{scores: map[uuid.UUID]float64{}})
	svc.WithRecommender(&mockRecommender{items: []feed.RecommendedItem{
		{ObjectID: own.ID.String(), Score: 1, Rank: 1},
		{ObjectID: other.ID.String(), Score: 1, Rank: 2},
	}})

	page, _, err := svc.GetFeed(context.Background(), userID, nil)
	if err != nil {
		t.Fatalf("GetFeed: %v", err)
	}
	if len(page) != 1 || page[0].Post.ID != other.ID {
		t.Fatalf("page = %+v, want only the other author's recommendation", page)
	}
}

func TestGetFeed_CodohueUnavailableFallsBackLocal(t *testing.T) {
	now := time.Now().UTC()
	userID := uuid.New()
	followedAuthor := uuid.New()
	local := testPost(now)
	local.AuthorID = followedAuthor
	reader := &mockPostReader{
		following: []*feedentity.Post{local},
		byID:      map[uuid.UUID]*feedentity.Post{local.ID: local},
	}

	svc := NewFeedService(
		reader,
		&mockFollowReader{ids: []uuid.UUID{followedAuthor}},
		&mockLikeReader{},
		&mockRanker{scores: map[uuid.UUID]float64{local.ID: 10}},
		feedcache.NewNopFeedCache(),
	)
	svc.WithRecommender(&mockRecommender{err: errors.New("codohue down")})

	page, _, err := svc.GetFeed(context.Background(), userID, nil)
	if err != nil {
		t.Fatalf("GetFeed: %v", err)
	}
	if len(page) != 1 || page[0].Post.ID != local.ID {
		t.Fatalf("expected local fallback item, got %+v", page)
	}
}

func TestGetDiscover_PreservesCursorOrderWithTimestampTies(t *testing.T) {
	now := time.Now().UTC()
	first := testPost(now)
	second := testPost(now)
	if first.ID.String() < second.ID.String() {
		first, second = second, first
	}
	reader := &mockPostReader{
		discover: []*feedentity.Post{first, second},
		byID: map[uuid.UUID]*feedentity.Post{
			first.ID:  first,
			second.ID: second,
		},
	}
	svc := newTestService(reader, &mockRanker{scores: map[uuid.UUID]float64{}})

	page, next, err := svc.GetDiscover(context.Background(), nil, nil, 1)
	if err != nil {
		t.Fatalf("GetDiscover page1: %v", err)
	}
	if len(page) != 1 || page[0].ID != first.ID || next == nil {
		t.Fatalf("page1 mismatch: page=%+v next=%+v", page, next)
	}
	page, _, err = svc.GetDiscover(context.Background(), nil, next, 1)
	if err != nil {
		t.Fatalf("GetDiscover page2: %v", err)
	}
	if len(page) != 1 || page[0].ID != second.ID {
		t.Fatalf("page2 mismatch: %+v", page)
	}
}

func TestGetDiscover_AuthenticatedEnrichmentDoesNotChangeMembership(t *testing.T) {
	now := time.Now().UTC()
	post := testPost(now)
	reader := &mockPostReader{
		discover: []*feedentity.Post{post},
		byID:     map[uuid.UUID]*feedentity.Post{post.ID: post},
	}
	svc := newTestService(reader, &mockRanker{scores: map[uuid.UUID]float64{}})
	viewerID := uuid.New()

	anonymous, _, err := svc.GetDiscover(context.Background(), nil, nil, 20)
	if err != nil {
		t.Fatalf("anonymous discover: %v", err)
	}
	authenticated, _, err := svc.GetDiscover(context.Background(), &viewerID, nil, 20)
	if err != nil {
		t.Fatalf("authenticated discover: %v", err)
	}
	if len(anonymous) != 1 || len(authenticated) != 1 || anonymous[0].ID != authenticated[0].ID {
		t.Fatalf("membership changed: anon=%+v auth=%+v", anonymous, authenticated)
	}
}

func testPost(createdAt time.Time) *feedentity.Post {
	return &feedentity.Post{
		ID:         uuid.New(),
		AuthorID:   uuid.New(),
		Content:    "post",
		Visibility: "public",
		CreatedAt:  createdAt,
		UpdatedAt:  createdAt,
	}
}

func takePosts(posts []*feedentity.Post, limit int) []*feedentity.Post {
	if len(posts) > limit {
		return posts[:limit]
	}
	return posts
}

func applyFollowingCursor(posts []*feedentity.Post, cursor *feed.FollowingCursor, limit int) []*feedentity.Post {
	filtered := make([]*feedentity.Post, 0, len(posts))
	for _, p := range posts {
		if cursor != nil && !isAfterCursor(p.CreatedAt, p.ID, cursor.CreatedAt, cursor.PostID) {
			continue
		}
		filtered = append(filtered, p)
	}
	return takePosts(filtered, limit)
}

func applyDiscoverCursor(posts []*feedentity.Post, cursor *feed.DiscoverCursor, limit int) []*feedentity.Post {
	filtered := make([]*feedentity.Post, 0, len(posts))
	for _, p := range posts {
		if cursor != nil && !isAfterCursor(p.CreatedAt, p.ID, cursor.CreatedAt, cursor.PostID) {
			continue
		}
		filtered = append(filtered, p)
	}
	return takePosts(filtered, limit)
}

func isAfterCursor(createdAt time.Time, id uuid.UUID, cursorCreatedAt time.Time, cursorPostID string) bool {
	return createdAt.Before(cursorCreatedAt) || (createdAt.Equal(cursorCreatedAt) && id.String() < cursorPostID)
}
