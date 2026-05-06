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

// flakyTimelineStore fails AddPost for a configurable subset of follower IDs.
type flakyTimelineStore struct {
	failFor map[uuid.UUID]bool
	added   map[uuid.UUID][]TimelineEntry
}

func (s *flakyTimelineStore) AddPost(_ context.Context, userID uuid.UUID, entry TimelineEntry) error {
	if s.failFor[userID] {
		return errors.New("redis flaky")
	}
	if s.added == nil {
		s.added = make(map[uuid.UUID][]TimelineEntry)
	}
	s.added[userID] = append(s.added[userID], entry)
	return nil
}

func (s *flakyTimelineStore) AddPostsBatch(_ context.Context, _ uuid.UUID, _ []TimelineEntry) error {
	return nil
}
func (s *flakyTimelineStore) ReadPage(_ context.Context, _ uuid.UUID, _ *TimelinePosition, _ int) (*TimelinePage, error) {
	return &TimelinePage{}, nil
}
func (s *flakyTimelineStore) Trim(_ context.Context, _ uuid.UUID) error { return nil }
func (s *flakyTimelineStore) RemovePostBestEffort(_ context.Context, _ uuid.UUID, _ uuid.UUID) error {
	return nil
}

func TestFanoutWorker_PartialFailureContinuesAndSucceeds(t *testing.T) {
	good1, bad, good2 := uuid.New(), uuid.New(), uuid.New()
	store := &flakyTimelineStore{failFor: map[uuid.UUID]bool{bad: true}}
	worker := NewFanoutWorker(&mockFollowerReader{ids: []uuid.UUID{good1, bad, good2}}, store, 10)

	err := worker.HandleFeedEvent(context.Background(), Event{
		Type: EventPostCreated, PostID: uuid.New(), AuthorID: uuid.New(), Score: 1,
	})
	if err != nil {
		t.Fatalf("HandleFeedEvent should succeed when at least one write lands: %v", err)
	}
	if len(store.added[good1]) != 1 || len(store.added[good2]) != 1 {
		t.Fatalf("good followers missed delivery: %+v", store.added)
	}
	if _, ok := store.added[bad]; ok {
		t.Fatal("bad follower should have no entry")
	}
}

func TestFanoutWorker_AllFailuresReturnError(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	store := &flakyTimelineStore{failFor: map[uuid.UUID]bool{a: true, b: true}}
	worker := NewFanoutWorker(&mockFollowerReader{ids: []uuid.UUID{a, b}}, store, 10)

	err := worker.HandleFeedEvent(context.Background(), Event{
		Type: EventPostCreated, PostID: uuid.New(), AuthorID: uuid.New(), Score: 1,
	})
	if err == nil {
		t.Fatal("expected error when all follower writes fail")
	}
}

func TestFanoutWorker_SkipsNilFollowerIDs(t *testing.T) {
	real := uuid.New()
	store := &recordingTimelineStore{}
	worker := NewFanoutWorker(&mockFollowerReader{ids: []uuid.UUID{uuid.Nil, real, uuid.Nil}}, store, 10)

	if err := worker.HandleFeedEvent(context.Background(), Event{Type: EventPostCreated, PostID: uuid.New(), AuthorID: uuid.New(), Score: 1}); err != nil {
		t.Fatalf("HandleFeedEvent: %v", err)
	}
	if len(store.added) != 1 || len(store.added[real]) != 1 {
		t.Fatalf("expected only real follower written, got: %+v", store.added)
	}
}

func TestFanoutWorker_BailsOnContextCancel(t *testing.T) {
	followers := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	store := &recordingTimelineStore{}
	worker := NewFanoutWorker(&mockFollowerReader{ids: followers}, store, 10)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before fanout starts

	err := worker.HandleFeedEvent(ctx, Event{Type: EventPostCreated, PostID: uuid.New(), AuthorID: uuid.New(), Score: 1})
	if err == nil {
		t.Fatal("expected error from cancelled context (zero successful writes)")
	}
	if len(store.added) != 0 {
		t.Fatalf("no writes expected after cancel, got: %+v", store.added)
	}
}

func TestFanoutWorker_FollowerReaderErrorPropagates(t *testing.T) {
	worker := NewFanoutWorker(
		&mockFollowerReader{err: errors.New("db down")},
		&recordingTimelineStore{},
		10,
	)
	err := worker.HandleFeedEvent(context.Background(), Event{Type: EventPostCreated, PostID: uuid.New(), AuthorID: uuid.New(), Score: 1})
	if err == nil {
		t.Fatal("expected error when follower reader fails")
	}
}
