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

func TestLogin_Success(t *testing.T) {
	userID := uuid.New()
	hashedPw, _ := hashPassword("SecurePass123")
	user := &entity.User{
		ID:           userID,
		Username:     "johndoe",
		PasswordHash: hashedPw,
		IsActive:     true,
		DisplayName:  "John Doe",
	}

	userRepo := &mockUserRepo{
		getUserByUsername: func(_ context.Context, _ string) (*entity.User, error) {
			return user, nil
		},
	}
	rtRepo := &mockRefreshTokenRepo{}
	jwtSvc := newTestJWT(t)

	svc := newAuthService(userRepo, rtRepo, jwtSvc)

	resp, err := svc.Login(context.Background(), &dto.LoginRequest{
		Username: "johndoe",
		Password: "SecurePass123",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.AccessToken == "" {
		t.Error("expected non-empty access token")
	}
	if resp.RefreshToken == "" {
		t.Error("expected non-empty refresh token")
	}
	if resp.TokenType != "Bearer" {
		t.Errorf("expected token type Bearer, got %q", resp.TokenType)
	}
}

func TestLogin_EmptyUsername(t *testing.T) {
	svc := newAuthService(&mockUserRepo{}, &mockRefreshTokenRepo{}, newTestJWT(t))

	_, err := svc.Login(context.Background(), &dto.LoginRequest{
		Username: "",
		Password: "SecurePass123",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "BAD_REQUEST")
}

func TestLogin_EmptyPassword(t *testing.T) {
	svc := newAuthService(&mockUserRepo{}, &mockRefreshTokenRepo{}, newTestJWT(t))

	_, err := svc.Login(context.Background(), &dto.LoginRequest{
		Username: "johndoe",
		Password: "",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "BAD_REQUEST")
}

func TestLogin_WrongPassword(t *testing.T) {
	hashedPw, _ := hashPassword("CorrectPass123")
	userRepo := &mockUserRepo{
		getUserByUsername: func(_ context.Context, _ string) (*entity.User, error) {
			return &entity.User{
				ID:           uuid.New(),
				Username:     "johndoe",
				PasswordHash: hashedPw,
				IsActive:     true,
				DisplayName:  "John Doe",
			}, nil
		},
	}
	svc := newAuthService(userRepo, &mockRefreshTokenRepo{}, newTestJWT(t))

	_, err := svc.Login(context.Background(), &dto.LoginRequest{
		Username: "johndoe",
		Password: "WrongPass123",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "UNAUTHORIZED")
}

func TestLogin_UserNotFound_ReturnsUnauthorized(t *testing.T) {
	userRepo := &mockUserRepo{
		getUserByUsername: func(_ context.Context, _ string) (*entity.User, error) {
			return nil, errors.New("NOT_FOUND", "not found", 404)
		},
	}
	svc := newAuthService(userRepo, &mockRefreshTokenRepo{}, newTestJWT(t))

	_, err := svc.Login(context.Background(), &dto.LoginRequest{
		Username: "nobody",
		Password: "SomePass123",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "UNAUTHORIZED")
}

func TestLogin_InactiveUser(t *testing.T) {
	hashedPw, _ := hashPassword("SecurePass123")
	userRepo := &mockUserRepo{
		getUserByUsername: func(_ context.Context, _ string) (*entity.User, error) {
			return &entity.User{
				ID:           uuid.New(),
				Username:     "johndoe",
				PasswordHash: hashedPw,
				IsActive:     false,
				DisplayName:  "John Doe",
			}, nil
		},
	}
	svc := newAuthService(userRepo, &mockRefreshTokenRepo{}, newTestJWT(t))

	_, err := svc.Login(context.Background(), &dto.LoginRequest{
		Username: "johndoe",
		Password: "SecurePass123",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "FORBIDDEN")
}

func TestLogin_AccessTokenGenerationFails(t *testing.T) {
	hashedPw, _ := hashPassword("SecurePass123")
	userRepo := &mockUserRepo{
		getUserByUsername: func(_ context.Context, _ string) (*entity.User, error) {
			return &entity.User{
				ID:           uuid.New(),
				Username:     "johndoe",
				PasswordHash: hashedPw,
				IsActive:     true,
				DisplayName:  "John Doe",
			}, nil
		},
	}
	svc := &AuthService{
		userRepo:    userRepo,
		userService: &UserService{userRepo: userRepo, storage: nil},
		accessTokenService: &mockAccessTokenService{
			generateToken: func(string) (string, error) {
				return "", errors.ErrInternal
			},
		},
		refreshTokenService: &RefreshTokenService{repo: &mockRefreshTokenRepo{}, expiry: 7 * 24 * time.Hour},
	}

	_, err := svc.Login(context.Background(), &dto.LoginRequest{
		Username: "johndoe",
		Password: "SecurePass123",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "INTERNAL_ERROR")
}
