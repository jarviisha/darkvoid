package jwt

import (
	"errors"
	"testing"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"
)

const (
	testIssuer   = "darkvoid"
	testAudience = "darkvoid-api"
)

var testSecret = []byte("test-secret-key-32-bytes-minimum!!")

func TestValidateToken_EnforcesTrustBoundary(t *testing.T) {
	t.Parallel()

	service := newTestService(t)

	tests := []struct {
		name          string
		method        jwtlib.SigningMethod
		issuer        string
		audience      jwtlib.ClaimStrings
		expiresAt     *jwtlib.NumericDate
		signingSecret []byte
		wantError     error
	}{
		{
			name:          "valid HS256 token",
			method:        jwtlib.SigningMethodHS256,
			issuer:        testIssuer,
			audience:      jwtlib.ClaimStrings{testAudience},
			expiresAt:     jwtlib.NewNumericDate(time.Now().Add(time.Minute)),
			signingSecret: testSecret,
		},
		{
			name:          "HS384 algorithm",
			method:        jwtlib.SigningMethodHS384,
			issuer:        testIssuer,
			audience:      jwtlib.ClaimStrings{testAudience},
			expiresAt:     jwtlib.NewNumericDate(time.Now().Add(time.Minute)),
			signingSecret: testSecret,
			wantError:     ErrInvalidToken,
		},
		{
			name:          "HS512 algorithm",
			method:        jwtlib.SigningMethodHS512,
			issuer:        testIssuer,
			audience:      jwtlib.ClaimStrings{testAudience},
			expiresAt:     jwtlib.NewNumericDate(time.Now().Add(time.Minute)),
			signingSecret: testSecret,
			wantError:     ErrInvalidToken,
		},
		{
			name:          "wrong issuer",
			method:        jwtlib.SigningMethodHS256,
			issuer:        "other-issuer",
			audience:      jwtlib.ClaimStrings{testAudience},
			expiresAt:     jwtlib.NewNumericDate(time.Now().Add(time.Minute)),
			signingSecret: testSecret,
			wantError:     ErrInvalidToken,
		},
		{
			name:          "missing issuer",
			method:        jwtlib.SigningMethodHS256,
			audience:      jwtlib.ClaimStrings{testAudience},
			expiresAt:     jwtlib.NewNumericDate(time.Now().Add(time.Minute)),
			signingSecret: testSecret,
			wantError:     ErrInvalidToken,
		},
		{
			name:          "wrong audience",
			method:        jwtlib.SigningMethodHS256,
			issuer:        testIssuer,
			audience:      jwtlib.ClaimStrings{"other-api"},
			expiresAt:     jwtlib.NewNumericDate(time.Now().Add(time.Minute)),
			signingSecret: testSecret,
			wantError:     ErrInvalidToken,
		},
		{
			name:          "missing audience",
			method:        jwtlib.SigningMethodHS256,
			issuer:        testIssuer,
			expiresAt:     jwtlib.NewNumericDate(time.Now().Add(time.Minute)),
			signingSecret: testSecret,
			wantError:     ErrInvalidToken,
		},
		{
			name:          "missing expiration",
			method:        jwtlib.SigningMethodHS256,
			issuer:        testIssuer,
			audience:      jwtlib.ClaimStrings{testAudience},
			signingSecret: testSecret,
			wantError:     ErrInvalidToken,
		},
		{
			name:          "wrong secret",
			method:        jwtlib.SigningMethodHS256,
			issuer:        testIssuer,
			audience:      jwtlib.ClaimStrings{testAudience},
			expiresAt:     jwtlib.NewNumericDate(time.Now().Add(time.Minute)),
			signingSecret: []byte("different-test-secret-key-32-bytes!!"),
			wantError:     ErrInvalidToken,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			claims := jwtlib.RegisteredClaims{
				Issuer:    tt.issuer,
				Audience:  tt.audience,
				Subject:   "user-1",
				ExpiresAt: tt.expiresAt,
			}
			signed := signToken(t, tt.method, tt.signingSecret, claims)

			parsedClaims, err := service.ValidateToken(signed)
			assertValidationError(t, err, tt.wantError)
			if tt.wantError == nil && parsedClaims.Subject != claims.Subject {
				t.Errorf("ValidateToken() subject = %q, want %q", parsedClaims.Subject, claims.Subject)
			}

			var customClaims jwtlib.RegisteredClaims
			err = service.ValidateTokenWithClaims(signed, &customClaims)
			assertValidationError(t, err, tt.wantError)
		})
	}
}

func TestGenerateToken_EmitsRequiredClaims(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	signed, err := service.GenerateToken("user-1")
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}

	claims, err := service.ValidateToken(signed)
	if err != nil {
		t.Fatalf("ValidateToken() error = %v", err)
	}
	if claims.Issuer != testIssuer {
		t.Errorf("issuer = %q, want %q", claims.Issuer, testIssuer)
	}
	if len(claims.Audience) != 1 || claims.Audience[0] != testAudience {
		t.Errorf("audience = %v, want %q", claims.Audience, testAudience)
	}
	if claims.ExpiresAt == nil {
		t.Error("expiration is missing")
	}
}

func TestConfigValidate_RequiresTrustBoundary(t *testing.T) {
	t.Parallel()

	valid := Config{
		Secret:   testSecret,
		Issuer:   testIssuer,
		Audience: testAudience,
		Expiry:   time.Minute,
	}

	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{name: "missing secret", mutate: func(cfg *Config) { cfg.Secret = nil }},
		{name: "missing issuer", mutate: func(cfg *Config) { cfg.Issuer = "" }},
		{name: "missing audience", mutate: func(cfg *Config) { cfg.Audience = "" }},
		{name: "invalid expiry", mutate: func(cfg *Config) { cfg.Expiry = 0 }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := valid
			tt.mutate(&cfg)
			if err := cfg.Validate(); !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("Validate() error = %v, want ErrInvalidConfig", err)
			}
		})
	}
}

func newTestService(t *testing.T) *Service {
	t.Helper()

	service, err := NewService(Config{
		Secret:   testSecret,
		Issuer:   testIssuer,
		Audience: testAudience,
		Expiry:   15 * time.Minute,
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return service
}

func signToken(t *testing.T, method jwtlib.SigningMethod, secret []byte, claims jwtlib.Claims) string {
	t.Helper()

	token := jwtlib.NewWithClaims(method, claims)
	signed, err := token.SignedString(secret)
	if err != nil {
		t.Fatalf("SignedString() error = %v", err)
	}
	return signed
}

func assertValidationError(t *testing.T, got, want error) {
	t.Helper()

	if want == nil && got != nil {
		t.Fatalf("validation error = %v, want nil", got)
	}
	if want != nil && !errors.Is(got, want) {
		t.Fatalf("validation error = %v, want %v", got, want)
	}
}
