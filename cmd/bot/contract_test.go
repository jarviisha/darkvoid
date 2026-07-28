package main

import (
	"encoding/json"
	"testing"

	botdto "github.com/jarviisha/darkvoid/internal/feature/bot/dto"
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
