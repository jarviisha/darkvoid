package service

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/feature/user/entity"
	apperrors "github.com/jarviisha/darkvoid/pkg/errors"
)

// --------------------------------------------------------------------------
// stubs — only the calls the role paths make are wired up
// --------------------------------------------------------------------------

type stubUserStore struct {
	userStore   // embedded so unrelated methods panic loudly if ever called
	getUserByID func(context.Context, uuid.UUID) (*entity.User, error)
}

func (s *stubUserStore) GetUserByID(ctx context.Context, id uuid.UUID) (*entity.User, error) {
	if s.getUserByID != nil {
		return s.getUserByID(ctx, id)
	}
	return &entity.User{ID: id}, nil
}

type stubRoleStore struct {
	getUserRoles func(context.Context, uuid.UUID) ([]*entity.RoleAssignment, error)
	assignRole   func(context.Context, uuid.UUID, entity.Role, *uuid.UUID) error
	removeRole   func(context.Context, uuid.UUID, entity.Role) error
}

func (s *stubRoleStore) GetUserRoles(ctx context.Context, userID uuid.UUID) ([]*entity.RoleAssignment, error) {
	if s.getUserRoles != nil {
		return s.getUserRoles(ctx, userID)
	}
	return nil, nil
}

func (s *stubRoleStore) AssignRole(ctx context.Context, userID uuid.UUID, role entity.Role, by *uuid.UUID) error {
	if s.assignRole != nil {
		return s.assignRole(ctx, userID, role, by)
	}
	return nil
}

func (s *stubRoleStore) RemoveRole(ctx context.Context, userID uuid.UUID, role entity.Role) error {
	if s.removeRole != nil {
		return s.removeRole(ctx, userID, role)
	}
	return nil
}

func (s *stubRoleStore) UserHasAnyRole(context.Context, uuid.UUID, []string) (bool, error) {
	return false, nil
}

func newRoleService(users *stubUserStore, roles *stubRoleStore) *AdminService {
	if users == nil {
		users = &stubUserStore{}
	}
	if roles == nil {
		roles = &stubRoleStore{}
	}
	return NewAdminService(users, roles, nil)
}

func assertStatus(t *testing.T, err error, want int) {
	t.Helper()
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected an *AppError, got %T: %v", err, err)
	}
	if appErr.HTTPStatus != want {
		t.Errorf("expected HTTP %d, got %d (%s)", want, appErr.HTTPStatus, appErr.Message)
	}
}

// --------------------------------------------------------------------------
// ListRoles
// --------------------------------------------------------------------------

func TestListRoles_ReturnsEveryKnownRole(t *testing.T) {
	resp := newRoleService(nil, nil).ListRoles()

	if len(resp.Data) != len(entity.AllRoles) {
		t.Fatalf("expected %d roles, got %d", len(entity.AllRoles), len(resp.Data))
	}
	for i, r := range entity.AllRoles {
		if resp.Data[i].Name != r.String() {
			t.Errorf("role %d: expected %q, got %q", i, r, resp.Data[i].Name)
		}
		if resp.Data[i].Description == "" {
			t.Errorf("role %q: description should be populated", r)
		}
	}
}

// --------------------------------------------------------------------------
// AssignRole
// --------------------------------------------------------------------------

func TestAssignRole_Success(t *testing.T) {
	adminID := uuid.New()
	userID := uuid.New()
	var gotRole entity.Role
	var gotBy *uuid.UUID

	svc := newRoleService(nil, &stubRoleStore{
		assignRole: func(_ context.Context, _ uuid.UUID, role entity.Role, by *uuid.UUID) error {
			gotRole, gotBy = role, by
			return nil
		},
	})

	if err := svc.AssignRole(context.Background(), userID, "moderator", adminID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotRole != entity.RoleModerator {
		t.Errorf("expected %q, got %q", entity.RoleModerator, gotRole)
	}
	if gotBy == nil || *gotBy != adminID {
		t.Error("the acting admin must be recorded as assigned_by")
	}
}

// An unknown name must be rejected in the service. Letting it through would hit
// the CHECK constraint and surface as an opaque 500.
func TestAssignRole_UnknownRoleIsBadRequest(t *testing.T) {
	called := false
	svc := newRoleService(nil, &stubRoleStore{
		assignRole: func(context.Context, uuid.UUID, entity.Role, *uuid.UUID) error {
			called = true
			return nil
		},
	})

	err := svc.AssignRole(context.Background(), uuid.New(), "superuser", uuid.New())
	if err == nil {
		t.Fatal("expected an error for an unknown role")
	}
	assertStatus(t, err, http.StatusBadRequest)
	if called {
		t.Error("an unknown role must never reach the store")
	}
}

func TestAssignRole_ValidationRunsBeforeUserLookup(t *testing.T) {
	looked := false
	svc := newRoleService(&stubUserStore{
		getUserByID: func(_ context.Context, id uuid.UUID) (*entity.User, error) {
			looked = true
			return &entity.User{ID: id}, nil
		},
	}, nil)

	if err := svc.AssignRole(context.Background(), uuid.New(), "nope", uuid.New()); err == nil {
		t.Fatal("expected an error")
	}
	if looked {
		t.Error("a malformed role should be rejected without a DB round trip")
	}
}

func TestAssignRole_UnknownUserPropagates(t *testing.T) {
	svc := newRoleService(&stubUserStore{
		getUserByID: func(context.Context, uuid.UUID) (*entity.User, error) {
			return nil, apperrors.NewNotFoundError("user")
		},
	}, nil)

	err := svc.AssignRole(context.Background(), uuid.New(), "admin", uuid.New())
	if err == nil {
		t.Fatal("expected an error")
	}
	assertStatus(t, err, http.StatusNotFound)
}

// The insert is ON CONFLICT DO NOTHING, so re-granting is a no-op rather than
// the 409 the old lookup-table implementation returned.
func TestAssignRole_RegrantIsIdempotent(t *testing.T) {
	svc := newRoleService(nil, &stubRoleStore{
		assignRole: func(context.Context, uuid.UUID, entity.Role, *uuid.UUID) error { return nil },
	})

	for range 2 {
		if err := svc.AssignRole(context.Background(), uuid.New(), "admin", uuid.New()); err != nil {
			t.Fatalf("re-granting should succeed, got: %v", err)
		}
	}
}

func TestAssignRole_StoreErrorIsInternal(t *testing.T) {
	svc := newRoleService(nil, &stubRoleStore{
		assignRole: func(context.Context, uuid.UUID, entity.Role, *uuid.UUID) error {
			return errors.New("connection refused")
		},
	})

	err := svc.AssignRole(context.Background(), uuid.New(), "admin", uuid.New())
	if err == nil {
		t.Fatal("expected an error")
	}
	assertStatus(t, err, http.StatusInternalServerError)
}

// --------------------------------------------------------------------------
// RemoveRole
// --------------------------------------------------------------------------

func TestRemoveRole_Success(t *testing.T) {
	var gotRole entity.Role
	svc := newRoleService(nil, &stubRoleStore{
		removeRole: func(_ context.Context, _ uuid.UUID, role entity.Role) error {
			gotRole = role
			return nil
		},
	})

	if err := svc.RemoveRole(context.Background(), uuid.New(), "admin", uuid.New()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotRole != entity.RoleAdmin {
		t.Errorf("expected %q, got %q", entity.RoleAdmin, gotRole)
	}
}

func TestRemoveRole_UnknownRoleIsBadRequest(t *testing.T) {
	err := newRoleService(nil, nil).RemoveRole(context.Background(), uuid.New(), "superuser", uuid.New())
	if err == nil {
		t.Fatal("expected an error for an unknown role")
	}
	assertStatus(t, err, http.StatusBadRequest)
}

// The old implementation returned a bare fmt.Errorf here, which carried no HTTP
// status and would have been reported as an unstructured error.
func TestRemoveRole_StoreErrorIsInternal(t *testing.T) {
	svc := newRoleService(nil, &stubRoleStore{
		removeRole: func(context.Context, uuid.UUID, entity.Role) error {
			return errors.New("connection refused")
		},
	})

	err := svc.RemoveRole(context.Background(), uuid.New(), "admin", uuid.New())
	if err == nil {
		t.Fatal("expected an error")
	}
	assertStatus(t, err, http.StatusInternalServerError)
}

// --------------------------------------------------------------------------
// GetUserRoles
// --------------------------------------------------------------------------

func TestGetUserRoles_IncludesAuditTrail(t *testing.T) {
	admin := uuid.New()
	assignedAt := time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC)

	svc := newRoleService(nil, &stubRoleStore{
		getUserRoles: func(context.Context, uuid.UUID) ([]*entity.RoleAssignment, error) {
			return []*entity.RoleAssignment{
				{Role: entity.RoleAdmin, AssignedAt: assignedAt, AssignedBy: &admin},
				{Role: entity.RoleModerator, AssignedAt: assignedAt},
			}, nil
		},
	})

	resp, err := svc.GetUserRoles(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Roles) != 2 {
		t.Fatalf("expected 2 roles, got %d", len(resp.Roles))
	}

	granted := resp.Roles[0]
	if granted.Role != "admin" || granted.Description == "" {
		t.Errorf("unexpected role payload: %+v", granted)
	}
	if granted.AssignedAt != "2026-01-15T10:30:00Z" {
		t.Errorf("unexpected assigned_at: %q", granted.AssignedAt)
	}
	if granted.AssignedBy == nil || *granted.AssignedBy != admin.String() {
		t.Error("assigned_by should carry the granting admin")
	}
	if resp.Roles[1].AssignedBy != nil {
		t.Error("assigned_by should be null for a system grant")
	}
}

func TestGetUserRoles_StoreErrorIsInternal(t *testing.T) {
	svc := newRoleService(nil, &stubRoleStore{
		getUserRoles: func(context.Context, uuid.UUID) ([]*entity.RoleAssignment, error) {
			return nil, errors.New("connection refused")
		},
	})

	_, err := svc.GetUserRoles(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("expected an error")
	}
	assertStatus(t, err, http.StatusInternalServerError)
}
