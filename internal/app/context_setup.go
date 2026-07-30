package app

import "context"

func (app *Application) setupContexts(ctx context.Context) error {
	app.log.Info("initializing bounded contexts")

	store, mail, err := app.setupInfrastructure()
	if err != nil {
		return err
	}

	app.setupUserContext(store, mail)
	app.wireMailDependencies(mail)
	app.setupStorageContext(store)
	app.setupPostContext(store)
	if err := app.ensureCodohueNamespaceConfig(ctx); err != nil {
		// Codohue is an auxiliary recommender: a provisioning failure must not
		// take the API down.
		//
		// Provisioning only creates the namespace and hands back its key. With a
		// key already configured there is nothing left to wait for, so a failure
		// here means Codohue was briefly unreachable, not that it is unusable —
		// carry on wired and degraded, and let the monitor notice it recover.
		// Without a key nothing can authenticate, and only then is it off.
		if app.cfg.Codohue.NamespaceKey != "" {
			app.log.Error("codohue provisioning failed, serving degraded with the configured namespace key",
				"base_url", app.cfg.Codohue.BaseURL,
				"error", err,
			)
		} else {
			app.log.Error("codohue provisioning failed and no namespace key is configured, disabling codohue",
				"base_url", app.cfg.Codohue.BaseURL,
				"error", err,
			)
			app.cfg.Codohue.Enabled = false
		}
	}
	codohueClient := app.setupFeedContext(store)
	app.wireFeedDependencies()
	app.wireCodohue(ctx, codohueClient)
	app.setupNotificationContext(store)
	app.wireNotificationDependencies()
	app.setupSearchContext(store)
	app.setupAdminContext(store)
	// After the post context: the bot activity log reads post previews through it.
	app.setupBotContext()

	return nil
}
