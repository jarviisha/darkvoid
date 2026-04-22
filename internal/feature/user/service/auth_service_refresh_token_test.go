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

func TestRefreshAccessToken_Success(t *testing.T) {
	userID := uuid.New()
	_, rtRepo := newValidToken(t, userID)
	rtRepo.revoke = func(_ context.Context, _ string) error { return nil }

	userRepo := &mockUserRepo{
		getUserByID: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
			return activeUser(userID), nil
		},
	}
	svc := newAuthService(userRepo, rtRepo, newTestJWT(t))

	resp, err := svc.RefreshAccessToken(context.Background(), &dto.RefreshTokenRequest{
		RefreshToken: "valid-refresh-token",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.AccessToken == "" {
		t.Error("expected non-empty access token")
	}
	if resp.RefreshToken == "" {
		t.Error("expected new refresh token")
	}
}

func TestRefreshAccessToken_EmptyToken(t *testing.T) {
	svc := newAuthService(&mockUserRepo{}, &mockRefreshTokenRepo{}, newTestJWT(t))

	_, err := svc.RefreshAccessToken(context.Background(), &dto.RefreshTokenRequest{
		RefreshToken: "",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "BAD_REQUEST")
}

func TestRefreshAccessToken_ExpiredToken(t *testing.T) {
	userID := uuid.New()
	expiredToken := &entity.RefreshToken{
		ID:        uuid.New(),
		Token:     "expired-token",
		UserID:    userID,
		ExpiresAt: time.Now().Add(-1 * time.Hour),
		IsRevoked: false,
	}
	rtRepo := &mockRefreshTokenRepo{
		getByToken: func(_ context.Context, _ string) (*entity.RefreshToken, error) {
			return expiredToken, nil
		},
	}
	svc := newAuthService(&mockUserRepo{}, rtRepo, newTestJWT(t))

	_, err := svc.RefreshAccessToken(context.Background(), &dto.RefreshTokenRequest{
		RefreshToken: "expired-token",
	})
	if err == nil {
		t.Fatal("expected error for expired token, got nil")
	}
	assertServiceErrorCode(t, err, "UNAUTHORIZED")
}

func TestRefreshAccessToken_RevokedToken(t *testing.T) {
	userID := uuid.New()
	revokedToken := &entity.RefreshToken{
		ID:        uuid.New(),
		Token:     "revoked-token",
		UserID:    userID,
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
		IsRevoked: true,
	}
	rtRepo := &mockRefreshTokenRepo{
		getByToken: func(_ context.Context, _ string) (*entity.RefreshToken, error) {
			return revokedToken, nil
		},
	}
	svc := newAuthService(&mockUserRepo{}, rtRepo, newTestJWT(t))

	_, err := svc.RefreshAccessToken(context.Background(), &dto.RefreshTokenRequest{
		RefreshToken: "revoked-token",
	})
	if err == nil {
		t.Fatal("expected error for revoked token, got nil")
	}
	assertServiceErrorCode(t, err, "UNAUTHORIZED")
}

func TestRefreshAccessToken_InactiveUser(t *testing.T) {
	userID := uuid.New()
	_, rtRepo := newValidToken(t, userID)

	userRepo := &mockUserRepo{
		getUserByID: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
			u := activeUser(userID)
			u.IsActive = false
			return u, nil
		},
	}
	svc := newAuthService(userRepo, rtRepo, newTestJWT(t))

	_, err := svc.RefreshAccessToken(context.Background(), &dto.RefreshTokenRequest{
		RefreshToken: "valid-refresh-token",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "FORBIDDEN")
}

func TestRefreshAccessToken_AccessTokenGenerationFails(t *testing.T) {
	userID := uuid.New()
	_, rtRepo := newValidToken(t, userID)
	userRepo := &mockUserRepo{
		getUserByID: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
			return activeUser(userID), nil
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
		refreshTokenService: &RefreshTokenService{repo: rtRepo, expiry: 7 * 24 * time.Hour},
	}

	_, err := svc.RefreshAccessToken(context.Background(), &dto.RefreshTokenRequest{
		RefreshToken: "valid-refresh-token",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "INTERNAL_ERROR")
}

func TestRefreshAccessToken_UserLookupFailsReturnsUnauthorized(t *testing.T) {
	userID := uuid.New()
	_, rtRepo := newValidToken(t, userID)
	userRepo := &mockUserRepo{
		getUserByID: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
			return nil, errors.ErrInternal
		},
	}
	svc := newAuthService(userRepo, rtRepo, newTestJWT(t))

	_, err := svc.RefreshAccessToken(context.Background(), &dto.RefreshTokenRequest{
		RefreshToken: "valid-refresh-token",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "UNAUTHORIZED")
}

func TestRefreshAccessToken_RevokeOldTokenFailureStillSucceeds(t *testing.T) {
	userID := uuid.New()
	revokeCalled := false
	rtRepo := &mockRefreshTokenRepo{
		getByToken: func(_ context.Context, _ string) (*entity.RefreshToken, error) {
			return &entity.RefreshToken{
				ID:        uuid.New(),
				Token:     "valid-refresh-token",
				UserID:    userID,
				ExpiresAt: time.Now().Add(time.Hour),
			}, nil
		},
		revoke: func(_ context.Context, _ string) error {
			revokeCalled = true
			return errors.ErrInternal
		},
		create: func(_ context.Context, token string, gotUserID uuid.UUID, expiresAt time.Time) (*entity.RefreshToken, error) {
			return &entity.RefreshToken{
				ID:        uuid.New(),
				Token:     token,
				UserID:    gotUserID,
				ExpiresAt: expiresAt,
			}, nil
		},
	}
	userRepo := &mockUserRepo{
		getUserByID: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
			return activeUser(userID), nil
		},
	}
	svc := newAuthService(userRepo, rtRepo, newTestJWT(t))

	resp, err := svc.RefreshAccessToken(context.Background(), &dto.RefreshTokenRequest{
		RefreshToken: "valid-refresh-token",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !revokeCalled {
		t.Fatal("expected RevokeToken to be called")
	}
	if resp.AccessToken == "" || resp.RefreshToken == "" {
		t.Fatal("expected refreshed tokens")
	}
}

func TestRefreshAccessToken_NewRefreshTokenGenerationFails(t *testing.T) {
	userID := uuid.New()
	revokeCalled := false
	rtRepo := &mockRefreshTokenRepo{
		getByToken: func(_ context.Context, _ string) (*entity.RefreshToken, error) {
			return &entity.RefreshToken{
				ID:        uuid.New(),
				Token:     "valid-refresh-token",
				UserID:    userID,
				ExpiresAt: time.Now().Add(time.Hour),
			}, nil
		},
		revoke: func(_ context.Context, _ string) error {
			revokeCalled = true
			return nil
		},
		create: func(_ context.Context, _ string, _ uuid.UUID, _ time.Time) (*entity.RefreshToken, error) {
			return nil, errors.ErrInternal
		},
	}
	userRepo := &mockUserRepo{
		getUserByID: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
			return activeUser(userID), nil
		},
	}
	svc := newAuthService(userRepo, rtRepo, newTestJWT(t))

	_, err := svc.RefreshAccessToken(context.Background(), &dto.RefreshTokenRequest{
		RefreshToken: "valid-refresh-token",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !revokeCalled {
		t.Fatal("expected old token revocation before new token generation")
	}
	assertServiceErrorCode(t, err, "INTERNAL_ERROR")
}
