package app

import (
	"github.com/go-chi/chi/v5"
	"github.com/jarviisha/darkvoid/internal/app/middleware"
)

// RegisterRoutes registers all routes for the Feed context.
func (ctx *FeedContext) RegisterRoutes(r chi.Router, auth middleware.AuthMiddleware) {
	r.Group(func(r chi.Router) {
		r.Use(auth.Optional)
		r.Get("/discover", ctx.feedHandler.GetDiscover)
	})

	r.Group(func(r chi.Router) {
		r.Use(auth.Required)
		r.Get("/feed", ctx.feedHandler.GetFeed)
	})
}
