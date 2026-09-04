package service

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jarviisha/darkvoid/internal/feature/user/entity"
	"github.com/jarviisha/darkvoid/internal/feature/user/repository"
	"github.com/jarviisha/darkvoid/internal/pagination"
	"github.com/jarviisha/darkvoid/pkg/deps"
	"github.com/jarviisha/darkvoid/pkg/errors"
	"github.com/jarviisha/darkvoid/pkg/logger"
)

var errSelfFollow = errors.New("SELF_FOLLOW", "cannot follow yourself", http.StatusBadRequest)

// FeedInvalidator is a narrow interface for cache invalidation.
// Defined here to avoid importing the feed package (would create a cycle).
type FeedInvalidator interface {
	InvalidateFollowingIDs(ctx context.Context, userID uuid.UUID) error
}

// FollowFeedEventEmitter emits feed-impacting follow events.
type FollowFeedEventEmitter interface {
	EmitFollowCreated(ctx context.Context, followerID, followeeID uuid.UUID) error
	EmitFollowDeleted(ctx context.Context, followerID, followeeID uuid.UUID) error
}

// FollowFeedEventOutbox persists follow feed events in the same transaction as
// the follow-graph mutation.
type FollowFeedEventOutbox interface {
	EnqueueFollowCreated(ctx context.Context, tx pgx.Tx, followerID, followeeID uuid.UUID) error
	EnqueueFollowDeleted(ctx context.Context, tx pgx.Tx, followerID, followeeID uuid.UUID) error
}

type followTxBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// FollowNotificationEmitter is a narrow interface for emitting follow notifications.
// Defined here to avoid importing the notification package (would create a cycle).
type FollowNotificationEmitter interface {
	EmitFollow(ctx context.Context, followerID, followeeID uuid.UUID) error
	DeleteNotification(ctx context.Context, actorID uuid.UUID, groupKey string) error
}

// FollowService handles follow/unfollow business logic.
type FollowService struct {
	followRepo      followRepo
	pool            followTxBeginner
	withTx          func(pgx.Tx) followRepo
	feedInvalidator FeedInvalidator
	feedOutbox      FollowFeedEventOutbox

	// These two cannot arrive through the constructor. The dispatcher's fanout
	// worker reads posts, so the feed context is built after this service; and
	// the notification context is built from the user repository, which
	// SetupUserContext creates alongside this service. Both setters refuse a nil
	// and refuse a second call, because they are the only remaining paths by
	// which this service can come up incompletely wired.
	feedEmitter  FollowFeedEventEmitter
	notifEmitter FollowNotificationEmitter
}

// FollowDeps carries everything FollowService needs at construction.
//
// Repo is the concrete repository rather than the followRepo port because the
// service needs its WithTx method to run a follow mutation and its outbox write
// in one transaction. Recovering that by type-asserting the port is what used to
// defer the failure to the first follow request.
type FollowDeps struct {
	Repo            *repository.FollowRepository
	Pool            followTxBeginner
	FeedInvalidator FeedInvalidator
	FeedOutbox      FollowFeedEventOutbox
}

func (d FollowDeps) validate() error {
	return deps.Missing(map[string]any{
		"Repo":            d.Repo,
		"Pool":            d.Pool,
		"FeedInvalidator": d.FeedInvalidator,
		"FeedOutbox":      d.FeedOutbox,
	})
}

// NewFollowService creates a new FollowService.
func NewFollowService(deps FollowDeps) (*FollowService, error) {
	if err := deps.validate(); err != nil {
		return nil, err
	}
	return &FollowService{
		followRepo:      deps.Repo,
		pool:            deps.Pool,
		withTx:          func(tx pgx.Tx) followRepo { return deps.Repo.WithTx(tx) },
		feedInvalidator: deps.FeedInvalidator,
		feedOutbox:      deps.FeedOutbox,
	}, nil
}

// WireFeedEventEmitter attaches the feed event dispatcher after the feed context
// exists. See the field comment for why it cannot come through the constructor.
func (s *FollowService) WireFeedEventEmitter(e FollowFeedEventEmitter) error {
	if e == nil {
		return errors.New("BAD_WIRING", "follow feed event emitter is nil", http.StatusInternalServerError)
	}
	if s.feedEmitter != nil {
		return errors.New("BAD_WIRING", "follow feed event emitter is already wired", http.StatusInternalServerError)
	}
	s.feedEmitter = e
	return nil
}

// WireNotificationEmitter attaches the notification emitter after the
// notification context exists. See the field comment for why it cannot come
// through the constructor.
func (s *FollowService) WireNotificationEmitter(e FollowNotificationEmitter) error {
	if e == nil {
		return errors.New("BAD_WIRING", "follow notification emitter is nil", http.StatusInternalServerError)
	}
	if s.notifEmitter != nil {
		return errors.New("BAD_WIRING", "follow notification emitter is already wired", http.StatusInternalServerError)
	}
	s.notifEmitter = e
	return nil
}

func (s *FollowService) invalidateFollowingIDs(ctx context.Context, userID uuid.UUID) {
	if s.feedInvalidator == nil {
		return
	}
	if err := s.feedInvalidator.InvalidateFollowingIDs(ctx, userID); err != nil {
		logger.LogError(ctx, err, "failed to invalidate following IDs cache", "user_id", userID)
	}
}

func (s *FollowService) Follow(ctx context.Context, followerID, followeeID uuid.UUID) error {
	if followerID == followeeID {
		return errSelfFollow
	}
	if err := s.persistFollowMutation(ctx, followerID, followeeID, true); err != nil {
		logger.LogError(ctx, err, "failed to follow", "follower", followerID, "followee", followeeID)
		return errors.NewInternalError(err)
	}
	logger.Info(ctx, "followed", "follower", followerID, "followee", followeeID)
	s.invalidateFollowingIDs(ctx, followerID)
	if s.feedOutbox == nil {
		s.emitFollowCreated(ctx, followerID, followeeID)
	}
	s.emitFollowNotification(ctx, followerID, followeeID)
	return nil
}

func (s *FollowService) Unfollow(ctx context.Context, followerID, followeeID uuid.UUID) error {
	if followerID == followeeID {
		return errSelfFollow
	}
	if err := s.persistFollowMutation(ctx, followerID, followeeID, false); err != nil {
		logger.LogError(ctx, err, "failed to unfollow", "follower", followerID, "followee", followeeID)
		return errors.NewInternalError(err)
	}
	logger.Info(ctx, "unfollowed", "follower", followerID, "followee", followeeID)
	s.invalidateFollowingIDs(ctx, followerID)
	if s.feedOutbox == nil {
		s.emitFollowDeleted(ctx, followerID, followeeID)
	}
	s.deleteFollowNotification(ctx, followerID, followeeID)
	return nil
}

func (s *FollowService) persistFollowMutation(ctx context.Context, followerID, followeeID uuid.UUID, created bool) error {
	if s.feedOutbox == nil {
		if created {
			return s.followRepo.Follow(ctx, followerID, followeeID)
		}
		return s.followRepo.Unfollow(ctx, followerID, followeeID)
	}
	if s.pool == nil || s.withTx == nil {
		return fmt.Errorf("feed outbox configured without transaction pool")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	txRepo := s.withTx(tx)
	if created {
		if err := txRepo.Follow(ctx, followerID, followeeID); err != nil {
			return err
		}
		if err := s.feedOutbox.EnqueueFollowCreated(ctx, tx, followerID, followeeID); err != nil {
			return err
		}
	} else {
		if err := txRepo.Unfollow(ctx, followerID, followeeID); err != nil {
			return err
		}
		if err := s.feedOutbox.EnqueueFollowDeleted(ctx, tx, followerID, followeeID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// --- notification helpers (fire-and-forget) ---

func (s *FollowService) emitFollowNotification(ctx context.Context, followerID, followeeID uuid.UUID) {
	if s.notifEmitter == nil {
		return
	}
	if err := s.notifEmitter.EmitFollow(ctx, followerID, followeeID); err != nil {
		logger.LogError(ctx, err, "failed to emit follow notification", "follower", followerID, "followee", followeeID)
	}
}

func (s *FollowService) emitFollowCreated(ctx context.Context, followerID, followeeID uuid.UUID) {
	if s.feedEmitter == nil {
		return
	}
	if err := s.feedEmitter.EmitFollowCreated(ctx, followerID, followeeID); err != nil {
		logger.LogError(ctx, err, "failed to emit follow-created feed event", "follower", followerID, "followee", followeeID)
	}
}

func (s *FollowService) emitFollowDeleted(ctx context.Context, followerID, followeeID uuid.UUID) {
	if s.feedEmitter == nil {
		return
	}
	if err := s.feedEmitter.EmitFollowDeleted(ctx, followerID, followeeID); err != nil {
		logger.LogError(ctx, err, "failed to emit follow-deleted feed event", "follower", followerID, "followee", followeeID)
	}
}

func (s *FollowService) deleteFollowNotification(ctx context.Context, followerID, followeeID uuid.UUID) {
	if s.notifEmitter == nil {
		return
	}
	if err := s.notifEmitter.DeleteNotification(ctx, followerID, fmt.Sprintf("follow:%s", followeeID)); err != nil {
		logger.LogError(ctx, err, "failed to delete follow notification", "follower", followerID, "followee", followeeID)
	}
}

func (s *FollowService) IsFollowing(ctx context.Context, followerID, followeeID uuid.UUID) (bool, error) {
	ok, err := s.followRepo.IsFollowing(ctx, followerID, followeeID)
	if err != nil {
		logger.LogError(ctx, err, "failed to check following", "follower", followerID, "followee", followeeID)
		return false, errors.NewInternalError(err)
	}
	return ok, nil
}

// GetFollowingAmong returns the requested users that followerID currently
// follows. It is intended for bounded response enrichment and performs one
// repository lookup regardless of the number of requested IDs.
func (s *FollowService) GetFollowingAmong(ctx context.Context, followerID uuid.UUID, followeeIDs []uuid.UUID) ([]uuid.UUID, error) {
	if len(followeeIDs) == 0 {
		return nil, nil
	}
	ids, err := s.followRepo.GetFollowingAmong(ctx, followerID, followeeIDs)
	if err != nil {
		logger.LogError(ctx, err, "failed to batch-check following", "follower", followerID, "followee_count", len(followeeIDs))
		return nil, errors.NewInternalError(err)
	}
	return ids, nil
}

func (s *FollowService) GetFollowers(ctx context.Context, targetID uuid.UUID, req pagination.PaginationRequest) ([]*entity.Follow, pagination.PaginationResponse, error) {
	req.Validate()
	follows, err := s.followRepo.GetFollowers(ctx, targetID, req.Limit, req.Offset)
	if err != nil {
		logger.LogError(ctx, err, "failed to get followers", "user_id", targetID)
		return nil, pagination.PaginationResponse{}, errors.NewInternalError(err)
	}
	total, err := s.followRepo.CountFollowers(ctx, targetID)
	if err != nil {
		logger.LogError(ctx, err, "failed to count followers", "user_id", targetID)
		return nil, pagination.PaginationResponse{}, errors.NewInternalError(err)
	}
	return follows, pagination.NewPaginationResponse(total, req.Limit, req.Offset), nil
}

func (s *FollowService) GetFollowingIDs(ctx context.Context, targetID uuid.UUID) ([]uuid.UUID, error) {
	follows, err := s.followRepo.GetFollowing(ctx, targetID, 5000, 0)
	if err != nil {
		logger.LogError(ctx, err, "failed to get following IDs", "user_id", targetID)
		return nil, errors.NewInternalError(err)
	}
	ids := make([]uuid.UUID, len(follows))
	for i, f := range follows {
		ids[i] = f.FolloweeID
	}
	return ids, nil
}

// GetFollowerIDs returns at most limit IDs of users who follow targetID.
func (s *FollowService) GetFollowerIDs(ctx context.Context, targetID uuid.UUID, limit int) ([]uuid.UUID, error) {
	if limit <= 0 {
		return nil, nil
	}
	follows, err := s.followRepo.GetFollowers(ctx, targetID, int32(limit), 0) //nolint:gosec // runtime feed settings validate the fanout cap as a bounded positive integer.
	if err != nil {
		logger.LogError(ctx, err, "failed to get follower IDs", "user_id", targetID)
		return nil, errors.NewInternalError(err)
	}
	ids := make([]uuid.UUID, len(follows))
	for i, f := range follows {
		ids[i] = f.FollowerID
	}
	return ids, nil
}

func (s *FollowService) GetFollowing(ctx context.Context, targetID uuid.UUID, req pagination.PaginationRequest) ([]*entity.Follow, pagination.PaginationResponse, error) {
	req.Validate()
	follows, err := s.followRepo.GetFollowing(ctx, targetID, req.Limit, req.Offset)
	if err != nil {
		logger.LogError(ctx, err, "failed to get following", "user_id", targetID)
		return nil, pagination.PaginationResponse{}, errors.NewInternalError(err)
	}
	total, err := s.followRepo.CountFollowing(ctx, targetID)
	if err != nil {
		logger.LogError(ctx, err, "failed to count following", "user_id", targetID)
		return nil, pagination.PaginationResponse{}, errors.NewInternalError(err)
	}
	return follows, pagination.NewPaginationResponse(total, req.Limit, req.Offset), nil
}
