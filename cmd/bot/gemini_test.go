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

	post, err := parseGeneratedPost(body)
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
	if _, err := parseGeneratedPost(geminiBody(t, generatedPost{Content: "   "})); err == nil {
		t.Fatal("expected error for empty content")
	}
}

func TestParseGeneratedPost_NoCandidates(t *testing.T) {
	if _, err := parseGeneratedPost([]byte(`{"candidates":[]}`)); err == nil {
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
	if _, err := parseGeneratedPost(body); err == nil {
		t.Fatal("expected error for non-JSON candidate text")
	}
}

func TestSanitizeTags_CapsAndFilters(t *testing.T) {
	got := sanitizeTags([]string{"#Golang", "", "tiếng-việt", "ok_1", "extra", "more"})
	if want := []string{"golang", "ok_1", "extra"}; !reflect.DeepEqual(got, want) {
		t.Errorf("sanitizeTags = %v, want %v", got, want)
	}
}

func TestBuildPrompt_IncludesPersonaTopicAndRecent(t *testing.T) {
	p := personas[0]
	prompt := buildPrompt(p, "chuyện đi làm", []string{"bài cũ số 1"})

	for _, want := range []string{p.displayName, p.style, "chuyện đi làm", "bài cũ số 1"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
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

	g := &geminiClient{baseURL: srv.URL, apiKey: "secret", models: []string{"gemini-2.5-flash"}, http: srv.Client()}
	post, err := g.GeneratePost(context.Background(), personas[0], "chủ đề", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
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

	g := &geminiClient{baseURL: srv.URL, apiKey: "k", models: []string{"m1", "m2"}, http: srv.Client()}
	if _, err := g.GeneratePost(context.Background(), personas[0], "chủ đề", nil); err == nil {
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

	g := &geminiClient{baseURL: srv.URL, apiKey: "k", models: []string{"busy-model", "spare-model"}, http: srv.Client()}
	post, err := g.GeneratePost(context.Background(), personas[0], "chủ đề", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
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

	g := &geminiClient{baseURL: srv.URL, apiKey: "k", models: []string{"m1", "m2"}, http: srv.Client()}
	if _, err := g.GeneratePost(context.Background(), personas[0], "chủ đề", nil); err == nil {
		t.Fatal("expected error for 400 response")
	}
	if calls != 1 {
		t.Fatalf("made %d calls, want 1 (no rotation on a real error)", calls)
	}
}

func TestJittered_StaysWithinBounds(t *testing.T) {
	r := newTestRunner(nil)
	base := 40 * time.Second
	for range 100 {
		d := r.jittered(base)
		if d < 30*time.Second || d > 50*time.Second {
			t.Fatalf("jittered(%v) = %v outside ±25%%", base, d)
		}
	}
}

func TestRemember_KeepsLastN(t *testing.T) {
	r := newTestRunner(nil)
	r.recent = map[string][]string{}
	for i := range recentMemory + 3 {
		r.remember("bot_sky", strings.Repeat("x", i+1))
	}
	if got := len(r.recent["bot_sky"]); got != recentMemory {
		t.Errorf("recent len = %d, want %d", got, recentMemory)
	}
}
