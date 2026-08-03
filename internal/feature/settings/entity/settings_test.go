package entity

import (
	"os"
	"regexp"
	"strconv"
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

// DefaultFeedSettings is the configuration the feed runs on before the first
// successful read, so it has to be the same configuration the database hands back
// on that read. The migration is the source; this parses it rather than restating
// it, so a changed DEFAULT fails here instead of quietly giving every restart a
// brief window on different numbers.
func TestDefaultFeedSettings_MatchesMigrationDefaults(t *testing.T) {
	raw, err := os.ReadFile("../../../../migrations/settings/000002_create_feed_settings_table.up.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	sql := stripSQLComments(string(raw))
	defaults := DefaultFeedSettings()

	for column, want := range map[string]string{
		"timeline_enabled":         strconv.FormatBool(defaults.TimelineEnabled),
		"timeline_rollout_percent": strconv.Itoa(defaults.TimelineRolloutPercent),
		"timeline_max_items":       strconv.Itoa(defaults.TimelineMaxItems),
		"timeline_ttl_seconds":     strconv.Itoa(int(DurationToSeconds(defaults.TimelineTTL))),
		"timeline_refresh_on_miss": strconv.FormatBool(defaults.TimelineRefreshOnMiss),
		"fanout_enabled":           strconv.FormatBool(defaults.FanoutEnabled),
		"fanout_max_followers":     strconv.Itoa(defaults.FanoutMaxFollowers),
		"relationship_bonus":       strconv.FormatFloat(defaults.RelationshipBonus, 'g', -1, 64),
		"recency_scale":            strconv.FormatFloat(defaults.RecencyScale, 'g', -1, 64),
		"decay_exponent":           strconv.FormatFloat(defaults.DecayExponent, 'g', -1, 64),
	} {
		got, ok := migrationDefault(sql, column)
		if !ok {
			t.Errorf("no DEFAULT found for %s in the migration", column)
			continue
		}
		if !strings.EqualFold(got, want) {
			t.Errorf("%s: migration DEFAULT %s, DefaultFeedSettings %s", column, got, want)
		}
	}
}

// migrationDefault pulls the DEFAULT literal for one column out of the CREATE
// TABLE body.
func migrationDefault(sql, column string) (string, bool) {
	re := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(column) + `\s+[A-Z ]+\s+NOT NULL DEFAULT\s+(\S+?)[\s,]`)
	m := re.FindStringSubmatch(sql)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// stripSQLComments removes -- comments so a number mentioned in prose cannot be
// mistaken for a DEFAULT.
func stripSQLComments(sql string) string {
	var b strings.Builder
	for line := range strings.SplitSeq(sql, "\n") {
		if i := strings.Index(line, "--"); i >= 0 {
			line = line[:i]
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

func boolPtr(v bool) *bool { return &v }
