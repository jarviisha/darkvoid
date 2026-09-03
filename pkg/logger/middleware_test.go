package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPMiddleware_AccessLogIncludesAuthenticatedUser(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	log := New(&Config{
		Level:  "info",
		Format: "json",
		Output: &output,
	})
	handler := HTTPMiddleware(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := WithUserID(r.Context(), "user-123")
		r = r.WithContext(ctx)
		if requestStateFromContext(r.Context()) == nil {
			t.Fatal("derived request lost shared access-log state")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/private", nil)

	handler.ServeHTTP(httptest.NewRecorder(), request)

	var entry map[string]any
	if err := json.Unmarshal(output.Bytes(), &entry); err != nil {
		t.Fatalf("decode access log: %v\n%s", err, output.String())
	}
	if got := entry["user_id"]; got != "user-123" {
		t.Fatalf("user_id = %v, want user-123", got)
	}
}

func TestHTTPMiddleware_AccessLogOmitsUserForAnonymousRequest(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	log := New(&Config{
		Level:  "info",
		Format: "json",
		Output: &output,
	})
	handler := HTTPMiddleware(log)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/public", nil)

	handler.ServeHTTP(httptest.NewRecorder(), request)

	var entry map[string]any
	if err := json.Unmarshal(output.Bytes(), &entry); err != nil {
		t.Fatalf("decode access log: %v\n%s", err, output.String())
	}
	if got, exists := entry["user_id"]; exists {
		t.Fatalf("user_id = %v, want field omitted", got)
	}
}
