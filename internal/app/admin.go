package app

import (
	"context"

	"github.com/google/uuid"
	appMiddleware "github.com/jarviisha/darkvoid/internal/app/middleware"
	adminHandler "github.com/jarviisha/darkvoid/internal/feature/admin/handler"
	adminService "github.com/jarviisha/darkvoid/internal/feature/admin/service"
	"github.com/jarviisha/darkvoid/internal/feature/user/repository"
	"github.com/jarviisha/darkvoid/pkg/errors"
	"github.com/jarviisha/darkvoid/pkg/storage"
)

// adminRoleName is the role required by the /api/v1/admin routes and the admin
// Swagger UI (see RequireRole wiring in admin_routes.go and app.go).
const adminRoleName = "admin"

// AdminContext holds all dependencies for the admin bounded context.
type AdminContext struct {
	roleRepo     *repository.RoleRepository
	adminService *adminService.AdminService
	adminHandler *adminHandler.AdminHandler
}

type AdminPorts struct {
	RoleChecker appMiddleware.RoleChecker
}

// SetupAdminContext initializes the admin context.
// It uses narrow adapters over the user repositories instead of reaching into
// the user sqlc layer directly.
func SetupAdminContext(userRepo adminUserStoreSource, roleRepo *repository.RoleRepository, store storage.Storage) *AdminContext {
	userStoreAdapter := newAdminUserStoreAdapter(userRepo)
	svc := adminService.NewAdminService(userStoreAdapter, roleRepo, store)
	h := adminHandler.NewAdminHandler(svc)

	return &AdminContext{
		roleRepo:     roleRepo,
		adminService: svc,
		adminHandler: h,
	}
}

func (ctx *AdminContext) Ports() AdminPorts {
	return AdminPorts{
		RoleChecker: ctx.adminService,
	}
}

func (ctx *AdminContext) WireNotificationEmitter(notif *NotificationContext) {
	ctx.adminService.WithNotificationEmitter(notif.notifService)
}

// GrantAdminRole makes sure the admin role exists and is held by userID.
// It is idempotent: the role is created only when the seed migration never ran,
// and re-granting an existing assignment is a no-op.
func (ctx *AdminContext) GrantAdminRole(c context.Context, userID uuid.UUID) error {
	role, err := ctx.roleRepo.GetRoleByName(c, adminRoleName)
	if err != nil {
		if !errors.Is(err, errors.ErrNotFound) {
			return err
		}
		desc := "Administrator"
		role, err = ctx.roleRepo.CreateRole(c, adminRoleName, &desc)
		if err != nil {
			return err
		}
	}
	return ctx.roleRepo.AssignRole(c, userID, role.ID, nil)
}
