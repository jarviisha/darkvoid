package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jarviisha/darkvoid/internal/feature/user/dto"
	httputil "github.com/jarviisha/darkvoid/internal/http"
	"github.com/jarviisha/darkvoid/pkg/errors"
)

// --------------------------------------------------------------------------
// Mock: emailService
// --------------------------------------------------------------------------

type mockEmailService struct {
	verifyEmail        func(ctx context.Context, token string) error
	resendVerification func(ctx context.Context, email string) error
	sendPasswordReset  func(ctx context.Context, email string) error
	resetPassword      func(ctx context.Context, token, newPassword string) error
}

func (m *mockEmailService) VerifyEmail(ctx context.Context, token string) error {
	if m.verifyEmail != nil {
		return m.verifyEmail(ctx, token)
	}
	return nil
}
func (m *mockEmailService) ResendVerification(ctx context.Context, email string) error {
	if m.resendVerification != nil {
		return m.resendVerification(ctx, email)
	}
	return nil
}
func (m *mockEmailService) SendPasswordReset(ctx context.Context, email string) error {
	if m.sendPasswordReset != nil {
		return m.sendPasswordReset(ctx, email)
	}
	return nil
}
func (m *mockEmailService) ResetPassword(ctx context.Context, token, newPassword string) error {
	if m.resetPassword != nil {
		return m.resetPassword(ctx, token, newPassword)
	}
	return nil
}

func newEmailHandler(svc emailService) *EmailHandler {
	return &EmailHandler{emailService: svc}
}

// --------------------------------------------------------------------------
// VerifyEmail tests
// --------------------------------------------------------------------------

func TestVerifyEmail_Success(t *testing.T) {
	svc := &mockEmailService{
		verifyEmail: func(_ context.Context, token string) error {
			if token != "valid-token" {
				t.Errorf("expected token valid-token, got %q", token)
			}
			return nil
		},
	}
	h := newEmailHandler(svc)

	b, _ := json.Marshal(dto.VerifyEmailRequest{Token: "valid-token"})
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "/auth/verify-email", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.VerifyEmail(w, req)

	assertStatus(t, w, http.StatusOK)

	var resp httputil.MessageResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Message == "" {
		t.Error("expected non-empty success message")
	}
}

func TestVerifyEmail_InvalidJSON(t *testing.T) {
	h := newEmailHandler(&mockEmailService{})

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "/auth/verify-email", bytes.NewBufferString("bad json"))
	w := httptest.NewRecorder()
	h.VerifyEmail(w, req)

	assertStatus(t, w, http.StatusBadRequest)
}

func TestVerifyEmail_ServiceError(t *testing.T) {
	svc := &mockEmailService{
		verifyEmail: func(_ context.Context, _ string) error {
			return errors.NewBadRequestError("invalid or expired token")
		},
	}
	h := newEmailHandler(svc)

	b, _ := json.Marshal(dto.VerifyEmailRequest{Token: "expired-token"})
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "/auth/verify-email", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.VerifyEmail(w, req)

	assertStatus(t, w, http.StatusBadRequest)
	assertErrorCode(t, w, "BAD_REQUEST")
}

// --------------------------------------------------------------------------
// ResendVerification tests
// --------------------------------------------------------------------------

func TestResendVerification_Success(t *testing.T) {
	called := false
	svc := &mockEmailService{
		resendVerification: func(_ context.Context, _ string) error {
			called = true
			return nil
		},
	}
	h := newEmailHandler(svc)

	b, _ := json.Marshal(dto.ResendVerificationRequest{Email: "user@example.com"})
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "/auth/resend-verification", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ResendVerification(w, req)

	assertStatus(t, w, http.StatusOK)
	if !called {
		t.Error("expected service to be called")
	}
	var resp httputil.MessageResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Message == "" {
		t.Error("expected non-empty message")
	}
}

func TestResendVerification_InvalidJSON(t *testing.T) {
	h := newEmailHandler(&mockEmailService{})

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "/auth/resend-verification", bytes.NewBufferString("bad json"))
	w := httptest.NewRecorder()
	h.ResendVerification(w, req)

	assertStatus(t, w, http.StatusBadRequest)
}

func TestResendVerification_ServiceError(t *testing.T) {
	svc := &mockEmailService{
		resendVerification: func(_ context.Context, _ string) error {
			return errors.ErrInternal
		},
	}
	h := newEmailHandler(svc)

	b, _ := json.Marshal(dto.ResendVerificationRequest{Email: "user@example.com"})
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "/auth/resend-verification", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ResendVerification(w, req)

	assertStatus(t, w, http.StatusInternalServerError)
}

// --------------------------------------------------------------------------
// ForgotPassword tests
// --------------------------------------------------------------------------

func TestForgotPassword_Success(t *testing.T) {
	called := false
	svc := &mockEmailService{
		sendPasswordReset: func(_ context.Context, _ string) error {
			called = true
			return nil
		},
	}
	h := newEmailHandler(svc)

	b, _ := json.Marshal(dto.ForgotPasswordRequest{Email: "user@example.com"})
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "/auth/forgot-password", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ForgotPassword(w, req)

	assertStatus(t, w, http.StatusOK)
	if !called {
		t.Error("expected service to be called")
	}
	var resp httputil.MessageResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Message == "" {
		t.Error("expected non-empty obscurity message")
	}
}

func TestForgotPassword_InvalidJSON(t *testing.T) {
	h := newEmailHandler(&mockEmailService{})

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "/auth/forgot-password", bytes.NewBufferString("bad json"))
	w := httptest.NewRecorder()
	h.ForgotPassword(w, req)

	assertStatus(t, w, http.StatusBadRequest)
}

func TestForgotPassword_ServiceError(t *testing.T) {
	svc := &mockEmailService{
		sendPasswordReset: func(_ context.Context, _ string) error {
			return errors.ErrInternal
		},
	}
	h := newEmailHandler(svc)

	b, _ := json.Marshal(dto.ForgotPasswordRequest{Email: "user@example.com"})
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "/auth/forgot-password", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ForgotPassword(w, req)

	assertStatus(t, w, http.StatusInternalServerError)
}

// --------------------------------------------------------------------------
// ResetPassword tests
// --------------------------------------------------------------------------

func TestResetPassword_Success(t *testing.T) {
	svc := &mockEmailService{
		resetPassword: func(_ context.Context, token, newPassword string) error {
			if token != "reset-token" {
				t.Errorf("expected token reset-token, got %q", token)
			}
			if newPassword != "NewSecurePass123" {
				t.Errorf("expected new password NewSecurePass123, got %q", newPassword)
			}
			return nil
		},
	}
	h := newEmailHandler(svc)

	b, _ := json.Marshal(dto.ResetPasswordRequest{Token: "reset-token", NewPassword: "NewSecurePass123"})
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "/auth/reset-password", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ResetPassword(w, req)

	assertStatus(t, w, http.StatusOK)

	var resp httputil.MessageResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Message == "" {
		t.Error("expected non-empty success message")
	}
}

func TestResetPassword_InvalidJSON(t *testing.T) {
	h := newEmailHandler(&mockEmailService{})

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "/auth/reset-password", bytes.NewBufferString("bad json"))
	w := httptest.NewRecorder()
	h.ResetPassword(w, req)

	assertStatus(t, w, http.StatusBadRequest)
}

func TestResetPassword_ServiceError(t *testing.T) {
	svc := &mockEmailService{
		resetPassword: func(_ context.Context, _, _ string) error {
			return errors.NewBadRequestError("invalid or expired token")
		},
	}
	h := newEmailHandler(svc)

	b, _ := json.Marshal(dto.ResetPasswordRequest{Token: "bad-token", NewPassword: "pass"})
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "/auth/reset-password", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ResetPassword(w, req)

	assertStatus(t, w, http.StatusBadRequest)
	assertErrorCode(t, w, "BAD_REQUEST")
}
