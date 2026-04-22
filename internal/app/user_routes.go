package app

import (
	"github.com/go-chi/chi/v5"
	"github.com/jarviisha/darkvoid/internal/app/middleware"
)

// RegisterRoutes registers all routes for the User context.
func (ctx *UserContext) RegisterRoutes(r chi.Router, auth middleware.AuthMiddleware) {
	r.Route("/auth", func(r chi.Router) {
		r.Post("/register", ctx.authHandler.Register)
		r.Post("/login", ctx.authHandler.Login)
		r.Post("/refresh", ctx.authHandler.RefreshToken)
		r.Post("/logout", ctx.authHandler.Logout)
		r.Post("/verify-email", ctx.emailHandler.VerifyEmail)
		r.Post("/resend-verification", ctx.emailHandler.ResendVerification)
		r.Post("/forgot-password", ctx.emailHandler.ForgotPassword)
		r.Post("/reset-password", ctx.emailHandler.ResetPassword)
	})

	r.Group(func(r chi.Router) {
		r.Use(auth.Required)
		r.Get("/auth/me", ctx.authHandler.GetMe)
		r.Post("/auth/logout-all", ctx.authHandler.LogoutAllSessions)
		r.Put("/auth/password", ctx.authHandler.ChangePassword)
	})

	r.Group(func(r chi.Router) {
		r.Use(auth.Required)
		r.Get("/me", ctx.profileHandler.GetMyProfile)
		r.Put("/me", ctx.profileHandler.UpdateMyProfile)
		r.Put("/me/avatar", ctx.profileHandler.UploadAvatar)
		r.Put("/me/cover", ctx.profileHandler.UploadCover)
	})

	r.Route("/users/{userKey}", func(r chi.Router) {
		r.With(auth.Optional).Get("/profile", ctx.profileHandler.GetUserProfile)
		r.Get("/followers", ctx.followHandler.GetFollowers)
		r.Get("/following", ctx.followHandler.GetFollowing)

		r.Group(func(r chi.Router) {
			r.Use(auth.Required)
			r.Get("/", ctx.userHandler.GetUser)
			r.Put("/", ctx.userHandler.UpdateUser)
			r.Delete("/", ctx.userHandler.DeactivateUser)
			r.Post("/follow", ctx.followHandler.Follow)
			r.Delete("/follow", ctx.followHandler.Unfollow)
		})
	})
}
