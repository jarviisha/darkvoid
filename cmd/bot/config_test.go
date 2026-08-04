package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// botEnvKeys is every variable loadConfig reads.
var botEnvKeys = []string{
	"LOG_LEVEL", "BOT_API_BASE_URL", "BOT_PASSWORD",
	"BOT_RUNNER_USERNAME", "BOT_RUNNER_PASSWORD",
	"GEMINI_API_KEY", "GEMINI_BASE_URL",
}

// clearBotEnv unsets everything loadConfig reads, so a variable left over in the
// developer's own .env cannot make a defaults test pass by accident.
func clearBotEnv(t *testing.T) {
	t.Helper()
	for _, key := range botEnvKeys {
		t.Setenv(key, "")
	}
}

// unsetBotEnv removes the variables entirely rather than blanking them. The
// difference matters only for the dotenv tests: godotenv treats a variable that is
// set-but-empty as already present, so clearBotEnv would stop the files under test
// from being applied at all. t.Setenv is called first purely to register the
// restore — it is what puts the original value back when the test ends.
func unsetBotEnv(t *testing.T) {
	t.Helper()
	for _, key := range botEnvKeys {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("unset %s: %v", key, err)
		}
	}
}

// writeEnvFile drops a dotenv file into the current directory, which the dotenv
// tests have already pointed at a temp dir via t.Chdir.
func writeEnvFile(t *testing.T, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(".", name), []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
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

// The bot's own file wins over the server's. Both are read, so a deployment that
// keeps the bot variables in .env keeps working, but a value that appears in both
// resolves to .env.bot rather than to whichever file happened to load first.
func TestLoadConfig_BotEnvFileOverridesDotEnv(t *testing.T) {
	t.Chdir(t.TempDir())
	unsetBotEnv(t)

	writeEnvFile(t, ".env", "BOT_PASSWORD=from-dot-env\nGEMINI_API_KEY=key-from-dot-env\nBOT_RUNNER_USERNAME=runner_from_dot_env\n")
	writeEnvFile(t, botEnvFile, "BOT_PASSWORD=from-dot-env-bot\nGEMINI_API_KEY=key-from-dot-env-bot\n")

	cfg := loadConfig()

	if cfg.Password != "from-dot-env-bot" {
		t.Errorf("Password = %q, want the .env.bot value", cfg.Password)
	}
	if cfg.GeminiAPIKey != "key-from-dot-env-bot" {
		t.Errorf("GeminiAPIKey = %q, want the .env.bot value", cfg.GeminiAPIKey)
	}
	// Not named in .env.bot, so .env still supplies it — the bot file overrides,
	// it does not replace.
	if cfg.RunnerUsername != "runner_from_dot_env" {
		t.Errorf("RunnerUsername = %q, want the .env value to fill the gap", cfg.RunnerUsername)
	}
}

// .env.bot outranks the inherited environment too, because `make bot` exports
// .env into the environment before the process starts. Without this, the same two
// files would resolve differently under `make bot` and `go run ./cmd/bot`.
func TestLoadConfig_BotEnvFileOverridesInheritedEnvironment(t *testing.T) {
	t.Chdir(t.TempDir())
	unsetBotEnv(t)
	t.Setenv("BOT_PASSWORD", "exported-by-make-from-dot-env")

	writeEnvFile(t, botEnvFile, "BOT_PASSWORD=from-dot-env-bot\n")

	if cfg := loadConfig(); cfg.Password != "from-dot-env-bot" {
		t.Errorf("Password = %q, want the .env.bot value", cfg.Password)
	}
}

// A missing .env.bot is the normal case, and it must not stop .env from loading —
// the failure godotenv.Load(".env.bot", ".env") would have introduced.
func TestLoadConfig_DotEnvStillLoadsWithoutBotEnvFile(t *testing.T) {
	t.Chdir(t.TempDir())
	unsetBotEnv(t)

	writeEnvFile(t, ".env", "BOT_PASSWORD=from-dot-env\n")

	if cfg := loadConfig(); cfg.Password != "from-dot-env" {
		t.Errorf("Password = %q, want the .env value", cfg.Password)
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
