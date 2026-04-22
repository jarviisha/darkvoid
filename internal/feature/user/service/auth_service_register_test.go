package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/feature/user/dto"
	"github.com/jarviisha/darkvoid/internal/feature/user/entity"
	"github.com/jarviisha/darkvoid/pkg/errors"
)

func TestRegister_Success(t *testing.T) {
	userID := uuid.New()
	userRepo := &mockUserRepo{
		createUser: func(_ context.Context, u *entity.User) (*entity.User, error) {
			u.ID = userID
			return u, nil
		},
	}
	svc := newAuthService(userRepo, &mockRefreshTokenRepo{}, newTestJWT(t))

	resp, err := svc.Register(context.Background(), &dto.RegisterRequest{
		Username:    "johndoe",
		Email:       "john@example.com",
		DisplayName: "John Doe",
		Password:    "SecurePass123",
	})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if resp.UserID != userID.String() {
		t.Errorf("UserID = %q, want %q", resp.UserID, userID.String())
	}
	if resp.AccessToken == "" {
		t.Error("AccessToken should not be empty")
	}
	if resp.RefreshToken == "" {
		t.Error("RefreshToken should not be empty")
	}
	if resp.TokenType != "Bearer" {
		t.Errorf("TokenType = %q, want %q", resp.TokenType, "Bearer")
	}
	wantAccess := int64((15 * time.Minute).Seconds())
	if resp.AccessExpiresIn != wantAccess {
		t.Errorf("AccessExpiresIn = %d, want %d", resp.AccessExpiresIn, wantAccess)
	}
	wantRefresh := int64((7 * 24 * time.Hour).Seconds())
	if resp.RefreshExpiresIn != wantRefresh {
		t.Errorf("RefreshExpiresIn = %d, want %d", resp.RefreshExpiresIn, wantRefresh)
	}
}

func TestRegister_SendsWelcomeAndVerificationEmails(t *testing.T) {
	userID := uuid.New()
	userRepo := &mockUserRepo{
		createUser: func(_ context.Context, u *entity.User) (*entity.User, error) {
			u.ID = userID
			return u, nil
		},
	}
	svc := newAuthService(userRepo, &mockRefreshTokenRepo{}, newTestJWT(t))

	var wg sync.WaitGroup
	wg.Add(2)
	sender := &mockEmailSender{wg: &wg}
	svc.WithEmailSender(sender)

	_, err := svc.Register(context.Background(), &dto.RegisterRequest{
		Username:    "johndoe",
		Email:       "john@example.com",
		DisplayName: "John Doe",
		Password:    "SecurePass123",
	})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	waitForGoroutines(t, &wg)

	sender.mu.Lock()
	defer sender.mu.Unlock()

	if len(sender.welcomeCalls) != 1 {
		t.Fatalf("expected 1 welcome call, got %d", len(sender.welcomeCalls))
	}
	if call := sender.welcomeCalls[0]; call.email != "john@example.com" || call.username != "johndoe" {
		t.Errorf("welcome call = {email:%q, username:%q}, want {email:%q, username:%q}",
			call.email, call.username, "john@example.com", "johndoe")
	}

	if len(sender.verificationCalls) != 1 {
		t.Fatalf("expected 1 verification call, got %d", len(sender.verificationCalls))
	}
	if call := sender.verificationCalls[0]; call.userID != userID || call.email != "john@example.com" || call.username != "johndoe" {
		t.Errorf("verification call = {userID:%v, email:%q, username:%q}, want {userID:%v, email:%q, username:%q}",
			call.userID, call.email, call.username, userID, "john@example.com", "johndoe")
	}
}

// The post-register emails are fire-and-forget and must outlive the request context
// (context.WithoutCancel in auth_service.go), otherwise a quick client disconnect
// would kill the welcome/verification mailers before SMTP finishes.
func TestRegister_EmailContextOutlivesParentCancel(t *testing.T) {
	userID := uuid.New()
	userRepo := &mockUserRepo{
		createUser: func(_ context.Context, u *entity.User) (*entity.User, error) {
			u.ID = userID
			return u, nil
		},
	}
	svc := newAuthService(userRepo, &mockRefreshTokenRepo{}, newTestJWT(t))

	var wg sync.WaitGroup
	wg.Add(2)
	sender := &mockEmailSender{wg: &wg}
	svc.WithEmailSender(sender)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := svc.Register(ctx, &dto.RegisterRequest{
		Username:    "johndoe",
		Email:       "john@example.com",
		DisplayName: "John Doe",
		Password:    "SecurePass123",
	})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	waitForGoroutines(t, &wg)

	sender.mu.Lock()
	defer sender.mu.Unlock()

	if len(sender.welcomeCalls) != 1 || len(sender.verificationCalls) != 1 {
		t.Fatalf("expected 1 welcome + 1 verification, got %d + %d",
			len(sender.welcomeCalls), len(sender.verificationCalls))
	}
	if cerr := sender.welcomeCalls[0].ctxErr; cerr != nil {
		t.Errorf("welcome ctx should survive parent cancel, got: %v", cerr)
	}
	if cerr := sender.verificationCalls[0].ctxErr; cerr != nil {
		t.Errorf("verification ctx should survive parent cancel, got: %v", cerr)
	}
}

const goroutineWaitTimeout = 2 * time.Second

func waitForGoroutines(t *testing.T, wg *sync.WaitGroup) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(goroutineWaitTimeout):
		t.Fatal("timed out waiting for goroutines")
	}
}

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
