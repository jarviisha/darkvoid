package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/feature/user/entity"
	"github.com/jarviisha/darkvoid/pkg/errors"
)

func TestChangePassword_Success(t *testing.T) {
	userID := uuid.New()
	hashedPw, _ := hashPassword("OldPass123")
	updateCalled := false

	userRepo := &mockUserRepo{
		getUserByID: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
			u := activeUser(userID)
			u.PasswordHash = hashedPw
			return u, nil
		},
		updateUserPassword: func(_ context.Context, _ uuid.UUID, _ string, _ *uuid.UUID) error {
			updateCalled = true
			return nil
		},
	}
	svc := newAuthService(userRepo, &mockRefreshTokenRepo{}, newTestJWT(t))

	err := svc.ChangePassword(context.Background(), userID, "OldPass123", "NewPass456")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !updateCalled {
		t.Error("expected UpdateUserPassword to be called")
	}
}

func TestChangePassword_WrongOldPassword(t *testing.T) {
	userID := uuid.New()
	hashedPw, _ := hashPassword("CorrectOldPass123")

	userRepo := &mockUserRepo{
		getUserByID: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
			u := activeUser(userID)
			u.PasswordHash = hashedPw
			return u, nil
		},
	}
	svc := newAuthService(userRepo, &mockRefreshTokenRepo{}, newTestJWT(t))

	err := svc.ChangePassword(context.Background(), userID, "WrongOldPass123", "NewPass456")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "UNAUTHORIZED")
}

func TestChangePassword_EmptyOldPassword(t *testing.T) {
	svc := newAuthService(&mockUserRepo{}, &mockRefreshTokenRepo{}, newTestJWT(t))

	err := svc.ChangePassword(context.Background(), uuid.New(), "", "NewPass456")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "BAD_REQUEST")
}

func TestChangePassword_EmptyNewPassword(t *testing.T) {
	svc := newAuthService(&mockUserRepo{}, &mockRefreshTokenRepo{}, newTestJWT(t))

	err := svc.ChangePassword(context.Background(), uuid.New(), "OldPass123", "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "BAD_REQUEST")
}

func TestChangePassword_UserNotFound(t *testing.T) {
	userRepo := &mockUserRepo{
		getUserByID: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
			return nil, errors.ErrNotFound
		},
	}
	svc := newAuthService(userRepo, &mockRefreshTokenRepo{}, newTestJWT(t))

	err := svc.ChangePassword(context.Background(), uuid.New(), "OldPass123", "NewPass456")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "NOT_FOUND")
}

func TestChangePassword_InactiveUser(t *testing.T) {
	userID := uuid.New()
	hashedPw, _ := hashPassword("OldPass123")
	userRepo := &mockUserRepo{
		getUserByID: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
			u := activeUser(userID)
			u.PasswordHash = hashedPw
			u.IsActive = false
			return u, nil
		},
	}
	svc := newAuthService(userRepo, &mockRefreshTokenRepo{}, newTestJWT(t))

	err := svc.ChangePassword(context.Background(), userID, "OldPass123", "NewPass456")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "FORBIDDEN")
}

func TestChangePassword_UpdatePasswordFailure(t *testing.T) {
	userID := uuid.New()
	hashedPw, _ := hashPassword("OldPass123")
	userRepo := &mockUserRepo{
		getUserByID: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
			u := activeUser(userID)
			u.PasswordHash = hashedPw
			return u, nil
		},
		updateUserPassword: func(_ context.Context, _ uuid.UUID, _ string, _ *uuid.UUID) error {
			return errors.ErrInternal
		},
	}
	svc := newAuthService(userRepo, &mockRefreshTokenRepo{}, newTestJWT(t))

	err := svc.ChangePassword(context.Background(), userID, "OldPass123", "NewPass456")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "INTERNAL_ERROR")
}

func TestChangePassword_RevokesAllTokensAfterChange(t *testing.T) {
	userID := uuid.New()
	hashedPw, _ := hashPassword("OldPass123")
	revokeAllCalled := false

	userRepo := &mockUserRepo{
		getUserByID: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
			u := activeUser(userID)
			u.PasswordHash = hashedPw
			return u, nil
		},
	}
	rtRepo := &mockRefreshTokenRepo{
		revokeAllUserTokens: func(_ context.Context, _ uuid.UUID) error {
			revokeAllCalled = true
			return nil
		},
	}
	svc := newAuthService(userRepo, rtRepo, newTestJWT(t))

	err := svc.ChangePassword(context.Background(), userID, "OldPass123", "NewPass456")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !revokeAllCalled {
		t.Error("expected all tokens to be revoked after password change")
	}
}

func TestChangePassword_RevokeAllTokensFailureStillSucceeds(t *testing.T) {
	userID := uuid.New()
	hashedPw, _ := hashPassword("OldPass123")
	updateCalled := false

	userRepo := &mockUserRepo{
		getUserByID: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
			u := activeUser(userID)
			u.PasswordHash = hashedPw
			return u, nil
		},
		updateUserPassword: func(_ context.Context, _ uuid.UUID, _ string, _ *uuid.UUID) error {
			updateCalled = true
			return nil
		},
	}
	rtRepo := &mockRefreshTokenRepo{
		revokeAllUserTokens: func(_ context.Context, _ uuid.UUID) error {
			return errors.ErrInternal
		},
	}
	svc := newAuthService(userRepo, rtRepo, newTestJWT(t))

	err := svc.ChangePassword(context.Background(), userID, "OldPass123", "NewPass456")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !updateCalled {
		t.Fatal("expected UpdateUserPassword to be called")
	}
}
