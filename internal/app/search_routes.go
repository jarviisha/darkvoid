package app

import (
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jarviisha/darkvoid/internal/app/middleware"
)

// RegisterRoutes mounts search routes. Search is public.
func (c *SearchContext) RegisterRoutes(r chi.Router, auth middleware.AuthMiddleware) {
	r.Group(func(r chi.Router) {
		r.Use(middleware.RateLimitByIP(120, time.Minute))
		r.Get("/search", c.handler.Search)
	})
}
