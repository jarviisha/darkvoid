package app

import (
	"github.com/go-chi/chi/v5"
)

// registerAdminRoutes mounts /admin/settings/* for operators.
//
// It takes the /admin group's router, from inside AdminContext.RegisterRoutes, so
// these routes inherit that group's auth and admin role check. The nesting is
// load-bearing: chi resolves a sibling r.Route("/admin/settings") to the right path
// but gives it none of the parent's middleware, which would leave the feed's
// ranking weights and rollout percent writable by anyone. Pinned in
// settings_routes_test.go and routes_test.go.
//
// There is no /settings group outside /admin. These are operator controls with no
// user-facing half, so there is nothing to expose under a second role.
func (ctx *SettingsContext) registerAdminRoutes(r chi.Router) {
	r.Route("/settings", func(r chi.Router) {
		r.Get("/feed", ctx.settingsHandler.GetFeedSettings)
		r.Patch("/feed", ctx.settingsHandler.UpdateFeedSettings)
	})
}
