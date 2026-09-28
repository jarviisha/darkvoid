package service

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/feature/user/dto"
	"github.com/jarviisha/darkvoid/internal/feature/user/entity"
	"github.com/jarviisha/darkvoid/pkg/errors"
)

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
	verify := mailRedeeming(entity.EmailTokenVerify)
	reset := mailRedeeming(entity.EmailTokenResetPassword)
	mail := newAccountMailServiceForTest(t, &mockEmailTokenRepo{}, &mockUserRepo{}, &mockMailer{})

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
		func(r dto.VerifyEmailRequest) error { return verify.VerifyEmail(ctx, r.Token) })
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
			tagged := schemaRequiresNonEmpty(field.Tag.Get("binding"))
			switch {
			case rejected && !tagged:
				t.Errorf(`%s: service rejects it empty, schema lacks binding:"required,min=1"`, name)
			case tagged && !rejected:
				t.Errorf("%s: schema marks it required, service accepts it empty", name)
			}
		}
	})
}

// Fields that set a password publish the rule's bounds, so a generated client
// refuses "abc" before sending it; the schema counts characters, as the
// minimum does. LoginRequest.password is left at min=1: an account whose
// password predates the rules must still be able to sign in.
func TestRequestSchema_PasswordBoundsMatchRules(t *testing.T) {
	want := fmt.Sprintf("required,min=%d,max=%d", minPasswordLength, maxPasswordLength)
	for _, f := range []struct {
		typ   reflect.Type
		field string
	}{
		{reflect.TypeFor[dto.RegisterRequest](), "Password"},
		{reflect.TypeFor[dto.ChangePasswordRequest](), "NewPassword"},
		{reflect.TypeFor[dto.ResetPasswordRequest](), "NewPassword"},
	} {
		sf, _ := f.typ.FieldByName(f.field)
		if got := sf.Tag.Get("binding"); got != want {
			t.Errorf("%s.%s: binding:%q, want %q", f.typ.Name(), f.field, got, want)
		}
	}
}

// schemaRequiresNonEmpty reports whether a binding tag makes swag mark the
// field required with a positive minLength. Nothing reads the tag at runtime.
func schemaRequiresNonEmpty(binding string) bool {
	opts := strings.Split(binding, ",")
	return slices.Contains(opts, "required") && slices.ContainsFunc(opts, func(opt string) bool {
		n, ok := strings.CutPrefix(opt, "min=")
		if !ok {
			return false
		}
		minLength, err := strconv.Atoi(n)
		return err == nil && minLength > 0
	})
}

func isBadRequest(err error) bool {
	appErr := errors.GetAppError(err)
	return appErr != nil && appErr.HTTPStatus == http.StatusBadRequest
}
