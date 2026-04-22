package service

import (
	"context"
	stderrors "errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/feature/user/entity"
	"github.com/jarviisha/darkvoid/internal/infrastructure/mailer"
	apperrors "github.com/jarviisha/darkvoid/pkg/errors"
)

type mockMailer struct {
	send func(ctx context.Context, msg *mailer.Message) error
}

func (m *mockMailer) Send(ctx context.Context, msg *mailer.Message) error {
	if m.send != nil {
		return m.send(ctx, msg)
	}
	return nil
}

type mockEmailTokenRepo struct {
	create              func(ctx context.Context, userID uuid.UUID, token string, tokenType entity.EmailTokenType, expiresAt time.Time) (*entity.EmailToken, error)
	getByToken          func(ctx context.Context, token string) (*entity.EmailToken, error)
	markUsed            func(ctx context.Context, id uuid.UUID) error
	deleteByUserAndType func(ctx context.Context, userID uuid.UUID, tokenType entity.EmailTokenType) error
}

func (m *mockEmailTokenRepo) Create(ctx context.Context, userID uuid.UUID, token string, tokenType entity.EmailTokenType, expiresAt time.Time) (*entity.EmailToken, error) {
	if m.create != nil {
		return m.create(ctx, userID, token, tokenType, expiresAt)
	}
	return &entity.EmailToken{
		ID:        uuid.New(),
		UserID:    userID,
		Token:     token,
		Type:      tokenType,
		ExpiresAt: expiresAt,
		CreatedAt: time.Now(),
	}, nil
}

func (m *mockEmailTokenRepo) GetByToken(ctx context.Context, token string) (*entity.EmailToken, error) {
	if m.getByToken != nil {
		return m.getByToken(ctx, token)
	}
	return nil, apperrors.ErrNotFound
}

func (m *mockEmailTokenRepo) MarkUsed(ctx context.Context, id uuid.UUID) error {
	if m.markUsed != nil {
		return m.markUsed(ctx, id)
	}
	return nil
}

func (m *mockEmailTokenRepo) DeleteByUserAndType(ctx context.Context, userID uuid.UUID, tokenType entity.EmailTokenType) error {
	if m.deleteByUserAndType != nil {
		return m.deleteByUserAndType(ctx, userID, tokenType)
	}
	return nil
}

func newEmailServiceForTest(t *testing.T, tokenRepo emailTokenRepo, userRepo userRepo, m mailer.Mailer) *EmailService {
	t.Helper()
	templates, err := mailer.LoadTemplates()
	if err != nil {
		t.Fatalf("failed to load templates: %v", err)
	}
	return NewEmailService(m, templates, tokenRepo, userRepo, "https://darkvoid.test")
}

func validVerifyToken(userID uuid.UUID) *entity.EmailToken {
	return &entity.EmailToken{
		ID:        uuid.New(),
		UserID:    userID,
		Token:     "verify-token",
		Type:      entity.EmailTokenVerify,
		ExpiresAt: time.Now().Add(time.Hour),
		CreatedAt: time.Now(),
	}
}

func validResetToken(userID uuid.UUID) *entity.EmailToken {
	return &entity.EmailToken{
		ID:        uuid.New(),
		UserID:    userID,
		Token:     "reset-token",
		Type:      entity.EmailTokenResetPassword,
		ExpiresAt: time.Now().Add(time.Hour),
		CreatedAt: time.Now(),
	}
}

func TestVerifyEmail_TokenRequired(t *testing.T) {
	svc := newEmailServiceForTest(t, &mockEmailTokenRepo{}, &mockUserRepo{}, &mockMailer{})

	err := svc.VerifyEmail(context.Background(), "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "BAD_REQUEST")
}

func TestVerifyEmail_InvalidOrExpiredToken(t *testing.T) {
	svc := newEmailServiceForTest(t, &mockEmailTokenRepo{
		getByToken: func(_ context.Context, _ string) (*entity.EmailToken, error) {
			return nil, apperrors.ErrNotFound
		},
	}, &mockUserRepo{}, &mockMailer{})

	err := svc.VerifyEmail(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "BAD_REQUEST")
}

func TestVerifyEmail_InvalidTokenType(t *testing.T) {
	userID := uuid.New()
	svc := newEmailServiceForTest(t, &mockEmailTokenRepo{
		getByToken: func(_ context.Context, _ string) (*entity.EmailToken, error) {
			token := validResetToken(userID)
			return token, nil
		},
	}, &mockUserRepo{}, &mockMailer{})

	err := svc.VerifyEmail(context.Background(), "wrong-type")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "BAD_REQUEST")
}

func TestVerifyEmail_UsedToken(t *testing.T) {
	userID := uuid.New()
	usedAt := time.Now()
	svc := newEmailServiceForTest(t, &mockEmailTokenRepo{
		getByToken: func(_ context.Context, _ string) (*entity.EmailToken, error) {
			token := validVerifyToken(userID)
			token.UsedAt = &usedAt
			return token, nil
		},
	}, &mockUserRepo{}, &mockMailer{})

	err := svc.VerifyEmail(context.Background(), "used")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "BAD_REQUEST")
}

func TestVerifyEmail_ExpiredToken(t *testing.T) {
	userID := uuid.New()
	svc := newEmailServiceForTest(t, &mockEmailTokenRepo{
		getByToken: func(_ context.Context, _ string) (*entity.EmailToken, error) {
			token := validVerifyToken(userID)
			token.ExpiresAt = time.Now().Add(-time.Minute)
			return token, nil
		},
	}, &mockUserRepo{}, &mockMailer{})

	err := svc.VerifyEmail(context.Background(), "expired")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "BAD_REQUEST")
}

func TestVerifyEmail_MarkUsedFailure(t *testing.T) {
	userID := uuid.New()
	svc := newEmailServiceForTest(t, &mockEmailTokenRepo{
		getByToken: func(_ context.Context, _ string) (*entity.EmailToken, error) {
			return validVerifyToken(userID), nil
		},
		markUsed: func(_ context.Context, _ uuid.UUID) error {
			return stderrors.New("db down")
		},
	}, &mockUserRepo{}, &mockMailer{})

	err := svc.VerifyEmail(context.Background(), "verify-token")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "INTERNAL_ERROR")
}

func TestVerifyEmail_Success(t *testing.T) {
	userID := uuid.New()
	markUsedCalled := false
	svc := newEmailServiceForTest(t, &mockEmailTokenRepo{
		getByToken: func(_ context.Context, _ string) (*entity.EmailToken, error) {
			return validVerifyToken(userID), nil
		},
		markUsed: func(_ context.Context, _ uuid.UUID) error {
			markUsedCalled = true
			return nil
		},
	}, &mockUserRepo{}, &mockMailer{})

	if err := svc.VerifyEmail(context.Background(), "verify-token"); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !markUsedCalled {
		t.Fatal("expected MarkUsed to be called")
	}
}

func TestResendVerification_EmailRequired(t *testing.T) {
	svc := newEmailServiceForTest(t, &mockEmailTokenRepo{}, &mockUserRepo{}, &mockMailer{})

	err := svc.ResendVerification(context.Background(), "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "BAD_REQUEST")
}

func TestResendVerification_UnknownEmailReturnsNil(t *testing.T) {
	svc := newEmailServiceForTest(t, &mockEmailTokenRepo{}, &mockUserRepo{
		getUserByEmail: func(_ context.Context, _ string) (*entity.User, error) {
			return nil, apperrors.ErrNotFound
		},
	}, &mockMailer{})

	if err := svc.ResendVerification(context.Background(), "missing@example.com"); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestResendVerification_SendsVerificationToken(t *testing.T) {
	userID := uuid.New()
	deleteCalled := false
	createCalled := false
	sendCalled := false
	svc := newEmailServiceForTest(t, &mockEmailTokenRepo{
		deleteByUserAndType: func(_ context.Context, gotUserID uuid.UUID, tokenType entity.EmailTokenType) error {
			deleteCalled = true
			if gotUserID != userID {
				t.Fatalf("expected user id %v, got %v", userID, gotUserID)
			}
			if tokenType != entity.EmailTokenVerify {
				t.Fatalf("expected verify token type, got %q", tokenType)
			}
			return nil
		},
		create: func(_ context.Context, gotUserID uuid.UUID, token string, tokenType entity.EmailTokenType, expiresAt time.Time) (*entity.EmailToken, error) {
			createCalled = true
			if gotUserID != userID {
				t.Fatalf("expected user id %v, got %v", userID, gotUserID)
			}
			if token == "" {
				t.Fatal("expected generated token")
			}
			if tokenType != entity.EmailTokenVerify {
				t.Fatalf("expected verify token type, got %q", tokenType)
			}
			if time.Until(expiresAt) <= 0 {
				t.Fatal("expected future expiry")
			}
			return &entity.EmailToken{ID: uuid.New(), UserID: gotUserID, Token: token, Type: tokenType, ExpiresAt: expiresAt}, nil
		},
	}, &mockUserRepo{
		getUserByEmail: func(_ context.Context, _ string) (*entity.User, error) {
			return &entity.User{ID: userID, Email: "john@example.com", Username: "johndoe"}, nil
		},
	}, &mockMailer{
		send: func(_ context.Context, msg *mailer.Message) error {
			sendCalled = true
			if len(msg.To) != 1 || msg.To[0] != "john@example.com" {
				t.Fatalf("unexpected recipients: %+v", msg.To)
			}
			if msg.Subject == "" || msg.HTML == "" || msg.Text == "" {
				t.Fatal("expected complete email message")
			}
			return nil
		},
	})

	if err := svc.ResendVerification(context.Background(), "john@example.com"); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !deleteCalled || !createCalled || !sendCalled {
		t.Fatalf("expected delete/create/send to be called, got delete=%v create=%v send=%v", deleteCalled, createCalled, sendCalled)
	}
}

func TestSendPasswordReset_EmailRequired(t *testing.T) {
	svc := newEmailServiceForTest(t, &mockEmailTokenRepo{}, &mockUserRepo{}, &mockMailer{})

	err := svc.SendPasswordReset(context.Background(), "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "BAD_REQUEST")
}

func TestSendPasswordReset_UnknownEmailReturnsNil(t *testing.T) {
	svc := newEmailServiceForTest(t, &mockEmailTokenRepo{}, &mockUserRepo{
		getUserByEmail: func(_ context.Context, _ string) (*entity.User, error) {
			return nil, apperrors.ErrNotFound
		},
	}, &mockMailer{})

	if err := svc.SendPasswordReset(context.Background(), "missing@example.com"); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestSendPasswordReset_CreateFailure(t *testing.T) {
	userID := uuid.New()
	svc := newEmailServiceForTest(t, &mockEmailTokenRepo{
		create: func(_ context.Context, _ uuid.UUID, _ string, _ entity.EmailTokenType, _ time.Time) (*entity.EmailToken, error) {
			return nil, stderrors.New("insert failed")
		},
	}, &mockUserRepo{
		getUserByEmail: func(_ context.Context, _ string) (*entity.User, error) {
			return &entity.User{ID: userID, Email: "john@example.com", Username: "johndoe"}, nil
		},
	}, &mockMailer{})

	err := svc.SendPasswordReset(context.Background(), "john@example.com")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "INTERNAL_ERROR")
}

func TestSendPasswordReset_MailerFailure(t *testing.T) {
	userID := uuid.New()
	svc := newEmailServiceForTest(t, &mockEmailTokenRepo{}, &mockUserRepo{
		getUserByEmail: func(_ context.Context, _ string) (*entity.User, error) {
			return &entity.User{ID: userID, Email: "john@example.com", Username: "johndoe"}, nil
		},
	}, &mockMailer{
		send: func(_ context.Context, _ *mailer.Message) error {
			return stderrors.New("smtp failed")
		},
	})

	err := svc.SendPasswordReset(context.Background(), "john@example.com")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "INTERNAL_ERROR")
}

func TestSendPasswordReset_Success(t *testing.T) {
	userID := uuid.New()
	deleteCalled := false
	createCalled := false
	sendCalled := false
	svc := newEmailServiceForTest(t, &mockEmailTokenRepo{
		deleteByUserAndType: func(_ context.Context, gotUserID uuid.UUID, tokenType entity.EmailTokenType) error {
			deleteCalled = true
			if gotUserID != userID {
				t.Fatalf("expected user id %v, got %v", userID, gotUserID)
			}
			if tokenType != entity.EmailTokenResetPassword {
				t.Fatalf("expected reset token type, got %q", tokenType)
			}
			return nil
		},
		create: func(_ context.Context, gotUserID uuid.UUID, token string, tokenType entity.EmailTokenType, expiresAt time.Time) (*entity.EmailToken, error) {
			createCalled = true
			if gotUserID != userID {
				t.Fatalf("expected user id %v, got %v", userID, gotUserID)
			}
			if token == "" {
				t.Fatal("expected generated token")
			}
			if tokenType != entity.EmailTokenResetPassword {
				t.Fatalf("expected reset token type, got %q", tokenType)
			}
			if time.Until(expiresAt) <= 0 {
				t.Fatal("expected future expiry")
			}
			return &entity.EmailToken{ID: uuid.New(), UserID: gotUserID, Token: token, Type: tokenType, ExpiresAt: expiresAt}, nil
		},
	}, &mockUserRepo{
		getUserByEmail: func(_ context.Context, _ string) (*entity.User, error) {
			return &entity.User{ID: userID, Email: "john@example.com", Username: "johndoe"}, nil
		},
	}, &mockMailer{
		send: func(_ context.Context, msg *mailer.Message) error {
			sendCalled = true
			if len(msg.To) != 1 || msg.To[0] != "john@example.com" {
				t.Fatalf("unexpected recipients: %+v", msg.To)
			}
			if msg.Subject == "" || msg.HTML == "" || msg.Text == "" {
				t.Fatal("expected complete email message")
			}
			return nil
		},
	})

	if err := svc.SendPasswordReset(context.Background(), "john@example.com"); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !deleteCalled || !createCalled || !sendCalled {
		t.Fatalf("expected delete/create/send to be called, got delete=%v create=%v send=%v", deleteCalled, createCalled, sendCalled)
	}
}

func TestResetPassword_TokenRequired(t *testing.T) {
	svc := newEmailServiceForTest(t, &mockEmailTokenRepo{}, &mockUserRepo{}, &mockMailer{})

	err := svc.ResetPassword(context.Background(), "", "NewPass123")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "BAD_REQUEST")
}

func TestResetPassword_NewPasswordRequired(t *testing.T) {
	svc := newEmailServiceForTest(t, &mockEmailTokenRepo{}, &mockUserRepo{}, &mockMailer{})

	err := svc.ResetPassword(context.Background(), "reset-token", "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "BAD_REQUEST")
}

func TestResetPassword_InvalidToken(t *testing.T) {
	svc := newEmailServiceForTest(t, &mockEmailTokenRepo{
		getByToken: func(_ context.Context, _ string) (*entity.EmailToken, error) {
			return nil, apperrors.ErrNotFound
		},
	}, &mockUserRepo{}, &mockMailer{})

	err := svc.ResetPassword(context.Background(), "missing", "NewPass123")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "BAD_REQUEST")
}

func TestResetPassword_InvalidTokenType(t *testing.T) {
	userID := uuid.New()
	svc := newEmailServiceForTest(t, &mockEmailTokenRepo{
		getByToken: func(_ context.Context, _ string) (*entity.EmailToken, error) {
			return validVerifyToken(userID), nil
		},
	}, &mockUserRepo{}, &mockMailer{})

	err := svc.ResetPassword(context.Background(), "verify-token", "NewPass123")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "BAD_REQUEST")
}

func TestResetPassword_ExpiredToken(t *testing.T) {
	userID := uuid.New()
	svc := newEmailServiceForTest(t, &mockEmailTokenRepo{
		getByToken: func(_ context.Context, _ string) (*entity.EmailToken, error) {
			token := validResetToken(userID)
			token.ExpiresAt = time.Now().Add(-time.Minute)
			return token, nil
		},
	}, &mockUserRepo{}, &mockMailer{})

	err := svc.ResetPassword(context.Background(), "expired", "NewPass123")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "BAD_REQUEST")
}

func TestResetPassword_UsedToken(t *testing.T) {
	userID := uuid.New()
	usedAt := time.Now()
	svc := newEmailServiceForTest(t, &mockEmailTokenRepo{
		getByToken: func(_ context.Context, _ string) (*entity.EmailToken, error) {
			token := validResetToken(userID)
			token.UsedAt = &usedAt
			return token, nil
		},
	}, &mockUserRepo{}, &mockMailer{})

	err := svc.ResetPassword(context.Background(), "used", "NewPass123")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "BAD_REQUEST")
}

func TestResetPassword_UpdatePasswordFailure(t *testing.T) {
	userID := uuid.New()
	svc := newEmailServiceForTest(t, &mockEmailTokenRepo{
		getByToken: func(_ context.Context, _ string) (*entity.EmailToken, error) {
			return validResetToken(userID), nil
		},
	}, &mockUserRepo{
		updateUserPassword: func(_ context.Context, _ uuid.UUID, _ string, _ *uuid.UUID) error {
			return stderrors.New("update failed")
		},
	}, &mockMailer{})

	err := svc.ResetPassword(context.Background(), "reset-token", "NewPass123")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "INTERNAL_ERROR")
}

func TestResetPassword_MarkUsedFailureDoesNotFail(t *testing.T) {
	userID := uuid.New()
	updateCalled := false
	svc := newEmailServiceForTest(t, &mockEmailTokenRepo{
		getByToken: func(_ context.Context, _ string) (*entity.EmailToken, error) {
			return validResetToken(userID), nil
		},
		markUsed: func(_ context.Context, _ uuid.UUID) error {
			return stderrors.New("mark failed")
		},
	}, &mockUserRepo{
		updateUserPassword: func(_ context.Context, _ uuid.UUID, passwordHash string, _ *uuid.UUID) error {
			updateCalled = true
			if passwordHash == "" {
				t.Fatal("expected hashed password")
			}
			return nil
		},
	}, &mockMailer{})

	if err := svc.ResetPassword(context.Background(), "reset-token", "NewPass123"); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !updateCalled {
		t.Fatal("expected UpdateUserPassword to be called")
	}
}

func TestResetPassword_Success(t *testing.T) {
	userID := uuid.New()
	updateCalled := false
	markUsedCalled := false
	svc := newEmailServiceForTest(t, &mockEmailTokenRepo{
		getByToken: func(_ context.Context, _ string) (*entity.EmailToken, error) {
			return validResetToken(userID), nil
		},
		markUsed: func(_ context.Context, _ uuid.UUID) error {
			markUsedCalled = true
			return nil
		},
	}, &mockUserRepo{
		updateUserPassword: func(_ context.Context, _ uuid.UUID, passwordHash string, _ *uuid.UUID) error {
			updateCalled = true
			if passwordHash == "" {
				t.Fatal("expected hashed password")
			}
			return nil
		},
	}, &mockMailer{})

	if err := svc.ResetPassword(context.Background(), "reset-token", "NewPass123"); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !updateCalled || !markUsedCalled {
		t.Fatalf("expected update and mark used, got update=%v markUsed=%v", updateCalled, markUsedCalled)
	}
}
