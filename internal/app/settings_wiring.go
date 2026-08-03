package app

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	settingsRepository "github.com/jarviisha/darkvoid/internal/feature/settings/repository"
)

// settingsLoadTimeout bounds the startup read and each refresh. Both are a single
// indexed one-row SELECT, so a second is generous; the point of the bound is that
// neither may hold up boot or pile up ticks if the database is wedged.
const settingsLoadTimeout = time.Second

func (app *Application) setupSettingsContext() {
	app.Settings = SetupSettingsContext(buildSettingsRepo(app.pool))
	app.log.Info("settings context initialized")
}

// wireSettings connects the settings service to the feed's holder, loads the
// stored settings once, and starts the refresh loop.
//
// The load happens here rather than in setupSettingsContext because it has to
// come after the sink is attached — a read with nothing wired would fetch the
// right values and drop them, and the feed would serve its defaults until the
// first refresh tick.
func (app *Application) wireSettings() {
	app.Settings.WireFeedSettings(app.Feed)

	ctx, cancel := context.WithTimeout(app.runCtx, settingsLoadTimeout)
	defer cancel()

	if err := app.Settings.settingsService.Refresh(ctx); err != nil {
		// Not fatal. These settings shape the feed, they do not gate it: the
		// defaults are a working configuration (timeline serving off, local
		// scoring on), and the refresh loop will pick up the stored values as
		// soon as the database answers. Failing boot here would take the whole
		// API down over a ranking weight.
		//
		// It is logged at ERROR rather than WARN because the visible effect is a
		// silent one — the feed serving different numbers than /admin/settings/feed
		// reports — and this line is the only thing that names it.
		app.log.Error("feed settings load failed, serving defaults until the next refresh",
			"error", err,
			"refresh_interval", app.cfg.Settings.RefreshInterval,
		)
	} else {
		app.log.Info("feed settings loaded", "refresh_interval", app.cfg.Settings.RefreshInterval)
	}

	app.startSettingsRefresher()
}

// startSettingsRefresher re-reads the settings for the life of the process.
//
// It exists for the multi-instance case. The instance that serves a PATCH applies
// the new snapshot itself, but its siblings never see that request — without this
// loop they would keep serving the previous values until their next restart, and
// a rollout percent raised on one instance would apply to a fraction of traffic
// nobody could predict.
//
// It runs unconditionally, including on a single-instance deployment: the cost is
// one indexed one-row SELECT per interval, and making it conditional would mean
// carrying a "how many of me are there" setting that is wrong the moment someone
// scales up.
func (app *Application) startSettingsRefresher() {
	interval := app.cfg.Settings.RefreshInterval
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-app.runCtx.Done():
				return
			case <-ticker.C:
				ctx, cancel := context.WithTimeout(app.runCtx, settingsLoadTimeout)
				err := app.Settings.settingsService.Refresh(ctx)
				cancel()
				if err != nil {
					// Logged every tick rather than once. A settings refresh that
					// has been failing for an hour is a real divergence between
					// what the admin API reports and what the feed is using, and a
					// single line at the start of it goes unnoticed.
					app.log.Error("feed settings refresh failed, keeping the last known values", "error", err)
				}
			}
		}
	}()
}

// buildSettingsRepo is kept alongside the other build* helpers so the pool stays
// the only thing app.go hands to a repository constructor.
func buildSettingsRepo(pool *pgxpool.Pool) *settingsRepository.SettingsRepository {
	return settingsRepository.NewSettingsRepository(pool)
}
