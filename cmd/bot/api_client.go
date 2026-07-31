package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// tokenSafetyMargin re-logins slightly before the access token actually
// expires so a request never races token expiry.
const tokenSafetyMargin = 30 * time.Second

// apiClient is a minimal DarkVoid API client for the bot. It holds two kinds of
// session: the runner account, which reads the plan and reports runs on /bot/*, and
// one per persona, which publish posts. They are separate because only the runner
// carries the bot role.
type apiClient struct {
	baseURL string
	http    *http.Client
	now     func() time.Time

	// timeout bounds one request. It comes from the plan, so the run loop rewrites it
	// between ticks; the runner is a single goroutine and every call it makes is
	// sequential, so no synchronisation is needed. It is applied per request through
	// the context rather than by writing http.Client.Timeout, because that field is
	// read inside Do and rewriting it would be a data race the moment anything in
	// this process became concurrent.
	timeout time.Duration

	runner botAccount
}

// botAccount is one authenticated account: a persona, or the runner.
type botAccount struct {
	persona     persona
	username    string
	password    string
	accessToken string
	expiresAt   time.Time
	// userID is the usr.users row this account authenticated as, reported back so
	// the admin view can show which user a persona posts as.
	userID string
}

// name returns the login username, which for a persona is its own.
func (a *botAccount) name() string {
	if a.username != "" {
		return a.username
	}
	return a.persona.username
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type registerRequest struct {
	Username    string `json:"username"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	Password    string `json:"password"`
}

// tokenResponse covers the fields shared by login and register responses. The two
// report the account id in different places — register at the top level, login
// nested under "user" — so both are decoded and userID picks whichever arrived.
type tokenResponse struct {
	UserID          string `json:"user_id"`
	AccessToken     string `json:"access_token"`
	AccessExpiresIn int64  `json:"access_expires_in"`
	User            struct {
		ID string `json:"id"`
	} `json:"user"`
}

func (t tokenResponse) userID() string {
	if t.UserID != "" {
		return t.UserID
	}
	return t.User.ID
}

type createPostRequest struct {
	Content    string   `json:"content"`
	Visibility string   `json:"visibility"`
	Tags       []string `json:"tags,omitempty"`
}

type createPostResponse struct {
	ID string `json:"id"`
}

// plan is the desired state served by GET /bot/plan. It mirrors the server's
// dto.PlanResponse; contract_test.go pins the two together.
//
// Temperature, MaxTagsPerPost and RecentMemory are pointers because zero is a legal
// value for each of them — deterministic sampling, no tags, no repetition guard —
// so an absent field and a deliberate zero have to stay distinguishable. Without
// that, a plan from an API predating these fields would decode as "ask for zero
// tags and disable the repetition guard" rather than as "not configured". The
// timeouts need no such treatment: zero is not a usable timeout, so it can mean
// absent on its own.
type plan struct {
	Paused              bool      `json:"paused"`
	PostIntervalSeconds int32     `json:"post_interval_seconds"`
	Models              []string  `json:"models"`
	Bots                []planBot `json:"bots"`
	Topics              []string  `json:"topics"`

	PromptTemplate       string   `json:"prompt_template"`
	Temperature          *float64 `json:"temperature"`
	MaxTagsPerPost       *int32   `json:"max_tags_per_post"`
	RecentMemory         *int32   `json:"recent_memory"`
	APITimeoutSeconds    int32    `json:"api_timeout_seconds"`
	GeminiTimeoutSeconds int32    `json:"gemini_timeout_seconds"`
}

type planBot struct {
	ID           string `json:"id"`
	Username     string `json:"username"`
	DisplayName  string `json:"display_name"`
	Style        string `json:"style"`
	RunRequested bool   `json:"run_requested"`
}

// interval returns the configured delay between posts, falling back to the retry
// interval if the server ever sends a nonsensical value.
func (p *plan) interval() time.Duration {
	if p.PostIntervalSeconds <= 0 {
		return planRetryInterval
	}
	return time.Duration(p.PostIntervalSeconds) * time.Second
}

// The accessors below all read "the server's answer, or the built-in default if it
// did not give one". They exist so the fallback is decided in one place per knob
// rather than at each use, which is what kept the old `interval()` honest when a
// server sent a nonsensical value.

// temperature is the Gemini sampling temperature for this tick.
func (p *plan) temperature() float64 {
	if p.Temperature == nil {
		return defaultTemperature
	}
	return *p.Temperature
}

// maxTags is how many tags to ask for and keep. Negative is treated as absent
// rather than clamped to zero: it can only come from a corrupt payload, and
// silently posting untagged is harder to notice than posting as configured.
func (p *plan) maxTags() int {
	if p.MaxTagsPerPost == nil || *p.MaxTagsPerPost < 0 {
		return defaultMaxTags
	}
	return int(*p.MaxTagsPerPost)
}

// recentMemory is how many of a persona's latest posts feed the repetition guard.
func (p *plan) recentMemory() int {
	if p.RecentMemory == nil || *p.RecentMemory < 0 {
		return defaultRecentMemory
	}
	return int(*p.RecentMemory)
}

// apiTimeout bounds one request to the DarkVoid API.
func (p *plan) apiTimeout() time.Duration {
	if p.APITimeoutSeconds <= 0 {
		return defaultAPITimeout
	}
	return time.Duration(p.APITimeoutSeconds) * time.Second
}

// geminiTimeout bounds one generateContent call.
func (p *plan) geminiTimeout() time.Duration {
	if p.GeminiTimeoutSeconds <= 0 {
		return defaultGeminiTimeout
	}
	return time.Duration(p.GeminiTimeoutSeconds) * time.Second
}

// persona converts a plan entry into the voice Gemini is prompted with.
func (b planBot) persona() persona {
	return persona{
		id:           b.ID,
		username:     b.Username,
		displayName:  b.DisplayName,
		style:        b.Style,
		runRequested: b.RunRequested,
	}
}

// runReport is one attempt reported to POST /bot/runs. A success carries PostID and
// no Error; a failure the reverse — the server rejects any other combination.
type runReport struct {
	BotID     string  `json:"bot_id"`
	Status    string  `json:"status"`
	PostID    *string `json:"post_id,omitempty"`
	ModelUsed *string `json:"model_used,omitempty"`
	Error     *string `json:"error,omitempty"`
	// HonoredRunRequest must be set only for the attempt that answered an
	// operator's run-now, because that is what clears the pending flag. Setting it
	// on an ordinary interval post would discard a request that arrived after the
	// last plan fetch, and that post would never happen.
	HonoredRunRequest bool `json:"honored_run_request"`
}

type linkIdentityRequest struct {
	UserID string `json:"user_id"`
}

// ─── Runner session ──────────────────────────────────────────────────────────

// LoginRunner authenticates the account that holds the bot role. Unlike a persona
// it is never registered on the fly: a fresh account would not carry the role, and
// the resulting 403 on every plan fetch would look like a server fault.
func (c *apiClient) LoginRunner(ctx context.Context, username, password string) error {
	c.runner.username = username
	c.runner.password = password
	return c.ensureRunner(ctx)
}

func (c *apiClient) ensureRunner(ctx context.Context) error {
	if c.runner.accessToken != "" && c.now().Before(c.runner.expiresAt) {
		return nil
	}

	var tok tokenResponse
	status, body, err := c.postJSON(ctx, "/auth/login", "", loginRequest{
		Username: c.runner.username,
		Password: c.runner.password,
	}, &tok)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("runner login %s: status %d: %s — the account must exist and hold the bot role",
			c.runner.username, status, truncate(string(body), 200))
	}
	if tok.AccessToken == "" {
		return fmt.Errorf("runner login %s returned no access token", c.runner.username)
	}

	c.runner.accessToken = tok.AccessToken
	c.runner.userID = tok.userID()
	c.runner.expiresAt = c.now().Add(time.Duration(tok.AccessExpiresIn)*time.Second - tokenSafetyMargin)
	return nil
}

// FetchPlan reads the desired state the bot should run under.
func (c *apiClient) FetchPlan(ctx context.Context) (*plan, error) {
	var p plan
	status, body, err := c.runnerCall(ctx, http.MethodGet, "/bot/plan", nil, &p)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("fetch plan: status %d: %s", status, truncate(string(body), 200))
	}
	return &p, nil
}

// ReportRun records one attempt in the activity log an operator reads.
func (c *apiClient) ReportRun(ctx context.Context, report runReport) error {
	status, body, err := c.runnerCall(ctx, http.MethodPost, "/bot/runs", report, nil)
	if err != nil {
		return err
	}
	if status != http.StatusNoContent && status != http.StatusOK {
		return fmt.Errorf("report run: status %d: %s", status, truncate(string(body), 200))
	}
	return nil
}

// LinkIdentity tells the server which user account a persona posts as.
func (c *apiClient) LinkIdentity(ctx context.Context, botID, userID string) error {
	path := "/bot/" + botID + "/identity"
	status, body, err := c.runnerCall(ctx, http.MethodPost, path, linkIdentityRequest{UserID: userID}, nil)
	if err != nil {
		return err
	}
	if status != http.StatusNoContent && status != http.StatusOK {
		return fmt.Errorf("link identity for %s: status %d: %s", botID, status, truncate(string(body), 200))
	}
	return nil
}

// runnerCall performs a request as the runner, re-authenticating once if the token
// was rejected — the API restarting with a new signing secret invalidates it
// without the expiry having passed.
func (c *apiClient) runnerCall(ctx context.Context, method, path string, in, out any) (int, []byte, error) {
	if err := c.ensureRunner(ctx); err != nil {
		return 0, nil, err
	}

	status, body, err := c.do(ctx, method, path, c.runner.accessToken, in, out)
	if err != nil {
		return status, body, err
	}
	if status != http.StatusUnauthorized {
		return status, body, nil
	}

	c.runner.accessToken = ""
	if err = c.ensureRunner(ctx); err != nil {
		return 0, nil, err
	}
	return c.do(ctx, method, path, c.runner.accessToken, in, out)
}

// ─── Persona sessions ────────────────────────────────────────────────────────

// EnsureLogin makes sure the account holds a valid access token,
// registering the account on first use.
func (c *apiClient) EnsureLogin(ctx context.Context, acc *botAccount) error {
	if acc.accessToken != "" && c.now().Before(acc.expiresAt) {
		return nil
	}

	var tok tokenResponse
	status, body, err := c.postJSON(ctx, "/auth/login", "", loginRequest{
		Username: acc.name(),
		Password: acc.password,
	}, &tok)
	if err != nil {
		return err
	}

	if status == http.StatusUnauthorized || status == http.StatusNotFound {
		// Account doesn't exist yet — register it (register returns tokens). This is
		// how a persona added through the admin API gets an account, with no
		// intervention beyond creating the row.
		status, body, err = c.postJSON(ctx, "/auth/register", "", registerRequest{
			Username:    acc.name(),
			Email:       acc.name() + "@bot.local",
			DisplayName: acc.persona.displayName,
			Password:    acc.password,
		}, &tok)
		if err != nil {
			return err
		}
		if status == http.StatusConflict {
			// The login 401'd but the username is taken: the account exists and the
			// password is what's wrong. Reporting the register 409 here would point
			// the operator at registration when the fault is the credential.
			return fmt.Errorf("login %s: the account exists but the password was rejected — check BOT_PASSWORD", acc.name())
		}
		if status != http.StatusCreated && status != http.StatusOK {
			return fmt.Errorf("register %s: status %d: %s", acc.name(), status, truncate(string(body), 200))
		}
	} else if status != http.StatusOK {
		return fmt.Errorf("login %s: status %d: %s", acc.name(), status, truncate(string(body), 200))
	}

	if tok.AccessToken == "" {
		return fmt.Errorf("auth for %s returned no access token", acc.name())
	}
	acc.accessToken = tok.AccessToken
	acc.userID = tok.userID()
	acc.expiresAt = c.now().Add(time.Duration(tok.AccessExpiresIn)*time.Second - tokenSafetyMargin)
	return nil
}

// CreatePost publishes a public post as the given account, transparently
// re-authenticating once if the token was rejected.
func (c *apiClient) CreatePost(ctx context.Context, acc *botAccount, content string, tags []string) (string, error) {
	if err := c.EnsureLogin(ctx, acc); err != nil {
		return "", err
	}

	body := createPostRequest{Content: content, Visibility: "public", Tags: tags}

	var created createPostResponse
	status, respBody, err := c.postJSON(ctx, "/posts", acc.accessToken, body, &created)
	if err != nil {
		return "", err
	}

	if status == http.StatusUnauthorized {
		// Token revoked server-side (e.g. restart with new secret) — retry once.
		acc.accessToken = ""
		if err = c.EnsureLogin(ctx, acc); err != nil {
			return "", err
		}
		status, respBody, err = c.postJSON(ctx, "/posts", acc.accessToken, body, &created)
		if err != nil {
			return "", err
		}
	}

	if status != http.StatusCreated {
		return "", fmt.Errorf("create post as %s: status %d: %s", acc.name(), status, truncate(string(respBody), 200))
	}
	return created.ID, nil
}

// ─── Transport ───────────────────────────────────────────────────────────────

// postJSON sends a JSON POST and decodes a 2xx body into out (when non-nil).
func (c *apiClient) postJSON(ctx context.Context, path, token string, in, out any) (int, []byte, error) {
	return c.do(ctx, http.MethodPost, path, token, in, out)
}

// do performs one request and decodes a 2xx body into out (when non-nil).
// Non-2xx statuses are returned to the caller for flow decisions (the raw body
// comes along for error messages); only transport-level failures produce an error.
func (c *apiClient) do(ctx context.Context, method, path, token string, in, out any) (int, []byte, error) {
	if c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		// Safe to cancel on return: the response body is fully read below, so nothing
		// is still reading from the request's context by the time this fires.
		defer cancel()
	}

	var reader io.Reader
	if in != nil {
		payload, err := json.Marshal(in)
		if err != nil {
			return 0, nil, fmt.Errorf("marshal %s request: %w", path, err)
		}
		reader = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return 0, nil, fmt.Errorf("build %s request: %w", path, err)
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("call %s: %w", path, err)
	}
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("read %s response: %w", path, err)
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 && out != nil && len(body) > 0 {
		if err = json.Unmarshal(body, out); err != nil {
			return resp.StatusCode, body, fmt.Errorf("decode %s response: %w", path, err)
		}
	}
	return resp.StatusCode, body, nil
}
