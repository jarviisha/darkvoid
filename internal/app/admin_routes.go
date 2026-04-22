package app

import (
	"github.com/go-chi/chi/v5"
	appMiddleware "github.com/jarviisha/darkvoid/internal/app/middleware"
)

// RegisterRoutes registers all /admin/* routes.
func (ctx *AdminContext) RegisterRoutes(r chi.Router, auth appMiddleware.AuthMiddleware) {
	r.Route("/admin", func(r chi.Router) {
		r.Use(auth.Required)
		r.Use(appMiddleware.RequireRole(ctx.adminService, "admin"))
		r.Get("/stats", ctx.adminHandler.GetStats)
		r.Get("/users", ctx.adminHandler.ListUsers)
		r.Get("/users/{id}", ctx.adminHandler.GetUser)
		r.Patch("/users/{id}/status", ctx.adminHandler.SetUserStatus)
		r.Get("/users/{id}/roles", ctx.adminHandler.GetUserRoles)
		r.Post("/users/{id}/roles", ctx.adminHandler.AssignRole)
		r.Delete("/users/{id}/roles/{roleId}", ctx.adminHandler.RemoveRole)
		r.Get("/roles", ctx.adminHandler.ListRoles)
		r.Post("/roles", ctx.adminHandler.CreateRole)
		r.Post("/notifications/users/{id}", ctx.adminHandler.SendNotificationToUser)
		r.Post("/notifications/broadcast", ctx.adminHandler.BroadcastNotification)
	})
}
