package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSecurityHeaders(t *testing.T) {
	t.Parallel()

	handler := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil))

	for header, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "no-referrer",
		"Permissions-Policy":     "camera=(), geolocation=(), microphone=(), payment=(), usb=()",
	} {
		if got := w.Header().Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
	if got := w.Header().Get("Content-Security-Policy"); got != "" {
		t.Errorf("Content-Security-Policy = %q, want empty for Swagger compatibility", got)
	}
}

func TestAPIHeaders_LockDownResourceContexts(t *testing.T) {
	t.Parallel()

	handler := APIHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/posts", nil))

	want := "default-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"
	if got := w.Header().Get("Content-Security-Policy"); got != want {
		t.Fatalf("Content-Security-Policy = %q, want %q", got, want)
	}
}

func TestUploadedFileHeaders_SandboxesContent(t *testing.T) {
	t.Parallel()

	handler := UploadedFileHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("content"))
	}))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/static/file", nil))

	wantCSP := "default-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'; sandbox"
	if got := w.Header().Get("Content-Security-Policy"); got != wantCSP {
		t.Fatalf("Content-Security-Policy = %q", got)
	}
	for header, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "no-referrer",
		"Permissions-Policy":     "camera=(), geolocation=(), microphone=(), payment=(), usb=()",
	} {
		if got := w.Header().Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
}
