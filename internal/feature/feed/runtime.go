package feed

import (
	"sync/atomic"
	"time"
)

// RuntimeSettings are the feed knobs that can change while the process runs.
// Every one of them was a FEED_* environment variable or, for ScorerConfig, a
// literal in this package; see migrations/settings/000002 for why they moved and
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
}

// DefaultRuntimeSettings returns the values the feed runs on before the first
// settings read succeeds, and the ones the feed package's own tests use. They
// match the column defaults in settings.feed — entity.DefaultFeedSettings is the
// other copy, and TestDefaultRuntimeSettings_MatchesEntityDefaults pins them
// together.
func DefaultRuntimeSettings() RuntimeSettings {
	return RuntimeSettings{
		TimelineEnabled:        false,
		TimelineRolloutPercent: 0,
		TimelineMaxItems:       1000,
		TimelineTTL:            7 * 24 * time.Hour,
		TimelineRefreshOnMiss:  true,
		FanoutEnabled:          true,
		FanoutMaxFollowers:     10000,
		Scorer:                 DefaultScorerConfig(),
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
func (s *Settings) TimelineWriteLimits() (maxItems int, ttl time.Duration) {
	rs := s.Get()
	if rs.TimelineMaxItems <= 0 {
		rs.TimelineMaxItems = DefaultRuntimeSettings().TimelineMaxItems
	}
	if rs.TimelineTTL <= 0 {
		rs.TimelineTTL = DefaultRuntimeSettings().TimelineTTL
	}
	return rs.TimelineMaxItems, rs.TimelineTTL
}
