package app

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jarviisha/darkvoid/internal/feature/user/repository"
	"github.com/jarviisha/darkvoid/pkg/storage"
)

func (app *Application) setupAdminContext(store storage.Storage) error {
	admin, err := SetupAdminContext(
		app.User.Ports().AdminUserStore,
		buildAdminRoleRepo(app.pool),
		store,
		app.Notification,
	)
	if err != nil {
		return err
	}
	app.Admin = admin
	app.log.Info("admin context initialized")
	return nil
}

func buildAdminRoleRepo(pool *pgxpool.Pool) *repository.RoleRepository {
	return repository.NewRoleRepository(pool)
}
