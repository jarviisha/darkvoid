package app

import (
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
	}
	if got != want {
		t.Fatalf("published snapshot = %+v, want %+v", got, want)
	}
}

// The feed's defaults are what it serves before the first settings read succeeds,
// and the entity's are what the database hands back on that read. If they
// disagree, every restart spends a moment ranking on different numbers than the
// admin API reports — a discrepancy with no error attached to it.
func TestFeedDefaults_MatchSettingsDefaults(t *testing.T) {
	settings := feed.NewSettings(feed.DefaultRuntimeSettings())
	sink := &feedSettingsSink{settings: settings}

	sink.ApplyFeedSettings(settingsentity.DefaultFeedSettings())

	if got, want := settings.Get(), feed.DefaultRuntimeSettings(); got != want {
		t.Fatalf("entity defaults map to %+v, but feed defaults are %+v", got, want)
	}
}
