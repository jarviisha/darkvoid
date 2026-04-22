package app

import (
	"context"

	"github.com/google/uuid"
	postentity "github.com/jarviisha/darkvoid/internal/feature/post/entity"
	userrepo "github.com/jarviisha/darkvoid/internal/feature/user/repository"
	userservice "github.com/jarviisha/darkvoid/internal/feature/user/service"
)

// postFollowChecker implements service.followChecker using the user context follow service.
type postFollowChecker struct {
	followService postFollowService
}

func (c *postFollowChecker) IsFollowing(ctx context.Context, followerID, followeeID uuid.UUID) (bool, error) {
	return c.followService.IsFollowing(ctx, followerID, followeeID)
}

// postUserReader implements service.userReader using the user repository port.
type postUserReader struct {
	userRepo postUserRepo
}

func (r *postUserReader) GetAuthorsByIDs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]*postentity.Author, error) {
	users, err := r.userRepo.GetUsersByIDsAny(ctx, ids)
	if err != nil {
		return nil, err
	}

	result := make(map[uuid.UUID]*postentity.Author, len(users))
	for _, u := range users {
		result[u.ID] = &postentity.Author{
			ID:          u.ID,
			Username:    u.Username,
			DisplayName: u.DisplayName,
			AvatarKey:   u.AvatarKey,
		}
	}
	return result, nil
}

type postUserRepoAdapter struct {
	userRepo *userrepo.UserRepository
}

func buildPostUserRepo(userRepo *userrepo.UserRepository) postUserRepo {
	return &postUserRepoAdapter{userRepo: userRepo}
}

func (r *postUserRepoAdapter) GetUsersByIDsAny(ctx context.Context, ids []uuid.UUID) ([]*postUser, error) {
	users, err := r.userRepo.GetUsersByIDsAny(ctx, ids)
	if err != nil {
		return nil, err
	}

	result := make([]*postUser, len(users))
	for i, u := range users {
		result[i] = &postUser{
			ID:          u.ID,
			Username:    u.Username,
			DisplayName: u.DisplayName,
			AvatarKey:   u.AvatarKey,
		}
	}
	return result, nil
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

// func buildPostFollowChecker(followService *userservice.FollowService) *postFollowChecker {
// 	return &postFollowChecker{followService: buildPostFollowService(followService)}
// }
