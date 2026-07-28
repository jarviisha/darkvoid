package app

import (
	"github.com/go-chi/chi/v5"
	appMiddleware "github.com/jarviisha/darkvoid/internal/app/middleware"
)

// registerAdminRoutes mounts /admin/bots/* for operators.
//
// It takes the /admin group's router, from inside AdminContext.RegisterRoutes, so
// these routes inherit that group's auth and admin role check. The nesting is
// load-bearing: chi accepts r.Route("/admin/bots") declared as a sibling of
// r.Route("/admin") and resolves it correctly, but a sibling subrouter does not
// inherit the parent group's middleware, so the entire operator surface would
// answer unauthenticated. Both properties are pinned in bot_routes_test.go.
//
// Static segments are registered alongside /{id}; chi's trie prefers the static
// match, so /admin/bots/config is not swallowed by the persona routes.
func (ctx *BotContext) registerAdminRoutes(r chi.Router) {
	r.Route("/bots", func(r chi.Router) {
		r.Get("/", ctx.botHandler.ListBots)
		r.Post("/", ctx.botHandler.CreateBot)

		r.Get("/config", ctx.botHandler.GetConfig)
		r.Put("/config", ctx.botHandler.UpdateConfig)

		r.Post("/pause", ctx.botHandler.Pause)
		r.Post("/resume", ctx.botHandler.Resume)

		r.Get("/runs", ctx.botHandler.ListRuns)

		r.Route("/topics", func(r chi.Router) {
			r.Get("/", ctx.botHandler.ListTopics)
			r.Post("/", ctx.botHandler.CreateTopic)
			r.Patch("/{id}", ctx.botHandler.SetTopicEnabled)
			r.Delete("/{id}", ctx.botHandler.DeleteTopic)
		})

		r.Patch("/{id}", ctx.botHandler.UpdateBot)
		r.Delete("/{id}", ctx.botHandler.DeleteBot)
		r.Post("/{id}/run-now", ctx.botHandler.RequestRun)
	})
}

// RegisterRoutes mounts the /bot agent plane the bot process polls.
//
// This is a group of its own rather than part of /admin: it is guarded by the bot
// role, so an admin token is rejected here and a bot token is rejected on /admin.
// It must be registered inside the timeout-bearing route group — nothing here
// streams.
func (ctx *BotContext) RegisterRoutes(r chi.Router, auth appMiddleware.AuthMiddleware, roleChecker appMiddleware.RoleChecker) {
	r.Route("/bot", func(r chi.Router) {
		r.Use(auth.Required)
		r.Use(appMiddleware.RequireRole(roleChecker, botRoleName))
		r.Get("/plan", ctx.botHandler.GetPlan)
		r.Post("/runs", ctx.botHandler.ReportRun)
		r.Post("/{id}/identity", ctx.botHandler.LinkIdentity)
	})
}
