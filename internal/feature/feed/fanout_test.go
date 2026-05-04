package feed

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type mockFollowerReader struct {
	ids []uuid.UUID
	err error
}

func (m *mockFollowerReader) GetFollowerIDs(_ context.Context, _ uuid.UUID) ([]uuid.UUID, error) {
	return m.ids, m.err
}

type recordingTimelineStore struct {
	added map[uuid.UUID][]TimelineEntry
	err   error
}

func (s *recordingTimelineStore) AddPost(_ context.Context, userID uuid.UUID, entry TimelineEntry) error {
	if s.err != nil {
		return s.err
	}
	if s.added == nil {
		s.added = make(map[uuid.UUID][]TimelineEntry)
	}
	s.added[userID] = append(s.added[userID], entry)
	return nil
}

func (s *recordingTimelineStore) AddPostsBatch(_ context.Context, userID uuid.UUID, entries []TimelineEntry) error {
	for _, entry := range entries {
		if err := s.AddPost(context.Background(), userID, entry); err != nil {
			return err
		}
	}
	return nil
}

func (s *recordingTimelineStore) ReadPage(_ context.Context, _ uuid.UUID, _ *TimelinePosition, _ int) (*TimelinePage, error) {
	return &TimelinePage{}, nil
}

func (s *recordingTimelineStore) Trim(_ context.Context, _ uuid.UUID) error { return nil }

func (s *recordingTimelineStore) RemovePostBestEffort(_ context.Context, _ uuid.UUID, _ uuid.UUID) error {
	return nil
}

func TestFanoutWorker_PostCreatedWritesFollowers(t *testing.T) {
	followerA, followerB := uuid.New(), uuid.New()
	postID := uuid.New()
	store := &recordingTimelineStore{}
	worker := NewFanoutWorker(&mockFollowerReader{ids: []uuid.UUID{followerA, followerB}}, store, 10)

	err := worker.HandleFeedEvent(context.Background(), Event{
		Type:      EventPostCreated,
		PostID:    postID,
		AuthorID:  uuid.New(),
		CreatedAt: time.Now().UTC(),
		Score:     123,
	})
	if err != nil {
		t.Fatalf("HandleFeedEvent: %v", err)
	}
	for _, followerID := range []uuid.UUID{followerA, followerB} {
		entries := store.added[followerID]
		if len(entries) != 1 || entries[0].PostID != postID || entries[0].Score != 123 {
			t.Fatalf("entries for %s = %+v", followerID, entries)
		}
	}
}

func TestFanoutWorker_MaxFollowerCap(t *testing.T) {
	followers := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	store := &recordingTimelineStore{}
	worker := NewFanoutWorker(&mockFollowerReader{ids: followers}, store, 2)

	if err := worker.HandleFeedEvent(context.Background(), Event{Type: EventPostCreated, PostID: uuid.New(), AuthorID: uuid.New(), Score: 1}); err != nil {
		t.Fatalf("HandleFeedEvent: %v", err)
	}
	if len(store.added) != 2 {
		t.Fatalf("fanout count = %d, want capped 2", len(store.added))
	}
}

func TestFanoutWorker_TimelineErrorsAreReturned(t *testing.T) {
	worker := NewFanoutWorker(
		&mockFollowerReader{ids: []uuid.UUID{uuid.New()}},
		&recordingTimelineStore{err: errors.New("redis down")},
		10,
	)
	if err := worker.HandleFeedEvent(context.Background(), Event{Type: EventPostCreated, PostID: uuid.New(), AuthorID: uuid.New(), Score: 1}); err == nil {
		t.Fatal("expected timeline error")
	}
}

func TestFanoutWorker_IgnoresNonPostCreatedEvents(t *testing.T) {
	store := &recordingTimelineStore{}
	worker := NewFanoutWorker(&mockFollowerReader{ids: []uuid.UUID{uuid.New()}}, store, 10)
	if err := worker.HandleFeedEvent(context.Background(), Event{Type: EventFollowCreated}); err != nil {
		t.Fatalf("HandleFeedEvent: %v", err)
	}
	if len(store.added) != 0 {
		t.Fatalf("unexpected timeline writes: %+v", store.added)
	}
}
