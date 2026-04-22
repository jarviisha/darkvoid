package app

import (
	"context"

	"github.com/google/uuid"
	notifentity "github.com/jarviisha/darkvoid/internal/feature/notification/entity"
	userentity "github.com/jarviisha/darkvoid/internal/feature/user/entity"
)

// notifUserReader implements notification/service.userReader using UserRepository.
type notifUserReader struct {
	userRepo notificationUserRepo
}

type notificationUserRepo interface {
	GetUsersByIDs(ctx context.Context, ids []uuid.UUID) ([]*userentity.User, error)
}

func buildNotificationUserReader(userRepo notificationUserRepo) notificationUserReader {
	return &notifUserReader{userRepo: userRepo}
}

func (r *notifUserReader) GetAuthorsByIDs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]*notifentity.Actor, error) {
	users, err := r.userRepo.GetUsersByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	result := make(map[uuid.UUID]*notifentity.Actor, len(users))
	for _, u := range users {
		result[u.ID] = &notifentity.Actor{
			ID:          u.ID,
			Username:    u.Username,
			DisplayName: u.DisplayName,
			AvatarKey:   u.AvatarKey,
		}
	}
	return result, nil
}
