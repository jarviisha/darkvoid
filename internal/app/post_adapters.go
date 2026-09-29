package app

import (
	"context"

	"github.com/google/uuid"
	userservice "github.com/jarviisha/darkvoid/internal/feature/user/service"
)

// postFollowChecker implements service.followChecker using the user context follow service.
type postFollowChecker struct {
	followService postFollowService
}

func (c *postFollowChecker) IsFollowing(ctx context.Context, followerID, followeeID uuid.UUID) (bool, error) {
	return c.followService.IsFollowing(ctx, followerID, followeeID)
}

func (c *postFollowChecker) GetFollowingAmong(ctx context.Context, followerID uuid.UUID, followeeIDs []uuid.UUID) ([]uuid.UUID, error) {
	return c.followService.GetFollowingAmong(ctx, followerID, followeeIDs)
}

type postFollowServiceAdapter struct {
	followService *userservice.FollowService
}

func buildPostFollowService(followService *userservice.FollowService) postFollowService {
	return &postFollowServiceAdapter{followService: followService}
}

func (s *postFollowServiceAdapter) IsFollowing(ctx context.Context, followerID, followeeID uuid.UUID) (bool, error) {
	return s.followService.IsFollowing(ctx, followerID, followeeID)
}

func (s *postFollowServiceAdapter) GetFollowingAmong(ctx context.Context, followerID uuid.UUID, followeeIDs []uuid.UUID) ([]uuid.UUID, error) {
	return s.followService.GetFollowingAmong(ctx, followerID, followeeIDs)
}

// func buildPostFollowChecker(followService *userservice.FollowService) *postFollowChecker {
// 	return &postFollowChecker{followService: buildPostFollowService(followService)}
// }
