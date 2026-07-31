package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func geminiBody(t *testing.T, post generatedPost) []byte {
	t.Helper()
	inner, err := json.Marshal(post)
	if err != nil {
		t.Fatalf("marshal inner post: %v", err)
	}
	body, err := json.Marshal(map[string]any{
		"candidates": []map[string]any{{
			"content": map[string]any{
				"parts": []map[string]string{{"text": string(inner)}},
			},
		}},
	})
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	return body
}

func TestParseGeneratedPost_Success(t *testing.T) {
	body := geminiBody(t, generatedPost{
		Content: "  Sáng nay cà phê ngon quá.  ",
		Tags:    []string{"#CaPhe", "hanoi", "hanoi", "có dấu!"},
	})

	post, err := parseGeneratedPost(body, defaultMaxTags)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if post.Content != "Sáng nay cà phê ngon quá." {
		t.Errorf("content not trimmed: %q", post.Content)
	}
	if want := []string{"caphe", "hanoi"}; !reflect.DeepEqual(post.Tags, want) {
		t.Errorf("tags = %v, want %v", post.Tags, want)
	}
}

func TestParseGeneratedPost_EmptyContent(t *testing.T) {
	if _, err := parseGeneratedPost(geminiBody(t, generatedPost{Content: "   "}), defaultMaxTags); err == nil {
		t.Fatal("expected error for empty content")
	}
}

func TestParseGeneratedPost_NoCandidates(t *testing.T) {
	if _, err := parseGeneratedPost([]byte(`{"candidates":[]}`), defaultMaxTags); err == nil {
		t.Fatal("expected error for missing candidates")
	}
}

func TestParseGeneratedPost_InvalidInnerJSON(t *testing.T) {
	body, err := json.Marshal(map[string]any{
		"candidates": []map[string]any{{
			"content": map[string]any{
				"parts": []map[string]string{{"text": "not json"}},
			},
		}},
	})
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	if _, err := parseGeneratedPost(body, defaultMaxTags); err == nil {
		t.Fatal("expected error for non-JSON candidate text")
	}
}

func TestSanitizeTags_CapsAndFilters(t *testing.T) {
	got := sanitizeTags([]string{"#Golang", "", "tiếng-việt", "ok_1", "extra", "more"}, defaultMaxTags)
	if want := []string{"golang", "ok_1", "extra"}; !reflect.DeepEqual(got, want) {
		t.Errorf("sanitizeTags = %v, want %v", got, want)
	}
}

// The cap is whatever the plan configured, not the built-in default: an operator
// lowering it must not keep getting three tags per post.
func TestSanitizeTags_HonoursConfiguredCap(t *testing.T) {
	got := sanitizeTags([]string{"a", "b", "c", "d"}, 1)
	if want := []string{"a"}; !reflect.DeepEqual(got, want) {
		t.Errorf("sanitizeTags = %v, want %v", got, want)
	}
}

// max_tags_per_post = 0 means "no tags". Falling through to the default here would
// publish tagged posts for an operator who explicitly turned tagging off.
func TestSanitizeTags_ZeroCapDropsEverything(t *testing.T) {
	if got := sanitizeTags([]string{"golang", "hanoi"}, 0); len(got) != 0 {
		t.Errorf("sanitizeTags = %v, want none", got)
	}
}

// The temperature and prompt on the wire have to be the ones the plan asked for —
// this is the whole point of moving them out of the binary.
func TestGeneratePost_SendsConfiguredPromptAndTemperature(t *testing.T) {
	var got generateContentRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode request: %v", err)
		}
		_, _ = w.Write(geminiBody(t, generatedPost{Content: "ok"}))
	}))
	defer srv.Close()

	g := &geminiClient{baseURL: srv.URL, apiKey: "k", http: srv.Client()}
	_, _, err := g.GeneratePost(context.Background(), []string{"m1"}, generationSettings{
		Prompt:      "prompt do người vận hành đặt",
		Temperature: 0.25,
		MaxTags:     2,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(got.Contents) != 1 || len(got.Contents[0].Parts) != 1 {
		t.Fatalf("request carried %+v", got.Contents)
	}
	if text := got.Contents[0].Parts[0].Text; text != "prompt do người vận hành đặt" {
		t.Errorf("prompt = %q, want the configured one", text)
	}
	if got.GenerationConfig.Temperature != 0.25 {
		t.Errorf("temperature = %v, want the configured 0.25", got.GenerationConfig.Temperature)
	}
}

// The timeout comes from the plan, so a hung Gemini must end the call rather than
// hold the tick open until the process is killed.
func TestGeneratePost_HonoursTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	defer srv.Close()
	defer close(release)

	g := &geminiClient{baseURL: srv.URL, apiKey: "k", http: srv.Client(), timeout: 50 * time.Millisecond}
	if _, _, err := g.GeneratePost(context.Background(), []string{"m1"}, testGen()); err == nil {
		t.Fatal("expected the call to time out")
	}
}

func TestGeneratePost_CallsEndpointWithKey(t *testing.T) {
	var gotPath, gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.Header.Get("x-goog-api-key")
		_, _ = w.Write(geminiBody(t, generatedPost{Content: "xin chào", Tags: []string{"test"}}))
	}))
	defer srv.Close()

	g := &geminiClient{baseURL: srv.URL, apiKey: "secret", http: srv.Client()}
	post, model, err := g.GeneratePost(context.Background(), []string{"gemini-2.5-flash"}, testGen())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if model != "gemini-2.5-flash" {
		t.Errorf("model = %q, want the one that answered", model)
	}
	if post.Content != "xin chào" {
		t.Errorf("content = %q", post.Content)
	}
	if gotPath != "/v1beta/models/gemini-2.5-flash:generateContent" {
		t.Errorf("path = %q", gotPath)
	}
	if gotKey != "secret" {
		t.Errorf("api key header = %q", gotKey)
	}
}

func TestGeneratePost_AllModelsRateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"quota"}`, http.StatusTooManyRequests)
	}))
	defer srv.Close()

	g := &geminiClient{baseURL: srv.URL, apiKey: "k", http: srv.Client()}
	if _, _, err := g.GeneratePost(context.Background(), []string{"m1", "m2"}, testGen()); err == nil {
		t.Fatal("expected error when every model is 429")
	}
}

// A 429 on the preferred model must roll over to the next model in the list.
func TestGeneratePost_RotatesPast429(t *testing.T) {
	var triedPaths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		triedPaths = append(triedPaths, r.URL.Path)
		if strings.Contains(r.URL.Path, "busy-model") {
			http.Error(w, `{"error":"quota"}`, http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write(geminiBody(t, generatedPost{Content: "đã xoay vòng", Tags: nil}))
	}))
	defer srv.Close()

	g := &geminiClient{baseURL: srv.URL, apiKey: "k", http: srv.Client()}
	post, model, err := g.GeneratePost(context.Background(), []string{"busy-model", "spare-model"}, testGen())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if model != "spare-model" {
		t.Errorf("model = %q, want the fallback that actually answered", model)
	}
	if post.Content != "đã xoay vòng" {
		t.Errorf("content = %q, want fallback model output", post.Content)
	}
	if len(triedPaths) != 2 {
		t.Fatalf("tried %d models, want 2 (busy then spare): %v", len(triedPaths), triedPaths)
	}
}

// A non-quota error (e.g. 400) must fail fast without burning the fallbacks.
func TestGeneratePost_RealErrorDoesNotRotate(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
	}))
	defer srv.Close()

	g := &geminiClient{baseURL: srv.URL, apiKey: "k", http: srv.Client()}
	if _, _, err := g.GeneratePost(context.Background(), []string{"m1", "m2"}, testGen()); err == nil {
		t.Fatal("expected error for 400 response")
	}
	if calls != 1 {
		t.Fatalf("made %d calls, want 1 (no rotation on a real error)", calls)
	}
}

func TestJittered_StaysWithinBounds(t *testing.T) {
	r := newTestRunner()
	base := 40 * time.Second
	for range 100 {
		d := r.jittered(base)
		if d < 30*time.Second || d > 50*time.Second {
			t.Fatalf("jittered(%v) = %v outside ±25%%", base, d)
		}
	}
}

func TestRemember_KeepsLastN(t *testing.T) {
	r := newTestRunner()
	r.recent = map[string][]string{}
	for i := range defaultRecentMemory + 3 {
		r.remember("bot_sky", strings.Repeat("x", i+1), defaultRecentMemory)
	}
	if got := len(r.recent["bot_sky"]); got != defaultRecentMemory {
		t.Errorf("recent len = %d, want %d", got, defaultRecentMemory)
	}
}

// Lowering recent_memory has to trim what is already held, or the surplus entries
// would keep going into the prompt until the process restarted — which is exactly
// the restart this change exists to remove.
func TestRemember_ShrinksToANewLowerLimit(t *testing.T) {
	r := newTestRunner()
	r.recent = map[string][]string{"bot_sky": {"a", "b", "c", "d", "e"}}

	r.remember("bot_sky", "f", 2)

	if want := []string{"e", "f"}; !reflect.DeepEqual(r.recent["bot_sky"], want) {
		t.Errorf("recent = %v, want the newest %v", r.recent["bot_sky"], want)
	}
}

// A limit of 0 turns the repetition guard off, which has to drop what was already
// remembered — otherwise "off" would still feed old posts into every prompt.
func TestRemember_ZeroLimitForgets(t *testing.T) {
	r := newTestRunner()
	r.recent = map[string][]string{"bot_sky": {"a", "b"}}

	r.remember("bot_sky", "c", 0)

	if got := r.recent["bot_sky"]; len(got) != 0 {
		t.Errorf("recent = %v, want nothing remembered", got)
	}
}

// Generated posts and API error bodies are routinely non-ASCII, so a byte-based cut
// would leave a broken rune in the middle of a log line.
func TestTruncate_IsRuneAware(t *testing.T) {
	const s = "Sáng nay cà phê ở góc phố quen, nắng vừa đủ"

	if got := truncate(s, 100); got != s {
		t.Errorf("truncate() = %q, want the input unchanged", got)
	}

	got := truncate(s, 12)
	if want := "Sáng nay cà " + "..."; got != want {
		t.Errorf("truncate() = %q, want %q", got, want)
	}
	if !utf8.ValidString(got) {
		t.Errorf("truncate() produced invalid UTF-8: %q", got)
	}
}
