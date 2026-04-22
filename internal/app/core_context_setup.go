package app

import (
	"github.com/jarviisha/darkvoid/internal/infrastructure/mailer"
	"github.com/jarviisha/darkvoid/pkg/storage"
)

func (app *Application) setupUserContext(store storage.Storage, templates *mailer.Templates, mailSvc mailer.Mailer) {
	app.User = SetupUserContext(app.pool, app.jwtService, store, app.cfg.RefreshToken.Expiry, !app.cfg.IsDevelopment(), mailSvc, templates, app.cfg.Mailer.BaseURL)
	app.log.Info("user context initialized",
		"secure_cookie", !app.cfg.IsDevelopment(),
		"refresh_token_expiry", app.cfg.RefreshToken.Expiry,
	)
}

func (app *Application) setupStorageContext(store storage.Storage) {
	app.Storage = SetupStorageContext(store)
	app.log.Info("storage context initialized")
}

func (app *Application) setupPostContext(store storage.Storage) {
	userPorts := app.User.Ports()
	app.Post = SetupPostContext(app.pool, store, userPorts.PostUserRepo, app.redis)
	app.Post.WireFollowChecker(userPorts.PostFollowService)
	app.log.Info("post context initialized", "hashtag_cache", app.redis != nil)
}
