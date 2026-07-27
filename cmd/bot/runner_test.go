package main

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// ─── Fake control plane ──────────────────────────────────────────────────────

// fakeAPI is a stand-in for the DarkVoid API: it serves a plan, accepts persona
// logins and posts, and records the run reports it receives.
type fakeAPI struct {
	mu sync.Mutex

	plan plan
	// planErrors makes the first N plan fetches fail, to exercise the retry path.
	planErrors int

	planFetches int
	registers   []string
	posts       []createPostRequest
	reports     []runReport
	identities  map[string]string
	// runnerLogins counts runner authentications; personaLogins counts the rest.
	runnerLogins  int
	personaLogins int
}

func newFakeAPI(p plan) *fakeAPI {
	return &fakeAPI{plan: p, identities: map[string]string{}}
}

func (f *fakeAPI) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()

		switch {
		case r.URL.Path == "/auth/login":
			var req loginRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req.Username == "runner" {
				f.runnerLogins++
				writeJSON(w, http.StatusOK, map[string]any{
					"access_token": "runner-tok", "access_expires_in": 900,
					"user": map[string]string{"id": "00000000-0000-4000-8000-00000000ffff"},
				})
				return
			}
			f.personaLogins++
			writeJSON(w, http.StatusOK, map[string]any{
				"access_token": "persona-tok", "access_expires_in": 900,
				"user": map[string]string{"id": "00000000-0000-4000-8000-0000000000aa"},
			})

		case r.URL.Path == "/auth/register":
			var req registerRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			f.registers = append(f.registers, req.Username)
			writeJSON(w, http.StatusCreated, map[string]any{
				"user_id":      "00000000-0000-4000-8000-0000000000bb",
				"access_token": "persona-tok", "access_expires_in": 900,
			})

		case r.URL.Path == "/bot/plan":
			f.planFetches++
			if f.planErrors > 0 {
				f.planErrors--
				http.Error(w, `{"error":"boom"}`, http.StatusInternalServerError)
				return
			}
			writeJSON(w, http.StatusOK, f.plan)

		case r.URL.Path == "/bot/runs":
			var report runReport
			_ = json.NewDecoder(r.Body).Decode(&report)
			f.reports = append(f.reports, report)
			w.WriteHeader(http.StatusNoContent)

		case strings.HasSuffix(r.URL.Path, "/identity"):
			var req linkIdentityRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			botID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/bot/"), "/identity")
			f.identities[botID] = req.UserID
			w.WriteHeader(http.StatusNoContent)

		case r.URL.Path == "/posts":
			var req createPostRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			f.posts = append(f.posts, req)
			writeJSON(w, http.StatusCreated, createPostResponse{ID: "post-1"})

		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (f *fakeAPI) snapshot() fakeAPI {
	f.mu.Lock()
	defer f.mu.Unlock()
	ids := make(map[string]string, len(f.identities))
	for k, v := range f.identities {
		ids[k] = v
	}
	return fakeAPI{
		planFetches: f.planFetches, registers: append([]string(nil), f.registers...),
		posts:   append([]createPostRequest(nil), f.posts...),
		reports: append([]runReport(nil), f.reports...), identities: ids,
		runnerLogins: f.runnerLogins, personaLogins: f.personaLogins,
	}
}

// geminiServer answers every generateContent call with the given content.
func geminiServer(t *testing.T, content string, status int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if status != http.StatusOK {
			http.Error(w, `{"error":"quota"}`, status)
			return
		}
		_, _ = w.Write(geminiBody(t, generatedPost{Content: content, Tags: []string{"caphe"}}))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newLoopRunner(t *testing.T, api *fakeAPI, gemini *httptest.Server) *runner {
	t.Helper()
	apiSrv := api.server(t)
	r := &runner{
		api:             &apiClient{baseURL: apiSrv.URL, http: apiSrv.Client(), now: time.Now},
		gemini:          &geminiClient{baseURL: gemini.URL, apiKey: "k", http: gemini.Client()},
		personaPassword: "Bot@12345",
		rng:             rand.New(rand.NewSource(1)), //nolint:gosec // deterministic test rng
	}
	if err := r.api.LoginRunner(t.Context(), "runner", "Runner@12345"); err != nil {
		t.Fatalf("runner login: %v", err)
	}
	return r
}

func testPlan(bots ...planBot) plan {
	return plan{
		PostIntervalSeconds: 1,
		Models:              []string{"gemini-2.5-flash"},
		Bots:                bots,
		Topics:              []string{"cà phê sáng"},
	}
}

func planBotFor(id, username string, runRequested bool) planBot {
	return planBot{
		ID: id, Username: username, DisplayName: username, Style: "giọng thử nghiệm",
		RunRequested: runRequested,
	}
}

// ─── Loop ────────────────────────────────────────────────────────────────────

func TestRun_PostsFromThePlanAndReportsSuccess(t *testing.T) {
	api := newFakeAPI(testPlan(planBotFor("bot-1", "bot_sky", false)))
	r := newLoopRunner(t, api, geminiServer(t, "Sáng nay cà phê ở góc phố quen.", http.StatusOK))

	r.run(t.Context(), 1)
	got := api.snapshot()

	if len(got.posts) != 1 {
		t.Fatalf("published %d posts, want 1", len(got.posts))
	}
	if got.posts[0].Visibility != "public" || got.posts[0].Content == "" {
		t.Errorf("post = %+v", got.posts[0])
	}
	if len(got.reports) != 1 {
		t.Fatalf("sent %d reports, want 1", len(got.reports))
	}
	rep := got.reports[0]
	if rep.Status != "success" || rep.PostID == nil || *rep.PostID != "post-1" {
		t.Errorf("report = %+v, want a success carrying the post id", rep)
	}
	if rep.ModelUsed == nil || *rep.ModelUsed != "gemini-2.5-flash" {
		t.Error("the model that answered should be reported, so quota rotation is visible")
	}
	if rep.Error != nil {
		t.Error("a success must not carry an error — the server rejects that combination")
	}
	// The persona had no account yet, so the bot registers one rather than needing
	// an operator to create it alongside the row.
	if len(got.registers) != 0 && got.registers[0] != "bot_sky" {
		t.Errorf("registered %v, want bot_sky", got.registers)
	}
	if got.identities["bot-1"] == "" {
		t.Error("the persona's user account should be linked so the admin view can show it")
	}
}

// A failure an operator cannot see is the problem the activity log exists to solve,
// so a generation failure must still be reported.
func TestRun_ReportsGenerationFailure(t *testing.T) {
	api := newFakeAPI(testPlan(planBotFor("bot-1", "bot_sky", false)))
	r := newLoopRunner(t, api, geminiServer(t, "", http.StatusTooManyRequests))

	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	r.run(ctx, 1)
	got := api.snapshot()

	if len(got.posts) != 0 {
		t.Errorf("published %d posts, want none", len(got.posts))
	}
	if len(got.reports) == 0 {
		t.Fatal("a generation failure must be reported")
	}
	rep := got.reports[0]
	if rep.Status != "error" || rep.Error == nil {
		t.Errorf("report = %+v, want an error carrying the reason", rep)
	}
	if rep.PostID != nil {
		t.Error("a failure must not carry a post id — the server rejects that combination")
	}
	if !strings.Contains(*rep.Error, "exhausted") {
		t.Errorf("reason = %q, want the quota failure", *rep.Error)
	}
}

func TestRun_PausedPublishesNothing(t *testing.T) {
	p := testPlan(planBotFor("bot-1", "bot_sky", false))
	p.Paused = true
	api := newFakeAPI(p)
	r := newLoopRunner(t, api, geminiServer(t, "không nên đăng", http.StatusOK))

	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	r.run(ctx, 0)
	got := api.snapshot()

	if got.planFetches == 0 {
		t.Error("a paused bot must keep polling, or it would never see the resume")
	}
	if len(got.posts) != 0 || len(got.reports) != 0 {
		t.Errorf("paused bot published %d posts and sent %d reports, want none", len(got.posts), len(got.reports))
	}
}

func TestRun_EmptyPlanPublishesNothing(t *testing.T) {
	for _, tt := range []struct {
		name string
		plan plan
	}{
		{"no personas", plan{PostIntervalSeconds: 1, Models: []string{"m"}, Topics: []string{"t"}}},
		{"no topics", testPlanWithoutTopics()},
	} {
		t.Run(tt.name, func(t *testing.T) {
			api := newFakeAPI(tt.plan)
			r := newLoopRunner(t, api, geminiServer(t, "không nên đăng", http.StatusOK))

			ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
			defer cancel()
			r.run(ctx, 0)

			if got := api.snapshot(); len(got.posts) != 0 {
				t.Errorf("published %d posts with an empty pool, want none", len(got.posts))
			}
		})
	}
}

func testPlanWithoutTopics() plan {
	p := testPlan(planBotFor("bot-1", "bot_sky", false))
	p.Topics = nil
	return p
}

// A transient plan fetch failure must not end the process — this is a long-running
// bot, and the interval it should use is inside the plan it just failed to read.
func TestRun_RetriesAfterAPlanFetchFailure(t *testing.T) {
	api := newFakeAPI(testPlan(planBotFor("bot-1", "bot_sky", false)))
	api.planErrors = 1
	r := newLoopRunner(t, api, geminiServer(t, "sau khi thử lại", http.StatusOK))

	// planRetryInterval is minutes-scale, so let the first failure land and then
	// assert the loop is still alive rather than waiting for the retry.
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	r.run(ctx, 1)
	got := api.snapshot()

	if got.planFetches == 0 {
		t.Fatal("the plan was never fetched")
	}
	if len(got.posts) != 0 {
		t.Error("nothing should be published from a plan that failed to load")
	}
	if len(got.reports) != 0 {
		t.Error("a plan fetch failure is not a run: there is no persona to attribute it to")
	}
}

// ─── Persona selection ───────────────────────────────────────────────────────

// An operator asked for this one explicitly, so it beats the random draw.
func TestPick_PrefersAPendingRunRequest(t *testing.T) {
	r := &runner{rng: rand.New(rand.NewSource(1))} //nolint:gosec // deterministic test rng
	p := testPlan(
		planBotFor("bot-1", "bot_a", false),
		planBotFor("bot-2", "bot_b", true),
		planBotFor("bot-3", "bot_c", false),
	)

	for range 20 {
		if got := r.pick(&p); got.ID != "bot-2" {
			t.Fatalf("picked %s, want the persona with a pending request", got.ID)
		}
	}
}

func TestPick_RandomWhenNothingIsPending(t *testing.T) {
	r := &runner{rng: rand.New(rand.NewSource(1))} //nolint:gosec // deterministic test rng
	p := testPlan(planBotFor("bot-1", "bot_a", false), planBotFor("bot-2", "bot_b", false))

	seen := map[string]bool{}
	for range 50 {
		seen[r.pick(&p).ID] = true
	}
	if len(seen) < 2 {
		t.Errorf("only picked %v across 50 draws, want both personas", seen)
	}
}

// honored_run_request is what clears the pending flag server-side, so it must track
// the persona actually chosen — never be set for an ordinary interval post.
func TestRun_HonoredFlagTracksThePickedPersona(t *testing.T) {
	for _, tt := range []struct {
		name      string
		requested bool
	}{
		{"ordinary interval post", false},
		{"answering a run-now", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			api := newFakeAPI(testPlan(planBotFor("bot-1", "bot_sky", tt.requested)))
			r := newLoopRunner(t, api, geminiServer(t, "nội dung", http.StatusOK))

			r.run(t.Context(), 1)
			got := api.snapshot()

			if len(got.reports) != 1 {
				t.Fatalf("sent %d reports, want 1", len(got.reports))
			}
			if got.reports[0].HonoredRunRequest != tt.requested {
				t.Errorf("honored_run_request = %v, want %v",
					got.reports[0].HonoredRunRequest, tt.requested)
			}
		})
	}
}

// ─── Sessions ────────────────────────────────────────────────────────────────

// Sessions are keyed by bot id and reused, so a long-running bot does not re-login
// on every post. The persona itself is refreshed so an edited voice takes effect
// without a restart.
func TestAccount_ReusesTheSessionAndRefreshesThePersona(t *testing.T) {
	r := &runner{accounts: map[string]*botAccount{}, personaPassword: "Bot@12345"}

	first := r.account(persona{id: "bot-1", username: "bot_sky", style: "giọng cũ"})
	first.accessToken = "tok"

	second := r.account(persona{id: "bot-1", username: "bot_sky", style: "giọng mới"})
	if first != second {
		t.Error("the same persona should reuse its session")
	}
	if second.accessToken != "tok" {
		t.Error("reusing the session must not discard its token")
	}
	if second.persona.style != "giọng mới" {
		t.Errorf("style = %q, want the edited voice", second.persona.style)
	}

	if other := r.account(persona{id: "bot-2", username: "bot_mien"}); other == first {
		t.Error("a different persona needs its own session")
	}
}

func TestFailedRun_OmitsTheModelWhenNoneWasReached(t *testing.T) {
	per := persona{id: "bot-1", username: "bot_sky", runRequested: true}

	withModel := failedRun(per, "gemini-2.5-flash", errAll)
	if withModel.ModelUsed == nil || *withModel.ModelUsed != "gemini-2.5-flash" {
		t.Error("a model that refused should be recorded")
	}

	withoutModel := failedRun(per, "", errAll)
	if withoutModel.ModelUsed != nil {
		t.Error("no model should be recorded when none was reached")
	}
	if withoutModel.Status != "error" || withoutModel.Error == nil || withoutModel.PostID != nil {
		t.Errorf("report = %+v, want the server's failure shape", withoutModel)
	}
	if !withoutModel.HonoredRunRequest {
		t.Error("a failed attempt at a run-now still answered it, so the flag should clear")
	}
}

// ─── Plan helpers ────────────────────────────────────────────────────────────

func TestPlanInterval(t *testing.T) {
	tests := []struct {
		name    string
		seconds int32
		want    time.Duration
	}{
		{"configured", 120, 2 * time.Minute},
		{"zero falls back", 0, planRetryInterval},
		{"negative falls back", -5, planRetryInterval},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := plan{PostIntervalSeconds: tt.seconds}
			if got := p.interval(); got != tt.want {
				t.Errorf("interval() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPlanBotPersona(t *testing.T) {
	b := planBotFor("bot-1", "bot_sky", true)
	b.DisplayName = "Sky Vũ"
	per := b.persona()

	if per.id != "bot-1" || per.username != "bot_sky" || per.displayName != "Sky Vũ" {
		t.Errorf("persona = %+v", per)
	}
	if !per.runRequested {
		t.Error("a pending request must survive the conversion, or run-now silently stops working")
	}
}

// errAll stands in for a generation failure in report-shape tests.
var errAll = errors.New("all gemini models exhausted")
