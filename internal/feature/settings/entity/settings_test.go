package entity

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFeedSettingsUpdate_IsEmpty(t *testing.T) {
	if !(FeedSettingsUpdate{}).IsEmpty() {
		t.Fatal("a zero update should be empty")
	}
	enabled := false
	if (FeedSettingsUpdate{TimelineEnabled: &enabled}).IsEmpty() {
		t.Fatal("an update naming timeline_enabled=false must not read as empty — false is the value a kill switch is set to")
	}
}

// UpdatedBy alone is not a change. The service sets it on every request, so
// counting it would make an empty body a valid edit that bumps updated_at and
// records an operator against a write that changed nothing.
func TestFeedSettingsUpdate_UpdatedByAloneIsEmpty(t *testing.T) {
	update := FeedSettingsUpdate{}
	if !update.IsEmpty() {
		t.Fatal("expected empty")
	}
	if err := update.Validate(); err == nil {
		t.Fatal("expected an update naming no field to be rejected")
	}
}

func TestFeedSettingsUpdate_Validate(t *testing.T) {
	ptr := func(v int) *int { return &v }
	fptr := func(v float64) *float64 { return &v }
	dptr := func(v time.Duration) *time.Duration { return &v }

	tests := map[string]struct {
		update  FeedSettingsUpdate
		wantErr bool
	}{
		"rollout 0":            {FeedSettingsUpdate{TimelineRolloutPercent: ptr(0)}, false},
		"rollout 100":          {FeedSettingsUpdate{TimelineRolloutPercent: ptr(100)}, false},
		"rollout 101":          {FeedSettingsUpdate{TimelineRolloutPercent: ptr(101)}, true},
		"rollout negative":     {FeedSettingsUpdate{TimelineRolloutPercent: ptr(-1)}, true},
		"max items 1":          {FeedSettingsUpdate{TimelineMaxItems: ptr(1)}, false},
		"max items 10000":      {FeedSettingsUpdate{TimelineMaxItems: ptr(10000)}, false},
		"max items 0":          {FeedSettingsUpdate{TimelineMaxItems: ptr(0)}, true},
		"max items 10001":      {FeedSettingsUpdate{TimelineMaxItems: ptr(10001)}, true},
		"ttl 1s":               {FeedSettingsUpdate{TimelineTTL: dptr(time.Second)}, false},
		"ttl 90d":              {FeedSettingsUpdate{TimelineTTL: dptr(MaxTimelineTTL)}, false},
		"ttl sub-second":       {FeedSettingsUpdate{TimelineTTL: dptr(500 * time.Millisecond)}, true},
		"ttl over 90d":         {FeedSettingsUpdate{TimelineTTL: dptr(MaxTimelineTTL + time.Second)}, true},
		"followers 1":          {FeedSettingsUpdate{FanoutMaxFollowers: ptr(1)}, false},
		"followers 0":          {FeedSettingsUpdate{FanoutMaxFollowers: ptr(0)}, true},
		"bonus 0":              {FeedSettingsUpdate{RelationshipBonus: fptr(0)}, false},
		"bonus 1001":           {FeedSettingsUpdate{RelationshipBonus: fptr(1001)}, true},
		"recency 1001":         {FeedSettingsUpdate{RecencyScale: fptr(1001)}, true},
		"decay 0.5":            {FeedSettingsUpdate{DecayExponent: fptr(0.5)}, false},
		"decay 10":             {FeedSettingsUpdate{DecayExponent: fptr(10)}, false},
		"decay 10.1":           {FeedSettingsUpdate{DecayExponent: fptr(10.1)}, true},
		"decay negative":       {FeedSettingsUpdate{DecayExponent: fptr(-1)}, true},
		"decay zero rejected":  {FeedSettingsUpdate{DecayExponent: fptr(0)}, true},
		"rec weight 0":         {FeedSettingsUpdate{RecommendationWeight: fptr(0)}, false},
		"rec weight 1000":      {FeedSettingsUpdate{RecommendationWeight: fptr(1000)}, false},
		"rec weight 1000.5":    {FeedSettingsUpdate{RecommendationWeight: fptr(1000.5)}, true},
		"rec weight negative":  {FeedSettingsUpdate{RecommendationWeight: fptr(-0.1)}, true},
		"empty update":         {FeedSettingsUpdate{}, true},
		"booleans are enough":  {FeedSettingsUpdate{FanoutEnabled: boolPtr(false)}, false},
		"several fields at on": {FeedSettingsUpdate{TimelineRolloutPercent: ptr(50), DecayExponent: fptr(1.2)}, false},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := tt.update.Validate()
			if tt.wantErr && err == nil {
				t.Fatalf("Validate() = nil, want an error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}
		})
	}
}

// Zero decay is singled out because it is the one out-of-range value that does
// not look wrong: it reads as "no decay", but it makes the recency term the same
// constant for every post, which removes recency from ranking rather than
// flattening it.
func TestFeedSettingsUpdate_ValidateRejectsZeroDecayWithAReason(t *testing.T) {
	zero := 0.0
	err := FeedSettingsUpdate{DecayExponent: &zero}.Validate()
	if err == nil {
		t.Fatal("decay_exponent 0 must be rejected")
	}
	if !strings.Contains(err.Error(), "decay_exponent") {
		t.Fatalf("error must name the field, got %q", err)
	}
}

func TestDurationSecondsRoundTrip(t *testing.T) {
	for _, d := range []time.Duration{time.Second, time.Hour, 7 * 24 * time.Hour, MaxTimelineTTL} {
		if got := SecondsToDuration(DurationToSeconds(d)); got != d {
			t.Fatalf("round trip of %v = %v", d, got)
		}
	}
}

// The bound constants are Validate's copy of the column CHECKs, kept so a bad
// value is a 400 naming the field instead of a 500 naming a constraint. If the
// two disagree, a value is either refused that the database would take or taken
// by Validate and then rejected as a 500 — so each CHECK is rebuilt from the
// constants and looked for in the migrations. timeline_ttl_seconds is absent on
// purpose: its CHECK is only "> 0" and MaxTimelineTTL has no column behind it.
func TestBounds_MatchMigrationChecks(t *testing.T) {
	files, err := filepath.Glob("../../../../migrations/settings/*.up.sql")
	if err != nil || len(files) == 0 {
		t.Fatalf("glob settings migrations: %v (found %d)", err, len(files))
	}
	var sql strings.Builder
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		for line := range strings.SplitSeq(string(raw), "\n") {
			if i := strings.Index(line, "--"); i >= 0 {
				line = line[:i]
			}
			sql.WriteString(line + " ")
		}
	}
	normalized := strings.Join(strings.Fields(sql.String()), " ")
	// ponytail: substring match over every migration, so a CHECK later dropped
	// and re-added with new bounds still finds the old text in 000001. Resolve
	// constraints by name, last one wins, if a CHECK ever changes.

	for _, check := range []string{
		fmt.Sprintf("timeline_rollout_percent >= 0 AND timeline_rollout_percent <= %d", MaxRolloutPercent),
		fmt.Sprintf("timeline_max_items >= %d AND timeline_max_items <= %d", MinTimelineMaxItems, MaxTimelineMaxItems),
		fmt.Sprintf("fanout_max_followers >= %d", MinFanoutMaxFollowers),
		fmt.Sprintf("relationship_bonus >= 0 AND relationship_bonus <= %d", MaxRelationshipBonus),
		fmt.Sprintf("recency_scale >= 0 AND recency_scale <= %d", MaxRecencyScale),
		fmt.Sprintf("decay_exponent > 0 AND decay_exponent <= %d", MaxDecayExponent),
		fmt.Sprintf("recommendation_weight >= 0 AND recommendation_weight <= %d", MaxRecommendationWeight),
	} {
		if !strings.Contains(normalized, "CHECK ("+check+")") {
			t.Errorf("no CHECK (%s) in the settings migrations — the constant and the column disagree", check)
		}
	}
}

func boolPtr(v bool) *bool { return &v }
