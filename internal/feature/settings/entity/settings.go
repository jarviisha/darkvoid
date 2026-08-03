package entity

import (
	"time"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/pkg/errors"
)

// Bounds mirror the CHECK constraints on settings.feed. They are duplicated here
// on purpose rather than left to the database: a constraint violation surfaces as
// a 500 naming a constraint the operator has never heard of, where a check here
// returns a 400 that names the field and its range. The database keeps its own
// copy as the backstop for any writer that is not this service.
const (
	MaxRolloutPercent = 100

	MinTimelineMaxItems = 1
	MaxTimelineMaxItems = 10000

	MinTimelineTTL = time.Second
	// MaxTimelineTTL has no CHECK behind it — timeline_ttl_seconds is only
	// constrained positive, because the column stores seconds in an INTEGER and
	// the real ceiling is what fits. 90 days is the product answer: a timeline
	// older than that is rebuilt from scratch faster than it is read.
	MaxTimelineTTL = 90 * 24 * time.Hour

	MinFanoutMaxFollowers = 1

	MaxRelationshipBonus = 1000
	MaxRecencyScale      = 1000
	MaxDecayExponent     = 10
)

// FeedSettings are the feed knobs an operator can change while the API is
// running. Everything here was either a FEED_* environment variable or, for the
// three ranking weights, a literal in the feed package — see
// migrations/settings/000002 for why each one moved and why the two fanout
// sizing knobs did not.
//
// This is a value type, copied on read. The feed components hold a snapshot
// rather than a reference so that one request cannot see the pre-update value of
// one field alongside the post-update value of another.
type FeedSettings struct {
	// TimelineEnabled gates serving reads from a prepared timeline at all;
	// TimelineRolloutPercent then decides for which share of users. Two fields
	// rather than one so a rollout can be paused without losing its position.
	TimelineEnabled        bool
	TimelineRolloutPercent int
	TimelineMaxItems       int
	TimelineTTL            time.Duration
	// TimelineRefreshOnMiss allows a miss to rebuild that user's timeline inline,
	// on the request goroutine.
	TimelineRefreshOnMiss bool

	// FanoutEnabled is the kill switch for writing prepared timelines. The worker
	// pool keeps running and idles, which is what makes it reversible without a
	// restart.
	FanoutEnabled      bool
	FanoutMaxFollowers int

	// RelationshipBonus, RecencyScale and DecayExponent are the local ranking
	// formula's weights:
	//
	//	score = log(1+likes)*10 + RecencyScale/(1+hours)^DecayExponent + RelationshipBonus
	RelationshipBonus float64
	RecencyScale      float64
	DecayExponent     float64

	UpdatedBy *uuid.UUID
	UpdatedAt time.Time
}

// DefaultFeedSettings returns the same values as the column defaults in
// settings.feed. It is what the feed runs on before the first successful read and
// in tests, so the two must agree — TestDefaultFeedSettings_MatchesMigration
// pins that against the migration file.
func DefaultFeedSettings() FeedSettings {
	return FeedSettings{
		TimelineEnabled:        false,
		TimelineRolloutPercent: 0,
		TimelineMaxItems:       1000,
		TimelineTTL:            7 * 24 * time.Hour,
		TimelineRefreshOnMiss:  true,
		FanoutEnabled:          true,
		FanoutMaxFollowers:     10000,
		RelationshipBonus:      10,
		RecencyScale:           20,
		DecayExponent:          1.5,
	}
}

// FeedSettingsUpdate is a partial update: a nil field keeps its stored value.
// Every field is a pointer, including the booleans — false is a meaningful value
// for all three of them, and it is the value an operator reaches for most (that
// is what a kill switch is), so a plain bool would make "turn this off" the one
// edit the API cannot express.
type FeedSettingsUpdate struct {
	TimelineEnabled        *bool
	TimelineRolloutPercent *int
	TimelineMaxItems       *int
	TimelineTTL            *time.Duration
	TimelineRefreshOnMiss  *bool

	FanoutEnabled      *bool
	FanoutMaxFollowers *int

	RelationshipBonus *float64
	RecencyScale      *float64
	DecayExponent     *float64

	UpdatedBy *uuid.UUID
}

// IsEmpty reports whether the update names no field. The service rejects such an
// update instead of writing it: it would still bump updated_at and updated_by,
// recording an edit that changed nothing against an operator's name.
func (u FeedSettingsUpdate) IsEmpty() bool {
	return u.TimelineEnabled == nil &&
		u.TimelineRolloutPercent == nil &&
		u.TimelineMaxItems == nil &&
		u.TimelineTTL == nil &&
		u.TimelineRefreshOnMiss == nil &&
		u.FanoutEnabled == nil &&
		u.FanoutMaxFollowers == nil &&
		u.RelationshipBonus == nil &&
		u.RecencyScale == nil &&
		u.DecayExponent == nil
}

// Validate checks every named field against the bounds above, returning a
// BadRequest error naming the first field that is out of range.
//
// It validates rather than clamps. A clamp turns "set the rollout to 150" into a
// silent 100, and the operator reads back a number they did not type — for a
// rollout percent that is the difference between a typo caught at the keyboard
// and one discovered from a traffic graph.
func (u FeedSettingsUpdate) Validate() error {
	if u.IsEmpty() {
		return errors.NewBadRequestError("no settings named in the request")
	}
	if u.TimelineRolloutPercent != nil {
		if p := *u.TimelineRolloutPercent; p < 0 || p > MaxRolloutPercent {
			return errors.NewBadRequestError("timeline_rollout_percent must be between 0 and 100")
		}
	}
	if u.TimelineMaxItems != nil {
		if n := *u.TimelineMaxItems; n < MinTimelineMaxItems || n > MaxTimelineMaxItems {
			return errors.NewBadRequestError("timeline_max_items must be between 1 and 10000")
		}
	}
	if u.TimelineTTL != nil {
		if d := *u.TimelineTTL; d < MinTimelineTTL || d > MaxTimelineTTL {
			return errors.NewBadRequestError("timeline_ttl_seconds must be between 1 and 7776000 (90 days)")
		}
	}
	if u.FanoutMaxFollowers != nil {
		if n := *u.FanoutMaxFollowers; n < MinFanoutMaxFollowers {
			return errors.NewBadRequestError("fanout_max_followers must be at least 1")
		}
	}
	if u.RelationshipBonus != nil {
		if v := *u.RelationshipBonus; v < 0 || v > MaxRelationshipBonus {
			return errors.NewBadRequestError("relationship_bonus must be between 0 and 1000")
		}
	}
	if u.RecencyScale != nil {
		if v := *u.RecencyScale; v < 0 || v > MaxRecencyScale {
			return errors.NewBadRequestError("recency_scale must be between 0 and 1000")
		}
	}
	if u.DecayExponent != nil {
		// Zero is rejected rather than allowed as "no decay": at 0 the recency term
		// becomes the constant RecencyScale for every post, which removes recency
		// from the formula instead of flattening it. A small exponent is how an
		// operator asks for a slow decay.
		if v := *u.DecayExponent; v <= 0 || v > MaxDecayExponent {
			return errors.NewBadRequestError("decay_exponent must be greater than 0 and at most 10")
		}
	}
	return nil
}

// SecondsToDuration and DurationToSeconds convert the TTL between its stored form
// and its Go form. Seconds are what the column holds and what the JSON carries —
// see the migration for why the column is not an INTERVAL.
func SecondsToDuration(seconds int32) time.Duration {
	return time.Duration(seconds) * time.Second
}

// DurationToSeconds truncates to whole seconds, which is the column's resolution.
// Sub-second precision is not lost so much as never expressible: a TTL of 500ms
// is not a setting this table can hold, and Validate rejects it as below the
// one-second minimum before it reaches here.
func DurationToSeconds(d time.Duration) int32 {
	return int32(d / time.Second) //nolint:gosec // bounded by Validate's MaxTimelineTTL
}
