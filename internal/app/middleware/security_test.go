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

// The upload directory holds more than uploads: the local storage provider
// writes a health probe into it to prove it is writable. Media keys never begin
// with a dot, so refusing dot-prefixed segments costs nothing and keeps anything
// the file server is merely sitting on top of unreachable.
func TestHiddenFileGuard(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		path   string
		served bool
	}{
		{name: "media key", path: "/static/media/9f8b.jpg", served: true},
		{name: "avatar key", path: "/static/avatars/9f8b.png", served: true},
		{name: "storage health probe", path: "/static/.storage-health-4021", served: false},
		{name: "hidden directory", path: "/static/.git/config", served: false},
		{name: "hidden file in a nested directory", path: "/static/media/.env", served: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			reached := false
			handler := HiddenFileGuard(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				reached = true
				w.WriteHeader(http.StatusOK)
			}))
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), http.MethodGet, tt.path, nil))

			if reached != tt.served {
				t.Fatalf("handler reached = %v, want %v", reached, tt.served)
			}
			if !tt.served && w.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404", w.Code)
			}
		})
	}
}

// A percent-encoded dot decodes before routing, so the guard has to see the
// decoded path or the probe is reachable under an escaped name.
func TestHiddenFileGuard_EncodedDot(t *testing.T) {
	t.Parallel()

	handler := HiddenFileGuard(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/static/%2Estorage-health-4021", nil))

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}
