package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/feature/user/entity"
	"github.com/jarviisha/darkvoid/pkg/errors"
)

func TestGetMe_Success(t *testing.T) {
	userID := uuid.New()
	userRepo := &mockUserRepo{
		getUserByID: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
			return activeUser(userID), nil
		},
	}
	svc := newAuthService(userRepo, &mockRefreshTokenRepo{}, newTestJWT(t))

	user, err := svc.GetMe(context.Background(), userID)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if user.ID != userID {
		t.Errorf("expected user id %v, got %v", userID, user.ID)
	}
}

func TestGetMe_InactiveUser(t *testing.T) {
	userID := uuid.New()
	userRepo := &mockUserRepo{
		getUserByID: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
			u := activeUser(userID)
			u.IsActive = false
			return u, nil
		},
	}
	svc := newAuthService(userRepo, &mockRefreshTokenRepo{}, newTestJWT(t))

	_, err := svc.GetMe(context.Background(), userID)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "FORBIDDEN")
}

func TestGetMe_NotFound(t *testing.T) {
	userRepo := &mockUserRepo{
		getUserByID: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
			return nil, errors.ErrNotFound
		},
	}
	svc := newAuthService(userRepo, &mockRefreshTokenRepo{}, newTestJWT(t))

	_, err := svc.GetMe(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "NOT_FOUND")
}
