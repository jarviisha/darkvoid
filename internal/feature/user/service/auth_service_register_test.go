package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/feature/user/dto"
	"github.com/jarviisha/darkvoid/internal/feature/user/entity"
	"github.com/jarviisha/darkvoid/pkg/errors"
)

func TestRegister_CreateUserFails(t *testing.T) {
	svc := newAuthService(&mockUserRepo{
		existsUsername: func(_ context.Context, _ string) (bool, error) {
			return true, nil
		},
	}, &mockRefreshTokenRepo{}, newTestJWT(t))

	_, err := svc.Register(context.Background(), &dto.RegisterRequest{
		Username:    "johndoe",
		Email:       "john@example.com",
		DisplayName: "John Doe",
		Password:    "SecurePass123",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "CONFLICT")
}

func TestRegister_RefreshTokenGenerationFails(t *testing.T) {
	userID := uuid.New()
	userRepo := &mockUserRepo{
		createUser: func(_ context.Context, u *entity.User) (*entity.User, error) {
			u.ID = userID
			return u, nil
		},
	}
	rtRepo := &mockRefreshTokenRepo{
		create: func(_ context.Context, _ string, _ uuid.UUID, _ time.Time) (*entity.RefreshToken, error) {
			return nil, errors.ErrInternal
		},
	}
	svc := newAuthService(userRepo, rtRepo, newTestJWT(t))

	_, err := svc.Register(context.Background(), &dto.RegisterRequest{
		Username:    "johndoe",
		Email:       "john@example.com",
		DisplayName: "John Doe",
		Password:    "SecurePass123",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "INTERNAL_ERROR")
}

func TestRegister_AccessTokenGenerationFails(t *testing.T) {
	userID := uuid.New()
	userRepo := &mockUserRepo{
		createUser: func(_ context.Context, u *entity.User) (*entity.User, error) {
			u.ID = userID
			return u, nil
		},
	}
	rtSvc := &RefreshTokenService{repo: &mockRefreshTokenRepo{}, expiry: 7 * 24 * time.Hour}
	userSvc := &UserService{userRepo: userRepo, storage: nil}
	svc := &AuthService{
		userRepo:    userRepo,
		userService: userSvc,
		accessTokenService: &mockAccessTokenService{
			generateToken: func(string) (string, error) {
				return "", errors.ErrInternal
			},
		},
		refreshTokenService: rtSvc,
	}

	_, err := svc.Register(context.Background(), &dto.RegisterRequest{
		Username:    "johndoe",
		Email:       "john@example.com",
		DisplayName: "John Doe",
		Password:    "SecurePass123",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "INTERNAL_ERROR")
}
