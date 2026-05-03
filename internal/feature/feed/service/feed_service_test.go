package service

import (
	"context"
	"errors"
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
}

func (m *mockPostReader) GetFollowingPostsWithCursor(_ context.Context, _ []uuid.UUID, cursor *feed.FollowingCursor, limit int32) ([]*feedentity.Post, error) {
	return applyFollowingCursor(m.following, cursor, int(limit)), nil
}

func (m *mockPostReader) GetTrendingPosts(_ context.Context, limit int32) ([]*feedentity.Post, error) {
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
	result := make([]*feedentity.Post, 0, len(ids))
	for _, id := range ids {
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
	return m.AddPostsBatch(context.Background(), userID, []feed.TimelineEntry{entry})
}

func (m *mockTimelineStore) AddPostsBatch(_ context.Context, userID uuid.UUID, entries []feed.TimelineEntry) error {
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
	page1, cursor, err := svc.GetFeed(context.Background(), uuid.New(), nil)
	if err != nil {
		t.Fatalf("GetFeed page1: %v", err)
	}
	if cursor != nil {
		t.Fatalf("cursor = %+v, want nil without v2 continuation state", cursor)
	}
	if len(page1) == 0 {
		t.Fatal("expected mixed fallback items")
	}
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
		entries = append(entries, feed.TimelineEntry{PostID: p.ID, Score: feed.TimelineScoreFromTime(p.CreatedAt)})
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
	if cursor == nil || cursor.Timeline == nil || cursor.Timeline.PostID != page[len(page)-1].Post.ID.String() {
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
		allEntries = append(allEntries, feed.TimelineEntry{PostID: p.ID, Score: feed.TimelineScoreFromTime(p.CreatedAt)})
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
		{PostID: visible.ID, Score: feed.TimelineScoreFromTime(visible.CreatedAt)},
		{PostID: private.ID, Score: feed.TimelineScoreFromTime(private.CreatedAt)},
		{PostID: unfollowed.ID, Score: feed.TimelineScoreFromTime(unfollowed.CreatedAt)},
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
		{Entries: []feed.TimelineEntry{{PostID: post.ID, Score: feed.TimelineScoreFromTime(post.CreatedAt)}}},
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
		{PostID: timelinePost.ID, Score: feed.TimelineScoreFromTime(timelinePost.CreatedAt)},
	}}}}

	svc := newTestService(reader, &mockRanker{scores: map[uuid.UUID]float64{fallbackPost.ID: 1}})
	svc.WithTimelineStore(store)
	svc.WithTimelineOptions(false, 100, true)

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
	svc.WithTimelineOptions(true, 100, false)

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
	if cursor != nil {
		t.Fatalf("cursor = %+v, want nil when supplemental providers fail and local page is exhausted", cursor)
	}
	if len(page) != 1 || page[0].Post.ID != local.ID {
		t.Fatalf("expected local feed item despite supplemental failures, got %+v", page)
	}
}

func TestGetFeed_DiscoverFallbackUsesV2FallbackCursor(t *testing.T) {
	now := time.Now().UTC()
	reader := &mockPostReader{byID: map[uuid.UUID]*feedentity.Post{}}
	posts := make([]*feedentity.Post, 0, pageSize+2)
	for i := 0; i < pageSize+2; i++ {
		p := testPost(now.Add(-time.Duration(i) * time.Minute))
		posts = append(posts, p)
		reader.byID[p.ID] = p
	}
	reader.discover = posts

	svc := newTestService(reader, &mockRanker{scores: map[uuid.UUID]float64{}})
	page1, cursor, err := svc.GetFeed(context.Background(), uuid.New(), &feed.FeedCursor{
		Version:        feed.FeedCursorVersion,
		FallbackCursor: nil,
	})
	if err != nil {
		t.Fatalf("GetFeed page1: %v", err)
	}
	if len(page1) != pageSize || cursor == nil || cursor.FallbackCursor == nil {
		t.Fatalf("page1 len/cursor = %d/%+v, want page and fallback cursor", len(page1), cursor)
	}
	page2, next, err := svc.GetFeed(context.Background(), uuid.New(), cursor)
	if err != nil {
		t.Fatalf("GetFeed page2: %v", err)
	}
	if next != nil {
		t.Fatalf("next cursor = %+v, want exhausted fallback", next)
	}
	if len(page2) != 2 || page2[0].Post.ID != posts[pageSize].ID {
		t.Fatalf("expected fallback page2 continuation, got %+v", page2)
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
	page, _, err := svc.GetFeed(context.Background(), userID, &feed.FeedCursor{
		Version:  feed.FeedCursorVersion,
		IssuedAt: now,
	})
	if err != nil {
		t.Fatalf("GetFeed with v2 cursor: %v", err)
	}
	if len(page) != 1 || page[0].Post.ID != local.ID {
		t.Fatalf("expected local item with v2 cursor, got %+v", page)
	}
}

func TestGetFeed_InvalidV2CursorReturnsError(t *testing.T) {
	reader := &mockPostReader{byID: map[uuid.UUID]*feedentity.Post{}}
	svc := newTestService(reader, &mockRanker{scores: map[uuid.UUID]float64{}})

	if _, _, err := svc.GetFeed(context.Background(), uuid.New(), &feed.FeedCursor{Version: 1}); err == nil {
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

	page1, cursor, err := svc.GetFeed(context.Background(), uuid.New(), nil)
	if err != nil {
		t.Fatalf("GetFeed page1: %v", err)
	}
	if len(page1) != pageSize || cursor == nil {
		t.Fatalf("page1 len/cursor = %d/%v", len(page1), cursor)
	}
	if cursor.RecommendationOffset != pageSize {
		t.Fatalf("recommendation offset = %d, want %d", cursor.RecommendationOffset, pageSize)
	}

	page2, _, err := svc.GetFeed(context.Background(), uuid.New(), cursor)
	if err != nil {
		t.Fatalf("GetFeed page2: %v", err)
	}
	if len(page2) != 5 {
		t.Fatalf("page2 len = %d, want 5", len(page2))
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
