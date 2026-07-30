package service

import (
	"context"
	stderrors "errors"
	"sync"
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

func (m *mockMailer) Send(ctx context.Context, msg *mailer.Message) (string, error) {
	if m.send != nil {
		return "mock-message-id", m.send(ctx, msg)
	}
	return "mock-message-id", nil
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

// recordedSend is one call the service made to the delivery recorder.
type recordedSend struct {
	userID    uuid.UUID
	messageID string
	recipient string
	kind      entity.EmailKind
}

type mockDeliveryRecorder struct {
	mu      sync.Mutex
	calls   []recordedSend
	failure error
}

func (m *mockDeliveryRecorder) RecordSend(_ context.Context, userID uuid.UUID, providerMessageID, recipient string, kind entity.EmailKind) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, recordedSend{userID: userID, messageID: providerMessageID, recipient: recipient, kind: kind})
	return m.failure
}

func (m *mockDeliveryRecorder) recorded() []recordedSend {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]recordedSend(nil), m.calls...)
}

func newAccountMailServiceForTest(t *testing.T, tokenRepo emailTokenRepo, userRepo userRepo, m mailer.Mailer) *AccountMailService {
	t.Helper()
	return newAccountMailServiceWithRecorder(t, tokenRepo, userRepo, m, &mockDeliveryRecorder{})
}

func newAccountMailServiceWithRecorder(
	t *testing.T,
	tokenRepo emailTokenRepo,
	userRepo userRepo,
	m mailer.Mailer,
	recorder deliveryRecorder,
) *AccountMailService {
	t.Helper()
	templates, err := mailer.LoadTemplates()
	if err != nil {
		t.Fatalf("failed to load templates: %v", err)
	}
	return NewAccountMailService(m, templates, tokenRepo, userRepo, recorder, "https://darkvoid.test")
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
	svc := newAccountMailServiceForTest(t, &mockEmailTokenRepo{}, &mockUserRepo{}, &mockMailer{})

	err := svc.VerifyEmail(context.Background(), "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "BAD_REQUEST")
}

func TestVerifyEmail_InvalidOrExpiredToken(t *testing.T) {
	svc := newAccountMailServiceForTest(t, &mockEmailTokenRepo{
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
	svc := newAccountMailServiceForTest(t, &mockEmailTokenRepo{
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
	svc := newAccountMailServiceForTest(t, &mockEmailTokenRepo{
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
	svc := newAccountMailServiceForTest(t, &mockEmailTokenRepo{
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
	svc := newAccountMailServiceForTest(t, &mockEmailTokenRepo{
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
	svc := newAccountMailServiceForTest(t, &mockEmailTokenRepo{
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
	svc := newAccountMailServiceForTest(t, &mockEmailTokenRepo{}, &mockUserRepo{}, &mockMailer{})

	err := svc.ResendVerification(context.Background(), "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "BAD_REQUEST")
}

func TestResendVerification_UnknownEmailReturnsNil(t *testing.T) {
	svc := newAccountMailServiceForTest(t, &mockEmailTokenRepo{}, &mockUserRepo{
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
	svc := newAccountMailServiceForTest(t, &mockEmailTokenRepo{
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
	svc := newAccountMailServiceForTest(t, &mockEmailTokenRepo{}, &mockUserRepo{}, &mockMailer{})

	err := svc.SendPasswordReset(context.Background(), "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "BAD_REQUEST")
}

func TestSendPasswordReset_UnknownEmailReturnsNil(t *testing.T) {
	svc := newAccountMailServiceForTest(t, &mockEmailTokenRepo{}, &mockUserRepo{
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
	svc := newAccountMailServiceForTest(t, &mockEmailTokenRepo{
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
	svc := newAccountMailServiceForTest(t, &mockEmailTokenRepo{}, &mockUserRepo{
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
	svc := newAccountMailServiceForTest(t, &mockEmailTokenRepo{
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
	svc := newAccountMailServiceForTest(t, &mockEmailTokenRepo{}, &mockUserRepo{}, &mockMailer{})

	err := svc.ResetPassword(context.Background(), "", "NewPass123")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "BAD_REQUEST")
}

func TestResetPassword_NewPasswordRequired(t *testing.T) {
	svc := newAccountMailServiceForTest(t, &mockEmailTokenRepo{}, &mockUserRepo{}, &mockMailer{})

	err := svc.ResetPassword(context.Background(), "reset-token", "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "BAD_REQUEST")
}

func TestResetPassword_InvalidToken(t *testing.T) {
	svc := newAccountMailServiceForTest(t, &mockEmailTokenRepo{
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
	svc := newAccountMailServiceForTest(t, &mockEmailTokenRepo{
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
	svc := newAccountMailServiceForTest(t, &mockEmailTokenRepo{
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
	svc := newAccountMailServiceForTest(t, &mockEmailTokenRepo{
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
	svc := newAccountMailServiceForTest(t, &mockEmailTokenRepo{
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
	svc := newAccountMailServiceForTest(t, &mockEmailTokenRepo{
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
	svc := newAccountMailServiceForTest(t, &mockEmailTokenRepo{
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

func TestSendPasswordReset_SuppressedRecipientAnswersLikeASuccess(t *testing.T) {
	userID := uuid.New()
	svc := newAccountMailServiceForTest(t, &mockEmailTokenRepo{}, &mockUserRepo{
		getUserByEmail: func(_ context.Context, _ string) (*entity.User, error) {
			return &entity.User{ID: userID, Email: "gone@example.com", Username: "johndoe"}, nil
		},
	}, &mockMailer{
		send: func(_ context.Context, _ *mailer.Message) error {
			return mailer.ErrSuppressed
		},
	})

	// A 500 for suppressed addresses only would reveal which addresses exist and
	// have bounced — the endpoint is deliberately uniform.
	if err := svc.SendPasswordReset(context.Background(), "gone@example.com"); err != nil {
		t.Fatalf("expected nil error for a suppressed recipient, got %v", err)
	}
}

func TestSendPasswordReset_RecordsTheDelivery(t *testing.T) {
	userID := uuid.New()
	recorder := &mockDeliveryRecorder{}
	svc := newAccountMailServiceWithRecorder(t, &mockEmailTokenRepo{}, &mockUserRepo{
		getUserByEmail: func(_ context.Context, _ string) (*entity.User, error) {
			return &entity.User{ID: userID, Email: "john@example.com", Username: "johndoe"}, nil
		},
	}, &mockMailer{}, recorder)

	if err := svc.SendPasswordReset(context.Background(), "john@example.com"); err != nil {
		t.Fatalf("SendPasswordReset: unexpected error: %v", err)
	}

	recorded := recorder.recorded()
	if len(recorded) != 1 {
		t.Fatalf("recorded %d sends, want 1", len(recorded))
	}
	got := recorded[0]
	if got.userID != userID || got.messageID != "mock-message-id" || got.recipient != "john@example.com" {
		t.Errorf("recorded %+v, want the reset send keyed by its provider message id", got)
	}
	if got.kind != entity.EmailKindResetPassword {
		t.Errorf("kind = %q, want %q", got.kind, entity.EmailKindResetPassword)
	}
}

func TestSendPasswordReset_RecordingFailureDoesNotFailTheSend(t *testing.T) {
	userID := uuid.New()
	recorder := &mockDeliveryRecorder{failure: stderrors.New("insert failed")}
	svc := newAccountMailServiceWithRecorder(t, &mockEmailTokenRepo{}, &mockUserRepo{
		getUserByEmail: func(_ context.Context, _ string) (*entity.User, error) {
			return &entity.User{ID: userID, Email: "john@example.com", Username: "johndoe"}, nil
		},
	}, &mockMailer{}, recorder)

	// The mail has already left; reporting a failure would make the caller retry a
	// send that worked.
	if err := svc.SendPasswordReset(context.Background(), "john@example.com"); err != nil {
		t.Fatalf("expected nil error when only bookkeeping failed, got %v", err)
	}
}

func TestSendVerification_RecordsTheDelivery(t *testing.T) {
	userID := uuid.New()
	recorder := &mockDeliveryRecorder{}
	svc := newAccountMailServiceWithRecorder(t, &mockEmailTokenRepo{}, &mockUserRepo{}, &mockMailer{}, recorder)

	svc.SendVerification(context.Background(), userID, "john@example.com", "johndoe")

	recorded := recorder.recorded()
	if len(recorded) != 1 || recorded[0].kind != entity.EmailKindVerifyEmail {
		t.Fatalf("recorded = %+v, want one verify_email send", recorded)
	}
	if recorded[0].userID != userID {
		t.Errorf("userID = %v, want %v", recorded[0].userID, userID)
	}
}

func TestSendWelcome_RecordsTheDeliveryAgainstTheUser(t *testing.T) {
	userID := uuid.New()
	recorder := &mockDeliveryRecorder{}
	svc := newAccountMailServiceWithRecorder(t, &mockEmailTokenRepo{}, &mockUserRepo{}, &mockMailer{}, recorder)

	svc.SendWelcome(context.Background(), userID, "john@example.com", "johndoe")

	recorded := recorder.recorded()
	if len(recorded) != 1 || recorded[0].kind != entity.EmailKindWelcome {
		t.Fatalf("recorded = %+v, want one welcome send", recorded)
	}
	if recorded[0].userID != userID {
		t.Errorf("userID = %v, want %v — the welcome flow now carries it so the log row can be attributed", recorded[0].userID, userID)
	}
}
