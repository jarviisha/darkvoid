package feed

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	feedentity "github.com/jarviisha/darkvoid/internal/feature/feed/entity"
)

// settingsWithMaxItems builds the snapshot a refresher reads its entry count
// from. It is read per refresh rather than captured at construction, so it cannot
// drift from what the timeline store trims to.
func settingsWithMaxItems(maxItems int) *Settings {
	rs := DefaultRuntimeSettings()
	rs.TimelineMaxItems = maxItems
	return NewSettings(rs)
}

type mockRefreshPostReader struct {
	posts     []*feedentity.Post
	lastLimit int32
	err       error
}

func (m *mockRefreshPostReader) GetFollowingPostsWithCursor(_ context.Context, _ []uuid.UUID, _ *FollowingCursor, limit int32) ([]*feedentity.Post, error) {
	m.lastLimit = limit
	if m.err != nil {
		return nil, m.err
	}
	return m.posts, nil
}

func (m *mockRefreshPostReader) GetTrendingPosts(_ context.Context, _ int32) ([]*feedentity.Post, error) {
	return nil, nil
}

func (m *mockRefreshPostReader) GetDiscoverWithCursor(_ context.Context, _ *DiscoverCursor, _ int32, _ *uuid.UUID) ([]*feedentity.Post, error) {
	return nil, nil
}

func (m *mockRefreshPostReader) GetPostsByIDs(_ context.Context, _ []uuid.UUID) ([]*feedentity.Post, error) {
	return nil, nil
}

type mockRefreshFollowReader struct {
	ids []uuid.UUID
	err error
}

func (m *mockRefreshFollowReader) GetFollowingIDs(_ context.Context, _ uuid.UUID) ([]uuid.UUID, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.ids, nil
}

// stubRefreshRanker returns fixed scores and records the followingSet it was
// given, so tests can pin both the packed entry scores and the ranker inputs.
type stubRefreshRanker struct {
	scores       map[string]float64
	err          error
	gotFollowing map[string]bool
}

func (r *stubRefreshRanker) RankPosts(_ context.Context, posts []*feedentity.Post, followingSet map[string]bool, _ time.Time) (map[string]float64, error) {
	if r.err != nil {
		return nil, r.err
	}
	r.gotFollowing = followingSet
	out := make(map[string]float64, len(posts))
	for _, p := range posts {
		out[p.ID.String()] = r.scores[p.ID.String()]
	}
	return out, nil
}

func TestPreparedTimelineRefresher_WarmTimelinesWritesRankedPackedEntries(t *testing.T) {
	now := time.Now().UTC()
	post := &feedentity.Post{ID: uuid.New(), CreatedAt: now}
	postReader := &mockRefreshPostReader{posts: []*feedentity.Post{post}}
	store := &recordingTimelineStore{}
	followed := uuid.New()
	ranker := &stubRefreshRanker{scores: map[string]float64{post.ID.String(): 42.5}}
	refresher := NewPreparedTimelineRefresher(postReader, &mockRefreshFollowReader{ids: []uuid.UUID{followed}}, store, ranker, settingsWithMaxItems(1))
	userA, userB := uuid.New(), uuid.New()

	if err := refresher.WarmTimelines(context.Background(), []uuid.UUID{userA, userB}); err != nil {
		t.Fatalf("WarmTimelines: %v", err)
	}
	if postReader.lastLimit != 1 {
		t.Fatalf("read limit = %d, want 1", postReader.lastLimit)
	}
	want := PackTimelineScore(42.5, now)
	for _, userID := range []uuid.UUID{userA, userB} {
		entries := store.set[userID]
		if len(entries) != 1 || entries[0].PostID != post.ID || entries[0].Score != want {
			t.Fatalf("entries for %s = %+v, want packed score %d", userID, entries, want)
		}
	}
	if len(store.added) != 0 {
		t.Fatalf("refresher must write via SetPostsBatch (upsert), got NX adds: %+v", store.added)
	}
	if !ranker.gotFollowing[followed.String()] || !ranker.gotFollowing[userB.String()] {
		t.Fatalf("ranker followingSet must contain followed authors and self, got %+v", ranker.gotFollowing)
	}
}

func TestPreparedTimelineRefresher_RankerErrorPropagates(t *testing.T) {
	post := &feedentity.Post{ID: uuid.New(), CreatedAt: time.Now().UTC()}
	refresher := NewPreparedTimelineRefresher(
		&mockRefreshPostReader{posts: []*feedentity.Post{post}},
		&mockRefreshFollowReader{ids: []uuid.UUID{uuid.New()}},
		&recordingTimelineStore{},
		&stubRefreshRanker{err: errors.New("ranker down")},
		settingsWithMaxItems(10),
	)
	if err := refresher.RefreshTimeline(context.Background(), uuid.New()); err == nil {
		t.Fatal("expected ranker error to propagate")
	}
}

func TestPreparedTimelineRefresher_NilTimelineNoOps(t *testing.T) {
	refresher := NewPreparedTimelineRefresher(&mockRefreshPostReader{}, &mockRefreshFollowReader{}, nil, &stubRefreshRanker{}, settingsWithMaxItems(10))
	if err := refresher.RefreshTimeline(context.Background(), uuid.New()); err != nil {
		t.Fatalf("RefreshTimeline with nil store should no-op: %v", err)
	}
	if err := refresher.WarmTimelines(context.Background(), []uuid.UUID{uuid.New()}); err != nil {
		t.Fatalf("WarmTimelines with nil store should no-op: %v", err)
	}
}

func TestPreparedTimelineRefresher_FollowReaderErrorPropagates(t *testing.T) {
	refresher := NewPreparedTimelineRefresher(
		&mockRefreshPostReader{},
		&mockRefreshFollowReader{err: errors.New("follow db down")},
		&recordingTimelineStore{},
		&stubRefreshRanker{},
		settingsWithMaxItems(10),
	)
	if err := refresher.RefreshTimeline(context.Background(), uuid.New()); err == nil {
		t.Fatal("expected follow reader error to propagate")
	}
}

func TestPreparedTimelineRefresher_PostReaderErrorPropagates(t *testing.T) {
	refresher := NewPreparedTimelineRefresher(
		&mockRefreshPostReader{err: errors.New("post db down")},
		&mockRefreshFollowReader{ids: []uuid.UUID{uuid.New()}},
		&recordingTimelineStore{},
		&stubRefreshRanker{},
		settingsWithMaxItems(10),
	)
	if err := refresher.RefreshTimeline(context.Background(), uuid.New()); err == nil {
		t.Fatal("expected post reader error to propagate")
	}
}

func TestPreparedTimelineRefresher_TimelineWriteErrorPropagates(t *testing.T) {
	post := &feedentity.Post{ID: uuid.New(), CreatedAt: time.Now().UTC()}
	refresher := NewPreparedTimelineRefresher(
		&mockRefreshPostReader{posts: []*feedentity.Post{post}},
		&mockRefreshFollowReader{ids: []uuid.UUID{uuid.New()}},
		&recordingTimelineStore{err: errors.New("redis down")},
		&stubRefreshRanker{},
		settingsWithMaxItems(10),
	)
	if err := refresher.RefreshTimeline(context.Background(), uuid.New()); err == nil {
		t.Fatal("expected timeline write error to propagate")
	}
}

func TestPreparedTimelineRefresher_WarmTimelinesStopsOnFirstError(t *testing.T) {
	post := &feedentity.Post{ID: uuid.New(), CreatedAt: time.Now().UTC()}
	store := &recordingTimelineStore{err: errors.New("redis down")}
	refresher := NewPreparedTimelineRefresher(
		&mockRefreshPostReader{posts: []*feedentity.Post{post}},
		&mockRefreshFollowReader{ids: []uuid.UUID{uuid.New()}},
		store,
		&stubRefreshRanker{},
		settingsWithMaxItems(10),
	)
	err := refresher.WarmTimelines(context.Background(), []uuid.UUID{uuid.New(), uuid.New()})
	if err == nil {
		t.Fatal("expected warm timelines to surface store error")
	}
}

// A non-positive stored item count must read as the default, not as zero.
// Refreshing every timeline into an empty one is the failure that looks like
// success — SetPostsBatch is called, no error is returned, and the feed simply
// starts missing.
func TestPreparedTimelineRefresher_DefaultMaxItems(t *testing.T) {
	for name, settings := range map[string]*Settings{
		"zero":     settingsWithMaxItems(0),
		"negative": settingsWithMaxItems(-5),
		"nil":      nil,
	} {
		t.Run(name, func(t *testing.T) {
			r := NewPreparedTimelineRefresher(&mockRefreshPostReader{}, &mockRefreshFollowReader{}, &recordingTimelineStore{}, &stubRefreshRanker{}, settings)
			if got := r.maxItems(); got != 1000 {
				t.Fatalf("maxItems() = %d, want default 1000", got)
			}
		})
	}
}

// The count is read per refresh, so an operator's edit reaches the next refresh
// rather than the next restart.
func TestPreparedTimelineRefresher_MaxItemsFollowsSettingsChange(t *testing.T) {
	settings := settingsWithMaxItems(50)
	postReader := &mockRefreshPostReader{}
	r := NewPreparedTimelineRefresher(postReader, &mockRefreshFollowReader{}, &recordingTimelineStore{}, &stubRefreshRanker{}, settings)

	if err := r.RefreshTimeline(context.Background(), uuid.New()); err != nil {
		t.Fatalf("RefreshTimeline: %v", err)
	}
	if postReader.lastLimit != 50 {
		t.Fatalf("read limit = %d, want 50", postReader.lastLimit)
	}

	rs := DefaultRuntimeSettings()
	rs.TimelineMaxItems = 7
	settings.Set(rs)

	if err := r.RefreshTimeline(context.Background(), uuid.New()); err != nil {
		t.Fatalf("RefreshTimeline after settings change: %v", err)
	}
	if postReader.lastLimit != 7 {
		t.Fatalf("read limit after settings change = %d, want 7", postReader.lastLimit)
	}
}
