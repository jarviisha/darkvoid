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

func TestAdminResetPassword_WeakPasswordRejected(t *testing.T) {
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

	// "short" fails the min-length + letters-and-numbers rule.
	err := newUserService(repo).AdminResetPassword(context.Background(), uuid.New(), "short")
	if err == nil {
		t.Fatal("expected error for weak password")
	}
	if called {
		t.Fatal("must not persist a rejected password")
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
