package main

import (
	"context"
	"encoding/json"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fixturePassword is a placeholder, not a credential: every server in these tests
// is a stub that never checks it. Named rather than inlined so a literal never sits
// next to a password-shaped field, which both gosec and secret scanners flag.
const fixturePassword = "fixture-value"

func newTestRunner(api *apiClient) *runner {
	return &runner{
		api: api,
		rng: rand.New(rand.NewSource(1)), //nolint:gosec // deterministic test rng
	}
}

// testPersona stands in for an entry the plan would hand out. Personas are no
// longer compiled into the binary, so tests build their own.
func testPersona() persona {
	return persona{
		id:          "9f1c3d64-0000-4000-8000-000000000001",
		username:    "bot_sky",
		displayName: "Sky Vũ",
		style:       "giọng trẻ trung, hài hước",
	}
}

func newTestClient(srv *httptest.Server) *apiClient {
	return &apiClient{
		baseURL: srv.URL,
		http:    srv.Client(),
		now:     time.Now,
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func TestEnsureLogin_RegistersWhenLoginFails(t *testing.T) {
	var registered bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/login":
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid username or password"})
		case "/auth/register":
			registered = true
			writeJSON(w, http.StatusCreated, tokenResponse{AccessToken: "tok-1", AccessExpiresIn: 900})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	acc := &botAccount{persona: testPersona(), password: fixturePassword}
	if err := newTestClient(srv).EnsureLogin(context.Background(), acc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !registered {
		t.Error("expected fallback to /auth/register")
	}
	if acc.accessToken != "tok-1" {
		t.Errorf("accessToken = %q", acc.accessToken)
	}
}

func TestEnsureLogin_SkipsWhenTokenValid(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request %s", r.URL.Path)
	}))
	defer srv.Close()

	acc := &botAccount{
		persona:     testPersona(),
		accessToken: "still-good",
		expiresAt:   time.Now().Add(time.Hour),
	}
	if err := newTestClient(srv).EnsureLogin(context.Background(), acc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreatePost_Success(t *testing.T) {
	var gotAuth string
	var gotReq createPostRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/login":
			writeJSON(w, http.StatusOK, tokenResponse{AccessToken: "tok", AccessExpiresIn: 900})
		case "/posts":
			gotAuth = r.Header.Get("Authorization")
			_ = json.NewDecoder(r.Body).Decode(&gotReq)
			writeJSON(w, http.StatusCreated, createPostResponse{ID: "post-1"})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	acc := &botAccount{persona: testPersona(), password: fixturePassword}
	id, err := newTestClient(srv).CreatePost(context.Background(), acc, "nội dung", []string{"tag1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "post-1" {
		t.Errorf("post id = %q", id)
	}
	if gotAuth != "Bearer tok" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotReq.Visibility != "public" || gotReq.Content != "nội dung" {
		t.Errorf("request = %+v", gotReq)
	}
}

func TestCreatePost_RetriesOnceOn401(t *testing.T) {
	logins, postCalls := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/login":
			logins++
			writeJSON(w, http.StatusOK, tokenResponse{AccessToken: "tok", AccessExpiresIn: 900})
		case "/posts":
			postCalls++
			if postCalls == 1 {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "expired"})
				return
			}
			writeJSON(w, http.StatusCreated, createPostResponse{ID: "post-2"})
		}
	}))
	defer srv.Close()

	acc := &botAccount{persona: testPersona(), password: fixturePassword}
	id, err := newTestClient(srv).CreatePost(context.Background(), acc, "nội dung", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "post-2" {
		t.Errorf("post id = %q", id)
	}
	if logins != 2 || postCalls != 2 {
		t.Errorf("logins = %d, postCalls = %d; want 2 and 2", logins, postCalls)
	}
}

func TestCreatePost_ErrorIncludesBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/login":
			writeJSON(w, http.StatusOK, tokenResponse{AccessToken: "tok", AccessExpiresIn: 900})
		case "/posts":
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid tag: bad!"})
		}
	}))
	defer srv.Close()

	acc := &botAccount{persona: testPersona(), password: fixturePassword}
	_, err := newTestClient(srv).CreatePost(context.Background(), acc, "nội dung", []string{"bad!"})
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
	if want := "invalid tag"; !strings.Contains(err.Error(), want) {
		t.Errorf("error %q missing %q", err, want)
	}
}

// ─── Runner session ──────────────────────────────────────────────────────────

// The runner must never be auto-registered: a fresh account would not carry the bot
// role, and the resulting 403 on every plan fetch looks like a server fault rather
// than a missing grant.
func TestLoginRunner_NeverRegisters(t *testing.T) {
	var registered bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/login":
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid username or password"})
		case "/auth/register":
			registered = true
			writeJSON(w, http.StatusCreated, tokenResponse{AccessToken: "tok", AccessExpiresIn: 900})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	err := newTestClient(srv).LoginRunner(context.Background(), "bot_runner", "pw")
	if err == nil {
		t.Fatal("expected a failure when the runner account does not exist")
	}
	if registered {
		t.Error("the runner must not be registered on the fly — it would lack the bot role")
	}
	if !strings.Contains(err.Error(), "bot role") {
		t.Errorf("error %q should point at the missing role", err)
	}
}

func TestLoginRunner_CapturesTokenAndUserID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"access_token": "runner-tok", "access_expires_in": 900,
			"user": map[string]string{"id": "00000000-0000-4000-8000-00000000ffff"},
		})
	}))
	defer srv.Close()

	c := newTestClient(srv)
	if err := c.LoginRunner(context.Background(), "bot_runner", "pw"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.runner.accessToken != "runner-tok" {
		t.Errorf("accessToken = %q", c.runner.accessToken)
	}
	// Login nests the id under "user" while register puts it at the top level; both
	// have to work or identity linking silently stops.
	if c.runner.userID != "00000000-0000-4000-8000-00000000ffff" {
		t.Errorf("userID = %q, want the id nested under \"user\"", c.runner.userID)
	}
}

func TestFetchPlan_SendsTheRunnerToken(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/login":
			writeJSON(w, http.StatusOK, tokenResponse{AccessToken: "runner-tok", AccessExpiresIn: 900})
		case "/bot/plan":
			gotAuth = r.Header.Get("Authorization")
			writeJSON(w, http.StatusOK, plan{
				PostIntervalSeconds: 120,
				Models:              []string{"gemini-2.5-flash"},
				Bots:                []planBot{{ID: "bot-1", Username: "bot_sky", RunRequested: true}},
				Topics:              []string{"cà phê sáng"},
			})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	c := newTestClient(srv)
	if err := c.LoginRunner(context.Background(), "bot_runner", "pw"); err != nil {
		t.Fatalf("login: %v", err)
	}
	p, err := c.FetchPlan(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotAuth != "Bearer runner-tok" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if p.interval() != 2*time.Minute {
		t.Errorf("interval = %v, want 2m", p.interval())
	}
	if len(p.Bots) != 1 || !p.Bots[0].RunRequested {
		t.Errorf("bots = %+v, want the pending request preserved", p.Bots)
	}
	if len(p.Topics) != 1 {
		t.Errorf("topics = %v", p.Topics)
	}
}

// The API restarting with a new signing secret invalidates the token without its
// expiry having passed, so a 401 has to trigger one silent re-login.
func TestRunnerCall_RetriesOnceOn401(t *testing.T) {
	logins, planCalls := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/login":
			logins++
			writeJSON(w, http.StatusOK, tokenResponse{AccessToken: "runner-tok", AccessExpiresIn: 900})
		case "/bot/plan":
			planCalls++
			if planCalls == 1 {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "expired"})
				return
			}
			writeJSON(w, http.StatusOK, plan{PostIntervalSeconds: 30})
		}
	}))
	defer srv.Close()

	c := newTestClient(srv)
	if err := c.LoginRunner(context.Background(), "bot_runner", "pw"); err != nil {
		t.Fatalf("login: %v", err)
	}
	if _, err := c.FetchPlan(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if logins != 2 || planCalls != 2 {
		t.Errorf("logins = %d, planCalls = %d; want 2 and 2", logins, planCalls)
	}
}

func TestReportRun_AcceptsNoContent(t *testing.T) {
	var got runReport
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/login":
			writeJSON(w, http.StatusOK, tokenResponse{AccessToken: "runner-tok", AccessExpiresIn: 900})
		case "/bot/runs":
			_ = json.NewDecoder(r.Body).Decode(&got)
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer srv.Close()

	c := newTestClient(srv)
	if err := c.LoginRunner(context.Background(), "bot_runner", "pw"); err != nil {
		t.Fatalf("login: %v", err)
	}
	postID := "post-1"
	err := c.ReportRun(context.Background(), runReport{
		BotID: "bot-1", Status: "success", PostID: &postID, HonoredRunRequest: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.BotID != "bot-1" || got.PostID == nil || !got.HonoredRunRequest {
		t.Errorf("report reached the server as %+v", got)
	}
}

func TestReportRun_SurfacesRejection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/login":
			writeJSON(w, http.StatusOK, tokenResponse{AccessToken: "runner-tok", AccessExpiresIn: 900})
		case "/bot/runs":
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "inconsistent outcome"})
		}
	}))
	defer srv.Close()

	c := newTestClient(srv)
	if err := c.LoginRunner(context.Background(), "bot_runner", "pw"); err != nil {
		t.Fatalf("login: %v", err)
	}
	err := c.ReportRun(context.Background(), runReport{BotID: "bot-1", Status: "success"})
	if err == nil {
		t.Fatal("expected the rejection to surface")
	}
	if !strings.Contains(err.Error(), "inconsistent") {
		t.Errorf("error %q should include the server's reason", err)
	}
}

func TestLinkIdentity_AddressesThePersona(t *testing.T) {
	var gotPath string
	var gotReq linkIdentityRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/login":
			writeJSON(w, http.StatusOK, tokenResponse{AccessToken: "runner-tok", AccessExpiresIn: 900})
		default:
			gotPath = r.URL.Path
			_ = json.NewDecoder(r.Body).Decode(&gotReq)
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer srv.Close()

	c := newTestClient(srv)
	if err := c.LoginRunner(context.Background(), "bot_runner", "pw"); err != nil {
		t.Fatalf("login: %v", err)
	}
	if err := c.LinkIdentity(context.Background(), "bot-1", "user-9"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/bot/bot-1/identity" {
		t.Errorf("path = %q", gotPath)
	}
	if gotReq.UserID != "user-9" {
		t.Errorf("user id = %q", gotReq.UserID)
	}
}

// Register reports the account id at the top level rather than nested, so a persona
// registered on first use still gets linked.
func TestEnsureLogin_CapturesUserIDFromRegister(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/login":
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "no such user"})
		case "/auth/register":
			writeJSON(w, http.StatusCreated, map[string]any{
				"user_id":      "00000000-0000-4000-8000-0000000000bb",
				"access_token": "tok", "access_expires_in": 900,
			})
		}
	}))
	defer srv.Close()

	acc := &botAccount{persona: testPersona(), password: fixturePassword}
	if err := newTestClient(srv).EnsureLogin(context.Background(), acc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if acc.userID != "00000000-0000-4000-8000-0000000000bb" {
		t.Errorf("userID = %q, want the top-level register id", acc.userID)
	}
}
