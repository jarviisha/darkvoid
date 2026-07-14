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

func newTestRunner(api *apiClient) *runner {
	return &runner{
		api: api,
		rng: rand.New(rand.NewSource(1)), //nolint:gosec // deterministic test rng
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

	acc := &botAccount{persona: personas[0], password: "Bot@12345"}
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
		persona:     personas[0],
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

	acc := &botAccount{persona: personas[0], password: "Bot@12345"}
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

	acc := &botAccount{persona: personas[0], password: "Bot@12345"}
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

	acc := &botAccount{persona: personas[0], password: "Bot@12345"}
	_, err := newTestClient(srv).CreatePost(context.Background(), acc, "nội dung", []string{"bad!"})
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
	if want := "invalid tag"; !strings.Contains(err.Error(), want) {
		t.Errorf("error %q missing %q", err, want)
	}
}
