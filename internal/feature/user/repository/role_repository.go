package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jarviisha/darkvoid/internal/feature/user/db"
	"github.com/jarviisha/darkvoid/internal/feature/user/entity"
	"github.com/jarviisha/darkvoid/internal/infrastructure/database"
)

// RoleRepository handles all DB operations for user-role assignments.
//
// Role names live inline on usr.user_roles and the valid set is fixed by a CHECK
// constraint, so there is no roles table to look up: the known roles are
// entity.AllRoles.
type RoleRepository struct {
	queries db.Querier
	pool    *pgxpool.Pool
}

// NewRoleRepository creates a RoleRepository backed by the given connection pool.
func NewRoleRepository(pool *pgxpool.Pool) *RoleRepository {
	return &RoleRepository{
		queries: db.New(pool),
		pool:    pool,
	}
}

// GetUserRoles returns every role assignment held by a user, ordered by role name.
func (r *RoleRepository) GetUserRoles(ctx context.Context, userID uuid.UUID) ([]*entity.RoleAssignment, error) {
	rows, err := r.queries.GetUserRoles(ctx, userID)
	if err != nil {
		return nil, database.MapDBError(err)
	}
	assignments := make([]*entity.RoleAssignment, 0, len(rows))
	for _, row := range rows {
		assignments = append(assignments, dbRoleAssignmentToEntity(row))
	}
	return assignments, nil
}

// AssignRole grants a role to a user, recording who performed the assignment.
// Re-granting a role the user already holds is a no-op.
func (r *RoleRepository) AssignRole(ctx context.Context, userID uuid.UUID, role entity.Role, assignedBy *uuid.UUID) error {
	var assignedByPg pgtype.UUID
	if assignedBy != nil {
		assignedByPg = pgtype.UUID{Bytes: *assignedBy, Valid: true}
	}
	err := r.queries.AssignRoleToUser(ctx, db.AssignRoleToUserParams{
		UserID:     userID,
		Role:       role.String(),
		AssignedBy: assignedByPg,
	})
	return database.MapDBError(err)
}

// RemoveRole revokes a role from a user. Revoking a role the user does not hold
// is a no-op.
func (r *RoleRepository) RemoveRole(ctx context.Context, userID uuid.UUID, role entity.Role) error {
	err := r.queries.RemoveRoleFromUser(ctx, db.RemoveRoleFromUserParams{
		UserID: userID,
		Role:   role.String(),
	})
	return database.MapDBError(err)
}

// UserHasAnyRole reports whether a user holds at least one of the named roles.
// It runs a single query regardless of how many names are supplied. This sits on
// the hot path of every admin request, and a DB failure here is returned as an
// error rather than swallowed — denying access is the only safe default.
func (r *RoleRepository) UserHasAnyRole(ctx context.Context, userID uuid.UUID, roleNames []string) (bool, error) {
	if len(roleNames) == 0 {
		return false, nil
	}
	ok, err := r.queries.CheckUserHasAnyRole(ctx, db.CheckUserHasAnyRoleParams{
		UserID: userID,
		Roles:  roleNames,
	})
	if err != nil {
		return false, database.MapDBError(err)
	}
	return ok, nil
}

func dbRoleAssignmentToEntity(row db.GetUserRolesRow) *entity.RoleAssignment {
	a := &entity.RoleAssignment{
		Role:       entity.Role(row.Role),
		AssignedAt: row.AssignedAt.Time,
	}
	if row.AssignedBy.Valid {
		id := uuid.UUID(row.AssignedBy.Bytes)
		a.AssignedBy = &id
	}
	return a
}
