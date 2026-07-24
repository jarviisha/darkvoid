package main

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// config holds everything the content bot needs. The bot is a standalone HTTP
// client — it does not share the API server's config (DB, JWT, storage), so it
// carries its own instead of borrowing pkg/config.
type config struct {
	// LogLevel is the slog level: debug, info, warn, error.
	LogLevel string
	// APIBaseURL is the DarkVoid API root the bot posts against,
	// e.g. "http://localhost:8080/api/v1".
	APIBaseURL string
	// Password shared by all bot accounts (must satisfy user password rules).
	Password string
	// Accounts is how many bot personas to activate (capped at the persona pool size).
	Accounts int
	// Interval is the delay between posts.
	Interval time.Duration
	// GeminiAPIKey is the AI Studio API key used for content generation.
	GeminiAPIKey string
	// GeminiModels is a priority-ordered fallback list of model ids. The bot
	// tries them from the top and rotates on quota exhaustion (429), so the
	// preferred model is reused automatically once its per-day quota resets.
	GeminiModels []string
	// GeminiBaseURL allows overriding the Gemini endpoint (tests, proxies).
	GeminiBaseURL string
}

// defaultGeminiModels is the fallback chain used when GEMINI_MODELS is unset.
// Ordered best→cheapest; each has its own per-model daily quota, so when the
// flagship is exhausted the bot drops to a lighter model instead of stalling.
var defaultGeminiModels = []string{
	"gemini-2.5-flash",
	"gemini-2.5-flash-lite",
	"gemini-2.0-flash",
	"gemini-flash-lite-latest",
}

// loadConfig reads bot configuration from the environment, loading a .env file
// first when present (silently ignored otherwise, e.g. under systemd).
//
//	LOG_LEVEL         (default: "info")
//	BOT_API_BASE_URL  (default: "http://localhost:8080/api/v1")
//	BOT_PASSWORD      (default: "Bot@12345")
//	BOT_ACCOUNTS      (default: 3)
//	BOT_POST_INTERVAL (default: 30s)
//	GEMINI_API_KEY    (default: "") — required
//	GEMINI_MODELS     (default: gemini-2.5-flash,gemini-2.5-flash-lite,gemini-2.0-flash,gemini-flash-lite-latest)
//	                  comma-separated priority list; falls back to the legacy
//	                  single-value GEMINI_MODEL when GEMINI_MODELS is unset
//	GEMINI_BASE_URL   (default: "https://generativelanguage.googleapis.com")
func loadConfig() config {
	_ = godotenv.Load()

	models := getEnvList("GEMINI_MODELS", nil)
	if models == nil {
		// Backward compat: honor the old single-model var as the first choice.
		if legacy := getEnv("GEMINI_MODEL", ""); legacy != "" {
			models = []string{legacy}
		} else {
			models = defaultGeminiModels
		}
	}

	return config{
		LogLevel:      getEnv("LOG_LEVEL", "info"),
		APIBaseURL:    getEnv("BOT_API_BASE_URL", "http://localhost:8080/api/v1"),
		Password:      getEnv("BOT_PASSWORD", "Bot@12345"),
		Accounts:      getEnvInt("BOT_ACCOUNTS", 3),
		Interval:      getEnvDuration("BOT_POST_INTERVAL", 30*time.Second),
		GeminiAPIKey:  getEnv("GEMINI_API_KEY", ""),
		GeminiModels:  models,
		GeminiBaseURL: getEnv("GEMINI_BASE_URL", "https://generativelanguage.googleapis.com"),
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvList parses a comma-separated env var into a trimmed, non-empty slice.
// Returns defaultValue when the var is unset or contains only blanks.
func getEnvList(key string, defaultValue []string) []string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	var out []string
	for _, part := range strings.Split(value, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	if len(out) == 0 {
		return defaultValue
	}
	return out
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
		}
	}
	return defaultValue
}
