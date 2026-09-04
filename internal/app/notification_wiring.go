package app

import (
	"github.com/jarviisha/darkvoid/pkg/storage"
)

func (app *Application) setupNotificationContext(store storage.Storage) {
	userReader := buildNotificationUserReader(app.User.Ports().NotificationUserRepo)
	app.Notification = SetupNotificationContext(app.pool, store, userReader, app.redis)
	app.log.Info("notification context initialized", "redis_pubsub", app.redis != nil)
}

// wireFollowNotificationEmitter is all that remains of the notification wiring:
// the post services take the emitter at construction, but the follow service
// cannot, because the notification context is built from the user repository
// that SetupUserContext creates alongside it.
func (app *Application) wireFollowNotificationEmitter() error {
	if err := app.User.WireNotificationEmitter(app.Notification); err != nil {
		return err
	}
	app.log.Info("notification emitter wired into the follow service")
	return nil
}
