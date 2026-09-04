package app

import "context"

func (app *Application) setupContexts(ctx context.Context) error {
	app.log.Info("initializing bounded contexts")

	store, mail, err := app.setupInfrastructure(ctx)
	if err != nil {
		return err
	}

	// The feed cache and the feed outbox come first because they need nothing
	// from any context — Redis and the pool respectively — while the follow
	// service and the post services take both as constructor arguments.
	feedCache, feedOutbox := app.setupFeedInfra()

	if err := app.setupUserContext(store, mail, feedCache, feedOutbox); err != nil {
		return err
	}
	if err := app.wireMailDependencies(mail); err != nil {
		return err
	}
	app.setupStorageContext(store)

	// Notification before Post: it reads the user repository and nothing else,
	// while four post services take its emitter at construction.
	app.setupNotificationContext(store)
	if err := app.wireFollowNotificationEmitter(); err != nil {
		return err
	}

	if provErr := app.ensureCodohueNamespaceConfig(ctx); provErr != nil {
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
				"error", provErr,
			)
		} else {
			app.log.Error("codohue provisioning failed and no namespace key is configured, disabling codohue",
				"base_url", app.cfg.Codohue.BaseURL,
				"error", provErr,
			)
			app.cfg.Codohue.Enabled = false
		}
	}

	// After provisioning, which is what fills in cfg.Codohue.NamespaceKey, and
	// before Post, whose services ingest into the catalog with this same client.
	codohueClient, clientErr := app.setupCodohueClient()
	if clientErr != nil {
		return clientErr
	}

	if err := app.setupPostContext(store, feedCache, feedOutbox, codohueClient); err != nil {
		return err
	}
	app.setupFeedContext(store, feedCache, feedOutbox, codohueClient)
	if err := app.wireFeedDependencies(); err != nil {
		return err
	}

	// After the feed context: the settings context owns the feed's runtime knobs,
	// so it needs the holder the feed just built. Before the server serves, so the
	// first request already sees the stored values rather than the defaults.
	app.setupSettingsContext()
	app.wireSettings()
	app.wireCodohue(ctx, codohueClient)
	app.setupSearchContext(store)
	if err := app.setupAdminContext(store); err != nil {
		return err
	}

	return nil
}
