package service

import (
	"context"
	stderrors "errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/feature/user/entity"
	"github.com/jarviisha/darkvoid/internal/infrastructure/mailer"
	"github.com/jarviisha/darkvoid/pkg/errors"
	"github.com/jarviisha/darkvoid/pkg/logger"
)

const (
	verifyTokenExpiry = 24 * time.Hour
	resetTokenExpiry  = 1 * time.Hour
)

// emailTokenRepo defines the repository operations needed by AccountMailService.
type emailTokenRepo interface {
	Create(ctx context.Context, userID uuid.UUID, token string, tokenType entity.EmailTokenType, expiresAt time.Time) (*entity.EmailToken, error)
	GetByToken(ctx context.Context, token string) (*entity.EmailToken, error)
	MarkUsed(ctx context.Context, id uuid.UUID) error
	DeleteByUserAndType(ctx context.Context, userID uuid.UUID, tokenType entity.EmailTokenType) error
}

// deliveryRecorder records an accepted send so a later provider delivery report
// can be attributed back to a user.
type deliveryRecorder interface {
	RecordSend(ctx context.Context, userID uuid.UUID, providerMessageID, recipient string, kind entity.EmailKind) error
}

// AccountMailService orchestrates user-account email flows (welcome, verification,
// password reset) and the associated one-shot email tokens. It is scoped to the
// user bounded context — not a general-purpose mailer for other features.
type AccountMailService struct {
	mailer    mailer.Mailer
	templates *mailer.Templates
	tokenRepo emailTokenRepo
	userRepo  userRepo
	recorder  deliveryRecorder // may be nil in tests
	baseURL   string
}

// NewAccountMailService creates a new AccountMailService.
func NewAccountMailService(
	m mailer.Mailer,
	templates *mailer.Templates,
	tokenRepo emailTokenRepo,
	userRepo userRepo,
	recorder deliveryRecorder,
	baseURL string,
) *AccountMailService {
	return &AccountMailService{
		mailer:    m,
		templates: templates,
		tokenRepo: tokenRepo,
		userRepo:  userRepo,
		recorder:  recorder,
		baseURL:   baseURL,
	}
}

// deliver sends one message and records it against the user.
//
// Every flow goes through here so that recording cannot be forgotten by the next
// one added. A suppressed recipient comes back as mailer.ErrSuppressed — that is
// the suppression list working, not a delivery fault, and each caller decides
// what it means for its own response.
func (s *AccountMailService) deliver(
	ctx context.Context,
	userID uuid.UUID,
	email, subject, html, text string,
	kind entity.EmailKind,
) error {
	messageID, err := s.mailer.Send(ctx, &mailer.Message{
		To:      []string{email},
		Subject: subject,
		HTML:    html,
		Text:    text,
	})
	if err != nil {
		return err
	}

	if s.recorder != nil {
		if err := s.recorder.RecordSend(ctx, userID, messageID, email, kind); err != nil {
			// The mail has already left. Reporting a failure now would only make the
			// caller retry a send that worked.
			logger.LogError(ctx, err, "failed to record email delivery", "email", email, "kind", kind)
		}
	}

	logger.Info(ctx, "email sent", "email", email, "kind", kind, "provider_message_id", messageID)
	return nil
}

// logSendFailure keeps a suppressed recipient out of the error log. It is an
// expected outcome, and logging it at error level trains people to ignore the
// entries that are not.
func logSendFailure(ctx context.Context, err error, kind entity.EmailKind, email string) {
	if stderrors.Is(err, mailer.ErrSuppressed) {
		logger.Warn(ctx, "email skipped, recipient is suppressed", "email", email, "kind", kind)
		return
	}
	logger.LogError(ctx, err, "failed to send email", "email", email, "kind", kind)
}

// SendWelcome sends a welcome email to the user. Errors are logged, not propagated.
func (s *AccountMailService) SendWelcome(ctx context.Context, userID uuid.UUID, email, username string) {
	html, err := s.templates.RenderWelcome(mailer.WelcomeData{
		Username: username,
	})
	if err != nil {
		logger.LogError(ctx, err, "failed to render welcome email template")
		return
	}

	text := fmt.Sprintf("Hi %s, welcome to DarkVoid! Your account has been created successfully.", username)
	if err := s.deliver(ctx, userID, email, "Welcome to DarkVoid", html, text, entity.EmailKindWelcome); err != nil {
		logSendFailure(ctx, err, entity.EmailKindWelcome, email)
	}
}

// SendVerification creates a verification token and sends a verification email.
// Errors are logged, not propagated (fire-and-forget side effect).
func (s *AccountMailService) SendVerification(ctx context.Context, userID uuid.UUID, email, username string) {
	// Clean up any existing verification tokens for this user
	if err := s.tokenRepo.DeleteByUserAndType(ctx, userID, entity.EmailTokenVerify); err != nil {
		logger.LogError(ctx, err, "failed to delete old verification tokens", "user_id", userID)
	}

	token, err := generateSecureToken()
	if err != nil {
		logger.LogError(ctx, err, "failed to generate verification token")
		return
	}

	_, err = s.tokenRepo.Create(ctx, userID, token, entity.EmailTokenVerify, time.Now().Add(verifyTokenExpiry))
	if err != nil {
		logger.LogError(ctx, err, "failed to create verification token", "user_id", userID)
		return
	}

	verifyURL := fmt.Sprintf("%s/verify-email?token=%s", s.baseURL, token)
	html, err := s.templates.RenderVerifyEmail(mailer.VerifyEmailData{
		Username:  username,
		VerifyURL: verifyURL,
		ExpiresIn: "24 hours",
	})
	if err != nil {
		logger.LogError(ctx, err, "failed to render verification email template")
		return
	}

	text := fmt.Sprintf("Hi %s, please verify your email by visiting: %s", username, verifyURL)
	if err := s.deliver(ctx, userID, email, "Verify your email - DarkVoid", html, text, entity.EmailKindVerifyEmail); err != nil {
		logSendFailure(ctx, err, entity.EmailKindVerifyEmail, email)
	}
}

// VerifyEmail validates a verification token and marks the associated user's email as verified.
func (s *AccountMailService) VerifyEmail(ctx context.Context, tokenStr string) error {
	if tokenStr == "" {
		return errors.NewBadRequestError("token is required")
	}

	token, err := s.tokenRepo.GetByToken(ctx, tokenStr)
	if err != nil {
		return errors.NewBadRequestError("invalid or expired token")
	}

	if token.Type != entity.EmailTokenVerify {
		return errors.NewBadRequestError("invalid token type")
	}

	if token.IsUsed() {
		return errors.NewBadRequestError("token has already been used")
	}

	if token.IsExpired() {
		return errors.NewBadRequestError("token has expired")
	}

	if err := s.tokenRepo.MarkUsed(ctx, token.ID); err != nil {
		logger.LogError(ctx, err, "failed to mark verification token as used")
		return errors.NewInternalError(err)
	}

	logger.Info(ctx, "email verified successfully", "user_id", token.UserID)
	return nil
}

// ResendVerification re-sends a verification email for the given email address.
func (s *AccountMailService) ResendVerification(ctx context.Context, email string) error {
	if email == "" {
		return errors.NewBadRequestError("email is required")
	}

	user, err := s.userRepo.GetUserByEmail(ctx, email)
	if err != nil {
		// Do not leak whether the email exists
		logger.Warn(ctx, "resend verification for unknown email", "email", email)
		return nil
	}

	s.SendVerification(ctx, user.ID, user.Email, user.Username)
	return nil
}

// SendPasswordReset creates a reset token and sends a password reset email.
func (s *AccountMailService) SendPasswordReset(ctx context.Context, email string) error {
	if email == "" {
		return errors.NewBadRequestError("email is required")
	}

	user, err := s.userRepo.GetUserByEmail(ctx, email)
	if err != nil {
		// Do not leak whether the email exists
		logger.Warn(ctx, "password reset for unknown email", "email", email)
		return nil
	}

	// Clean up any existing reset tokens for this user
	if err = s.tokenRepo.DeleteByUserAndType(ctx, user.ID, entity.EmailTokenResetPassword); err != nil {
		logger.LogError(ctx, err, "failed to delete old reset tokens", "user_id", user.ID)
	}

	token, err := generateSecureToken()
	if err != nil {
		logger.LogError(ctx, err, "failed to generate reset token")
		return errors.NewInternalError(err)
	}

	_, err = s.tokenRepo.Create(ctx, user.ID, token, entity.EmailTokenResetPassword, time.Now().Add(resetTokenExpiry))
	if err != nil {
		logger.LogError(ctx, err, "failed to create reset token", "user_id", user.ID)
		return errors.NewInternalError(err)
	}

	resetURL := fmt.Sprintf("%s/reset-password?token=%s", s.baseURL, token)
	html, err := s.templates.RenderResetPassword(mailer.ResetPasswordData{
		Username:  user.Username,
		ResetURL:  resetURL,
		ExpiresIn: "1 hour",
	})
	if err != nil {
		logger.LogError(ctx, err, "failed to render reset password email template")
		return errors.NewInternalError(err)
	}

	text := fmt.Sprintf("Hi %s, reset your password by visiting: %s", user.Username, resetURL)
	if err := s.deliver(ctx, user.ID, user.Email, "Reset your password - DarkVoid", html, text, entity.EmailKindResetPassword); err != nil {
		logSendFailure(ctx, err, entity.EmailKindResetPassword, user.Email)
		if stderrors.Is(err, mailer.ErrSuppressed) {
			// Answer exactly as for a successful send. This endpoint already refuses
			// to reveal whether an address exists, and a 500 for suppressed
			// addresses only would leak that distinction.
			return nil
		}
		return errors.NewInternalError(err)
	}

	return nil
}

// ResetPassword validates a reset token and sets the new password.
func (s *AccountMailService) ResetPassword(ctx context.Context, tokenStr, newPassword string) error {
	if tokenStr == "" {
		return errors.NewBadRequestError("token is required")
	}
	if newPassword == "" {
		return errors.NewBadRequestError("new password is required")
	}

	token, err := s.tokenRepo.GetByToken(ctx, tokenStr)
	if err != nil {
		return errors.NewBadRequestError("invalid or expired token")
	}

	if token.Type != entity.EmailTokenResetPassword {
		return errors.NewBadRequestError("invalid token type")
	}

	if token.IsUsed() {
		return errors.NewBadRequestError("token has already been used")
	}

	if token.IsExpired() {
		return errors.NewBadRequestError("token has expired")
	}

	hashedPassword, err := hashPassword(newPassword)
	if err != nil {
		return errors.NewInternalError(err)
	}

	if err := s.userRepo.UpdateUserPassword(ctx, token.UserID, hashedPassword, nil); err != nil {
		return errors.NewInternalError(err)
	}

	if err := s.tokenRepo.MarkUsed(ctx, token.ID); err != nil {
		logger.LogError(ctx, err, "failed to mark reset token as used")
	}

	logger.Info(ctx, "password reset successfully", "user_id", token.UserID)
	return nil
}
