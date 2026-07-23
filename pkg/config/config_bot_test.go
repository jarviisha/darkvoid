package config

import "testing"

// LoadBot must work in a bare environment: the bot is a pure HTTP client and
// server-side requirements (JWT secret, DB settings) must not apply.
func TestLoadBot_NoServerEnvRequired(t *testing.T) {
	cfg := LoadBot()
	if cfg.Bot.APIBaseURL == "" {
		t.Fatal("expected default BOT_API_BASE_URL")
	}
	if cfg.Bot.Accounts <= 0 {
		t.Fatalf("accounts = %d, want positive default", cfg.Bot.Accounts)
	}
	if cfg.Bot.Interval <= 0 {
		t.Fatalf("interval = %v, want positive default", cfg.Bot.Interval)
	}
}
