package app

import (
	"context"

	"github.com/google/uuid"
	adminService "github.com/jarviisha/darkvoid/internal/feature/admin/service"
	"github.com/jarviisha/darkvoid/internal/feature/user/entity"
)

// adminUserStoreAdapter adapts user repository operations to the admin service port.
type adminUserStoreAdapter struct {
	userRepo adminUserStoreSource
}

type adminUserStoreSource interface {
	GetUserByIDAny(ctx context.Context, id uuid.UUID) (*entity.User, error)
	AdminListUsers(ctx context.Context, query *string, isActive *bool, limit, offset int32) ([]*entity.User, error)
	AdminCountUsers(ctx context.Context, query *string, isActive *bool) (int64, error)
	AdminSetUserActive(ctx context.Context, id uuid.UUID, isActive bool, updatedBy uuid.UUID) error
	ListAllActiveUserIDs(ctx context.Context) ([]uuid.UUID, error)
}

func newAdminUserStoreAdapter(userRepo adminUserStoreSource) *adminUserStoreAdapter {
	return &adminUserStoreAdapter{userRepo: userRepo}
}

func (a *adminUserStoreAdapter) GetUserByID(ctx context.Context, id uuid.UUID) (*entity.User, error) {
	return a.userRepo.GetUserByIDAny(ctx, id)
}

func (a *adminUserStoreAdapter) AdminListUsers(ctx context.Context, filter adminService.AdminListUsersFilter) ([]*entity.User, error) {
	return a.userRepo.AdminListUsers(ctx, filter.Query, filter.IsActive, filter.Limit, filter.Offset)
}

func (a *adminUserStoreAdapter) AdminCountUsers(ctx context.Context, filter adminService.AdminListUsersFilter) (int64, error) {
	return a.userRepo.AdminCountUsers(ctx, filter.Query, filter.IsActive)
}

func (a *adminUserStoreAdapter) AdminSetUserActive(ctx context.Context, id uuid.UUID, isActive bool, updatedBy uuid.UUID) error {
	return a.userRepo.AdminSetUserActive(ctx, id, isActive, updatedBy)
}

func (a *adminUserStoreAdapter) ListAllActiveUserIDs(ctx context.Context) ([]uuid.UUID, error) {
	return a.userRepo.ListAllActiveUserIDs(ctx)
}
