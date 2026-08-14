package config

import (
	"strings"
	"testing"
)

func TestLoadJWTConfig_Audience(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		t.Setenv("JWT_AUDIENCE", "")

		if got := loadJWTConfig().Audience; got != "darkvoid-api" {
			t.Fatalf("Audience = %q, want darkvoid-api", got)
		}
	})

	t.Run("environment override", func(t *testing.T) {
		t.Setenv("JWT_AUDIENCE", "admin-api")

		if got := loadJWTConfig().Audience; got != "admin-api" {
			t.Fatalf("Audience = %q, want admin-api", got)
		}
	})
}

func TestValidate_RequiresJWTAudience(t *testing.T) {
	cfg := validCookieConfig()
	cfg.JWT.Audience = ""

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "JWT audience") {
		t.Fatalf("Validate() error = %v, want JWT audience error", err)
	}
}
