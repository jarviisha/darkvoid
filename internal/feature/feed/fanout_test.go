package feed

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

// settingsWithFanoutCap builds the snapshot a worker reads its follower cap from.
// The cap is no longer a constructor argument — it is read per event, so an
// operator lowering it during an incident takes effect on the next post.
func settingsWithFanoutCap(maxFollowers int) *Settings {
	rs := DefaultRuntimeSettings()
	rs.FanoutMaxFollowers = maxFollowers
	return NewSettings(rs)
}

type mockFollowerReader struct {
	ids       []uuid.UUID
	err       error
	lastLimit int
}

func (m *mockFollowerReader) GetFollowerIDs(_ context.Context, _ uuid.UUID, limit int) ([]uuid.UUID, error) {
	m.lastLimit = limit
	if limit > 0 && len(m.ids) > limit {
		return m.ids[:limit], m.err
	}
	return m.ids, m.err
}

type recordingTimelineStore struct {
	added map[uuid.UUID][]TimelineEntry
	set   map[uuid.UUID][]TimelineEntry
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

func (s *recordingTimelineStore) SetPostsBatch(_ context.Context, userID uuid.UUID, entries []TimelineEntry) error {
	if s.err != nil {
		return s.err
	}
	if s.set == nil {
		s.set = make(map[uuid.UUID][]TimelineEntry)
	}
	s.set[userID] = append(s.set[userID], entries...)
	return nil
}

func (s *recordingTimelineStore) ReplacePosts(_ context.Context, userID uuid.UUID, entries []TimelineEntry, _ time.Time) error {
	return s.SetPostsBatch(context.Background(), userID, entries)
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
	worker := NewFanoutWorker(&mockFollowerReader{ids: []uuid.UUID{followerA, followerB}}, store, nil, settingsWithFanoutCap(10))

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
	reader := &mockFollowerReader{ids: followers}
	worker := NewFanoutWorker(reader, store, nil, settingsWithFanoutCap(2))

	authorID := uuid.New()
	if err := worker.HandleFeedEvent(context.Background(), Event{Type: EventPostCreated, PostID: uuid.New(), AuthorID: authorID, Score: 1}); err != nil {
		t.Fatalf("HandleFeedEvent: %v", err)
	}
	// The author rides outside the cap: 2 capped followers + the author.
	if len(store.added) != 3 {
		t.Fatalf("fanout count = %d, want capped 2 followers + author", len(store.added))
	}
	if len(store.added[authorID]) != 1 {
		t.Fatalf("author entries = %+v, want own post delivered", store.added[authorID])
	}
	if reader.lastLimit != 3 {
		t.Fatalf("follower query limit = %d, want configured cap + 1", reader.lastLimit)
	}
}

func TestFanoutWorker_TimelineErrorsAreReturned(t *testing.T) {
	worker := NewFanoutWorker(
		&mockFollowerReader{ids: []uuid.UUID{uuid.New()}},
		&recordingTimelineStore{err: errors.New("redis down")},
		nil,
		settingsWithFanoutCap(10),
	)
	if err := worker.HandleFeedEvent(context.Background(), Event{Type: EventPostCreated, PostID: uuid.New(), AuthorID: uuid.New(), Score: 1}); err == nil {
		t.Fatal("expected timeline error")
	}
}

func TestFanoutWorker_IgnoresUnhandledEvents(t *testing.T) {
	store := &recordingTimelineStore{}
	worker := NewFanoutWorker(&mockFollowerReader{ids: []uuid.UUID{uuid.New()}}, store, nil, settingsWithFanoutCap(10))
	if err := worker.HandleFeedEvent(context.Background(), Event{Type: EventPostDeleted}); err != nil {
		t.Fatalf("HandleFeedEvent: %v", err)
	}
	if len(store.added) != 0 {
		t.Fatalf("unexpected timeline writes: %+v", store.added)
	}
}

// recordingRefresher records RefreshTimeline calls for follow-change tests.
type recordingRefresher struct {
	refreshed []uuid.UUID
	err       error
}

func (r *recordingRefresher) RefreshTimeline(_ context.Context, userID uuid.UUID) error {
	r.refreshed = append(r.refreshed, userID)
	return r.err
}

func TestFanoutWorker_PostCreatedWritesAuthorTimeline(t *testing.T) {
	authorID, postID := uuid.New(), uuid.New()
	store := &recordingTimelineStore{}
	worker := NewFanoutWorker(&mockFollowerReader{}, store, nil, settingsWithFanoutCap(10))

	err := worker.HandleFeedEvent(context.Background(), Event{
		Type: EventPostCreated, PostID: postID, AuthorID: authorID, Visibility: "public", Score: 7,
	})
	if err != nil {
		t.Fatalf("HandleFeedEvent: %v", err)
	}
	entries := store.added[authorID]
	if len(entries) != 1 || entries[0].PostID != postID || entries[0].Score != 7 {
		t.Fatalf("author entries = %+v, want the author's own post", entries)
	}
}

func TestFanoutWorker_PrivatePostOnlyReachesAuthor(t *testing.T) {
	store := &recordingTimelineStore{}
	followerID, authorID, postID := uuid.New(), uuid.New(), uuid.New()
	worker := NewFanoutWorker(&mockFollowerReader{ids: []uuid.UUID{followerID}}, store, nil, settingsWithFanoutCap(10))

	err := worker.HandleFeedEvent(context.Background(), Event{
		Type: EventPostCreated, PostID: postID, AuthorID: authorID, Visibility: "private", Score: 1,
	})
	if err != nil {
		t.Fatalf("HandleFeedEvent: %v", err)
	}
	if len(store.added[authorID]) != 1 || store.added[authorID][0].PostID != postID {
		t.Fatalf("private post missing from author timeline: %+v", store.added)
	}
	if len(store.added[followerID]) != 0 {
		t.Fatalf("private post reached follower timeline: %+v", store.added[followerID])
	}
}

func TestFanoutWorker_FollowChangeRefreshesActorTimeline(t *testing.T) {
	follower, followee := uuid.New(), uuid.New()
	store := &recordingTimelineStore{}
	refresher := &recordingRefresher{}
	worker := NewFanoutWorker(&mockFollowerReader{}, store, refresher, settingsWithFanoutCap(10))

	for _, eventType := range []EventType{EventFollowCreated, EventFollowDeleted} {
		if err := worker.HandleFeedEvent(context.Background(), Event{Type: eventType, ActorID: follower, FolloweeID: followee}); err != nil {
			t.Fatalf("HandleFeedEvent(%s): %v", eventType, err)
		}
	}
	if len(refresher.refreshed) != 2 || refresher.refreshed[0] != follower || refresher.refreshed[1] != follower {
		t.Fatalf("refreshed = %+v, want the follower's timeline rebuilt on both events", refresher.refreshed)
	}
	if len(store.added) != 0 {
		t.Fatalf("follow change wrote timeline entries directly: %+v", store.added)
	}
}

func TestFanoutWorker_FollowChangeRefreshErrorPropagates(t *testing.T) {
	refresher := &recordingRefresher{err: errors.New("refresh failed")}
	worker := NewFanoutWorker(&mockFollowerReader{}, &recordingTimelineStore{}, refresher, settingsWithFanoutCap(10))

	if err := worker.HandleFeedEvent(context.Background(), Event{Type: EventFollowCreated, ActorID: uuid.New()}); err == nil {
		t.Fatal("expected refresh error to propagate")
	}
}

func TestFanoutWorker_FollowChangeWithoutRefresherIsNoOp(t *testing.T) {
	worker := NewFanoutWorker(&mockFollowerReader{}, &recordingTimelineStore{}, nil, settingsWithFanoutCap(10))
	if err := worker.HandleFeedEvent(context.Background(), Event{Type: EventFollowCreated, ActorID: uuid.New()}); err != nil {
		t.Fatalf("HandleFeedEvent: %v", err)
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

func (s *flakyTimelineStore) SetPostsBatch(_ context.Context, _ uuid.UUID, _ []TimelineEntry) error {
	return nil
}

func (s *flakyTimelineStore) ReplacePosts(_ context.Context, _ uuid.UUID, _ []TimelineEntry, _ time.Time) error {
	return nil
}
func (s *flakyTimelineStore) ReadPage(_ context.Context, _ uuid.UUID, _ *TimelinePosition, _ int) (*TimelinePage, error) {
	return &TimelinePage{}, nil
}
func (s *flakyTimelineStore) Trim(_ context.Context, _ uuid.UUID) error { return nil }
func (s *flakyTimelineStore) RemovePostBestEffort(_ context.Context, _ uuid.UUID, _ uuid.UUID) error {
	return nil
}

func TestFanoutWorker_PartialFailureContinuesAndReturnsErrorForRetry(t *testing.T) {
	good1, bad, good2 := uuid.New(), uuid.New(), uuid.New()
	store := &flakyTimelineStore{failFor: map[uuid.UUID]bool{bad: true}}
	worker := NewFanoutWorker(&mockFollowerReader{ids: []uuid.UUID{good1, bad, good2}}, store, nil, settingsWithFanoutCap(10))

	err := worker.HandleFeedEvent(context.Background(), Event{
		Type: EventPostCreated, PostID: uuid.New(), AuthorID: uuid.New(), Score: 1,
	})
	if err == nil {
		t.Fatal("HandleFeedEvent should return an error so the outbox retries failed recipients")
	}
	if len(store.added[good1]) != 1 || len(store.added[good2]) != 1 {
		t.Fatalf("good followers missed delivery: %+v", store.added)
	}
	if _, ok := store.added[bad]; ok {
		t.Fatal("bad follower should have no entry")
	}
}

func TestFanoutWorker_AllFailuresReturnError(t *testing.T) {
	a, b, author := uuid.New(), uuid.New(), uuid.New()
	store := &flakyTimelineStore{failFor: map[uuid.UUID]bool{a: true, b: true, author: true}}
	worker := NewFanoutWorker(&mockFollowerReader{ids: []uuid.UUID{a, b}}, store, nil, settingsWithFanoutCap(10))

	err := worker.HandleFeedEvent(context.Background(), Event{
		Type: EventPostCreated, PostID: uuid.New(), AuthorID: author, Score: 1,
	})
	if err == nil {
		t.Fatal("expected error when every timeline write fails")
	}
}

func TestFanoutWorker_SkipsNilFollowerIDs(t *testing.T) {
	real := uuid.New()
	store := &recordingTimelineStore{}
	worker := NewFanoutWorker(&mockFollowerReader{ids: []uuid.UUID{uuid.Nil, real, uuid.Nil}}, store, nil, settingsWithFanoutCap(10))

	if err := worker.HandleFeedEvent(context.Background(), Event{Type: EventPostCreated, PostID: uuid.New(), AuthorID: uuid.New(), Score: 1}); err != nil {
		t.Fatalf("HandleFeedEvent: %v", err)
	}
	if len(store.added) != 2 || len(store.added[real]) != 1 {
		t.Fatalf("expected the real follower and the author written, got: %+v", store.added)
	}
}

func TestFanoutWorker_BailsOnContextCancel(t *testing.T) {
	followers := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	store := &recordingTimelineStore{}
	worker := NewFanoutWorker(&mockFollowerReader{ids: followers}, store, nil, settingsWithFanoutCap(10))

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
		nil,
		settingsWithFanoutCap(10),
	)
	err := worker.HandleFeedEvent(context.Background(), Event{Type: EventPostCreated, PostID: uuid.New(), AuthorID: uuid.New(), Score: 1})
	if err == nil {
		t.Fatal("expected error when follower reader fails")
	}
}
