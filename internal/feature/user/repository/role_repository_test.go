package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jarviisha/darkvoid/internal/feature/user/db"
)

// --------------------------------------------------------------------------
// dbRoleToEntity mapping
// --------------------------------------------------------------------------

func TestDbRoleToEntity_NilDescription(t *testing.T) {
	role := dbRoleToEntity(db.UsrRole{
		ID:          uuid.New(),
		Name:        "admin",
		Description: nil,
	})

	if role.Description != "" {
		t.Errorf("expected empty description, got %q", role.Description)
	}
}

func TestDbRoleToEntity_WithDescription(t *testing.T) {
	desc := "full access"
	role := dbRoleToEntity(db.UsrRole{
		Name:        "admin",
		Description: &desc,
	})

	if role.Description != desc {
		t.Errorf("expected %q, got %q", desc, role.Description)
	}
}

func TestDbRoleToEntity_UpdatedAtNil(t *testing.T) {
	role := dbRoleToEntity(db.UsrRole{Name: "viewer"})

	if role.UpdatedAt != nil {
		t.Errorf("UpdatedAt should be nil when DB field invalid")
	}
}

func TestDbRoleToEntity_UpdatedAtSet(t *testing.T) {
	ts := time.Now().UTC().Truncate(time.Microsecond)
	role := dbRoleToEntity(db.UsrRole{
		Name:      "editor",
		UpdatedAt: pgtype.Timestamp{Time: ts, Valid: true},
	})

	if role.UpdatedAt == nil {
		t.Fatal("UpdatedAt should not be nil")
	}
	if !role.UpdatedAt.Equal(ts) {
		t.Errorf("UpdatedAt mismatch: want %v got %v", ts, *role.UpdatedAt)
	}
}

// --------------------------------------------------------------------------
// UserHasAnyRole — branching logic
// --------------------------------------------------------------------------

func TestUserHasAnyRole_NoRolesExist(t *testing.T) {
	// GetRoleByName always returns ErrNoRows — role names are unknown, should skip all.
	q := &mockQuerier{
		getRoleByName: func(_ context.Context, _ string) (db.UsrRole, error) {
			return db.UsrRole{}, pgx.ErrNoRows
		},
	}

	repo := &RoleRepository{queries: q}
	ok, err := repo.UserHasAnyRole(context.Background(), uuid.New(), []string{"admin", "editor"})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Errorf("expected false when no roles exist")
	}
}

func TestUserHasAnyRole_FirstRoleMatches(t *testing.T) {
	roleID := uuid.New()
	userID := uuid.New()

	q := &mockQuerier{
		getRoleByName: func(_ context.Context, name string) (db.UsrRole, error) {
			return db.UsrRole{ID: roleID, Name: name}, nil
		},
		checkUserHasRole: func(_ context.Context, p db.CheckUserHasRoleParams) (bool, error) {
			return p.UserID == userID && p.RoleID == roleID, nil
		},
	}

	repo := &RoleRepository{queries: q}
	ok, err := repo.UserHasAnyRole(context.Background(), userID, []string{"admin"})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Errorf("expected true when user has the role")
	}
}

func TestUserHasAnyRole_SecondRoleMatchesAfterMiss(t *testing.T) {
	adminID := uuid.New()
	editorID := uuid.New()
	userID := uuid.New()

	q := &mockQuerier{
		getRoleByName: func(_ context.Context, name string) (db.UsrRole, error) {
			switch name {
			case "admin":
				return db.UsrRole{ID: adminID, Name: "admin"}, nil
			case "editor":
				return db.UsrRole{ID: editorID, Name: "editor"}, nil
			}
			return db.UsrRole{}, pgx.ErrNoRows
		},
		checkUserHasRole: func(_ context.Context, p db.CheckUserHasRoleParams) (bool, error) {
			// user has editor but not admin
			return p.RoleID == editorID, nil
		},
	}

	repo := &RoleRepository{queries: q}
	ok, err := repo.UserHasAnyRole(context.Background(), userID, []string{"admin", "editor"})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Errorf("expected true when user has second role")
	}
}

func TestUserHasAnyRole_UserHasNone(t *testing.T) {
	roleID := uuid.New()

	q := &mockQuerier{
		getRoleByName: func(_ context.Context, name string) (db.UsrRole, error) {
			return db.UsrRole{ID: roleID, Name: name}, nil
		},
		checkUserHasRole: func(_ context.Context, _ db.CheckUserHasRoleParams) (bool, error) {
			return false, nil
		},
	}

	repo := &RoleRepository{queries: q}
	ok, err := repo.UserHasAnyRole(context.Background(), uuid.New(), []string{"admin", "editor"})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Errorf("expected false when user has no matching role")
	}
}

func TestUserHasAnyRole_CheckUserHasRoleError(t *testing.T) {
	roleID := uuid.New()
	sentinel := errors.New("db error")

	q := &mockQuerier{
		getRoleByName: func(_ context.Context, name string) (db.UsrRole, error) {
			return db.UsrRole{ID: roleID, Name: name}, nil
		},
		checkUserHasRole: func(_ context.Context, _ db.CheckUserHasRoleParams) (bool, error) {
			return false, sentinel
		},
	}

	repo := &RoleRepository{queries: q}
	ok, err := repo.UserHasAnyRole(context.Background(), uuid.New(), []string{"admin"})

	if err == nil {
		t.Fatal("expected error from CheckUserHasRole, got nil")
	}
	if ok {
		t.Errorf("expected false on error")
	}
}

func TestUserHasAnyRole_EmptyRoleNames(t *testing.T) {
	repo := &RoleRepository{queries: &mockQuerier{}}
	ok, err := repo.UserHasAnyRole(context.Background(), uuid.New(), []string{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Errorf("expected false for empty role list")
	}
}
