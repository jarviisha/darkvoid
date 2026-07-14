package app

import "context"

func (app *Application) setupContexts(ctx context.Context) error {
	app.log.Info("initializing bounded contexts")

	store, templates, mailSvc, err := app.setupInfrastructure()
	if err != nil {
		return err
	}

	app.setupUserContext(store, templates, mailSvc)
	app.setupStorageContext(store)
	app.setupPostContext(store)
	if err := app.ensureCodohueNamespaceConfig(ctx); err != nil {
		// Codohue is an auxiliary recommender: a provisioning failure must not
		// take the API down. Disable it for this run — the feed falls back to
		// local scoring, matching how wireCodohue degrades on ping failure.
		app.log.Warn("codohue provisioning failed, disabling codohue for this run",
			"base_url", app.cfg.Codohue.BaseURL,
			"error", err,
		)
		app.cfg.Codohue.Enabled = false
	}
	codohueClient := app.setupFeedContext(store)
	app.wireFeedDependencies()
	app.wireCodohue(ctx, codohueClient)
	app.setupNotificationContext(store)
	app.wireNotificationDependencies()
	app.setupSearchContext(store)
	app.setupAdminContext(store)

	return nil
}
