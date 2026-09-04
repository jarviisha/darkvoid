package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	post "github.com/jarviisha/darkvoid/internal/feature/post"
	"github.com/jarviisha/darkvoid/internal/feature/post/repository"
	"github.com/jarviisha/darkvoid/pkg/deps"
	"github.com/jarviisha/darkvoid/pkg/errors"
	"github.com/jarviisha/darkvoid/pkg/logger"
)

// LikeService handles like/unlike business logic
type LikeService struct {
	likeRepo       likeRepo
	postRepo       postRepo
	notifEmitter   LikeNotificationEmitter
	followChecker  followChecker
	eventPublisher BehaviorEventPublisher // optional: absent unless Codohue is enabled
}

// LikeDeps carries the dependencies LikeService cannot work without.
type LikeDeps struct {
	Likes         *repository.LikeRepository
	Posts         *repository.PostRepository
	FollowChecker followChecker
	Notifications LikeNotificationEmitter
}

func (d LikeDeps) validate() error {
	return deps.Missing(map[string]any{
		"Likes":         d.Likes,
		"Posts":         d.Posts,
		"FollowChecker": d.FollowChecker,
		"Notifications": d.Notifications,
	})
}

// LikeServiceOption configures the dependencies LikeService can run without.
type LikeServiceOption func(*LikeService)

// WithLikeBehaviorEventPublisher attaches the Codohue behaviour-event publisher.
// Absent on every deployment with CODOHUE_ENABLED unset, which is why it is an
// option rather than a LikeDeps field.
func WithLikeBehaviorEventPublisher(p BehaviorEventPublisher) LikeServiceOption {
	return func(s *LikeService) { s.eventPublisher = p }
}

// NewLikeService creates a new LikeService.
func NewLikeService(deps LikeDeps, opts ...LikeServiceOption) (*LikeService, error) {
	if err := deps.validate(); err != nil {
		return nil, err
	}
	s := &LikeService{
		likeRepo:      deps.Likes,
		postRepo:      &postRepoTxable{deps.Posts},
		followChecker: deps.FollowChecker,
		notifEmitter:  deps.Notifications,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s, nil
}

// Like adds a like from userID to postID
func (s *LikeService) Like(ctx context.Context, userID, postID uuid.UUID) error {
	p, err := getVisiblePost(ctx, s.postRepo, s.followChecker, postID, &userID)
	if err != nil {
		return err
	}
	if p.AuthorID == userID {
		return post.ErrSelfLike
	}

	if err := s.likeRepo.Like(ctx, userID, postID); err != nil {
		logger.LogError(ctx, err, "failed to like post", "user_id", userID, "post_id", postID)
		return errors.NewInternalError(err)
	}
	s.emitLikeNotification(ctx, userID, p.AuthorID, postID)
	s.publishBehaviorEvent(ctx, userID, postID, "LIKE", &p.CreatedAt)
	logger.Info(ctx, "post liked", "user_id", userID, "post_id", postID)
	return nil
}

// Unlike removes a like from userID to postID
func (s *LikeService) Unlike(ctx context.Context, userID, postID uuid.UUID) error {
	if _, err := getVisiblePost(ctx, s.postRepo, s.followChecker, postID, &userID); err != nil {
		return err
	}

	if err := s.likeRepo.Unlike(ctx, userID, postID); err != nil {
		logger.LogError(ctx, err, "failed to unlike post", "user_id", userID, "post_id", postID)
		return errors.NewInternalError(err)
	}
	s.deleteLikeNotification(ctx, userID, postID)
	// No behavior event on unlike: SKIP means "saw it, not interested", while
	// an unlike is usually a mis-tap or a changed mind — feeding it to the
	// recommender as a negative signal teaches it a dislike that never
	// happened. The like it retracts has already been published; the model
	// simply keeps a signal that is now mildly stale.
	logger.Info(ctx, "post unliked", "user_id", userID, "post_id", postID)
	return nil
}

// Toggle likes or unlikes a post depending on current state. Returns true if now liked.
func (s *LikeService) Toggle(ctx context.Context, userID, postID uuid.UUID) (bool, error) {
	p, err := getVisiblePost(ctx, s.postRepo, s.followChecker, postID, &userID)
	if err != nil {
		return false, err
	}
	if p.AuthorID == userID {
		return false, post.ErrSelfLike
	}

	liked, err := s.likeRepo.Toggle(ctx, userID, postID)
	if err != nil {
		logger.LogError(ctx, err, "failed to toggle like", "user_id", userID, "post_id", postID)
		return false, errors.NewInternalError(err)
	}

	if !liked {
		s.deleteLikeNotification(ctx, userID, postID)
		logger.Info(ctx, "post unliked", "user_id", userID, "post_id", postID)
		return false, nil
	}
	s.emitLikeNotification(ctx, userID, p.AuthorID, postID)
	s.publishBehaviorEvent(ctx, userID, postID, "LIKE", &p.CreatedAt)
	logger.Info(ctx, "post liked", "user_id", userID, "post_id", postID)
	return true, nil
}

// publishBehaviorEvent sends a behavior event to the recommendation system (fire-and-forget).
func (s *LikeService) publishBehaviorEvent(ctx context.Context, userID, postID uuid.UUID, action string, objectCreatedAt *time.Time) {
	if s.eventPublisher == nil {
		return
	}
	if err := s.eventPublisher.PublishBehaviorEvent(ctx, userID.String(), postID.String(), action, objectCreatedAt); err != nil {
		logger.LogError(ctx, err, "failed to publish behavior event", "action", action, "user_id", userID, "post_id", postID)
	}
}

// --- notification helpers (fire-and-forget) ---

func (s *LikeService) emitLikeNotification(ctx context.Context, actorID, recipientID, postID uuid.UUID) {
	if s.notifEmitter == nil {
		return
	}
	if err := s.notifEmitter.EmitLike(ctx, actorID, recipientID, postID); err != nil {
		logger.LogError(ctx, err, "failed to emit like notification", "actor", actorID, "post", postID)
	}
}

func (s *LikeService) deleteLikeNotification(ctx context.Context, actorID, postID uuid.UUID) {
	if s.notifEmitter == nil {
		return
	}
	if err := s.notifEmitter.DeleteNotification(ctx, actorID, fmt.Sprintf("like:%s", postID)); err != nil {
		logger.LogError(ctx, err, "failed to delete like notification", "actor", actorID, "post", postID)
	}
}
