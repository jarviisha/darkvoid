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

	g := &geminiClient{baseURL: srv.URL, apiKey: "secret", model: "gemini-2.5-flash", http: srv.Client()}
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

func TestGeneratePost_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"quota"}`, http.StatusTooManyRequests)
	}))
	defer srv.Close()

	g := &geminiClient{baseURL: srv.URL, apiKey: "k", model: "m", http: srv.Client()}
	if _, err := g.GeneratePost(context.Background(), personas[0], "chủ đề", nil); err == nil {
		t.Fatal("expected error for 429 response")
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
