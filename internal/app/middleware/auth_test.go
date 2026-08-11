package middleware

import (
	"context"
	"net/http"
	"testing"
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
