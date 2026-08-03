package feed

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	feedentity "github.com/jarviisha/darkvoid/internal/feature/feed/entity"
)

// A nil holder and an unseeded one must both read as the defaults, never as the
// zero value. The zero RuntimeSettings has DecayExponent 0, which turns the
// recency term into the same constant for every post — the formula still
// evaluates, still returns numbers, and silently stops ranking by recency.
func TestSettings_NilAndUnseededReadAsDefaults(t *testing.T) {
	var nilHolder *Settings
	if got := nilHolder.Get(); got != DefaultRuntimeSettings() {
		t.Fatalf("nil holder Get() = %+v, want the defaults", got)
	}
	if got := (&Settings{}).Get(); got != DefaultRuntimeSettings() {
		t.Fatalf("unseeded holder Get() = %+v, want the defaults", got)
	}
}

func TestSettings_SetIsVisibleToSubsequentGets(t *testing.T) {
	s := NewSettings(DefaultRuntimeSettings())
	rs := DefaultRuntimeSettings()
	rs.TimelineRolloutPercent = 42
	s.Set(rs)

	if got := s.Get().TimelineRolloutPercent; got != 42 {
		t.Fatalf("rollout percent = %d, want 42", got)
	}
}

// Reads happen on every ranked request while writes come from the refresh loop
// and from admin requests. Run under -race, this is the check that the holder is
// actually safe for that.
func TestSettings_ConcurrentReadWrite(t *testing.T) {
	s := NewSettings(DefaultRuntimeSettings())
	var wg sync.WaitGroup

	for i := range 8 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rs := DefaultRuntimeSettings()
			rs.TimelineRolloutPercent = i * 10
			for range 100 {
				s.Set(rs)
			}
		}(i)
	}
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				_ = s.Get()
			}
		}()
	}
	wg.Wait()
}

func TestSettings_TimelineWriteLimitsFallBackToDefaults(t *testing.T) {
	rs := DefaultRuntimeSettings()
	rs.TimelineMaxItems = 0
	rs.TimelineTTL = 0
	maxItems, ttl := NewSettings(rs).TimelineWriteLimits()

	if maxItems != DefaultRuntimeSettings().TimelineMaxItems {
		t.Fatalf("maxItems = %d, want the default rather than 0 — trimming to 0 empties every timeline", maxItems)
	}
	if ttl != DefaultRuntimeSettings().TimelineTTL {
		t.Fatalf("ttl = %v, want the default rather than 0 — Redis treats a 0 TTL as an immediate expiry", ttl)
	}
}

// The whole point of moving the weights into settings: a change reaches the read
// path without a restart.
func TestLocalRanker_FollowsSettingsChange(t *testing.T) {
	now := time.Now().UTC()
	author := uuid.New()
	post := &feedentity.Post{ID: uuid.New(), AuthorID: author, CreatedAt: now}
	following := map[string]bool{author.String(): true}

	settings := NewSettings(DefaultRuntimeSettings())
	ranker := NewLocalRanker(settings)

	before, err := ranker.RankPosts(context.Background(), []*feedentity.Post{post}, following, now)
	if err != nil {
		t.Fatalf("RankPosts: %v", err)
	}

	rs := DefaultRuntimeSettings()
	rs.Scorer.RelationshipBonus = 100
	settings.Set(rs)

	after, err := ranker.RankPosts(context.Background(), []*feedentity.Post{post}, following, now)
	if err != nil {
		t.Fatalf("RankPosts after settings change: %v", err)
	}

	if got, want := after[post.ID.String()]-before[post.ID.String()], 90.0; got != want {
		t.Fatalf("score delta = %v, want %v — the ranker is still using the weights it was built with", got, want)
	}
}

// Fan-out's write-time score is the local formula's degenerate case for a fresh
// post, so it has to move with the weights. If it were captured at construction,
// a weight change would leave fan-out writes and read-path ranking on different
// scales and the two would sort against each other.
func TestEventDispatcher_WriteScoreFollowsSettingsChange(t *testing.T) {
	settings := NewSettings(DefaultRuntimeSettings())
	handler := &recordingEventHandler{events: make(chan Event, 2)}
	dispatcher := NewEventDispatcher(settings, 1, 2, handler)
	defer dispatcher.Close()

	createdAt := time.Date(2026, 7, 24, 9, 30, 0, 0, time.UTC)
	if err := dispatcher.EmitPostCreated(context.Background(), uuid.New(), uuid.New(), "public", createdAt); err != nil {
		t.Fatalf("EmitPostCreated: %v", err)
	}
	first := <-handler.events

	rs := DefaultRuntimeSettings()
	rs.Scorer.RecencyScale = 200
	rs.Scorer.RelationshipBonus = 100
	settings.Set(rs)

	if err := dispatcher.EmitPostCreated(context.Background(), uuid.New(), uuid.New(), "public", createdAt); err != nil {
		t.Fatalf("EmitPostCreated after settings change: %v", err)
	}
	second := <-handler.events

	if want := PackTimelineScore(300, createdAt); second.Score != want {
		t.Fatalf("write score after settings change = %d, want %d", second.Score, want)
	}
	if second.Score == first.Score {
		t.Fatal("write score did not move with the weights")
	}
}

// The kill switch has to be reversible without a restart, which means the worker
// pool must already exist while fanout is off.
func TestEventDispatcher_FanoutSwitchTakesEffectWithoutRestart(t *testing.T) {
	settings := NewSettings(fanoutDisabledSettings())
	handler := &recordingEventHandler{events: make(chan Event, 1)}
	dispatcher := NewEventDispatcher(settings, 1, 1, handler)
	defer dispatcher.Close()

	if dispatcher.Dispatch(context.Background(), Event{Type: EventPostCreated}) {
		t.Fatal("dispatch succeeded while fanout was disabled")
	}

	settings.Set(DefaultRuntimeSettings())

	if !dispatcher.Dispatch(context.Background(), Event{Type: EventPostCreated}) {
		t.Fatal("dispatch failed after fanout was re-enabled — the worker pool was never started")
	}
	select {
	case <-handler.events:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for the event to be handled")
	}
}

// The follower cap is read per event for the same reason: lowering it during an
// incident has to take effect on the next post.
func TestFanoutWorker_MaxFollowersFollowsSettingsChange(t *testing.T) {
	settings := settingsWithFanoutCap(3)
	followers := make([]uuid.UUID, 5)
	for i := range followers {
		followers[i] = uuid.New()
	}
	store := &recordingTimelineStore{}
	worker := NewFanoutWorker(&mockFollowerReader{ids: followers}, store, settings)

	event := Event{Type: EventPostCreated, PostID: uuid.New(), AuthorID: uuid.New(), Score: 1}
	if err := worker.HandleFeedEvent(context.Background(), event); err != nil {
		t.Fatalf("HandleFeedEvent: %v", err)
	}
	if got := countTimelineWrites(store); got != 3 {
		t.Fatalf("writes = %d, want 3", got)
	}

	rs := DefaultRuntimeSettings()
	rs.FanoutMaxFollowers = 1
	settings.Set(rs)

	store.added = nil
	if err := worker.HandleFeedEvent(context.Background(), event); err != nil {
		t.Fatalf("HandleFeedEvent after settings change: %v", err)
	}
	if got := countTimelineWrites(store); got != 1 {
		t.Fatalf("writes after lowering the cap = %d, want 1", got)
	}
}

// A non-positive stored cap must read as the default, not as zero: fanning out to
// nobody returns no error and looks exactly like a successful run.
func TestFanoutWorker_NonPositiveCapReadsAsDefault(t *testing.T) {
	for name, settings := range map[string]*Settings{
		"zero":     settingsWithFanoutCap(0),
		"negative": settingsWithFanoutCap(-1),
		"nil":      nil,
	} {
		t.Run(name, func(t *testing.T) {
			w := NewFanoutWorker(&mockFollowerReader{}, &recordingTimelineStore{}, settings)
			if got := w.maxFollowers(); got != DefaultRuntimeSettings().FanoutMaxFollowers {
				t.Fatalf("maxFollowers() = %d, want the default", got)
			}
		})
	}
}

func countTimelineWrites(store *recordingTimelineStore) int {
	total := 0
	for _, entries := range store.added {
		total += len(entries)
	}
	return total
}
