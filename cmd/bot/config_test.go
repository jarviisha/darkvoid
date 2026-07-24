package main

import (
	"testing"
	"time"
)

// The bot must run in a bare environment: it is a pure HTTP client and no
// server-side settings (JWT secret, DB) apply. loadConfig should return usable
// defaults with nothing set.
func TestLoadConfig_Defaults(t *testing.T) {
	t.Setenv("BOT_API_BASE_URL", "")
	t.Setenv("BOT_ACCOUNTS", "")
	t.Setenv("BOT_POST_INTERVAL", "")
	t.Setenv("GEMINI_API_KEY", "")

	cfg := loadConfig()

	if cfg.APIBaseURL != "http://localhost:8080/api/v1" {
		t.Fatalf("APIBaseURL = %q, want default", cfg.APIBaseURL)
	}
	if cfg.Accounts != 3 {
		t.Fatalf("Accounts = %d, want default 3", cfg.Accounts)
	}
	if cfg.Interval != 30*time.Second {
		t.Fatalf("Interval = %v, want default 30s", cfg.Interval)
	}
	if len(cfg.GeminiModels) == 0 || cfg.GeminiModels[0] != "gemini-2.5-flash" {
		t.Fatalf("GeminiModels = %v, want default chain led by gemini-2.5-flash", cfg.GeminiModels)
	}
}

func TestLoadConfig_GeminiModelsRotationList(t *testing.T) {
	t.Setenv("GEMINI_MODELS", " gemini-2.5-flash-lite , gemini-2.0-flash ,")
	cfg := loadConfig()
	want := []string{"gemini-2.5-flash-lite", "gemini-2.0-flash"}
	if len(cfg.GeminiModels) != len(want) {
		t.Fatalf("GeminiModels = %v, want %v (trimmed, blanks dropped)", cfg.GeminiModels, want)
	}
	for i, m := range want {
		if cfg.GeminiModels[i] != m {
			t.Fatalf("GeminiModels[%d] = %q, want %q", i, cfg.GeminiModels[i], m)
		}
	}
}

func TestLoadConfig_LegacyGeminiModelFallback(t *testing.T) {
	t.Setenv("GEMINI_MODELS", "")
	t.Setenv("GEMINI_MODEL", "gemini-2.0-flash-lite")
	cfg := loadConfig()
	if len(cfg.GeminiModels) != 1 || cfg.GeminiModels[0] != "gemini-2.0-flash-lite" {
		t.Fatalf("GeminiModels = %v, want single legacy model", cfg.GeminiModels)
	}
}

func TestLoadConfig_EnvOverrides(t *testing.T) {
	t.Setenv("BOT_API_BASE_URL", "https://darkvoid-dev-api.jarviisha.com/api/v1")
	t.Setenv("BOT_ACCOUNTS", "5")
	t.Setenv("BOT_POST_INTERVAL", "2m")
	t.Setenv("GEMINI_API_KEY", "test-key")

	cfg := loadConfig()

	if cfg.APIBaseURL != "https://darkvoid-dev-api.jarviisha.com/api/v1" {
		t.Fatalf("APIBaseURL = %q, want override", cfg.APIBaseURL)
	}
	if cfg.Accounts != 5 {
		t.Fatalf("Accounts = %d, want 5", cfg.Accounts)
	}
	if cfg.Interval != 2*time.Minute {
		t.Fatalf("Interval = %v, want 2m", cfg.Interval)
	}
	if cfg.GeminiAPIKey != "test-key" {
		t.Fatalf("GeminiAPIKey = %q, want test-key", cfg.GeminiAPIKey)
	}
}
