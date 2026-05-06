package feed

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	feedentity "github.com/jarviisha/darkvoid/internal/feature/feed/entity"
)

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

func TestPreparedTimelineRefresher_WarmTimelinesWritesBoundedEntries(t *testing.T) {
	now := time.Now().UTC()
	post := &feedentity.Post{ID: uuid.New(), CreatedAt: now}
	postReader := &mockRefreshPostReader{posts: []*feedentity.Post{post}}
	store := &recordingTimelineStore{}
	refresher := NewPreparedTimelineRefresher(postReader, &mockRefreshFollowReader{ids: []uuid.UUID{uuid.New()}}, store, 1)
	userA, userB := uuid.New(), uuid.New()

	if err := refresher.WarmTimelines(context.Background(), []uuid.UUID{userA, userB}); err != nil {
		t.Fatalf("WarmTimelines: %v", err)
	}
	if postReader.lastLimit != 1 {
		t.Fatalf("read limit = %d, want 1", postReader.lastLimit)
	}
	for _, userID := range []uuid.UUID{userA, userB} {
		entries := store.added[userID]
		if len(entries) != 1 || entries[0].PostID != post.ID || entries[0].Score != TimelineScoreFromTime(now) {
			t.Fatalf("entries for %s = %+v", userID, entries)
		}
	}
}

func TestPreparedTimelineRefresher_NilTimelineNoOps(t *testing.T) {
	refresher := NewPreparedTimelineRefresher(&mockRefreshPostReader{}, &mockRefreshFollowReader{}, nil, 10)
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
		10,
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
		10,
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
		10,
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
		10,
	)
	err := refresher.WarmTimelines(context.Background(), []uuid.UUID{uuid.New(), uuid.New()})
	if err == nil {
		t.Fatal("expected warm timelines to surface store error")
	}
}

func TestPreparedTimelineRefresher_DefaultMaxItems(t *testing.T) {
	r := NewPreparedTimelineRefresher(&mockRefreshPostReader{}, &mockRefreshFollowReader{}, &recordingTimelineStore{}, 0)
	if r.maxItems != 1000 {
		t.Fatalf("maxItems with 0 input = %d, want default 1000", r.maxItems)
	}
	r2 := NewPreparedTimelineRefresher(&mockRefreshPostReader{}, &mockRefreshFollowReader{}, &recordingTimelineStore{}, -5)
	if r2.maxItems != 1000 {
		t.Fatalf("maxItems with negative input = %d, want default 1000", r2.maxItems)
	}
}
