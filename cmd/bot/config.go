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

// Run reports are retried because a lost one costs more than a late one: for a
// run-now attempt the server clears its pending flag only on the report, so a
// dropped report means the same request is honored again next tick — a duplicate
// post the operator asked for once. The timeout bounds how long a report can hold
// up shutdown, since reporting detaches from the loop's context.
const (
	reportAttempts   = 3
	reportRetryDelay = time.Second
	reportTimeout    = 20 * time.Second
)

// Defaults for the generation knobs served by GET /bot/plan. They are not dead
// code waiting on a migration: the bot has to be able to generate before it has
// ever seen a plan (the first API calls happen during runner login) and after a
// plan fetch that failed or came from an older API that does not send the fields.
// A default of zero would mean no tags, no repetition guard and a timeout that
// fails every request, so each of these is the value the bot shipped with when
// they were compile-time constants.
//
// They are deliberately not environment variables. Putting them back in the
// environment would recreate exactly the problem this change removes; their real
// home is bot.config, and these are only the floor under a missing answer.
const (
	defaultTemperature   = 1.0
	defaultMaxTags       = 3
	defaultRecentMemory  = 5
	defaultAPITimeout    = 15 * time.Second
	defaultGeminiTimeout = 60 * time.Second
)

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
