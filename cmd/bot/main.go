// cmd/bot/main.go — AI content bot
//
// Usage:
//
//	go run ./cmd/bot [--max-posts=0]
//
// Flags:
//
//	--max-posts   stop after N successful posts; 0 runs until interrupted
//
// There are no --interval or --accounts flags any more: post interval, account
// count, model fallback chain, personas, and topics are served by GET /bot/plan and
// changed through the admin API, so the bot re-reads them on every tick instead of
// being restarted. The environment carries only credentials and an address.
//
// Requires GEMINI_API_KEY, BOT_RUNNER_PASSWORD, and a running DarkVoid API at
// BOT_API_BASE_URL whose runner account holds the bot role.
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

func main() {
	maxPosts := flag.Int("max-posts", 0, "stop after N successful posts (0 = run forever)")
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

	if missing := cfg.missing(); len(missing) > 0 {
		logger.Error(ctx, "required configuration is missing", "variables", strings.Join(missing, ", "))
		os.Exit(1)
	}

	// Both clients start on the built-in timeouts and are moved onto the plan's on
	// every tick. The timeout is carried on the client rather than on http.Client so
	// it can change without rewriting a field Do reads concurrently.
	bot := &runner{
		api: &apiClient{
			baseURL: cfg.APIBaseURL,
			http:    &http.Client{},
			now:     time.Now,
			timeout: defaultAPITimeout,
		},
		gemini: &geminiClient{
			baseURL: cfg.GeminiBaseURL,
			apiKey:  cfg.GeminiAPIKey,
			http:    &http.Client{},
			timeout: defaultGeminiTimeout,
		},
		personaPassword: cfg.Password,
		rng:             rand.New(rand.NewSource(time.Now().UnixNano())), //nolint:gosec // content variety, not crypto
	}

	// Fail fast on a bad runner credential rather than logging one 403 per tick.
	if err := bot.api.LoginRunner(ctx, cfg.RunnerUsername, cfg.RunnerPassword); err != nil {
		logger.LogError(ctx, err, "runner login failed", "username", cfg.RunnerUsername)
		os.Exit(1)
	}

	logger.Info(ctx, "bot starting", "api", cfg.APIBaseURL, "runner", cfg.RunnerUsername)
	bot.run(ctx, *maxPosts)
	logger.Info(ctx, "bot stopped")
}

// runner drives the fetch-plan → generate → post → report loop.
type runner struct {
	api    *apiClient
	gemini *geminiClient
	rng    *rand.Rand

	// personaPassword is shared by every persona account.
	personaPassword string

	// accounts holds one session per persona, keyed by bot id and created on first
	// sight. Building them from the plan rather than up front is what lets a persona
	// added through the admin API start posting without a restart.
	accounts map[string]*botAccount

	// recent maps username → openings of its latest posts (repetition guard).
	recent map[string][]string

	// linked records the personas whose account has already been reported, so the
	// identity call happens once rather than every post.
	linked map[string]bool
}

// run fetches the plan and posts once per interval until ctx is cancelled or
// maxPosts is reached (0 = unlimited).
//
// Everything that can go wrong here is logged and retried rather than fatal: a
// transient API or Gemini failure must not end a long-running bot. The interval
// itself comes from the plan, so a failed fetch falls back to planRetryInterval —
// there is nothing else to read it from.
func (r *runner) run(ctx context.Context, maxPosts int) {
	r.accounts = make(map[string]*botAccount)
	r.recent = make(map[string][]string)
	r.linked = make(map[string]bool)

	posted := 0
	for {
		wait := planRetryInterval

		p, err := r.api.FetchPlan(ctx)
		if err == nil {
			// Applied before anything uses the plan, and outside the switch so a
			// paused or empty plan still moves the timeouts — those are the states an
			// operator is most likely to be sitting in while tuning them.
			r.api.timeout = p.apiTimeout()
			r.gemini.timeout = p.geminiTimeout()
		}

		switch {
		case err != nil:
			if ctx.Err() != nil {
				return
			}
			logger.Warn(ctx, "plan fetch failed, retrying", "error", err, "retry_in", wait.String())

		case p.Paused:
			wait = p.interval()
			logger.Info(ctx, "paused by the control plane, publishing nothing", "next_check", wait.String())

		case len(p.Bots) == 0 || len(p.Topics) == 0:
			wait = p.interval()
			logger.Warn(ctx, "plan has nothing to post with",
				"personas", len(p.Bots), "topics", len(p.Topics))

		default:
			wait = p.interval()
			if err := r.postOnce(ctx, p); err != nil {
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
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(r.jittered(wait)):
		}
	}
}

// postOnce picks a persona and topic from the plan, generates content, publishes
// it, and reports the outcome either way — a failure an operator cannot see is the
// problem this log exists to solve.
func (r *runner) postOnce(ctx context.Context, p *plan) error {
	pb := r.pick(p)
	per := pb.persona()
	topic := p.Topics[r.rng.Intn(len(p.Topics))]
	acc := r.account(per)

	maxTags := p.maxTags()
	post, model, err := r.gemini.GeneratePost(ctx, p.Models, generationSettings{
		Prompt:      buildPrompt(ctx, p.PromptTemplate, per.promptData(topic, r.recent[per.username], maxTags)),
		Temperature: p.temperature(),
		MaxTags:     maxTags,
	})
	if err != nil {
		r.report(ctx, per, failedRun(per, model, err))
		return err
	}

	postID, err := r.api.CreatePost(ctx, acc, post.Content, post.Tags)
	if err != nil {
		r.report(ctx, per, failedRun(per, model, err))
		return err
	}

	r.linkIdentity(ctx, per, acc)
	r.remember(per.username, post.Content, p.recentMemory())
	r.report(ctx, per, runReport{
		BotID:             per.id,
		Status:            "success",
		PostID:            &postID,
		ModelUsed:         &model,
		HonoredRunRequest: per.runRequested,
	})

	logger.Info(ctx, "post created",
		"bot", per.username,
		"post_id", postID,
		"model", model,
		"tags", post.Tags,
		"honored_run_request", per.runRequested,
		"preview", truncate(post.Content, 80),
	)
	return nil
}

// pick chooses which persona posts next. A pending run-now wins over the random
// draw, since an operator asked for it explicitly.
func (r *runner) pick(p *plan) planBot {
	for _, b := range p.Bots {
		if b.RunRequested {
			return b
		}
	}
	return p.Bots[r.rng.Intn(len(p.Bots))]
}

// account returns the session for a persona, creating it on first sight. The stored
// persona is refreshed each time so an edited display name or voice takes effect
// without a restart.
func (r *runner) account(per persona) *botAccount {
	acc, ok := r.accounts[per.id]
	if !ok {
		acc = &botAccount{password: r.personaPassword}
		r.accounts[per.id] = acc
	}
	acc.persona = per
	return acc
}

// report sends the outcome to the activity log. It detaches from the caller's
// context: a SIGTERM landing mid-attempt cancels the loop's context before this
// runs, and without the detach every shutdown that interrupted a post logged a
// spurious reporting failure and the aborted attempt never reached the server.
//
// Transient failures are retried (see the constants in config.go for why), then
// logged and swallowed: the post itself already happened, and failing here would
// make the bot treat a published post as an error.
func (r *runner) report(ctx context.Context, per persona, report runReport) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), reportTimeout)
	defer cancel()

	var err error
	for attempt := 1; attempt <= reportAttempts; attempt++ {
		if err = r.api.ReportRun(ctx, report); err == nil {
			return
		}
		if attempt == reportAttempts || ctx.Err() != nil {
			break
		}
		select {
		case <-ctx.Done():
		case <-time.After(reportRetryDelay):
		}
	}
	logger.Warn(ctx, "failed to report the run", "bot", per.username, "error", err)
}

// failedRun builds an error report. ModelUsed is included when a model was actually
// reached, so the log distinguishes "the model refused" from "we never got that far".
func failedRun(per persona, model string, cause error) runReport {
	reason := cause.Error()
	report := runReport{
		BotID:             per.id,
		Status:            "error",
		Error:             &reason,
		HonoredRunRequest: per.runRequested,
	}
	if model != "" {
		report.ModelUsed = &model
	}
	return report
}

// linkIdentity reports which user account a persona posts as, once per process.
func (r *runner) linkIdentity(ctx context.Context, per persona, acc *botAccount) {
	if r.linked[per.id] || acc.userID == "" {
		return
	}
	if err := r.api.LinkIdentity(ctx, per.id, acc.userID); err != nil {
		// Cosmetic for the admin view only — retried on the next post.
		logger.Warn(ctx, "failed to link the persona account", "bot", per.username, "error", err)
		return
	}
	r.linked[per.id] = true
}

// remember keeps the opening of the bot's latest posts for the prompt, trimmed to
// the limit the plan asks for.
//
// The trim happens on write rather than on read so that lowering the limit through
// the admin API takes effect immediately instead of leaving the surplus entries
// sitting in the map until the process restarts. A limit of 0 disables the guard and
// drops what was already remembered, which is what "off" should mean.
func (r *runner) remember(username, content string, limit int) {
	if limit <= 0 {
		delete(r.recent, username)
		return
	}

	entries := append(r.recent[username], truncate(content, 60))
	if len(entries) > limit {
		entries = entries[len(entries)-limit:]
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
