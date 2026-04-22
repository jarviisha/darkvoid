package app

import (
	appMiddleware "github.com/jarviisha/darkvoid/internal/app/middleware"
	adminHandler "github.com/jarviisha/darkvoid/internal/feature/admin/handler"
	adminService "github.com/jarviisha/darkvoid/internal/feature/admin/service"
	"github.com/jarviisha/darkvoid/internal/feature/user/repository"
	"github.com/jarviisha/darkvoid/pkg/storage"
)

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
