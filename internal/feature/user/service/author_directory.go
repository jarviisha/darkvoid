package service

import (
	"context"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/author"
	"github.com/jarviisha/darkvoid/internal/feature/user/entity"
)

type authorLookup func(ctx context.Context, ids []uuid.UUID) ([]*entity.User, error)

// AuthorDirectory resolves user ids to author.Author for every context that
// renders one. Ids with no matching user are absent from the result.
type AuthorDirectory struct {
	lookup authorLookup
}

type authorDirectoryRepo interface {
	GetUsersByIDs(ctx context.Context, ids []uuid.UUID) ([]*entity.User, error)
	GetUsersByIDsAny(ctx context.Context, ids []uuid.UUID) ([]*entity.User, error)
}

// NewAuthorDirectory resolves users whatever their active status: content
// stays attributed to an author who deactivated after writing it.
func NewAuthorDirectory(repo authorDirectoryRepo) *AuthorDirectory {
	return &AuthorDirectory{lookup: repo.GetUsersByIDsAny}
}

// NewActiveAuthorDirectory resolves active users only. Notifications use it,
// so a deactivated actor drops out of them instead of being named.
func NewActiveAuthorDirectory(repo authorDirectoryRepo) *AuthorDirectory {
	return &AuthorDirectory{lookup: repo.GetUsersByIDs}
}

// GetAuthorsByIDs returns userID → Author for the requested ids.
func (d *AuthorDirectory) GetAuthorsByIDs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]*author.Author, error) {
	if len(ids) == 0 {
		return map[uuid.UUID]*author.Author{}, nil
	}
	users, err := d.lookup(ctx, ids)
	if err != nil {
		return nil, err
	}
	result := make(map[uuid.UUID]*author.Author, len(users))
	for _, u := range users {
		result[u.ID] = &author.Author{
			ID:          u.ID,
			Username:    u.Username,
			DisplayName: u.DisplayName,
			AvatarKey:   u.AvatarKey,
		}
	}
	return result, nil
}
