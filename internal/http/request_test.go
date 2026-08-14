package httputil

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		body       string
		wantName   string
		wantReason string
	}{
		{
			name:     "valid object",
			body:     "{\"name\":\"darkvoid\"}\n",
			wantName: "darkvoid",
		},
		{
			name:       "empty body",
			body:       " \n\t",
			wantReason: "request body must not be empty",
		},
		{
			name:       "malformed JSON",
			body:       "{\"name\":",
			wantReason: "request body contains malformed JSON",
		},
		{
			name:       "wrong field type",
			body:       "{\"name\":7}",
			wantReason: `request body contains an invalid value for field "name"`,
		},
		{
			name:       "unknown field",
			body:       "{\"name\":\"darkvoid\",\"admin\":true}",
			wantReason: `request body contains unknown field "admin"`,
		},
		{
			name:       "multiple JSON values",
			body:       "{\"name\":\"first\"} {\"name\":\"second\"}",
			wantReason: "request body must contain a single JSON value",
		},
		{
			name:       "body too large",
			body:       "{\"name\":\"" + strings.Repeat("a", 1<<20) + "\"}",
			wantReason: "request body must not exceed 1048576 bytes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			request := httptest.NewRequestWithContext(context.Background(), "POST", "/", strings.NewReader(tt.body))
			response := httptest.NewRecorder()
			var destination struct {
				Name string `json:"name"`
			}

			err := DecodeJSON(response, request, &destination)
			if tt.wantReason == "" {
				if err != nil {
					t.Fatalf("DecodeJSON() error = %v", err)
				}
				if destination.Name != tt.wantName {
					t.Fatalf("DecodeJSON() name = %q, want %q", destination.Name, tt.wantName)
				}
				return
			}

			if err == nil {
				t.Fatal("DecodeJSON() error = nil")
			}
			if err.Code != "BAD_REQUEST" {
				t.Errorf("DecodeJSON() code = %q, want BAD_REQUEST", err.Code)
			}
			if err.Message != "invalid request body" {
				t.Errorf("DecodeJSON() message = %q, want invalid request body", err.Message)
			}
			if reason, _ := err.Details["reason"].(string); reason != tt.wantReason {
				t.Errorf("DecodeJSON() reason = %q, want %q", reason, tt.wantReason)
			}
		})
	}
}
