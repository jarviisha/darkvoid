package app

import (
	settingsHandler "github.com/jarviisha/darkvoid/internal/feature/settings/handler"
	settingsRepository "github.com/jarviisha/darkvoid/internal/feature/settings/repository"
	settingsService "github.com/jarviisha/darkvoid/internal/feature/settings/service"
)

// SettingsContext holds the dependencies of the settings bounded context.
type SettingsContext struct {
	settingsRepo    *settingsRepository.SettingsRepository
	settingsService *settingsService.SettingsService
	settingsHandler *settingsHandler.SettingsHandler
}

// SetupSettingsContext initializes the settings context.
func SetupSettingsContext(repo *settingsRepository.SettingsRepository) *SettingsContext {
	svc := settingsService.NewSettingsService(repo)
	h := settingsHandler.NewSettingsHandler(svc)

	return &SettingsContext{
		settingsRepo:    repo,
		settingsService: svc,
		settingsHandler: h,
	}
}

// WireFeedSettings attaches the feed's settings holder as the destination for
// every snapshot this context reads or writes.
//
// Deferred, like every other cross-context wire: this context is constructed
// before the feed exists, and the alternative — having the settings context
// import the feed — is the coupling the whole layout is arranged to avoid.
func (ctx *SettingsContext) WireFeedSettings(feed *FeedContext) {
	ctx.settingsService.WithFeedSettingsSink(&feedSettingsSink{settings: feed.Ports().Settings})
}
