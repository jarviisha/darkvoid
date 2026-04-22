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
	codohueClient := app.setupFeedContext(store)
	app.wireFeedDependencies()
	app.wireCodohue(ctx, codohueClient)
	app.setupNotificationContext(store)
	app.wireNotificationDependencies()
	app.setupSearchContext(store)
	app.setupAdminContext(store)

	return nil
}
