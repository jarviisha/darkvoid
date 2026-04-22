package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/feature/user/entity"
	"github.com/jarviisha/darkvoid/pkg/errors"
	"github.com/jarviisha/darkvoid/pkg/jwt"
)

type mockRefreshTokenRepo struct {
	create              func(ctx context.Context, token string, userID uuid.UUID, expiresAt time.Time) (*entity.RefreshToken, error)
	getByToken          func(ctx context.Context, token string) (*entity.RefreshToken, error)
	revoke              func(ctx context.Context, token string) error
	revokeAllUserTokens func(ctx context.Context, userID uuid.UUID) error
	deleteExpired       func(ctx context.Context) error
}

func (m *mockRefreshTokenRepo) Create(ctx context.Context, token string, userID uuid.UUID, expiresAt time.Time) (*entity.RefreshToken, error) {
	if m.create != nil {
		return m.create(ctx, token, userID, expiresAt)
	}
	return &entity.RefreshToken{
		ID:        uuid.New(),
		Token:     token,
		UserID:    userID,
		ExpiresAt: expiresAt,
		CreatedAt: time.Now(),
	}, nil
}

func (m *mockRefreshTokenRepo) GetByToken(ctx context.Context, token string) (*entity.RefreshToken, error) {
	if m.getByToken != nil {
		return m.getByToken(ctx, token)
	}
	return nil, errors.New("NOT_FOUND", "token not found", 404)
}

func (m *mockRefreshTokenRepo) Revoke(ctx context.Context, token string) error {
	if m.revoke != nil {
		return m.revoke(ctx, token)
	}
	return nil
}

func (m *mockRefreshTokenRepo) RevokeAllUserTokens(ctx context.Context, userID uuid.UUID) error {
	if m.revokeAllUserTokens != nil {
		return m.revokeAllUserTokens(ctx, userID)
	}
	return nil
}

func (m *mockRefreshTokenRepo) DeleteExpired(ctx context.Context) error {
	if m.deleteExpired != nil {
		return m.deleteExpired(ctx)
	}
	return nil
}

type mockAccessTokenService struct {
	generateToken     func(subject string) (string, error)
	getExpiryDuration func() time.Duration
}

func (m *mockAccessTokenService) GenerateToken(subject string) (string, error) {
	if m.generateToken != nil {
		return m.generateToken(subject)
	}
	return "access-token", nil
}

func (m *mockAccessTokenService) GetExpiryDuration() time.Duration {
	if m.getExpiryDuration != nil {
		return m.getExpiryDuration()
	}
	return 15 * time.Minute
}

func newTestJWT(t *testing.T) *jwt.Service {
	t.Helper()
	svc, err := jwt.NewService(jwt.Config{
		Secret: []byte("test-secret-key-32-bytes-minimum!!"),
		Issuer: "test",
		Expiry: 15 * time.Minute,
	})
	if err != nil {
		t.Fatalf("failed to create JWT service: %v", err)
	}
	return svc
}

func newValidToken(t *testing.T, userID uuid.UUID) (*entity.RefreshToken, *mockRefreshTokenRepo) { //nolint:unparam // first return used by callers for setup context
	t.Helper()
	tok := &entity.RefreshToken{
		ID:        uuid.New(),
		Token:     "valid-refresh-token",
		UserID:    userID,
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
		IsRevoked: false,
	}
	repo := &mockRefreshTokenRepo{
		getByToken: func(_ context.Context, _ string) (*entity.RefreshToken, error) {
			return tok, nil
		},
	}
	return tok, repo
}

type welcomeEmailCall struct {
	ctxErr   error
	email    string
	username string
}

type verificationEmailCall struct {
	ctxErr   error
	userID   uuid.UUID
	email    string
	username string
}

type mockEmailSender struct {
	mu                sync.Mutex
	welcomeCalls      []welcomeEmailCall
	verificationCalls []verificationEmailCall
	wg                *sync.WaitGroup
}

func (m *mockEmailSender) SendWelcome(ctx context.Context, email, username string) {
	m.mu.Lock()
	m.welcomeCalls = append(m.welcomeCalls, welcomeEmailCall{ctxErr: ctx.Err(), email: email, username: username})
	m.mu.Unlock()
	if m.wg != nil {
		m.wg.Done()
	}
}

func (m *mockEmailSender) SendVerification(ctx context.Context, userID uuid.UUID, email, username string) {
	m.mu.Lock()
	m.verificationCalls = append(m.verificationCalls, verificationEmailCall{ctxErr: ctx.Err(), userID: userID, email: email, username: username})
	m.mu.Unlock()
	if m.wg != nil {
		m.wg.Done()
	}
}

func newAuthService(userRepo userRepo, rtRepo refreshTokenRepo, jwtSvc *jwt.Service) *AuthService {
	rtSvc := &RefreshTokenService{repo: rtRepo, expiry: 7 * 24 * time.Hour}
	userSvc := &UserService{userRepo: userRepo, storage: nil}
	return &AuthService{
		userRepo:            userRepo,
		userService:         userSvc,
		accessTokenService:  jwtSvc,
		refreshTokenService: rtSvc,
	}
}
