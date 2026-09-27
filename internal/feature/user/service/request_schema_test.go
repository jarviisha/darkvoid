package service

import (
	"context"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/feature/user/dto"
	"github.com/jarviisha/darkvoid/internal/feature/user/entity"
	"github.com/jarviisha/darkvoid/pkg/errors"
)

// schemaRequiredTag marks a field required in the OpenAPI schema: swag reads
// "required" into the schema's required list and "min=1" into minLength.
// Nothing reads it at runtime.
const schemaRequiredTag = "required,min=1"

// The schema's required fields and the services' empty-field checks live in
// different places. When they drifted, generated clients typed display_name
// optional and signup broke on every request. Each request below is one that
// passes validation; blanking a field must draw a 400 exactly where the tag is.
// A request type missing from this list is not checked.
func TestRequestSchema_RequiredMatchesServiceValidation(t *testing.T) {
	ctx := context.Background()
	auth := newAuthService(&mockUserRepo{}, &mockRefreshTokenRepo{}, newTestJWT(t))
	// An unknown token is itself a 400, so each token flow gets a repo that
	// redeems any lookup as a token of that flow's type. A blanked token then
	// draws a 400 only from the service's own empty check.
	mailRedeeming := func(typ entity.EmailTokenType) *AccountMailService {
		tokens := &mockEmailTokenRepo{
			getByToken: func(context.Context, string) (*entity.EmailToken, error) {
				return &entity.EmailToken{ID: uuid.New(), Type: typ, ExpiresAt: time.Now().Add(time.Hour)}, nil
			},
		}
		return newAccountMailServiceForTest(t, tokens, &mockUserRepo{}, &mockMailer{})
	}
	mail := mailRedeeming(entity.EmailTokenVerify)
	reset := mailRedeeming(entity.EmailTokenResetPassword)

	checkRequiredFields(t,
		dto.RegisterRequest{Username: "johndoe", Email: "john@example.com", DisplayName: "John Doe", Password: "SecurePass123"},
		func(r dto.RegisterRequest) error { _, err := auth.Register(ctx, &r); return err })
	checkRequiredFields(t,
		dto.LoginRequest{Username: "johndoe", Password: "SecurePass123"},
		func(r dto.LoginRequest) error { _, err := auth.Login(ctx, &r); return err })
	checkRequiredFields(t,
		dto.RefreshTokenRequest{RefreshToken: "token"},
		func(r dto.RefreshTokenRequest) error { _, err := auth.RefreshAccessToken(ctx, &r); return err })
	checkRequiredFields(t,
		dto.LogoutRequest{RefreshToken: "token"},
		func(r dto.LogoutRequest) error { return auth.Logout(ctx, &r) })
	checkRequiredFields(t,
		dto.ChangePasswordRequest{OldPassword: "OldPass123", NewPassword: "NewPass123"},
		func(r dto.ChangePasswordRequest) error {
			return auth.ChangePassword(ctx, uuid.New(), r.OldPassword, r.NewPassword)
		})
	checkRequiredFields(t,
		dto.VerifyEmailRequest{Token: "token"},
		func(r dto.VerifyEmailRequest) error { return mail.VerifyEmail(ctx, r.Token) })
	checkRequiredFields(t,
		dto.ResendVerificationRequest{Email: "john@example.com"},
		func(r dto.ResendVerificationRequest) error { return mail.ResendVerification(ctx, r.Email) })
	checkRequiredFields(t,
		dto.ForgotPasswordRequest{Email: "john@example.com"},
		func(r dto.ForgotPasswordRequest) error { return mail.SendPasswordReset(ctx, r.Email) })
	checkRequiredFields(t,
		dto.ResetPasswordRequest{Token: "token", NewPassword: "NewPass123"},
		func(r dto.ResetPasswordRequest) error { return reset.ResetPassword(ctx, r.Token, r.NewPassword) })
}

func checkRequiredFields[T any](t *testing.T, valid T, call func(T) error) {
	t.Helper()
	typ := reflect.TypeFor[T]()
	t.Run(typ.Name(), func(t *testing.T) {
		if err := call(valid); isBadRequest(err) {
			t.Fatalf("valid request fails validation, so no field can be judged: %v", err)
		}
		for i := range typ.NumField() {
			field := typ.Field(i)
			name, _, _ := strings.Cut(field.Tag.Get("json"), ",")

			blanked := valid
			reflect.ValueOf(&blanked).Elem().Field(i).SetZero()

			rejected := isBadRequest(call(blanked))
			tagged := field.Tag.Get("binding") == schemaRequiredTag
			switch {
			case rejected && !tagged:
				t.Errorf("%s: service rejects it empty, schema lacks binding:%q", name, schemaRequiredTag)
			case tagged && !rejected:
				t.Errorf("%s: schema marks it required, service accepts it empty", name)
			}
		}
	})
}

func isBadRequest(err error) bool {
	appErr := errors.GetAppError(err)
	return appErr != nil && appErr.HTTPStatus == http.StatusBadRequest
}
