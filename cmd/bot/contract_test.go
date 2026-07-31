package main

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	botdto "github.com/jarviisha/darkvoid/internal/feature/bot/dto"
	botentity "github.com/jarviisha/darkvoid/internal/feature/bot/entity"
	postdto "github.com/jarviisha/darkvoid/internal/feature/post/dto"
	userdto "github.com/jarviisha/darkvoid/internal/feature/user/dto"
)

// The bot talks to the API over HTTP, so nothing makes the compiler check that its
// request and response structs still match the server's DTOs. These tests do it by
// round-tripping real DTO values through the bot's own types: a renamed or retyped
// JSON field breaks `make test` here instead of breaking the bot at runtime on
// whichever host it happens to be running.
//
// The bot deliberately does not import these DTOs in production code — it is an
// ordinary HTTP client and should stay one — which is why the coupling is pinned in
// a test rather than removed.

// roundTrip marshals from and unmarshals into, failing the test on either error.
func roundTrip(t *testing.T, from, into any) {
	t.Helper()
	raw, err := json.Marshal(from)
	if err != nil {
		t.Fatalf("marshal %T: %v", from, err)
	}
	if err := json.Unmarshal(raw, into); err != nil {
		t.Fatalf("unmarshal into %T: %v", into, err)
	}
}

// ─── Agent plane ─────────────────────────────────────────────────────────────

func TestContract_PlanResponse(t *testing.T) {
	server := botdto.PlanResponse{
		Paused:              true,
		PostIntervalSeconds: 120,
		Models:              []string{"gemini-2.5-flash", "gemini-2.0-flash"},
		Topics:              []string{"cà phê sáng", "chuyện đi làm"},
		Bots: []botdto.PlanBot{{
			ID:           "9f1c3d64-0000-4000-8000-000000000001",
			Username:     "bot_sky",
			DisplayName:  "Sky Vũ",
			Style:        "giọng trẻ trung",
			RunRequested: true,
		}},
		PromptTemplate:       "viết như {{.DisplayName}}",
		Temperature:          0.25,
		MaxTagsPerPost:       2,
		RecentMemory:         7,
		APITimeoutSeconds:    11,
		GeminiTimeoutSeconds: 45,
	}

	var got plan
	roundTrip(t, server, &got)

	if got.Paused != server.Paused {
		t.Error("paused did not survive: the bot would publish while paused")
	}
	if got.PostIntervalSeconds != server.PostIntervalSeconds {
		t.Errorf("post_interval_seconds = %d, want %d", got.PostIntervalSeconds, server.PostIntervalSeconds)
	}
	if len(got.Models) != len(server.Models) || got.Models[0] != server.Models[0] {
		t.Errorf("models = %v, want %v — an empty chain stalls generation", got.Models, server.Models)
	}
	if len(got.Topics) != len(server.Topics) || got.Topics[0] != server.Topics[0] {
		t.Errorf("topics = %v, want %v", got.Topics, server.Topics)
	}
	if len(got.Bots) != 1 {
		t.Fatalf("bots = %+v, want one persona", got.Bots)
	}

	b := got.Bots[0]
	if b.ID != server.Bots[0].ID {
		t.Error("bot id did not survive: run reports could not be attributed")
	}
	if b.Username != server.Bots[0].Username || b.DisplayName != server.Bots[0].DisplayName {
		t.Errorf("persona identity = %+v", b)
	}
	if b.Style != server.Bots[0].Style {
		t.Error("style did not survive: every persona would write in the same voice")
	}
	if !b.RunRequested {
		t.Error("run_requested did not survive: run-now would silently never fire")
	}

	if got.PromptTemplate != server.PromptTemplate {
		t.Errorf("prompt_template = %q — every persona would fall back to the built-in voice", got.PromptTemplate)
	}
	if got.temperature() != server.Temperature {
		t.Errorf("temperature = %v, want %v", got.temperature(), server.Temperature)
	}
	if got.maxTags() != int(server.MaxTagsPerPost) {
		t.Errorf("max_tags_per_post = %d, want %d", got.maxTags(), server.MaxTagsPerPost)
	}
	if got.recentMemory() != int(server.RecentMemory) {
		t.Errorf("recent_memory = %d, want %d", got.recentMemory(), server.RecentMemory)
	}
	if got.apiTimeout() != 11*time.Second {
		t.Errorf("api_timeout_seconds = %v, want 11s", got.apiTimeout())
	}
	if got.geminiTimeout() != 45*time.Second {
		t.Errorf("gemini_timeout_seconds = %v, want 45s", got.geminiTimeout())
	}
}

// A plan from an API predating the generation knobs omits them entirely. Each has to
// read as "not configured" rather than as the zero value, or an upgrade of the bot
// ahead of the server would silently turn off tagging and the repetition guard and
// pin temperature to 0.
func TestContract_PlanResponse_AbsentGenerationKnobsFallBackToDefaults(t *testing.T) {
	var got plan
	if err := json.Unmarshal([]byte(`{"paused":false,"post_interval_seconds":30}`), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.temperature() != defaultTemperature {
		t.Errorf("temperature = %v, want the built-in default %v", got.temperature(), defaultTemperature)
	}
	if got.maxTags() != defaultMaxTags {
		t.Errorf("max_tags_per_post = %d, want the built-in default %d", got.maxTags(), defaultMaxTags)
	}
	if got.recentMemory() != defaultRecentMemory {
		t.Errorf("recent_memory = %d, want the built-in default %d", got.recentMemory(), defaultRecentMemory)
	}
	if got.apiTimeout() != defaultAPITimeout || got.geminiTimeout() != defaultGeminiTimeout {
		t.Errorf("timeouts = %v/%v, want the built-in defaults", got.apiTimeout(), got.geminiTimeout())
	}
	if got.PromptTemplate != "" {
		t.Errorf("prompt_template = %q, want empty so the built-in prompt is used", got.PromptTemplate)
	}
}

// A deliberate zero is not the same as an absent field: an operator who sets
// temperature to 0, tags to 0 or the repetition guard to 0 means it.
func TestContract_PlanResponse_ExplicitZerosAreHonoured(t *testing.T) {
	var got plan
	raw := `{"temperature":0,"max_tags_per_post":0,"recent_memory":0}`
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.temperature() != 0 {
		t.Errorf("temperature = %v, want the configured 0", got.temperature())
	}
	if got.maxTags() != 0 {
		t.Errorf("max_tags_per_post = %d, want the configured 0", got.maxTags())
	}
	if got.recentMemory() != 0 {
		t.Errorf("recent_memory = %d, want the configured 0", got.recentMemory())
	}
}

// The bot renders operator-written templates against its own promptData; the admin
// API validates them against entity.PromptData. Nothing at compile time connects the
// two, so a field renamed on one side would let a template pass validation and then
// fail to render on the bot host — for every persona, until somebody noticed.
func TestContract_PromptDataMatchesTheServersShape(t *testing.T) {
	fields := func(v any) map[string]string {
		typ := reflect.TypeOf(v)
		out := make(map[string]string, typ.NumField())
		for i := range typ.NumField() {
			f := typ.Field(i)
			out[f.Name] = f.Type.String()
		}
		return out
	}

	bot := fields(promptData{})
	server := fields(botentity.PromptData{})

	if !reflect.DeepEqual(bot, server) {
		t.Errorf("prompt template data shapes differ:\n bot    = %v\n server = %v", bot, server)
	}
}

// The server rejects a template it cannot render. If it would reject the bot's own
// default, an operator could never save an edited copy of it.
func TestContract_ServerAcceptsTheBotsDefaultPromptTemplate(t *testing.T) {
	if err := botentity.ValidatePromptTemplate(defaultPromptTemplate); err != nil {
		t.Fatalf("the server would reject the bot's built-in prompt: %v", err)
	}
}

func TestContract_ReportRunRequest(t *testing.T) {
	postID := "9f1c3d64-0000-4000-8000-0000000000aa"
	model := "gemini-2.5-flash"
	reason := "all gemini models exhausted"

	for _, tt := range []struct {
		name string
		sent runReport
	}{
		{
			name: "success",
			sent: runReport{
				BotID: "bot-1", Status: "success", PostID: &postID,
				ModelUsed: &model, HonoredRunRequest: true,
			},
		},
		{
			name: "failure",
			sent: runReport{
				BotID: "bot-1", Status: "error", Error: &reason, ModelUsed: &model,
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var server botdto.ReportRunRequest
			roundTrip(t, tt.sent, &server)

			if server.BotID != tt.sent.BotID || server.Status != tt.sent.Status {
				t.Errorf("server read %+v", server)
			}
			if (server.PostID == nil) != (tt.sent.PostID == nil) {
				t.Error("post_id presence changed — the server's outcome CHECK would reject it")
			}
			if (server.Error == nil) != (tt.sent.Error == nil) {
				t.Error("error presence changed — the server's outcome CHECK would reject it")
			}
			if server.ModelUsed == nil || *server.ModelUsed != model {
				t.Error("model_used did not survive: quota rotation would be invisible")
			}
			if server.HonoredRunRequest != tt.sent.HonoredRunRequest {
				t.Error("honored_run_request did not survive: the pending flag would clear wrongly")
			}
		})
	}
}

// An omitted honored_run_request must decode to false, or an ordinary interval post
// would clear a run-now the bot never acted on.
func TestContract_HonoredRunRequestDefaultsToFalse(t *testing.T) {
	var server botdto.ReportRunRequest
	if err := json.Unmarshal([]byte(`{"bot_id":"bot-1","status":"error","error":"x"}`), &server); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if server.HonoredRunRequest {
		t.Error("honored_run_request should default to false")
	}
}

func TestContract_LinkIdentityRequest(t *testing.T) {
	var server botdto.LinkIdentityRequest
	roundTrip(t, linkIdentityRequest{UserID: "9f1c3d64-0000-4000-8000-0000000000bb"}, &server)

	if server.UserID != "9f1c3d64-0000-4000-8000-0000000000bb" {
		t.Errorf("user_id = %q", server.UserID)
	}
}

// ─── Auth ────────────────────────────────────────────────────────────────────

// Login nests the account id under "user" while register reports it at the top
// level. The bot decodes both into one struct, so this pins the asymmetry rather
// than trusting it.
func TestContract_LoginResponse(t *testing.T) {
	server := userdto.LoginResponse{
		AccessToken:     "tok",
		AccessExpiresIn: 900,
		User:            userdto.UserResponse{ID: "9f1c3d64-0000-4000-8000-00000000ffff"},
	}

	var got tokenResponse
	roundTrip(t, server, &got)

	if got.AccessToken != server.AccessToken {
		t.Errorf("access_token = %q", got.AccessToken)
	}
	if got.AccessExpiresIn != server.AccessExpiresIn {
		t.Error("access_expires_in did not survive: the bot would not know when to re-login")
	}
	if got.userID() != server.User.ID {
		t.Errorf("userID() = %q, want the id nested under \"user\"", got.userID())
	}
}

func TestContract_RegisterResponse(t *testing.T) {
	server := userdto.RegisterResponse{
		UserID:          "9f1c3d64-0000-4000-8000-0000000000bb",
		AccessToken:     "tok",
		AccessExpiresIn: 900,
	}

	var got tokenResponse
	roundTrip(t, server, &got)

	if got.AccessToken != server.AccessToken || got.AccessExpiresIn != server.AccessExpiresIn {
		t.Errorf("token = %+v", got)
	}
	if got.userID() != server.UserID {
		t.Errorf("userID() = %q, want the top-level user_id", got.userID())
	}
}

func TestContract_LoginRequest(t *testing.T) {
	var server userdto.LoginRequest
	roundTrip(t, loginRequest{Username: "bot_runner", Password: "pw"}, &server)

	if server.Username != "bot_runner" || server.Password != "pw" {
		t.Errorf("server read %+v", server)
	}
}

func TestContract_RegisterRequest(t *testing.T) {
	sent := registerRequest{
		Username:    "bot_sky",
		Email:       "bot_sky@bot.local",
		DisplayName: "Sky Vũ",
		Password:    fixturePassword,
	}

	var server userdto.RegisterRequest
	roundTrip(t, sent, &server)

	if server.Username != sent.Username || server.Email != sent.Email {
		t.Errorf("server read %+v", server)
	}
	if server.DisplayName != sent.DisplayName || server.Password != sent.Password {
		t.Errorf("server read %+v", server)
	}
}

// ─── Posting ─────────────────────────────────────────────────────────────────

func TestContract_CreatePostRequest(t *testing.T) {
	sent := createPostRequest{
		Content:    "Sáng nay cà phê ở góc phố quen.",
		Visibility: "public",
		Tags:       []string{"caphe", "hanoi"},
	}

	var server postdto.CreatePostRequest
	roundTrip(t, sent, &server)

	if server.Content != sent.Content {
		t.Errorf("content = %q", server.Content)
	}
	if server.Visibility != "public" {
		t.Errorf("visibility = %q — a wrong value fails the posts CHECK constraint", server.Visibility)
	}
	if len(server.Tags) != 2 || server.Tags[0] != "caphe" {
		t.Errorf("tags = %v", server.Tags)
	}
}

func TestContract_CreatePostResponse(t *testing.T) {
	server := postdto.PostResponse{ID: "9f1c3d64-0000-4000-8000-0000000000cc"}

	var got createPostResponse
	roundTrip(t, server, &got)

	// Without the id there is nothing to report, and the run would look like a
	// success that produced nothing — which the server rejects outright.
	if got.ID != server.ID {
		t.Errorf("id = %q, want %q", got.ID, server.ID)
	}
}
