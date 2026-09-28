package service

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/jarviisha/darkvoid/internal/feature/user/entity"
	pkgerrors "github.com/jarviisha/darkvoid/pkg/errors"
)

func TestAdminResetPassword_Success(t *testing.T) {
	id := uuid.New()
	var gotHash string
	repo := &mockUserRepo{
		getUserByID: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
			return &entity.User{ID: id, Username: "root", IsActive: true}, nil
		},
		updateUserPassword: func(_ context.Context, _ uuid.UUID, hash string, _ *uuid.UUID) error {
			gotHash = hash
			return nil
		},
	}

	if err := newUserService(repo).AdminResetPassword(context.Background(), id, "NewPass123"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotHash == "" || gotHash == "NewPass123" {
		t.Fatalf("password must be stored hashed, got %q", gotHash)
	}
}

// An operator reset usually answers a compromised account, so it ends the
// sessions the password protected.
func TestAdminResetPassword_RevokesSessions(t *testing.T) {
	id := uuid.New()
	var revoked uuid.UUID
	svc := &UserService{
		userRepo: &mockUserRepo{
			getUserByID: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
				return &entity.User{ID: id, IsActive: true}, nil
			},
		},
		sessions: &mockRefreshTokenRepo{
			revokeAllUserTokens: func(_ context.Context, userID uuid.UUID) error {
				revoked = userID
				return nil
			},
		},
	}

	if err := svc.AdminResetPassword(context.Background(), id, "NewPass123"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if revoked != id {
		t.Fatalf("expected sessions of %s revoked, got %s", id, revoked)
	}
}

// The operator is told when the sessions survived, so they can rerun it.
func TestAdminResetPassword_RevokeFailure(t *testing.T) {
	svc := &UserService{
		userRepo: &mockUserRepo{
			getUserByID: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
				return &entity.User{IsActive: true}, nil
			},
		},
		sessions: &mockRefreshTokenRepo{
			revokeAllUserTokens: func(context.Context, uuid.UUID) error { return pkgerrors.ErrInternal },
		},
	}

	err := svc.AdminResetPassword(context.Background(), uuid.New(), "NewPass123")
	assertServiceErrorCode(t, err, "INTERNAL_ERROR")
}

func TestAdminResetPassword_WeakPasswordRejected(t *testing.T) {
	for name, tc := range rejectedPasswords {
		t.Run(name, func(t *testing.T) {
			called := false
			repo := &mockUserRepo{
				getUserByID: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
					return &entity.User{ID: uuid.New(), IsActive: true}, nil
				},
				updateUserPassword: func(_ context.Context, _ uuid.UUID, _ string, _ *uuid.UUID) error {
					called = true
					return nil
				},
			}

			err := newUserService(repo).AdminResetPassword(context.Background(), uuid.New(), tc.password)
			assertServiceErrorCode(t, err, tc.code)
			assertErrorField(t, err, "password")
			if called {
				t.Fatal("must not persist a rejected password")
			}
		})
	}
}

func TestAdminResetPassword_UserNotFound(t *testing.T) {
	repo := &mockUserRepo{
		getUserByID: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
			return nil, pkgerrors.ErrNotFound
		},
	}

	err := newUserService(repo).AdminResetPassword(context.Background(), uuid.New(), "NewPass123")
	if err == nil {
		t.Fatal("expected error when user is missing")
	}
}
