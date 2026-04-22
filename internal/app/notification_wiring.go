package app

import (
	"github.com/jarviisha/darkvoid/pkg/storage"
)

func (app *Application) setupNotificationContext(store storage.Storage) {
	userReader := buildNotificationUserReader(app.User.Ports().NotificationUserRepo)
	app.Notification = SetupNotificationContext(app.pool, store, userReader, app.redis)
	app.log.Info("notification context initialized", "redis_pubsub", app.redis != nil)
}

func (app *Application) wireNotificationDependencies() {
	app.Post.WireNotificationEmitter(app.Notification)
	app.User.WireNotificationEmitter(app.Notification)
	app.log.Info("notification emitter wired into post and follow services")
}
