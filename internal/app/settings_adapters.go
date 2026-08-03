package app

import (
	"github.com/jarviisha/darkvoid/internal/feature/feed"
	settingsentity "github.com/jarviisha/darkvoid/internal/feature/settings/entity"
)

// feedSettingsSink translates a settings snapshot into the feed's own runtime
// type and publishes it.
//
// The translation is the point of the adapter: the two contexts describe the same
// knobs, but neither imports the other, so this is the one place that knows the
// field names line up. A rename on either side fails to compile here rather than
// silently leaving a knob at its default.
//
// Note what is not copied — UpdatedBy and UpdatedAt. They are audit fields for the
// admin API; the feed has no use for them, and carrying them through would make
// every save look like a settings change to anything comparing snapshots.
type feedSettingsSink struct {
	settings *feed.Settings
}

func (s *feedSettingsSink) ApplyFeedSettings(fs settingsentity.FeedSettings) {
	s.settings.Set(feed.RuntimeSettings{
		TimelineEnabled:        fs.TimelineEnabled,
		TimelineRolloutPercent: fs.TimelineRolloutPercent,
		TimelineMaxItems:       fs.TimelineMaxItems,
		TimelineTTL:            fs.TimelineTTL,
		TimelineRefreshOnMiss:  fs.TimelineRefreshOnMiss,
		FanoutEnabled:          fs.FanoutEnabled,
		FanoutMaxFollowers:     fs.FanoutMaxFollowers,
		Scorer: feed.ScorerConfig{
			RelationshipBonus: fs.RelationshipBonus,
			RecencyScale:      fs.RecencyScale,
			DecayExponent:     fs.DecayExponent,
		},
	})
}
