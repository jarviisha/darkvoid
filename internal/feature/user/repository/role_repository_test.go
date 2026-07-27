package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jarviisha/darkvoid/internal/feature/user/db"
	"github.com/jarviisha/darkvoid/internal/feature/user/entity"
)

// --------------------------------------------------------------------------
// dbRoleAssignmentToEntity mapping
// --------------------------------------------------------------------------

func TestDbRoleAssignmentToEntity_NilAssignedBy(t *testing.T) {
	a := dbRoleAssignmentToEntity(db.GetUserRolesRow{
		Role:       "admin",
		AssignedBy: pgtype.UUID{Valid: false},
	})

	if a.Role != entity.RoleAdmin {
		t.Errorf("expected role %q, got %q", entity.RoleAdmin, a.Role)
	}
	if a.AssignedBy != nil {
		t.Errorf("AssignedBy should be nil for a system grant, got %v", *a.AssignedBy)
	}
}

func TestDbRoleAssignmentToEntity_WithAssignedBy(t *testing.T) {
	admin := uuid.New()
	a := dbRoleAssignmentToEntity(db.GetUserRolesRow{
		Role:       "moderator",
		AssignedBy: pgtype.UUID{Bytes: admin, Valid: true},
	})

	if a.AssignedBy == nil {
		t.Fatal("AssignedBy should not be nil")
	}
	if *a.AssignedBy != admin {
		t.Errorf("AssignedBy mismatch: want %v got %v", admin, *a.AssignedBy)
	}
}

func TestDbRoleAssignmentToEntity_AssignedAt(t *testing.T) {
	ts := time.Now().UTC().Truncate(time.Microsecond)
	a := dbRoleAssignmentToEntity(db.GetUserRolesRow{
		Role:       "admin",
		AssignedAt: pgtype.Timestamp{Time: ts, Valid: true},
	})

	if !a.AssignedAt.Equal(ts) {
		t.Errorf("AssignedAt mismatch: want %v got %v", ts, a.AssignedAt)
	}
}

// --------------------------------------------------------------------------
// GetUserRoles
// --------------------------------------------------------------------------

func TestGetUserRoles_MapsEveryRow(t *testing.T) {
	q := &mockQuerier{
		getUserRoles: func(_ context.Context, _ uuid.UUID) ([]db.GetUserRolesRow, error) {
			return []db.GetUserRolesRow{
				{Role: "admin"},
				{Role: "moderator"},
			}, nil
		},
	}

	repo := &RoleRepository{queries: q}
	got, err := repo.GetUserRoles(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 assignments, got %d", len(got))
	}
	if got[0].Role != entity.RoleAdmin || got[1].Role != entity.RoleModerator {
		t.Errorf("unexpected roles: %q, %q", got[0].Role, got[1].Role)
	}
}

func TestGetUserRoles_NoRolesReturnsEmptySlice(t *testing.T) {
	q := &mockQuerier{
		getUserRoles: func(_ context.Context, _ uuid.UUID) ([]db.GetUserRolesRow, error) {
			return nil, nil
		},
	}

	repo := &RoleRepository{queries: q}
	got, err := repo.GetUserRoles(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Error("expected an empty slice, not nil — it is serialized straight to JSON")
	}
	if len(got) != 0 {
		t.Errorf("expected 0 assignments, got %d", len(got))
	}
}

func TestGetUserRoles_DBError(t *testing.T) {
	q := &mockQuerier{
		getUserRoles: func(_ context.Context, _ uuid.UUID) ([]db.GetUserRolesRow, error) {
			return nil, errors.New("connection refused")
		},
	}

	repo := &RoleRepository{queries: q}
	if _, err := repo.GetUserRoles(context.Background(), uuid.New()); err == nil {
		t.Fatal("expected an error to propagate")
	}
}

// --------------------------------------------------------------------------
// UserHasAnyRole — this gates every admin request
// --------------------------------------------------------------------------

func TestUserHasAnyRole_EmptyRoleNamesSkipsQuery(t *testing.T) {
	called := false
	q := &mockQuerier{
		checkUserHasAnyRole: func(_ context.Context, _ db.CheckUserHasAnyRoleParams) (bool, error) {
			called = true
			return true, nil
		},
	}

	repo := &RoleRepository{queries: q}
	ok, err := repo.UserHasAnyRole(context.Background(), uuid.New(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("no role names can never be satisfied")
	}
	if called {
		t.Error("should not hit the DB when there is nothing to check")
	}
}

func TestUserHasAnyRole_SingleQueryForAllNames(t *testing.T) {
	userID := uuid.New()
	calls := 0
	var gotParams db.CheckUserHasAnyRoleParams

	q := &mockQuerier{
		checkUserHasAnyRole: func(_ context.Context, arg db.CheckUserHasAnyRoleParams) (bool, error) {
			calls++
			gotParams = arg
			return true, nil
		},
	}

	repo := &RoleRepository{queries: q}
	ok, err := repo.UserHasAnyRole(context.Background(), userID, []string{"admin", "moderator"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected true")
	}
	if calls != 1 {
		t.Errorf("expected exactly 1 query regardless of role count, got %d", calls)
	}
	if gotParams.UserID != userID {
		t.Errorf("user_id mismatch: want %v got %v", userID, gotParams.UserID)
	}
	if len(gotParams.Roles) != 2 || gotParams.Roles[0] != "admin" || gotParams.Roles[1] != "moderator" {
		t.Errorf("role names not passed through: %v", gotParams.Roles)
	}
}

func TestUserHasAnyRole_UserHasNone(t *testing.T) {
	q := &mockQuerier{
		checkUserHasAnyRole: func(_ context.Context, _ db.CheckUserHasAnyRoleParams) (bool, error) {
			return false, nil
		},
	}

	repo := &RoleRepository{queries: q}
	ok, err := repo.UserHasAnyRole(context.Background(), uuid.New(), []string{"admin"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected false")
	}
}

// A DB failure must surface as an error so the middleware returns 500 and denies
// the request. Reporting "false, nil" would make an outage indistinguishable from
// a genuine permission denial — the old per-name loop swallowed exactly this.
func TestUserHasAnyRole_DBErrorIsNotSwallowed(t *testing.T) {
	q := &mockQuerier{
		checkUserHasAnyRole: func(_ context.Context, _ db.CheckUserHasAnyRoleParams) (bool, error) {
			return false, errors.New("connection refused")
		},
	}

	repo := &RoleRepository{queries: q}
	ok, err := repo.UserHasAnyRole(context.Background(), uuid.New(), []string{"admin"})
	if err == nil {
		t.Fatal("a DB failure must not be reported as a clean permission denial")
	}
	if ok {
		t.Error("expected false alongside the error")
	}
}

// --------------------------------------------------------------------------
// AssignRole / RemoveRole
// --------------------------------------------------------------------------

func TestAssignRole_SystemGrantHasNullAssignedBy(t *testing.T) {
	var got db.AssignRoleToUserParams
	q := &mockQuerier{
		assignRoleToUser: func(_ context.Context, arg db.AssignRoleToUserParams) error {
			got = arg
			return nil
		},
	}

	repo := &RoleRepository{queries: q}
	if err := repo.AssignRole(context.Background(), uuid.New(), entity.RoleAdmin, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Role != "admin" {
		t.Errorf("expected role %q, got %q", "admin", got.Role)
	}
	if got.AssignedBy.Valid {
		t.Error("AssignedBy must be NULL when the system grants the role")
	}
}

func TestAssignRole_RecordsAssigningAdmin(t *testing.T) {
	admin := uuid.New()
	var got db.AssignRoleToUserParams
	q := &mockQuerier{
		assignRoleToUser: func(_ context.Context, arg db.AssignRoleToUserParams) error {
			got = arg
			return nil
		},
	}

	repo := &RoleRepository{queries: q}
	if err := repo.AssignRole(context.Background(), uuid.New(), entity.RoleModerator, &admin); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.AssignedBy.Valid {
		t.Fatal("AssignedBy should be set")
	}
	if uuid.UUID(got.AssignedBy.Bytes) != admin {
		t.Errorf("AssignedBy mismatch: want %v got %v", admin, uuid.UUID(got.AssignedBy.Bytes))
	}
	if got.Role != "moderator" {
		t.Errorf("expected role %q, got %q", "moderator", got.Role)
	}
}

func TestAssignRole_DBError(t *testing.T) {
	q := &mockQuerier{
		assignRoleToUser: func(_ context.Context, _ db.AssignRoleToUserParams) error {
			return errors.New("check constraint violated")
		},
	}

	repo := &RoleRepository{queries: q}
	if err := repo.AssignRole(context.Background(), uuid.New(), entity.RoleAdmin, nil); err == nil {
		t.Fatal("expected an error to propagate")
	}
}

func TestRemoveRole_PassesRoleName(t *testing.T) {
	userID := uuid.New()
	var got db.RemoveRoleFromUserParams
	q := &mockQuerier{
		removeRoleFromUser: func(_ context.Context, arg db.RemoveRoleFromUserParams) error {
			got = arg
			return nil
		},
	}

	repo := &RoleRepository{queries: q}
	if err := repo.RemoveRole(context.Background(), userID, entity.RoleAdmin); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.UserID != userID || got.Role != "admin" {
		t.Errorf("unexpected params: %+v", got)
	}
}

func TestRemoveRole_DBError(t *testing.T) {
	q := &mockQuerier{
		removeRoleFromUser: func(_ context.Context, _ db.RemoveRoleFromUserParams) error {
			return errors.New("connection refused")
		},
	}

	repo := &RoleRepository{queries: q}
	if err := repo.RemoveRole(context.Background(), uuid.New(), entity.RoleAdmin); err == nil {
		t.Fatal("expected an error to propagate")
	}
}
