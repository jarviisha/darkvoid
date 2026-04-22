package app

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jarviisha/darkvoid/internal/feature/user/repository"
	"github.com/jarviisha/darkvoid/pkg/storage"
)

func (app *Application) setupAdminContext(store storage.Storage) {
	userPorts := app.User.Ports()
	roleRepo := buildAdminRoleRepo(app.pool)
	app.Admin = SetupAdminContext(userPorts.AdminUserStore, roleRepo, store)
	app.Admin.WireNotificationEmitter(app.Notification)
	app.log.Info("admin context initialized")
}

func buildAdminRoleRepo(pool *pgxpool.Pool) *repository.RoleRepository {
	return repository.NewRoleRepository(pool)
}
