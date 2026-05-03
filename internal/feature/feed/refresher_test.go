package feed

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	feedentity "github.com/jarviisha/darkvoid/internal/feature/feed/entity"
)

type mockRefreshPostReader struct {
	posts     []*feedentity.Post
	lastLimit int32
}

func (m *mockRefreshPostReader) GetFollowingPostsWithCursor(_ context.Context, _ []uuid.UUID, _ *FollowingCursor, limit int32) ([]*feedentity.Post, error) {
	m.lastLimit = limit
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
}

func (m *mockRefreshFollowReader) GetFollowingIDs(_ context.Context, _ uuid.UUID) ([]uuid.UUID, error) {
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
