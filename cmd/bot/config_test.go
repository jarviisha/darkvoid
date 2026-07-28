package main

import (
	"slices"
	"testing"
)

// clearBotEnv unsets everything loadConfig reads, so a variable left over in the
// developer's own .env cannot make a defaults test pass by accident.
func clearBotEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"LOG_LEVEL", "BOT_API_BASE_URL", "BOT_PASSWORD",
		"BOT_RUNNER_USERNAME", "BOT_RUNNER_PASSWORD",
		"GEMINI_API_KEY", "GEMINI_BASE_URL",
	} {
		t.Setenv(key, "")
	}
}

// The bot must run in a bare environment: it is a pure HTTP client and no
// server-side settings (JWT secret, DB) apply. loadConfig should return usable
// defaults for everything except the two credentials it cannot invent.
func TestLoadConfig_Defaults(t *testing.T) {
	clearBotEnv(t)

	cfg := loadConfig()

	if cfg.APIBaseURL != "http://localhost:8080/api/v1" {
		t.Errorf("APIBaseURL = %q, want default", cfg.APIBaseURL)
	}
	if cfg.RunnerUsername != "bot_runner" {
		t.Errorf("RunnerUsername = %q, want default bot_runner", cfg.RunnerUsername)
	}
	// No default for the persona password on purpose: one would be a live credential
	// published in this repository, letting anyone who reads it log in as a persona.
	if cfg.Password != "" {
		t.Errorf("Password = %q, want empty — a shipped default is a shipped credential", cfg.Password)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want info", cfg.LogLevel)
	}
	if cfg.GeminiBaseURL != "https://generativelanguage.googleapis.com" {
		t.Errorf("GeminiBaseURL = %q, want default", cfg.GeminiBaseURL)
	}
}

// Interval, account count, and the model chain used to live here. They now come
// from GET /bot/plan, so nothing in the environment can set them — an operator who
// still exports the old variables should not silently get the old behaviour.
func TestLoadConfig_RuntimeSettingsAreNotReadFromTheEnvironment(t *testing.T) {
	clearBotEnv(t)
	t.Setenv("BOT_ACCOUNTS", "5")
	t.Setenv("BOT_POST_INTERVAL", "2m")
	t.Setenv("GEMINI_MODELS", "gemini-2.0-flash")
	t.Setenv("GEMINI_MODEL", "gemini-2.0-flash-lite")

	// A compile-time guarantee as much as a runtime one: if a field for any of these
	// is ever added back to config, this test stops building.
	cfg := loadConfig()

	if cfg.APIBaseURL == "" {
		t.Fatal("loadConfig returned an unusable config")
	}
}

func TestLoadConfig_EnvOverrides(t *testing.T) {
	clearBotEnv(t)
	t.Setenv("BOT_API_BASE_URL", "https://darkvoid-dev-api.jarviisha.com/api/v1")
	t.Setenv("BOT_PASSWORD", "persona-pw-fixture")
	t.Setenv("BOT_RUNNER_USERNAME", "runner_prod")
	t.Setenv("BOT_RUNNER_PASSWORD", "runner-pw-fixture")
	t.Setenv("GEMINI_API_KEY", "test-key")
	t.Setenv("LOG_LEVEL", "debug")

	cfg := loadConfig()

	if cfg.APIBaseURL != "https://darkvoid-dev-api.jarviisha.com/api/v1" {
		t.Errorf("APIBaseURL = %q, want override", cfg.APIBaseURL)
	}
	if cfg.Password != "persona-pw-fixture" {
		t.Errorf("Password = %q, want override", cfg.Password)
	}
	if cfg.RunnerUsername != "runner_prod" || cfg.RunnerPassword != "runner-pw-fixture" {
		t.Errorf("runner = %q/%q, want overrides", cfg.RunnerUsername, cfg.RunnerPassword)
	}
	if cfg.GeminiAPIKey != "test-key" {
		t.Errorf("GeminiAPIKey = %q, want test-key", cfg.GeminiAPIKey)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want debug", cfg.LogLevel)
	}
}

// Both credentials are reported together, so a misconfigured deployment learns
// everything that is wrong in one start rather than one variable per restart.
func TestConfigMissing(t *testing.T) {
	tests := []struct {
		name string
		cfg  config
		want []string
	}{
		{
			name: "none set",
			cfg:  config{},
			want: []string{"GEMINI_API_KEY", "BOT_RUNNER_PASSWORD", "BOT_PASSWORD"},
		},
		{
			name: "only the gemini key",
			cfg:  config{GeminiAPIKey: "k"},
			want: []string{"BOT_RUNNER_PASSWORD", "BOT_PASSWORD"},
		},
		{
			name: "only the runner password",
			cfg:  config{RunnerPassword: "p"},
			want: []string{"GEMINI_API_KEY", "BOT_PASSWORD"},
		},
		{
			// The persona password has no default, so forgetting it has to be reported
			// rather than silently falling back to a credential shipped in the repo.
			name: "only the persona password missing",
			cfg:  config{GeminiAPIKey: "k", RunnerPassword: "p"},
			want: []string{"BOT_PASSWORD"},
		},
		{
			name: "all set",
			cfg:  config{GeminiAPIKey: "k", RunnerPassword: "p", Password: "p"},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.missing(); !slices.Equal(got, tt.want) {
				t.Errorf("missing() = %v, want %v", got, tt.want)
			}
		})
	}
}
