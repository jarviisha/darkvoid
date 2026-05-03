package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/feature/user/entity"
	"github.com/jarviisha/darkvoid/internal/pagination"
	"github.com/jarviisha/darkvoid/pkg/errors"
)

// --------------------------------------------------------------------------
// Mocks
// --------------------------------------------------------------------------

type mockFollowRepo struct {
	follow         func(ctx context.Context, followerID, followeeID uuid.UUID) error
	unfollow       func(ctx context.Context, followerID, followeeID uuid.UUID) error
	isFollowing    func(ctx context.Context, followerID, followeeID uuid.UUID) (bool, error)
	getFollowers   func(ctx context.Context, targetID uuid.UUID, limit, offset int32) ([]*entity.Follow, error)
	getFollowing   func(ctx context.Context, targetID uuid.UUID, limit, offset int32) ([]*entity.Follow, error)
	countFollowers func(ctx context.Context, targetID uuid.UUID) (int64, error)
	countFollowing func(ctx context.Context, targetID uuid.UUID) (int64, error)
}

func (m *mockFollowRepo) Follow(ctx context.Context, followerID, followeeID uuid.UUID) error {
	if m.follow != nil {
		return m.follow(ctx, followerID, followeeID)
	}
	return nil
}
func (m *mockFollowRepo) Unfollow(ctx context.Context, followerID, followeeID uuid.UUID) error {
	if m.unfollow != nil {
		return m.unfollow(ctx, followerID, followeeID)
	}
	return nil
}
func (m *mockFollowRepo) IsFollowing(ctx context.Context, followerID, followeeID uuid.UUID) (bool, error) {
	if m.isFollowing != nil {
		return m.isFollowing(ctx, followerID, followeeID)
	}
	return false, nil
}
func (m *mockFollowRepo) GetFollowers(ctx context.Context, targetID uuid.UUID, limit, offset int32) ([]*entity.Follow, error) {
	if m.getFollowers != nil {
		return m.getFollowers(ctx, targetID, limit, offset)
	}
	return nil, nil
}
func (m *mockFollowRepo) GetFollowing(ctx context.Context, targetID uuid.UUID, limit, offset int32) ([]*entity.Follow, error) {
	if m.getFollowing != nil {
		return m.getFollowing(ctx, targetID, limit, offset)
	}
	return nil, nil
}
func (m *mockFollowRepo) CountFollowers(ctx context.Context, targetID uuid.UUID) (int64, error) {
	if m.countFollowers != nil {
		return m.countFollowers(ctx, targetID)
	}
	return 0, nil
}
func (m *mockFollowRepo) CountFollowing(ctx context.Context, targetID uuid.UUID) (int64, error) {
	if m.countFollowing != nil {
		return m.countFollowing(ctx, targetID)
	}
	return 0, nil
}

type mockFeedInvalidator struct {
	invalidate func(ctx context.Context, userID uuid.UUID) error
}

func (m *mockFeedInvalidator) InvalidateFollowingIDs(ctx context.Context, userID uuid.UUID) error {
	if m.invalidate != nil {
		return m.invalidate(ctx, userID)
	}
	return nil
}

type mockNotifEmitter struct {
	emitFollow         func(ctx context.Context, followerID, followeeID uuid.UUID) error
	deleteNotification func(ctx context.Context, actorID uuid.UUID, groupKey string) error
}

func (m *mockNotifEmitter) EmitFollow(ctx context.Context, followerID, followeeID uuid.UUID) error {
	if m.emitFollow != nil {
		return m.emitFollow(ctx, followerID, followeeID)
	}
	return nil
}
func (m *mockNotifEmitter) DeleteNotification(ctx context.Context, actorID uuid.UUID, groupKey string) error {
	if m.deleteNotification != nil {
		return m.deleteNotification(ctx, actorID, groupKey)
	}
	return nil
}

type mockFollowFeedEmitter struct {
	createdFollower uuid.UUID
	createdFollowee uuid.UUID
	deletedFollower uuid.UUID
	deletedFollowee uuid.UUID
	createCalls     int
	deleteCalls     int
	err             error
}

func (m *mockFollowFeedEmitter) EmitFollowCreated(_ context.Context, followerID, followeeID uuid.UUID) error {
	m.createCalls++
	m.createdFollower = followerID
	m.createdFollowee = followeeID
	return m.err
}

func (m *mockFollowFeedEmitter) EmitFollowDeleted(_ context.Context, followerID, followeeID uuid.UUID) error {
	m.deleteCalls++
	m.deletedFollower = followerID
	m.deletedFollowee = followeeID
	return m.err
}

func newFollowService(repo followRepo) *FollowService {
	return NewFollowService(repo)
}

// --------------------------------------------------------------------------
// Follow tests
// --------------------------------------------------------------------------

// Self-follow is rejected before the DB is ever touched — enforced at service level,
// not relying on a DB constraint.
func TestFollow_SelfFollow(t *testing.T) {
	id := uuid.New()
	followCalled := false
	svc := newFollowService(&mockFollowRepo{
		follow: func(_ context.Context, _, _ uuid.UUID) error {
			followCalled = true
			return nil
		},
	})

	err := svc.Follow(context.Background(), id, id)
	if err == nil {
		t.Fatal("expected error for self-follow, got nil")
	}
	assertServiceErrorCode(t, err, "SELF_FOLLOW")
	if followCalled {
		t.Error("repo Follow must not be called when self-follow is detected")
	}
}

func TestFollow_Success(t *testing.T) {
	followerID, followeeID := uuid.New(), uuid.New()
	invalidatedID := uuid.Nil
	emittedFollower, emittedFollowee := uuid.Nil, uuid.Nil

	svc := newFollowService(&mockFollowRepo{})
	svc.WithFeedInvalidator(&mockFeedInvalidator{
		invalidate: func(_ context.Context, userID uuid.UUID) error {
			invalidatedID = userID
			return nil
		},
	})
	svc.WithNotificationEmitter(&mockNotifEmitter{
		emitFollow: func(_ context.Context, fwer, fwee uuid.UUID) error {
			emittedFollower = fwer
			emittedFollowee = fwee
			return nil
		},
	})

	if err := svc.Follow(context.Background(), followerID, followeeID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The follower's following-IDs cache must be invalidated so the feed reflects the new follow.
	if invalidatedID != followerID {
		t.Errorf("expected cache invalidation for follower %v, got %v", followerID, invalidatedID)
	}
	if emittedFollower != followerID || emittedFollowee != followeeID {
		t.Errorf("expected notification emitted for (%v→%v), got (%v→%v)",
			followerID, followeeID, emittedFollower, emittedFollowee)
	}
}

func TestFollow_EmitsFeedEventAfterSuccess(t *testing.T) {
	followerID, followeeID := uuid.New(), uuid.New()
	emitter := &mockFollowFeedEmitter{}
	svc := newFollowService(&mockFollowRepo{})
	svc.WithFeedEventEmitter(emitter)

	if err := svc.Follow(context.Background(), followerID, followeeID); err != nil {
		t.Fatalf("Follow: %v", err)
	}
	if emitter.createCalls != 1 || emitter.createdFollower != followerID || emitter.createdFollowee != followeeID {
		t.Fatalf("created feed event mismatch: %+v", emitter)
	}
}

func TestFollow_FeedEmitterFailureIsNonFatal(t *testing.T) {
	svc := newFollowService(&mockFollowRepo{})
	svc.WithFeedEventEmitter(&mockFollowFeedEmitter{err: fmt.Errorf("queue full")})

	if err := svc.Follow(context.Background(), uuid.New(), uuid.New()); err != nil {
		t.Fatalf("Follow should ignore feed emitter error: %v", err)
	}
}

func TestFollow_RepoError(t *testing.T) {
	svc := newFollowService(&mockFollowRepo{
		follow: func(_ context.Context, _, _ uuid.UUID) error {
			return fmt.Errorf("db down")
		},
	})

	err := svc.Follow(context.Background(), uuid.New(), uuid.New())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "INTERNAL_ERROR")
}

// Cache invalidation errors are logged and swallowed — they must not bubble up
// and fail the Follow call.
func TestFollow_CacheInvalidationError_DoesNotFail(t *testing.T) {
	svc := newFollowService(&mockFollowRepo{})
	svc.WithFeedInvalidator(&mockFeedInvalidator{
		invalidate: func(_ context.Context, _ uuid.UUID) error {
			return errors.ErrInternal
		},
	})

	if err := svc.Follow(context.Background(), uuid.New(), uuid.New()); err != nil {
		t.Fatalf("cache invalidation error must not propagate: %v", err)
	}
}

// When no FeedInvalidator is wired (nil), Follow must not panic.
func TestFollow_NilFeedInvalidator_NoPanic(t *testing.T) {
	svc := newFollowService(&mockFollowRepo{})
	// no WithFeedInvalidator call

	if err := svc.Follow(context.Background(), uuid.New(), uuid.New()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// When no NotificationEmitter is wired (nil), Follow must not panic.
func TestFollow_NilNotifEmitter_NoPanic(t *testing.T) {
	svc := newFollowService(&mockFollowRepo{})
	// no WithNotificationEmitter call

	if err := svc.Follow(context.Background(), uuid.New(), uuid.New()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --------------------------------------------------------------------------
// Unfollow tests
// --------------------------------------------------------------------------

func TestUnfollow_SelfFollow(t *testing.T) {
	id := uuid.New()
	unfollowCalled := false
	svc := newFollowService(&mockFollowRepo{
		unfollow: func(_ context.Context, _, _ uuid.UUID) error {
			unfollowCalled = true
			return nil
		},
	})

	err := svc.Unfollow(context.Background(), id, id)
	if err == nil {
		t.Fatal("expected error for self-unfollow, got nil")
	}
	assertServiceErrorCode(t, err, "SELF_FOLLOW")
	if unfollowCalled {
		t.Error("repo Unfollow must not be called when self-follow is detected")
	}
}

func TestUnfollow_Success(t *testing.T) {
	followerID, followeeID := uuid.New(), uuid.New()
	invalidatedID := uuid.Nil

	svc := newFollowService(&mockFollowRepo{})
	svc.WithFeedInvalidator(&mockFeedInvalidator{
		invalidate: func(_ context.Context, userID uuid.UUID) error {
			invalidatedID = userID
			return nil
		},
	})

	if err := svc.Unfollow(context.Background(), followerID, followeeID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// After unfollowing, the follower's feed cache must be purged so the next feed
	// request no longer includes the unfollowed user's posts.
	if invalidatedID != followerID {
		t.Errorf("expected cache invalidation for follower %v, got %v", followerID, invalidatedID)
	}
}

func TestUnfollow_EmitsFeedEventAfterSuccess(t *testing.T) {
	followerID, followeeID := uuid.New(), uuid.New()
	emitter := &mockFollowFeedEmitter{}
	svc := newFollowService(&mockFollowRepo{})
	svc.WithFeedEventEmitter(emitter)

	if err := svc.Unfollow(context.Background(), followerID, followeeID); err != nil {
		t.Fatalf("Unfollow: %v", err)
	}
	if emitter.deleteCalls != 1 || emitter.deletedFollower != followerID || emitter.deletedFollowee != followeeID {
		t.Fatalf("deleted feed event mismatch: %+v", emitter)
	}
}

func TestUnfollow_FeedEmitterFailureIsNonFatal(t *testing.T) {
	svc := newFollowService(&mockFollowRepo{})
	svc.WithFeedEventEmitter(&mockFollowFeedEmitter{err: fmt.Errorf("queue full")})

	if err := svc.Unfollow(context.Background(), uuid.New(), uuid.New()); err != nil {
		t.Fatalf("Unfollow should ignore feed emitter error: %v", err)
	}
}

func TestUnfollow_RepoError(t *testing.T) {
	svc := newFollowService(&mockFollowRepo{
		unfollow: func(_ context.Context, _, _ uuid.UUID) error {
			return fmt.Errorf("db down")
		},
	})

	err := svc.Unfollow(context.Background(), uuid.New(), uuid.New())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "INTERNAL_ERROR")
}

func TestUnfollow_NilFeedInvalidator_NoPanic(t *testing.T) {
	svc := newFollowService(&mockFollowRepo{})

	if err := svc.Unfollow(context.Background(), uuid.New(), uuid.New()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --------------------------------------------------------------------------
// IsFollowing tests
// --------------------------------------------------------------------------

func TestIsFollowing_True(t *testing.T) {
	svc := newFollowService(&mockFollowRepo{
		isFollowing: func(_ context.Context, _, _ uuid.UUID) (bool, error) {
			return true, nil
		},
	})

	ok, err := svc.IsFollowing(context.Background(), uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected IsFollowing=true")
	}
}

func TestIsFollowing_False(t *testing.T) {
	svc := newFollowService(&mockFollowRepo{
		isFollowing: func(_ context.Context, _, _ uuid.UUID) (bool, error) {
			return false, nil
		},
	})

	ok, err := svc.IsFollowing(context.Background(), uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected IsFollowing=false")
	}
}

func TestIsFollowing_RepoError(t *testing.T) {
	svc := newFollowService(&mockFollowRepo{
		isFollowing: func(_ context.Context, _, _ uuid.UUID) (bool, error) {
			return false, fmt.Errorf("db down")
		},
	})

	_, err := svc.IsFollowing(context.Background(), uuid.New(), uuid.New())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "INTERNAL_ERROR")
}

// --------------------------------------------------------------------------
// GetFollowers tests
// --------------------------------------------------------------------------

func TestGetFollowers_Success(t *testing.T) {
	targetID := uuid.New()
	followerID := uuid.New()
	svc := newFollowService(&mockFollowRepo{
		getFollowers: func(_ context.Context, _ uuid.UUID, _, _ int32) ([]*entity.Follow, error) {
			return []*entity.Follow{{FollowerID: followerID, FolloweeID: targetID}}, nil
		},
		countFollowers: func(_ context.Context, _ uuid.UUID) (int64, error) {
			return 1, nil
		},
	})

	follows, page, err := svc.GetFollowers(context.Background(), targetID, pagination.PaginationRequest{Limit: 20, Offset: 0})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(follows) != 1 || follows[0].FollowerID != followerID {
		t.Errorf("unexpected followers: %v", follows)
	}
	if page.Total != 1 {
		t.Errorf("expected total=1, got %d", page.Total)
	}
}

func TestGetFollowers_GetRepoError(t *testing.T) {
	svc := newFollowService(&mockFollowRepo{
		getFollowers: func(_ context.Context, _ uuid.UUID, _, _ int32) ([]*entity.Follow, error) {
			return nil, fmt.Errorf("db down")
		},
	})

	_, _, err := svc.GetFollowers(context.Background(), uuid.New(), pagination.PaginationRequest{Limit: 20})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "INTERNAL_ERROR")
}

func TestGetFollowers_CountRepoError(t *testing.T) {
	svc := newFollowService(&mockFollowRepo{
		getFollowers: func(_ context.Context, _ uuid.UUID, _, _ int32) ([]*entity.Follow, error) {
			return []*entity.Follow{}, nil
		},
		countFollowers: func(_ context.Context, _ uuid.UUID) (int64, error) {
			return 0, fmt.Errorf("db down")
		},
	})

	_, _, err := svc.GetFollowers(context.Background(), uuid.New(), pagination.PaginationRequest{Limit: 20})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "INTERNAL_ERROR")
}

// --------------------------------------------------------------------------
// GetFollowing tests
// --------------------------------------------------------------------------

func TestGetFollowing_Success(t *testing.T) {
	targetID := uuid.New()
	followeeID := uuid.New()
	svc := newFollowService(&mockFollowRepo{
		getFollowing: func(_ context.Context, _ uuid.UUID, _, _ int32) ([]*entity.Follow, error) {
			return []*entity.Follow{{FollowerID: targetID, FolloweeID: followeeID}}, nil
		},
		countFollowing: func(_ context.Context, _ uuid.UUID) (int64, error) {
			return 1, nil
		},
	})

	follows, page, err := svc.GetFollowing(context.Background(), targetID, pagination.PaginationRequest{Limit: 20, Offset: 0})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(follows) != 1 || follows[0].FolloweeID != followeeID {
		t.Errorf("unexpected following: %v", follows)
	}
	if page.Total != 1 {
		t.Errorf("expected total=1, got %d", page.Total)
	}
}

func TestGetFollowing_GetRepoError(t *testing.T) {
	svc := newFollowService(&mockFollowRepo{
		getFollowing: func(_ context.Context, _ uuid.UUID, _, _ int32) ([]*entity.Follow, error) {
			return nil, fmt.Errorf("db down")
		},
	})

	_, _, err := svc.GetFollowing(context.Background(), uuid.New(), pagination.PaginationRequest{Limit: 20})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "INTERNAL_ERROR")
}

func TestGetFollowing_CountRepoError(t *testing.T) {
	svc := newFollowService(&mockFollowRepo{
		getFollowing: func(_ context.Context, _ uuid.UUID, _, _ int32) ([]*entity.Follow, error) {
			return []*entity.Follow{}, nil
		},
		countFollowing: func(_ context.Context, _ uuid.UUID) (int64, error) {
			return 0, fmt.Errorf("db down")
		},
	})

	_, _, err := svc.GetFollowing(context.Background(), uuid.New(), pagination.PaginationRequest{Limit: 20})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "INTERNAL_ERROR")
}

// --------------------------------------------------------------------------
// GetFollowingIDs tests
// --------------------------------------------------------------------------

// GetFollowingIDs is used by the feed to build the set of users whose posts
// the caller should see. The IDs must be extracted from the FolloweeID field.
func TestGetFollowingIDs_ReturnsFolloweeIDs(t *testing.T) {
	targetID := uuid.New()
	a, b := uuid.New(), uuid.New()
	svc := newFollowService(&mockFollowRepo{
		getFollowing: func(_ context.Context, _ uuid.UUID, _, _ int32) ([]*entity.Follow, error) {
			return []*entity.Follow{
				{FollowerID: targetID, FolloweeID: a},
				{FollowerID: targetID, FolloweeID: b},
			}, nil
		},
	})

	ids, err := svc.GetFollowingIDs(context.Background(), targetID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("expected 2 IDs, got %d", len(ids))
	}
	got := map[uuid.UUID]bool{ids[0]: true, ids[1]: true}
	if !got[a] || !got[b] {
		t.Errorf("expected IDs {%v, %v}, got %v", a, b, ids)
	}
}

func TestGetFollowingIDs_RepoError(t *testing.T) {
	svc := newFollowService(&mockFollowRepo{
		getFollowing: func(_ context.Context, _ uuid.UUID, _, _ int32) ([]*entity.Follow, error) {
			return nil, fmt.Errorf("db down")
		},
	})

	_, err := svc.GetFollowingIDs(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "INTERNAL_ERROR")
}

// --------------------------------------------------------------------------
// GetFollowerIDs tests
// --------------------------------------------------------------------------

// GetFollowerIDs is used by the notification fanout to know who should receive
// a new-post notification. IDs must come from the FollowerID field.
func TestGetFollowerIDs_ReturnsFollowerIDs(t *testing.T) {
	targetID := uuid.New()
	a, b := uuid.New(), uuid.New()
	svc := newFollowService(&mockFollowRepo{
		getFollowers: func(_ context.Context, _ uuid.UUID, _, _ int32) ([]*entity.Follow, error) {
			return []*entity.Follow{
				{FollowerID: a, FolloweeID: targetID},
				{FollowerID: b, FolloweeID: targetID},
			}, nil
		},
	})

	ids, err := svc.GetFollowerIDs(context.Background(), targetID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("expected 2 IDs, got %d", len(ids))
	}
	got := map[uuid.UUID]bool{ids[0]: true, ids[1]: true}
	if !got[a] || !got[b] {
		t.Errorf("expected IDs {%v, %v}, got %v", a, b, ids)
	}
}

func TestGetFollowerIDs_RepoError(t *testing.T) {
	svc := newFollowService(&mockFollowRepo{
		getFollowers: func(_ context.Context, _ uuid.UUID, _, _ int32) ([]*entity.Follow, error) {
			return nil, fmt.Errorf("db down")
		},
	})

	_, err := svc.GetFollowerIDs(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "INTERNAL_ERROR")
}
