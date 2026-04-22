package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/feature/user/dto"
	"github.com/jarviisha/darkvoid/pkg/errors"
)

func TestLogout_Success(t *testing.T) {
	revokeCalled := false
	rtRepo := &mockRefreshTokenRepo{
		revoke: func(_ context.Context, _ string) error {
			revokeCalled = true
			return nil
		},
	}
	svc := newAuthService(&mockUserRepo{}, rtRepo, newTestJWT(t))

	err := svc.Logout(context.Background(), &dto.LogoutRequest{RefreshToken: "some-token"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !revokeCalled {
		t.Error("expected Revoke to be called")
	}
}

func TestLogout_EmptyToken(t *testing.T) {
	svc := newAuthService(&mockUserRepo{}, &mockRefreshTokenRepo{}, newTestJWT(t))

	err := svc.Logout(context.Background(), &dto.LogoutRequest{RefreshToken: ""})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "BAD_REQUEST")
}

func TestLogout_RevokeFailure(t *testing.T) {
	rtRepo := &mockRefreshTokenRepo{
		revoke: func(_ context.Context, _ string) error {
			return errors.ErrInternal
		},
	}
	svc := newAuthService(&mockUserRepo{}, rtRepo, newTestJWT(t))

	err := svc.Logout(context.Background(), &dto.LogoutRequest{RefreshToken: "some-token"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "INTERNAL_ERROR")
}

func TestLogoutAllSessions_Success(t *testing.T) {
	revokeAllCalled := false
	rtRepo := &mockRefreshTokenRepo{
		revokeAllUserTokens: func(_ context.Context, _ uuid.UUID) error {
			revokeAllCalled = true
			return nil
		},
	}
	svc := newAuthService(&mockUserRepo{}, rtRepo, newTestJWT(t))

	err := svc.LogoutAllSessions(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !revokeAllCalled {
		t.Error("expected RevokeAllUserTokens to be called")
	}
}

func TestLogoutAllSessions_RevokeAllFails(t *testing.T) {
	rtRepo := &mockRefreshTokenRepo{
		revokeAllUserTokens: func(_ context.Context, _ uuid.UUID) error {
			return errors.ErrInternal
		},
	}
	svc := newAuthService(&mockUserRepo{}, rtRepo, newTestJWT(t))

	err := svc.LogoutAllSessions(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "INTERNAL_ERROR")
}
