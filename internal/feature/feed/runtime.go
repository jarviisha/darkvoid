package feed

import (
	"sync/atomic"
	"time"
)

// RuntimeSettings are the feed knobs that can change while the process runs.
// Every one of them was a FEED_* environment variable or, for ScorerConfig, a
// literal in this package; see migrations/settings/000001_init.up.sql for why they moved and
// why the fanout worker count and queue size did not.
//
// It is a value type and every reader takes a copy, so a single request cannot
// observe the old value of one field next to the new value of another.
type RuntimeSettings struct {
	TimelineEnabled        bool
	TimelineRolloutPercent int
	TimelineMaxItems       int
	TimelineTTL            time.Duration
	TimelineRefreshOnMiss  bool

	FanoutEnabled      bool
	FanoutMaxFollowers int

	Scorer ScorerConfig

	// RecommendationWeight scales a Codohue relevance score where the mixed feed
	// adds it to the local score. It lives beside ScorerConfig rather than in it:
	// the local formula never sees a provider score, so the timeline writers that
	// share ScorerConfig have no use for it.
	RecommendationWeight float64
}

// DefaultRuntimeSettings returns the values the feed runs on before the first
// settings read succeeds, and the ones the feed package's own tests use. This is
// the only copy in Go; the other is the column defaults in settings.feed, and
// TestFeedDefaults_MatchMigrationDefaults in internal/app pins the two together.
func DefaultRuntimeSettings() RuntimeSettings {
	return RuntimeSettings{
		TimelineEnabled:        false,
		TimelineRolloutPercent: 0,
		TimelineMaxItems:       1000,
		TimelineTTL:            7 * 24 * time.Hour,
		TimelineRefreshOnMiss:  true,
		FanoutEnabled:          true,
		FanoutMaxFollowers:     10000,
		Scorer: ScorerConfig{
			RelationshipBonus: 10,
			RecencyScale:      20,
			DecayExponent:     1.5,
		},
		RecommendationWeight: 20,
	}
}

// Settings is the live snapshot every feed component reads its tunable knobs
// from.
//
// One instance is shared by the read path, the ranker, the fanout worker, the
// refresher and the timeline store, which is the point: before this, each of them
// captured its value in SetupFeedContext, so changing a weight meant rebuilding
// all five and there was no way to change one without a restart. Now they read
// the same pointer, and applying an operator's edit is a single swap.
//
// Reads are lock-free and happen on every ranked request; writes come from the
// settings service, on a request goroutine after a PATCH and on the refresh loop.
// atomic.Pointer rather than a mutex because the read side is hot and the write
// side is rare.
type Settings struct {
	current atomic.Pointer[RuntimeSettings]
}

// NewSettings returns a holder seeded with rs.
func NewSettings(rs RuntimeSettings) *Settings {
	s := &Settings{}
	s.current.Store(&rs)
	return s
}

// Get returns the current snapshot.
//
// A nil receiver and an unseeded holder both yield the defaults rather than a
// zero value. That is not defensive habit: the zero RuntimeSettings has
// DecayExponent 0, which makes the recency term a constant and silently removes
// recency from ranking. A component handed no settings should behave like one
// handed the defaults, not like one handed a broken formula.
func (s *Settings) Get() RuntimeSettings {
	if s == nil {
		return DefaultRuntimeSettings()
	}
	if rs := s.current.Load(); rs != nil {
		return *rs
	}
	return DefaultRuntimeSettings()
}

// Set publishes a new snapshot. Subsequent Get calls return it; calls already in
// flight finish on the snapshot they read.
func (s *Settings) Set(rs RuntimeSettings) {
	if s == nil {
		return
	}
	s.current.Store(&rs)
}

// TimelineWriteLimits returns the two numbers a timeline write needs: how many
// entries to keep and how long to keep them. Grouped into one accessor because
// every writer needs both, and reading them from two separate Get calls would let
// an edit land between them and trim to the new count under the old TTL.
//
// Neither value is checked for zero: Get never returns an unseeded snapshot, and
// every seeded one comes from DefaultRuntimeSettings or from a settings.feed row,
// whose CHECKs keep both positive.
func (s *Settings) TimelineWriteLimits() (maxItems int, ttl time.Duration) {
	rs := s.Get()
	return rs.TimelineMaxItems, rs.TimelineTTL
}
