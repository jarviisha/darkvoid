package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	httputil "github.com/jarviisha/darkvoid/internal/http"
	"github.com/jarviisha/darkvoid/pkg/jwt"
	"github.com/jarviisha/darkvoid/pkg/logger"
)

func TestExtractToken_HeaderOnly(t *testing.T) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "/notifications/stream?token=leaked", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := extractToken(req); got != "" {
		t.Fatalf("query token accepted: %q", got)
	}
	req.Header.Set("Authorization", "Bearer header-token")
	if got := extractToken(req); got != "header-token" {
		t.Fatalf("header token = %q, want header-token", got)
	}
}

func TestAuthMiddleware_EnrichesAccessLog(t *testing.T) {
	t.Parallel()

	jwtService, err := jwt.NewService(jwt.Config{
		Secret:   []byte("test-secret-key-32-bytes-minimum!!"),
		Issuer:   "test",
		Audience: "test-api",
		Expiry:   time.Minute,
	})
	if err != nil {
		t.Fatalf("jwt.NewService() error = %v", err)
	}
	userID := uuid.New()
	token, err := jwtService.GenerateToken(userID.String())
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}

	tests := []struct {
		name string
		auth func(*jwt.Service) func(http.Handler) http.Handler
	}{
		{name: "required auth", auth: Auth},
		{name: "optional auth", auth: OptionalAuth},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var output bytes.Buffer
			log := logger.New(&logger.Config{Level: "info", Format: "json", Output: &output})
			handler := logger.HTTPMiddleware(log)(tt.auth(jwtService)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotUserID := httputil.GetUserID(r.Context())
				if gotUserID == nil || *gotUserID != userID {
					t.Errorf("context user ID = %v, want %s", gotUserID, userID)
				}
				w.WriteHeader(http.StatusNoContent)
			})))
			request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/private", nil)
			request.Header.Set("Authorization", "Bearer "+token)

			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != http.StatusNoContent {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
			}
			var entry map[string]any
			if err := json.Unmarshal(output.Bytes(), &entry); err != nil {
				t.Fatalf("decode access log: %v\n%s", err, output.String())
			}
			if got := entry["user_id"]; got != userID.String() {
				t.Fatalf("access log user_id = %v, want %s", got, userID)
			}
		})
	}
}
