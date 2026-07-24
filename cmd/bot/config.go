package main

import (
	"os"
	"strconv"
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
	// GeminiModel is the Gemini model id, e.g. "gemini-2.5-flash".
	GeminiModel string
	// GeminiBaseURL allows overriding the Gemini endpoint (tests, proxies).
	GeminiBaseURL string
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
//	GEMINI_MODEL      (default: "gemini-2.5-flash")
//	GEMINI_BASE_URL   (default: "https://generativelanguage.googleapis.com")
func loadConfig() config {
	_ = godotenv.Load()

	return config{
		LogLevel:      getEnv("LOG_LEVEL", "info"),
		APIBaseURL:    getEnv("BOT_API_BASE_URL", "http://localhost:8080/api/v1"),
		Password:      getEnv("BOT_PASSWORD", "Bot@12345"),
		Accounts:      getEnvInt("BOT_ACCOUNTS", 3),
		Interval:      getEnvDuration("BOT_POST_INTERVAL", 30*time.Second),
		GeminiAPIKey:  getEnv("GEMINI_API_KEY", ""),
		GeminiModel:   getEnv("GEMINI_MODEL", "gemini-2.5-flash"),
		GeminiBaseURL: getEnv("GEMINI_BASE_URL", "https://generativelanguage.googleapis.com"),
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
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
