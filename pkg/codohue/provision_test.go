package codohue

import (
	"context"
	"encoding/json"
	"fmt"
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

func jsonResponse(t *testing.T, status int, payload map[string]any) *http.Response {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(string(body))),
	}
}

func TestProvisionNamespaceConfig_LogsInThenUpsertsNamespace(t *testing.T) {
	const namespace = "darkvoid_feed"
	const adminKey = "admin-secret"
	const sessionToken = "session-token"

	var loginBody map[string]string
	var gotUpsertPath string
	var gotSessionCookie string
	var gotPayload namespaceProvisionPayload

	originalClient := provisionHTTPClient
	t.Cleanup(func() { provisionHTTPClient = originalClient })
	provisionHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/sessions":
			if err := json.NewDecoder(r.Body).Decode(&loginBody); err != nil {
				t.Fatalf("decode login request: %v", err)
			}
			resp := jsonResponse(t, http.StatusCreated, map[string]any{
				"expires_at": time.Date(2026, 7, 23, 1, 0, 0, 0, time.UTC),
			})
			resp.Header.Add("Set-Cookie", "codohue_admin_session="+sessionToken+"; Path=/api; HttpOnly")
			return resp, nil

		case r.Method == http.MethodPut:
			gotUpsertPath = r.URL.Path
			if cookie, err := r.Cookie("codohue_admin_session"); err == nil {
				gotSessionCookie = cookie.Value
			}
			if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
				t.Fatalf("decode upsert request: %v", err)
			}
			return jsonResponse(t, http.StatusCreated, map[string]any{
				"namespace":  namespace,
				"updated_at": time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC),
				"api_key":    "namespace-key",
			}), nil

		default:
			return nil, fmt.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	})}

	result, err := ProvisionNamespaceConfig(context.Background(), NamespaceProvisionConfig{
		AdminBaseURL: "http://codohue-admin.test/",
		AdminKey:     adminKey,
		Namespace:    namespace,
		EmbeddingDim: 64,
	})
	if err != nil {
		t.Fatalf("ProvisionNamespaceConfig() error = %v", err)
	}

	if loginBody["api_key"] != adminKey {
		t.Fatalf("login api_key = %q, want admin key", loginBody["api_key"])
	}
	if gotUpsertPath != "/api/admin/v1/namespaces/"+namespace {
		t.Fatalf("path = %q, want admin namespace upsert endpoint", gotUpsertPath)
	}
	if gotSessionCookie != sessionToken {
		t.Fatalf("session cookie = %q, want %q", gotSessionCookie, sessionToken)
	}
	if gotPayload.DenseSource != "byoe" {
		t.Fatalf("dense_source = %q, want byoe", gotPayload.DenseSource)
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

func TestProvisionNamespaceConfig_CatalogMode(t *testing.T) {
	const namespace = "darkvoid_feed"

	var upsertRaw map[string]any
	var gotCatalogPath string
	var gotCatalogPayload catalogConfigPayload

	originalClient := provisionHTTPClient
	t.Cleanup(func() { provisionHTTPClient = originalClient })
	provisionHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/sessions":
			resp := jsonResponse(t, http.StatusCreated, map[string]any{
				"expires_at": time.Date(2026, 7, 23, 1, 0, 0, 0, time.UTC),
			})
			resp.Header.Add("Set-Cookie", "codohue_admin_session=session-token; Path=/api; HttpOnly")
			return resp, nil

		case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/catalog"):
			gotCatalogPath = r.URL.Path
			if err := json.NewDecoder(r.Body).Decode(&gotCatalogPayload); err != nil {
				t.Fatalf("decode catalog request: %v", err)
			}
			return jsonResponse(t, http.StatusOK, map[string]any{"namespace": namespace}), nil

		case r.Method == http.MethodPut:
			if err := json.NewDecoder(r.Body).Decode(&upsertRaw); err != nil {
				t.Fatalf("decode upsert request: %v", err)
			}
			return jsonResponse(t, http.StatusOK, map[string]any{
				"namespace":  namespace,
				"updated_at": time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC),
			}), nil

		default:
			return nil, fmt.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	})}

	_, err := ProvisionNamespaceConfig(context.Background(), NamespaceProvisionConfig{
		AdminBaseURL: "http://codohue-admin.test",
		AdminKey:     "admin-secret",
		Namespace:    namespace,
		EmbeddingDim: 64,
		DenseSource:  DenseSourceCatalog,
	})
	if err != nil {
		t.Fatalf("ProvisionNamespaceConfig() error = %v", err)
	}

	if _, present := upsertRaw["dense_source"]; present {
		t.Fatalf("upsert payload contains dense_source = %v, want omitted in catalog mode", upsertRaw["dense_source"])
	}
	if gotCatalogPath != "/api/admin/v1/namespaces/"+namespace+"/catalog" {
		t.Fatalf("catalog path = %q, want catalog config endpoint", gotCatalogPath)
	}
	if !gotCatalogPayload.Enabled {
		t.Fatal("catalog payload enabled = false, want true")
	}
	if gotCatalogPayload.StrategyID != catalogStrategyID || gotCatalogPayload.StrategyVersion != catalogStrategyVersion {
		t.Fatalf("catalog strategy = %s@%s, want %s@%s",
			gotCatalogPayload.StrategyID, gotCatalogPayload.StrategyVersion, catalogStrategyID, catalogStrategyVersion)
	}
	if dim, ok := gotCatalogPayload.Params["dim"].(float64); !ok || int(dim) != 64 {
		t.Fatalf("catalog params dim = %v, want 64", gotCatalogPayload.Params["dim"])
	}
}

func TestProvisionNamespaceConfig_RejectsInvalidDenseSource(t *testing.T) {
	_, err := ProvisionNamespaceConfig(context.Background(), NamespaceProvisionConfig{
		AdminBaseURL: "http://codohue-admin.test",
		AdminKey:     "admin-secret",
		Namespace:    "darkvoid_feed",
		EmbeddingDim: 64,
		DenseSource:  "item2vec",
	})
	if err == nil {
		t.Fatal("expected error for unsupported dense source")
	}
	if !strings.Contains(err.Error(), "dense source must be") {
		t.Fatalf("error = %v, want dense source validation", err)
	}
}

func TestProvisionNamespaceConfig_LoginFailure(t *testing.T) {
	originalClient := provisionHTTPClient
	t.Cleanup(func() { provisionHTTPClient = originalClient })
	provisionHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonResponse(t, http.StatusUnauthorized, map[string]any{
			"error": map[string]string{"code": "unauthorized", "message": "invalid api key"},
		}), nil
	})}

	_, err := ProvisionNamespaceConfig(context.Background(), NamespaceProvisionConfig{
		AdminBaseURL: "http://codohue-admin.test",
		AdminKey:     "wrong-key",
		Namespace:    "darkvoid_feed",
		EmbeddingDim: 64,
	})
	if err == nil {
		t.Fatal("expected error for failed login")
	}
	if !strings.Contains(err.Error(), "admin session login failed") {
		t.Fatalf("error = %v, want login failure", err)
	}
}

func TestProvisionNamespaceConfig_RequiresAdminKey(t *testing.T) {
	_, err := ProvisionNamespaceConfig(context.Background(), NamespaceProvisionConfig{
		AdminBaseURL: "http://codohue-admin.test",
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

func TestProvisionNamespaceConfig_RequiresAdminURL(t *testing.T) {
	_, err := ProvisionNamespaceConfig(context.Background(), NamespaceProvisionConfig{
		AdminKey:     "admin-secret",
		Namespace:    "darkvoid_feed",
		EmbeddingDim: 64,
	})
	if err == nil {
		t.Fatal("expected error for missing admin base URL")
	}
	if !strings.Contains(err.Error(), "admin base URL is required") {
		t.Fatalf("error = %v, want admin base URL requirement", err)
	}
}
