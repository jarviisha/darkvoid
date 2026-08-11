package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSecurityHeaders(t *testing.T) {
	handler := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil))

	for header, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "no-referrer",
	} {
		if got := w.Header().Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
}

func TestUploadedFileHeaders_SandboxesContent(t *testing.T) {
	handler := UploadedFileHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("content"))
	}))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/static/file", nil))

	if got := w.Header().Get("Content-Security-Policy"); got != "default-src 'none'; sandbox" {
		t.Fatalf("Content-Security-Policy = %q", got)
	}
	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q", got)
	}
}
