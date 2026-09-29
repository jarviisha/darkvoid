package app

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/jarviisha/darkvoid/internal/feature/feed"
	feedentity "github.com/jarviisha/darkvoid/internal/feature/feed/entity"
	postentity "github.com/jarviisha/darkvoid/internal/feature/post/entity"
	postservice "github.com/jarviisha/darkvoid/internal/feature/post/service"
	pkgerrors "github.com/jarviisha/darkvoid/pkg/errors"
	"github.com/jarviisha/darkvoid/pkg/logger"
)

// --- Entity conversion ---

// toFeedPost converts a post.entity.Post to feedentity.Post.
// This conversion lives exclusively at the app layer — the only place allowed to know both contexts.
func toFeedPost(p *postentity.Post) *feedentity.Post {
	media := make([]feedentity.PostMedia, len(p.Media))
	for i, m := range p.Media {
		media[i] = feedentity.PostMedia{
			ID:        m.ID,
			PostID:    m.PostID,
			MediaKey:  m.MediaKey,
			MediaType: m.MediaType,
			Position:  m.Position,
			CreatedAt: m.CreatedAt,
		}
	}
	return &feedentity.Post{
		ID:                p.ID,
		AuthorID:          p.AuthorID,
		Content:           p.Content,
		Visibility:        string(p.Visibility),
		CreatedAt:         p.CreatedAt,
		UpdatedAt:         p.UpdatedAt,
		Media:             media,
		LikeCount:         p.LikeCount,
		CommentCount:      p.CommentCount,
		IsLiked:           p.IsLiked,
		IsFollowingAuthor: p.IsFollowingAuthor,
		Author:            p.Author,
	}
}

// --- postReader ---

// postReader implements feed.PostReader using post repositories directly.
type postReader struct {
	postRepo feedPostRepo
	hydrator *postservice.Hydrator
}

type feedPostRepo interface {
	GetFollowingPostsWithCursor(ctx context.Context, authorIDs []uuid.UUID, viewerID uuid.UUID, cursorCreatedAt pgtype.Timestamptz, cursorID uuid.UUID, limit int32) ([]*postentity.Post, error)
	GetTrendingPosts(ctx context.Context, limit int32) ([]*postentity.Post, error)
	GetPostsByIDs(ctx context.Context, ids []uuid.UUID) ([]*postentity.Post, error)
	GetDiscoverWithCursor(ctx context.Context, cursorCreatedAt pgtype.Timestamptz, cursorID uuid.UUID, limit int32) ([]*postentity.Post, error)
}

type feedLikeRepo interface {
	GetLikedPostIDs(ctx context.Context, userID uuid.UUID, postIDs []uuid.UUID) ([]uuid.UUID, error)
}

func (r *postReader) GetFollowingPostsWithCursor(ctx context.Context, authorIDs []uuid.UUID, viewerID uuid.UUID, cursor *feed.FollowingCursor, limit int32) ([]*feedentity.Post, error) {
	var cursorTS pgtype.Timestamptz
	var cursorID uuid.UUID

	if cursor != nil {
		var err error
		cursorTS, cursorID, err = cursor.PgParams()
		if err != nil {
			return nil, pkgerrors.NewBadRequestError("invalid cursor post_id")
		}
	} else {
		cursorTS, cursorID = feed.DefaultDiscoverPgParams()
	}

	posts, err := r.postRepo.GetFollowingPostsWithCursor(ctx, authorIDs, viewerID, cursorTS, cursorID, limit)
	if err != nil {
		return nil, pkgerrors.NewInternalError(err)
	}
	return r.hydrate(ctx, posts, nil), nil
}

func (r *postReader) GetTrendingPosts(ctx context.Context, limit int32) ([]*feedentity.Post, error) {
	posts, err := r.postRepo.GetTrendingPosts(ctx, limit)
	if err != nil {
		return nil, pkgerrors.NewInternalError(err)
	}
	return r.hydrate(ctx, posts, nil), nil
}

func (r *postReader) GetPostsByIDs(ctx context.Context, ids []uuid.UUID) ([]*feedentity.Post, error) {
	posts, err := r.postRepo.GetPostsByIDs(ctx, ids)
	if err != nil {
		return nil, pkgerrors.NewInternalError(err)
	}
	byID := make(map[uuid.UUID]*postentity.Post, len(posts))
	for _, p := range posts {
		byID[p.ID] = p
	}
	ordered := make([]*postentity.Post, 0, len(posts))
	for _, id := range ids {
		if p, ok := byID[id]; ok {
			ordered = append(ordered, p)
		}
	}
	return r.hydrate(ctx, ordered, nil), nil
}

func (r *postReader) GetDiscoverWithCursor(ctx context.Context, cursor *feed.DiscoverCursor, limit int32, viewerID *uuid.UUID) ([]*feedentity.Post, error) {
	var cursorTS pgtype.Timestamptz
	var cursorID uuid.UUID

	if cursor != nil {
		var err error
		cursorTS, cursorID, err = cursor.PgParams()
		if err != nil {
			return nil, pkgerrors.NewBadRequestError("invalid cursor post_id")
		}
	} else {
		cursorTS, cursorID = feed.DefaultDiscoverPgParams()
	}

	posts, err := r.postRepo.GetDiscoverWithCursor(ctx, cursorTS, cursorID, limit)
	if err != nil {
		logger.LogError(ctx, err, "failed to get discover feed")
		return nil, pkgerrors.NewInternalError(err)
	}
	return r.hydrate(ctx, posts, viewerID), nil
}

// hydrate fills media and authors, plus the liked flag when viewerID is set,
// and converts to the feed's view. Following and trending pass nil: trending
// is cached across viewers, and the feed service fills likes for those itself.
func (r *postReader) hydrate(ctx context.Context, posts []*postentity.Post, viewerID *uuid.UUID) []*feedentity.Post {
	if len(posts) == 0 {
		return nil
	}
	r.hydrator.Hydrate(ctx, posts, viewerID, postservice.FieldMedia|postservice.FieldAuthor|postservice.FieldLiked)
	result := make([]*feedentity.Post, len(posts))
	for i, p := range posts {
		result[i] = toFeedPost(p)
	}
	return result
}

// --- followReader ---

// followReader implements feed.FollowReader using FollowService.
type followReader struct {
	followService feedFollowService
}

type feedFollowService interface {
	GetFollowingIDs(ctx context.Context, targetID uuid.UUID) ([]uuid.UUID, error)
	GetFollowerIDs(ctx context.Context, targetID uuid.UUID, limit int) ([]uuid.UUID, error)
}

func (r *followReader) GetFollowingIDs(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error) {
	return r.followService.GetFollowingIDs(ctx, userID)
}

func (r *followReader) GetFollowerIDs(ctx context.Context, targetID uuid.UUID, limit int) ([]uuid.UUID, error) {
	return r.followService.GetFollowerIDs(ctx, targetID, limit)
}

// --- likeReader ---

// likeReader implements feed.LikeReader using LikeRepository.
type likeReader struct {
	likeRepo feedLikeRepo
}

func (r *likeReader) GetLikedPostIDs(ctx context.Context, userID uuid.UUID, postIDs []uuid.UUID) ([]uuid.UUID, error) {
	return r.likeRepo.GetLikedPostIDs(ctx, userID, postIDs)
}
