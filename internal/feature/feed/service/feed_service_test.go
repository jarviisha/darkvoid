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
	following []*feedentity.Post
	trending  []*feedentity.Post
	discover  []*feedentity.Post
	byID      map[uuid.UUID]*feedentity.Post
}

func (m *mockPostReader) GetFollowingPostsWithCursor(_ context.Context, _ []uuid.UUID, cursor *feed.FollowingCursor, limit int32) ([]*feedentity.Post, error) {
	return applyFollowingCursor(m.following, cursor, int(limit)), nil
}

func (m *mockPostReader) GetTrendingPosts(_ context.Context, limit int32) ([]*feedentity.Post, error) {
	return takePosts(m.trending, int(limit)), nil
}

func (m *mockPostReader) GetDiscoverWithCursor(_ context.Context, cursor *feed.DiscoverCursor, limit int32, _ *uuid.UUID) ([]*feedentity.Post, error) {
	return applyDiscoverCursor(m.discover, cursor, int(limit)), nil
}

func (m *mockPostReader) GetPostsByIDs(_ context.Context, ids []uuid.UUID) ([]*feedentity.Post, error) {
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
		feedcache.NewNopFeedSessionCache(),
	)
}

func TestGetFeed_StableMixedPaginationNoDuplicates(t *testing.T) {
	now := time.Now().UTC()
	reader := &mockPostReader{byID: map[uuid.UUID]*feedentity.Post{}}
	scores := map[uuid.UUID]float64{}

	for i := 0; i < 35; i++ {
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
	if cursor == nil {
		t.Fatal("expected next cursor")
	}
	page2, _, err := svc.GetFeed(context.Background(), uuid.New(), cursor)
	if err != nil {
		t.Fatalf("GetFeed page2: %v", err)
	}

	seen := make(map[uuid.UUID]bool)
	for _, item := range append(page1, page2...) {
		if seen[item.Post.ID] {
			t.Fatalf("duplicate post returned across pages: %s", item.Post.ID)
		}
		seen[item.Post.ID] = true
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

func TestGetFeed_DiscoverFallbackExcludesSeenPosts(t *testing.T) {
	now := time.Now().UTC()
	reader := &mockPostReader{byID: map[uuid.UUID]*feedentity.Post{}}
	seen := testPost(now)
	next := testPost(now.Add(-time.Minute))
	reader.discover = []*feedentity.Post{seen, next}
	reader.byID[seen.ID] = seen
	reader.byID[next.ID] = next

	svc := newTestService(reader, &mockRanker{scores: map[uuid.UUID]float64{}})
	state := feed.NewFeedPageState(now)
	state.Mode = feed.PhaseDiscoverFallback
	state.AddSeen(seen.ID.String())

	page, _, err := svc.GetFeed(context.Background(), uuid.New(), &feed.FeedCursor{State: state})
	if err != nil {
		t.Fatalf("GetFeed: %v", err)
	}
	if len(page) != 1 || page[0].Post.ID != next.ID {
		t.Fatalf("expected fallback to skip seen post, got %+v", page)
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
	if cursor.State.RecommendationOffset != pageSize {
		t.Fatalf("recommendation offset = %d, want %d", cursor.State.RecommendationOffset, pageSize)
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
	local := testPost(now)
	reader := &mockPostReader{
		following: []*feedentity.Post{local},
		byID:      map[uuid.UUID]*feedentity.Post{local.ID: local},
	}

	svc := newTestService(reader, &mockRanker{scores: map[uuid.UUID]float64{local.ID: 10}})
	svc.WithRecommender(&mockRecommender{err: errors.New("codohue down")})

	page, _, err := svc.GetFeed(context.Background(), uuid.New(), nil)
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
