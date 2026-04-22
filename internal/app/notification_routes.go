package app

import (
	"github.com/go-chi/chi/v5"
	"github.com/jarviisha/darkvoid/internal/app/middleware"
)

// RegisterRoutes registers all routes for the Notification context.
func (ctx *NotificationContext) RegisterRoutes(r chi.Router, auth middleware.AuthMiddleware) {
	r.Group(func(r chi.Router) {
		r.Use(auth.Required)
		r.Get("/notifications", ctx.notifHandler.GetNotifications)
		r.Get("/notifications/unread-count", ctx.notifHandler.GetUnreadCount)
		r.Post("/notifications/{notificationID}/read", ctx.notifHandler.MarkAsRead)
		r.Post("/notifications/read-all", ctx.notifHandler.MarkAllAsRead)
	})
}

// RegisterSSERoute registers the SSE stream endpoint outside the normal timeout middleware.
func (ctx *NotificationContext) RegisterSSERoute(r chi.Router, auth middleware.AuthMiddleware) {
	r.Group(func(r chi.Router) {
		r.Use(auth.Required)
		r.Get("/notifications/stream", ctx.notifHandler.Stream)
	})
}
