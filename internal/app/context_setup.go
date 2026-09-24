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
		// Provisioning creates the namespace; the key is configuration and is
		// already in hand, so a failure here means Codohue was briefly
		// unreachable rather than unusable. Carry on wired and degraded and let
		// the monitor notice it recover. The branch that disabled Codohue when no
		// key was configured is gone with the model that produced it: validateCodohue
		// refuses that boot outright, so by here an enabled Codohue always has one.
		app.log.Error("codohue provisioning failed, serving degraded with the configured namespace key",
			"base_url", app.cfg.Codohue.BaseURL,
			"error", provErr,
		)
	}

	// After provisioning, so the namespace exists before anything calls into it,
	// and before Post, whose services ingest into the catalog with this same
	// client. Provisioning no longer supplies the namespace key — we send it —
	// so this order is about the namespace, not about a value being filled in.
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
