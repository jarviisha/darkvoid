package app

import (
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jarviisha/darkvoid/internal/app/middleware"
)

// RegisterRoutes registers all routes for the Post context.
func (ctx *PostContext) RegisterRoutes(r chi.Router, auth middleware.AuthMiddleware) {
	r.Group(func(r chi.Router) {
		r.Get("/posts/{postID}", ctx.postHandler.GetPost)
		r.Get("/users/{userID}/posts", ctx.postHandler.GetUserPosts)
		r.Get("/posts/{postID}/comments", ctx.commentHandler.GetComments)
		r.Get("/posts/{postID}/comments/{commentID}/replies", ctx.commentHandler.GetReplies)

		r.Group(func(r chi.Router) {
			r.Use(middleware.RateLimitByIP(60, time.Minute))
			r.Get("/hashtags/trending", ctx.hashtagHandler.GetTrendingHashtags)
			r.Get("/hashtags/search", ctx.hashtagHandler.SearchHashtags)
			r.Get("/hashtags/{name}/posts", ctx.hashtagHandler.GetPostsByHashtag)
		})
	})

	r.Group(func(r chi.Router) {
		r.Use(auth.Required)
		r.Post("/posts", ctx.postHandler.CreatePost)
		r.Put("/posts/{postID}", ctx.postHandler.UpdatePost)
		r.Delete("/posts/{postID}", ctx.postHandler.DeletePost)
		r.Post("/posts/{postID}/like", ctx.likeHandler.ToggleLike)
		r.Post("/posts/{postID}/comments", ctx.commentHandler.CreateComment)
		r.Delete("/posts/{postID}/comments/{commentID}", ctx.commentHandler.DeleteComment)
		r.Post("/posts/{postID}/comments/{commentID}/like", ctx.commentLikeHandler.ToggleCommentLike)
	})
}
