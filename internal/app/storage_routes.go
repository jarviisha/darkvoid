package app

import (
	"github.com/go-chi/chi/v5"
	"github.com/jarviisha/darkvoid/internal/app/middleware"
)

// RegisterRoutes registers all routes for the Storage context.
func (ctx *StorageContext) RegisterRoutes(r chi.Router, auth middleware.AuthMiddleware) {
	r.Group(func(r chi.Router) {
		r.Use(auth.Required)
		r.Post("/media/upload", ctx.MediaHandler.Upload)
	})
}
