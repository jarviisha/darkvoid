package app

import (
	"github.com/jarviisha/darkvoid/pkg/storage"
)

func (app *Application) setupUserContext(store storage.Storage, mail *mailInfra) {
	app.User = SetupUserContext(app.pool, app.jwtService, store, app.cfg.RefreshToken.Expiry, app.cfg.Cookie, mail)
	// The resolved cookie attributes are logged, not the raw variables: Secure is
	// derived from ENVIRONMENT unless COOKIE_SECURE overrides it, and a wrong
	// value shows up as a missing cookie in a browser rather than as an error
	// here. The boot log is the cheapest place to see what was actually applied.
	app.log.Info("user context initialized",
		"cookie_secure", app.cfg.Cookie.Secure,
		"cookie_samesite", app.cfg.Cookie.SameSite,
		"cookie_domain", app.cfg.Cookie.Domain,
		"refresh_token_expiry", app.cfg.RefreshToken.Expiry,
	)
}

// wireMailDependencies gives the suppression gate its source of truth.
//
// Deferred because the mailer is built before the user context that owns the
// suppression table exists. Until this runs the gate passes everything through,
// which is correct: no request is served yet, and the bootstrap mail (if any)
// predates any suppression.
func (app *Application) wireMailDependencies(mail *mailInfra) {
	mail.gate.WithChecker(app.User.SuppressionChecker())
	app.log.Info("mail suppression gate wired")
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
