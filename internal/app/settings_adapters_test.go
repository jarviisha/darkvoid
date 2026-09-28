package app

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/feature/feed"
	settingsentity "github.com/jarviisha/darkvoid/internal/feature/settings/entity"
)

// The adapter is the only place that knows the two contexts describe the same
// knobs, so a field dropped here is a setting that reads back correctly from
// /admin/settings/feed and never reaches the feed. Every value below is
// deliberately different from the default, so a field left uncopied fails rather
// than coincidentally matching.
func TestFeedSettingsSink_CopiesEveryKnob(t *testing.T) {
	settings := feed.NewSettings(feed.DefaultRuntimeSettings())
	sink := &feedSettingsSink{settings: settings}
	admin := uuid.New()

	sink.ApplyFeedSettings(settingsentity.FeedSettings{
		TimelineEnabled:        true,
		TimelineRolloutPercent: 37,
		TimelineMaxItems:       555,
		TimelineTTL:            3 * time.Hour,
		TimelineRefreshOnMiss:  false,
		FanoutEnabled:          false,
		FanoutMaxFollowers:     4242,
		RelationshipBonus:      3.5,
		RecencyScale:           7.25,
		DecayExponent:          2.75,
		RecommendationWeight:   0.5,
		UpdatedBy:              &admin,
		UpdatedAt:              time.Now().UTC(),
	})

	got := settings.Get()
	want := feed.RuntimeSettings{
		TimelineEnabled:        true,
		TimelineRolloutPercent: 37,
		TimelineMaxItems:       555,
		TimelineTTL:            3 * time.Hour,
		TimelineRefreshOnMiss:  false,
		FanoutEnabled:          false,
		FanoutMaxFollowers:     4242,
		Scorer: feed.ScorerConfig{
			RelationshipBonus: 3.5,
			RecencyScale:      7.25,
			DecayExponent:     2.75,
		},
		RecommendationWeight: 0.5,
	}
	if got != want {
		t.Fatalf("published snapshot = %+v, want %+v", got, want)
	}
}

// The feed's defaults are what it serves before the first settings read
// succeeds, and the column DEFAULTs are what the database hands back on that
// read. If they disagree, every restart spends a moment ranking on different
// numbers than the admin API reports — a discrepancy with no error attached to
// it. The migrations are parsed rather than restated, so a changed DEFAULT fails
// here — whether it changed by ADD COLUMN or by a later SET DEFAULT. Every
// settings migration is read in apply order, so a column added later is covered
// by adding one line to the map below.
func TestFeedDefaults_MatchMigrationDefaults(t *testing.T) {
	files, err := filepath.Glob("../../migrations/settings/*.up.sql")
	if err != nil || len(files) == 0 {
		t.Fatalf("glob settings migrations: %v (found %d)", err, len(files))
	}
	var sql strings.Builder
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		sql.WriteString(stripSQLComments(string(raw)))
	}
	d := feed.DefaultRuntimeSettings()
	float := func(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }

	for column, want := range map[string]string{
		"timeline_enabled":         strconv.FormatBool(d.TimelineEnabled),
		"timeline_rollout_percent": strconv.Itoa(d.TimelineRolloutPercent),
		"timeline_max_items":       strconv.Itoa(d.TimelineMaxItems),
		"timeline_ttl_seconds":     strconv.Itoa(int(settingsentity.DurationToSeconds(d.TimelineTTL))),
		"timeline_refresh_on_miss": strconv.FormatBool(d.TimelineRefreshOnMiss),
		"fanout_enabled":           strconv.FormatBool(d.FanoutEnabled),
		"fanout_max_followers":     strconv.Itoa(d.FanoutMaxFollowers),
		"relationship_bonus":       float(d.Scorer.RelationshipBonus),
		"recency_scale":            float(d.Scorer.RecencyScale),
		"decay_exponent":           float(d.Scorer.DecayExponent),
		"recommendation_weight":    float(d.RecommendationWeight),
	} {
		got, ok := migrationDefault(sql.String(), column)
		if !ok {
			t.Errorf("no DEFAULT found for %s in the settings migrations", column)
			continue
		}
		if !strings.EqualFold(got, want) {
			t.Errorf("%s: migration DEFAULT %s, feed.DefaultRuntimeSettings %s", column, got, want)
		}
	}
}

// migrationDefault returns the DEFAULT literal a column ends up with: from its
// definition in a CREATE TABLE or ADD COLUMN, or from a later ALTER COLUMN …
// SET DEFAULT. The sql is the migrations in apply order, so the last match wins.
func migrationDefault(sql, column string) (string, bool) {
	col := regexp.QuoteMeta(column)
	re := regexp.MustCompile(`(?i)\b` + col + `\s+[A-Z ]+\s+NOT NULL DEFAULT\s+(\S+?)[\s,;]` +
		`|ALTER\s+COLUMN\s+` + col + `\s+SET\s+DEFAULT\s+(\S+?)[\s,;]`)
	matches := re.FindAllStringSubmatch(sql, -1)
	if len(matches) == 0 {
		return "", false
	}
	last := matches[len(matches)-1]
	if last[1] != "" {
		return last[1], true
	}
	return last[2], true
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

// Deployed migrations are immutable, so a default changes by a later
// ALTER COLUMN … SET DEFAULT rather than an edit to the CREATE TABLE. The helper
// has to read that form and let the latest statement win, or the drift test keeps
// comparing against a value the database no longer has.
func TestMigrationDefault_LaterSetDefaultWins(t *testing.T) {
	sql := stripSQLComments(`
CREATE TABLE settings.feed (
    decay_exponent DOUBLE PRECISION NOT NULL DEFAULT 1.5
        CHECK (decay_exponent > 0),
    recency_scale  DOUBLE PRECISION NOT NULL DEFAULT 20
);
ALTER TABLE settings.feed ALTER COLUMN decay_exponent SET DEFAULT 2;
`)
	if got, ok := migrationDefault(sql, "decay_exponent"); !ok || got != "2" {
		t.Fatalf("decay_exponent = %q (found %v), want the later SET DEFAULT 2", got, ok)
	}
	if got, ok := migrationDefault(sql, "recency_scale"); !ok || got != "20" {
		t.Fatalf("recency_scale = %q (found %v), want 20", got, ok)
	}
}
