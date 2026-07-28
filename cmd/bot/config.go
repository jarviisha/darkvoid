package main

import (
	"os"
	"time"

	"github.com/joho/godotenv"
)

// planRetryInterval is how long the bot waits before asking for its plan again
// after a failed fetch. The interval it should normally use lives in the plan, so
// when the fetch itself fails there is nothing to read it from.
const planRetryInterval = 30 * time.Second

// config holds everything the content bot needs from its environment, which is now
// only credentials and an address. Post interval, account count, model fallback
// chain, personas, and topics all come from GET /bot/plan instead, so an operator
// changes them through the admin API rather than by editing a unit file and
// restarting.
//
// The bot is a standalone HTTP client — it does not share the API server's config
// (DB, JWT, storage), so it carries its own instead of borrowing pkg/config.
type config struct {
	// LogLevel is the slog level: debug, info, warn, error.
	LogLevel string
	// APIBaseURL is the DarkVoid API root the bot posts against,
	// e.g. "http://localhost:8080/api/v1".
	APIBaseURL string
	// Password is shared by all persona accounts, which are registered with it on
	// first use (must satisfy the user password rules). Required, with no default:
	// a default would be a live credential published in this repository, and any
	// deployment that forgot to set it would let a reader of the source log in as
	// its personas and post as them.
	Password string
	// RunnerUsername and RunnerPassword identify the account that holds the bot
	// role. It is the only account allowed on /bot/*, and it is deliberately not
	// one of the personas: the personas publish posts, the runner reads the plan and
	// reports results. The bot never registers it — an auto-created runner would
	// lack the role and every plan fetch would 403 for a reason nothing explains.
	RunnerUsername string
	RunnerPassword string
	// GeminiAPIKey is the AI Studio API key used for content generation.
	GeminiAPIKey string
	// GeminiBaseURL allows overriding the Gemini endpoint (tests, proxies).
	GeminiBaseURL string
}

// loadConfig reads bot configuration from the environment, loading a .env file
// first when present (silently ignored otherwise, e.g. in the container, where
// compose supplies the environment directly).
//
//	LOG_LEVEL            (default: "info")
//	BOT_API_BASE_URL     (default: "http://localhost:8080/api/v1")
//	BOT_PASSWORD         (default: "") — required
//	BOT_RUNNER_USERNAME  (default: "bot_runner")
//	BOT_RUNNER_PASSWORD  (default: "") — required
//	GEMINI_API_KEY       (default: "") — required
//	GEMINI_BASE_URL      (default: "https://generativelanguage.googleapis.com")
func loadConfig() config {
	_ = godotenv.Load()

	return config{
		LogLevel:       getEnv("LOG_LEVEL", "info"),
		APIBaseURL:     getEnv("BOT_API_BASE_URL", "http://localhost:8080/api/v1"),
		Password:       getEnv("BOT_PASSWORD", ""),
		RunnerUsername: getEnv("BOT_RUNNER_USERNAME", "bot_runner"),
		RunnerPassword: getEnv("BOT_RUNNER_PASSWORD", ""),
		GeminiAPIKey:   getEnv("GEMINI_API_KEY", ""),
		GeminiBaseURL:  getEnv("GEMINI_BASE_URL", "https://generativelanguage.googleapis.com"),
	}
}

// missing returns the names of the required variables that are unset, so startup
// can report all of them at once instead of one per restart.
func (c config) missing() []string {
	var out []string
	if c.GeminiAPIKey == "" {
		out = append(out, "GEMINI_API_KEY")
	}
	if c.RunnerPassword == "" {
		out = append(out, "BOT_RUNNER_PASSWORD")
	}
	if c.Password == "" {
		out = append(out, "BOT_PASSWORD")
	}
	return out
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
