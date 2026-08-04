package config

import (
	"net/http"
	"strings"
	"testing"
)

// The refresh token cookie is the one config here whose failures are invisible
// at boot: a browser that rejects the cookie says nothing to the server, and the
// symptom surfaces as a 401 on refresh that reads like an expired session. These
// tests pin the two rules that keep such a combination from starting at all, and
// the Secure default that decides which of them applies.

func TestLoadCookieConfig_Defaults(t *testing.T) {
	cfg := loadCookieConfig(true)

	if cfg.SameSite != "lax" {
		t.Errorf("SameSite = %q, want %q", cfg.SameSite, "lax")
	}
	if cfg.Domain != "" {
		t.Errorf("Domain = %q, want empty so the cookie stays host-only", cfg.Domain)
	}
	if !cfg.Secure {
		t.Errorf("Secure = false with an unset COOKIE_SECURE; want the passed-in default")
	}
}

func TestLoadCookieConfig_SecureFollowsEnvironmentWhenUnset(t *testing.T) {
	// The derived default is the whole reason COOKIE_SECURE is a tri-state: a
	// development deployment must not be handed a Secure cookie it serves over
	// plain HTTP, and a production one must never lose it by omission.
	if cfg := loadCookieConfig(false); cfg.Secure {
		t.Errorf("Secure = true for a development default; want false")
	}
	if cfg := loadCookieConfig(true); !cfg.Secure {
		t.Errorf("Secure = false for a production default; want true")
	}
}

func TestLoadCookieConfig_SecureOverridesEnvironment(t *testing.T) {
	// The staging box behind TLS termination that still calls itself development.
	t.Setenv("COOKIE_SECURE", "true")

	if cfg := loadCookieConfig(false); !cfg.Secure {
		t.Errorf("Secure = false; want COOKIE_SECURE=true to win over the derived default")
	}
}

func TestLoadCookieConfig_SameSiteIsNormalized(t *testing.T) {
	// Case and stray whitespace are transcription noise, not operator intent —
	// Validate would otherwise reject "Lax " as an unknown mode.
	t.Setenv("COOKIE_SAMESITE", "  None ")
	t.Setenv("COOKIE_DOMAIN", " example.com ")

	cfg := loadCookieConfig(true)

	if cfg.SameSite != "none" {
		t.Errorf("SameSite = %q, want %q", cfg.SameSite, "none")
	}
	if cfg.Domain != "example.com" {
		t.Errorf("Domain = %q, want %q", cfg.Domain, "example.com")
	}
}

func TestCookieSameSiteMode_Mapping(t *testing.T) {
	tests := []struct {
		name string
		want http.SameSite
	}{
		{"lax", http.SameSiteLaxMode},
		{"strict", http.SameSiteStrictMode},
		{"none", http.SameSiteNoneMode},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := (CookieConfig{SameSite: tt.name}).SameSiteMode(); got != tt.want {
				t.Errorf("SameSiteMode() = %v, want %v", got, tt.want)
			}
		})
	}

	// Never SameSiteDefaultMode, which emits no attribute at all. Validate keeps
	// an unknown name from reaching here, but the fallback must still be a real
	// mode rather than the zero value if that guard is ever moved.
	if got := (CookieConfig{SameSite: "bogus"}).SameSiteMode(); got == http.SameSiteDefaultMode {
		t.Errorf("SameSiteMode() = SameSiteDefaultMode for an unknown name; want a concrete mode")
	}
}

// validCookieConfig returns a Config that passes Validate, so each test below
// can change one field and attribute the failure to that field.
func validCookieConfig() *Config {
	cfg := &Config{}
	cfg.App = AppConfig{Name: "darkvoid", Environment: "production"}
	cfg.Database = DatabaseConfig{Host: "localhost", Port: 5432, User: "postgres", Database: "darkvoid", MaxConns: 25, MinConns: 5}
	cfg.Logger = LoggerConfig{Level: "info", Format: "json"}
	cfg.Server = ServerConfig{Port: 8080}
	cfg.Cookie = CookieConfig{SameSite: "lax", Secure: true}
	cfg.JWT = JWTConfig{Secret: "secret", Issuer: "darkvoid", AccessTokenExpiry: 15 * 60 * 1e9}
	cfg.RefreshToken = RefreshTokenConfig{Expiry: 24 * 60 * 60 * 1e9}
	cfg.FeedFanout = FeedFanoutConfig{Workers: 1, QueueSize: 1}
	cfg.Settings = SettingsConfig{RefreshInterval: 30 * 1e9}
	return cfg
}

func TestValidate_CookieBaselineIsValid(t *testing.T) {
	if err := validCookieConfig().Validate(); err != nil {
		t.Fatalf("baseline config failed to validate: %v", err)
	}
}

func TestValidate_RejectsUnknownSameSite(t *testing.T) {
	cfg := validCookieConfig()
	cfg.Cookie.SameSite = "lx"

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() = nil for an unknown samesite; want an error naming the field")
	}
	if !strings.Contains(err.Error(), "samesite") {
		t.Errorf("error %q does not name samesite", err)
	}
}

func TestValidate_RejectsSameSiteNoneWithoutSecure(t *testing.T) {
	cfg := validCookieConfig()
	cfg.Cookie.SameSite = "none"
	cfg.Cookie.Secure = false

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() = nil for samesite=none without Secure; want an error — the browser would drop the cookie")
	}
	if !strings.Contains(err.Error(), "COOKIE_SECURE") {
		t.Errorf("error %q does not point at COOKIE_SECURE, which is the way out", err)
	}
}

func TestValidate_AllowsSameSiteNoneWithSecure(t *testing.T) {
	cfg := validCookieConfig()
	cfg.Cookie.SameSite = "none"
	cfg.Cookie.Secure = true

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() = %v; want nil for the cross-site configuration", err)
	}
}

func TestValidate_RejectsMalformedCookieDomain(t *testing.T) {
	// Each of these produces a cookie the browser discards without comment.
	for _, domain := range []string{"https://example.com", "example.com:8443", "example.com/app"} {
		t.Run(domain, func(t *testing.T) {
			cfg := validCookieConfig()
			cfg.Cookie.Domain = domain

			if err := cfg.Validate(); err == nil {
				t.Errorf("Validate() = nil for domain %q; want an error", domain)
			}
		})
	}
}

func TestValidate_AllowsBareCookieDomain(t *testing.T) {
	cfg := validCookieConfig()
	cfg.Cookie.Domain = "example.com"

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() = %v; want nil for a bare hostname", err)
	}
}
