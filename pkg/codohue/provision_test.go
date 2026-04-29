package codohue

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestProvisionNamespaceConfig_SendsAdminConfig(t *testing.T) {
	const namespace = "darkvoid_feed"
	const adminKey = "admin-secret"

	var gotPath string
	var gotAuth string
	var gotPayload namespaceProvisionPayload

	originalClient := provisionHTTPClient
	t.Cleanup(func() { provisionHTTPClient = originalClient })
	provisionHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		if r.Method != http.MethodPut {
			t.Fatalf("method = %s, want PUT", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		body, err := json.Marshal(map[string]any{
			"namespace":  namespace,
			"updated_at": time.Date(2026, 4, 29, 0, 0, 0, 0, time.UTC),
			"api_key":    "namespace-key",
		})
		if err != nil {
			t.Fatalf("marshal response: %v", err)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(string(body))),
		}, nil
	})}

	result, err := ProvisionNamespaceConfig(context.Background(), NamespaceProvisionConfig{
		BaseURL:      "http://codohue.test/",
		AdminKey:     adminKey,
		Namespace:    namespace,
		EmbeddingDim: 64,
	})
	if err != nil {
		t.Fatalf("ProvisionNamespaceConfig() error = %v", err)
	}

	if gotPath != "/v1/config/namespaces/"+namespace {
		t.Fatalf("path = %q, want namespace config endpoint", gotPath)
	}
	if gotAuth != "Bearer "+adminKey {
		t.Fatalf("authorization header = %q, want bearer admin key", gotAuth)
	}
	if gotPayload.DenseStrategy != "byoe" {
		t.Fatalf("dense_strategy = %q, want byoe", gotPayload.DenseStrategy)
	}
	if gotPayload.EmbeddingDim != 64 {
		t.Fatalf("embedding_dim = %d, want 64", gotPayload.EmbeddingDim)
	}
	if gotPayload.ActionWeights[string(ActionLike)] != 5 {
		t.Fatalf("LIKE weight = %.1f, want 5", gotPayload.ActionWeights[string(ActionLike)])
	}
	if result.APIKey != "namespace-key" {
		t.Fatalf("api_key = %q, want namespace-key", result.APIKey)
	}
}

func TestProvisionNamespaceConfig_RequiresAdminKey(t *testing.T) {
	_, err := ProvisionNamespaceConfig(context.Background(), NamespaceProvisionConfig{
		BaseURL:      "http://codohue.test",
		Namespace:    "darkvoid_feed",
		EmbeddingDim: 64,
	})
	if err == nil {
		t.Fatal("expected error for missing admin key")
	}
	if !strings.Contains(err.Error(), "admin key is required") {
		t.Fatalf("error = %v, want admin key requirement", err)
	}
}
