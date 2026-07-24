// cmd/bot/main.go — AI content bot
//
// Usage:
//
//	go run ./cmd/bot [--interval=30s] [--accounts=3] [--max-posts=0]
//
// Flags (each overrides its BOT_* env default when set):
//
//	--interval    delay between posts (default: BOT_POST_INTERVAL)
//	--accounts    number of bot personas to use (default: BOT_ACCOUNTS)
//	--max-posts   stop after N posts; 0 runs until interrupted
//
// Requires GEMINI_API_KEY and a running DarkVoid API at BOT_API_BASE_URL.
package main

import (
	"context"
	"flag"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jarviisha/darkvoid/pkg/logger"
)

// recentMemory is how many of a bot's latest posts are echoed back into the
// prompt to steer Gemini away from repeating itself.
const recentMemory = 5

func main() {
	interval := flag.Duration("interval", 0, "delay between posts (0 = use BOT_POST_INTERVAL)")
	accounts := flag.Int("accounts", 0, "number of bot accounts (0 = use BOT_ACCOUNTS)")
	maxPosts := flag.Int("max-posts", 0, "stop after N posts (0 = run forever)")
	flag.Parse()

	cfg := loadConfig()
	log := logger.New(&logger.Config{
		Level:   cfg.LogLevel,
		Format:  "text",
		Output:  os.Stdout,
		Service: "darkvoid-bot",
	})
	logger.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// FromContext falls back to a fresh default-config logger, so the bot's
	// logger must ride along in the context for the package-level helpers.
	ctx = logger.WithLogger(ctx, log)

	if cfg.GeminiAPIKey == "" {
		logger.Error(ctx, "GEMINI_API_KEY is required")
		os.Exit(1)
	}
	if *interval > 0 {
		cfg.Interval = *interval
	}
	if *accounts > 0 {
		cfg.Accounts = *accounts
	}
	n := min(max(cfg.Accounts, 1), len(personas))

	bot := &runner{
		api: &apiClient{
			baseURL: cfg.APIBaseURL,
			http:    &http.Client{Timeout: 15 * time.Second},
			now:     time.Now,
		},
		gemini: &geminiClient{
			baseURL: cfg.GeminiBaseURL,
			apiKey:  cfg.GeminiAPIKey,
			models:  cfg.GeminiModels,
			http:    &http.Client{Timeout: 60 * time.Second},
		},
		rng: rand.New(rand.NewSource(time.Now().UnixNano())), //nolint:gosec // content variety, not crypto
	}
	for _, p := range personas[:n] {
		bot.accounts = append(bot.accounts, &botAccount{persona: p, password: cfg.Password})
	}

	logger.Info(ctx, "bot starting",
		"api", cfg.APIBaseURL,
		"models", strings.Join(cfg.GeminiModels, ","),
		"accounts", n,
		"interval", cfg.Interval.String(),
	)
	bot.run(ctx, cfg.Interval, *maxPosts)
	logger.Info(ctx, "bot stopped")
}

// runner drives the generate→post loop across the bot accounts.
type runner struct {
	api      *apiClient
	gemini   *geminiClient
	accounts []*botAccount
	rng      *rand.Rand

	// recent maps username → openings of its latest posts (repetition guard).
	recent map[string][]string
}

// run posts once per interval until ctx is cancelled or maxPosts is reached
// (0 = unlimited). Individual failures are logged and skipped — the loop
// keeps going so a transient API/Gemini error doesn't kill the bot.
func (r *runner) run(ctx context.Context, interval time.Duration, maxPosts int) {
	r.recent = make(map[string][]string, len(r.accounts))

	posted := 0
	for {
		if err := r.postOnce(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}
			logger.Warn(ctx, "post attempt failed", "error", err)
		} else {
			posted++
			if maxPosts > 0 && posted >= maxPosts {
				return
			}
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(r.jittered(interval)):
		}
	}
}

// postOnce picks a random persona, generates content, and publishes it.
func (r *runner) postOnce(ctx context.Context) error {
	acc := r.accounts[r.rng.Intn(len(r.accounts))]
	topic := topics[r.rng.Intn(len(topics))]

	post, err := r.gemini.GeneratePost(ctx, acc.persona, topic, r.recent[acc.persona.username])
	if err != nil {
		return err
	}

	postID, err := r.api.CreatePost(ctx, acc, post.Content, post.Tags)
	if err != nil {
		return err
	}

	r.remember(acc.persona.username, post.Content)
	logger.Info(ctx, "post created",
		"bot", acc.persona.username,
		"post_id", postID,
		"tags", post.Tags,
		"preview", truncate(post.Content, 80),
	)
	return nil
}

// remember keeps the opening of the bot's latest posts for the prompt.
func (r *runner) remember(username, content string) {
	entries := append(r.recent[username], truncate(content, 60))
	if len(entries) > recentMemory {
		entries = entries[len(entries)-recentMemory:]
	}
	r.recent[username] = entries
}

// jittered spreads posts ±25% around the base interval so activity doesn't
// look metronomic in the feed.
func (r *runner) jittered(interval time.Duration) time.Duration {
	if interval <= 0 {
		return time.Second
	}
	quarter := interval / 4
	return interval - quarter + time.Duration(r.rng.Int63n(int64(2*quarter)+1))
}
