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

func TestProvisionNamespaceConfig_SendsBearerTokenAndSuppliesTheNamespaceKey(t *testing.T) {
	const namespace = "darkvoid_feed"
	const adminToken = "admin-service-token"
	const namespaceKey = "our-namespace-key"

	var gotUpsertPath string
	var gotUpsertAuth string
	var sawSessionLogin bool
	var gotPayload namespaceProvisionPayload

	originalClient := provisionHTTPClient
	t.Cleanup(func() { provisionHTTPClient = originalClient })
	provisionHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.URL.Path == "/api/v1/auth/sessions":
			sawSessionLogin = true
			return jsonResponse(t, http.StatusForbidden, map[string]any{
				"error": map[string]string{"code": "forbidden", "message": "legacy admin auth disabled"},
			}), nil

		case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/catalog"):
			return jsonResponse(t, http.StatusOK, map[string]any{"namespace": namespace}), nil

		case r.Method == http.MethodPut:
			gotUpsertPath = r.URL.Path
			gotUpsertAuth = r.Header.Get("Authorization")
			if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
				t.Fatalf("decode upsert request: %v", err)
			}
			return jsonResponse(t, http.StatusCreated, map[string]any{
				"namespace":  namespace,
				"updated_at": time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC),
			}), nil

		default:
			return nil, fmt.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	})}

	result, err := ProvisionNamespaceConfig(context.Background(), NamespaceProvisionConfig{
		AdminBaseURL: "http://codohue-admin.test/",
		AdminToken:   adminToken,
		NamespaceKey: namespaceKey,
		Namespace:    namespace,
		EmbeddingDim: 64,
	})
	if err != nil {
		t.Fatalf("ProvisionNamespaceConfig() error = %v", err)
	}

	// The regression guard: Codohue answers 403 to the old login, so reaching for
	// it again would break provisioning without changing a single assertion below.
	if sawSessionLogin {
		t.Error("provisioning logged in for a session; the admin plane takes a bearer token")
	}
	if gotUpsertAuth != "Bearer "+adminToken {
		t.Errorf("Authorization = %q, want the admin service token as a bearer", gotUpsertAuth)
	}
	if gotPayload.ProvisionAPIKey != namespaceKey {
		t.Errorf("provision_api_key = %q, want the key we supplied — a minted one is not idempotent", gotPayload.ProvisionAPIKey)
	}
	if gotUpsertPath != "/api/admin/v1/namespaces/"+namespace {
		t.Errorf("path = %q, want admin namespace upsert endpoint", gotUpsertPath)
	}
	if gotPayload.EmbeddingDim != 64 {
		t.Errorf("embedding_dim = %d, want 64", gotPayload.EmbeddingDim)
	}
	if gotPayload.ActionWeights[string(ActionLike)] != 5 {
		t.Errorf("LIKE weight = %.1f, want 5", gotPayload.ActionWeights[string(ActionLike)])
	}
	if result.Namespace != namespace {
		t.Errorf("namespace = %q, want %q", result.Namespace, namespace)
	}
}

// The full payload is why this is not sdk/go/admin: that package's request type
// carries action_weights, alpha and dense_distance only, so these seven would
// silently fall back to server defaults.
func TestProvisionNamespaceConfig_SendsTheKnobsTheAdminSDKOmits(t *testing.T) {
	var raw map[string]any

	originalClient := provisionHTTPClient
	t.Cleanup(func() { provisionHTTPClient = originalClient })
	provisionHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/catalog") {
			return jsonResponse(t, http.StatusOK, map[string]any{"namespace": "darkvoid_feed"}), nil
		}
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			t.Fatalf("decode upsert request: %v", err)
		}
		return jsonResponse(t, http.StatusCreated, map[string]any{"namespace": "darkvoid_feed"}), nil
	})}

	if _, err := ProvisionNamespaceConfig(context.Background(), NamespaceProvisionConfig{
		AdminBaseURL: "http://codohue-admin.test",
		AdminToken:   "admin-service-token",
		NamespaceKey: "our-namespace-key",
		Namespace:    "darkvoid_feed",
		EmbeddingDim: 64,
	}); err != nil {
		t.Fatalf("ProvisionNamespaceConfig() error = %v", err)
	}

	for _, field := range []string{"lambda", "gamma", "max_results", "seen_items_days", "trending_window", "trending_ttl", "lambda_trending"} {
		if _, present := raw[field]; !present {
			t.Errorf("upsert payload omits %q, which the server would then default", field)
		}
	}
}

// Catalog auto-embedding is not optional: it is how vectors get produced at all,
// so every provisioning run must enable it, and the namespace upsert must leave
// dense_source out — that route rejects "catalog".
func TestProvisionNamespaceConfig_EnablesCatalogAutoEmbedding(t *testing.T) {
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
		AdminToken:   "admin-service-token",
		NamespaceKey: "our-namespace-key",
		Namespace:    namespace,
		EmbeddingDim: 64,
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

// A rejected service token must surface as a provisioning failure, not as a
// half-provisioned namespace.
func TestProvisionNamespaceConfig_RejectedToken(t *testing.T) {
	originalClient := provisionHTTPClient
	t.Cleanup(func() { provisionHTTPClient = originalClient })
	provisionHTTPClient = &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return jsonResponse(t, http.StatusUnauthorized, map[string]any{
			"error": map[string]string{"code": "unauthorized", "message": "invalid or missing bearer token"},
		}), nil
	})}

	_, err := ProvisionNamespaceConfig(context.Background(), NamespaceProvisionConfig{
		AdminBaseURL: "http://codohue-admin.test",
		AdminToken:   "wrong-token",
		NamespaceKey: "our-namespace-key",
		Namespace:    "darkvoid_feed",
		EmbeddingDim: 64,
	})
	if err == nil {
		t.Fatal("expected error for a rejected token")
	}
	if !strings.Contains(err.Error(), "namespace config upsert failed") {
		t.Fatalf("error = %v, want the upsert failure", err)
	}
}

// The create path for the catalog route is the one no deployment has run since
// the baseline, and a 201 there used to abort provisioning after the namespace
// upsert had already succeeded — leaving dense_source unset, so nothing embeds.
func TestProvisionNamespaceConfig_AcceptsCreatedOnTheCatalogRoute(t *testing.T) {
	originalClient := provisionHTTPClient
	t.Cleanup(func() { provisionHTTPClient = originalClient })
	provisionHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/catalog") {
			return jsonResponse(t, http.StatusCreated, map[string]any{"namespace": "darkvoid_feed"}), nil
		}
		return jsonResponse(t, http.StatusCreated, map[string]any{"namespace": "darkvoid_feed"}), nil
	})}

	if _, err := ProvisionNamespaceConfig(context.Background(), NamespaceProvisionConfig{
		AdminBaseURL: "http://codohue-admin.test",
		AdminToken:   "admin-service-token",
		NamespaceKey: "our-namespace-key",
		Namespace:    "darkvoid_feed",
		EmbeddingDim: 64,
	}); err != nil {
		t.Fatalf("201 on the catalog route must be success, got error = %v", err)
	}
}

// provision_api_key is sent unconditionally, so an empty key would ask the server
// to fix an immutable credential to "".
func TestProvisionNamespaceConfig_RequiresNamespaceKey(t *testing.T) {
	_, err := ProvisionNamespaceConfig(context.Background(), NamespaceProvisionConfig{
		AdminBaseURL: "http://codohue-admin.test",
		AdminToken:   "admin-service-token",
		Namespace:    "darkvoid_feed",
		EmbeddingDim: 64,
	})
	if err == nil {
		t.Fatal("expected error for missing namespace key")
	}
	if !strings.Contains(err.Error(), "namespace key is required") {
		t.Fatalf("error = %v, want namespace key requirement", err)
	}
}

func TestProvisionNamespaceConfig_RequiresAdminToken(t *testing.T) {
	_, err := ProvisionNamespaceConfig(context.Background(), NamespaceProvisionConfig{
		AdminBaseURL: "http://codohue-admin.test",
		Namespace:    "darkvoid_feed",
		EmbeddingDim: 64,
	})
	if err == nil {
		t.Fatal("expected error for missing admin service token")
	}
	if !strings.Contains(err.Error(), "admin service token is required") {
		t.Fatalf("error = %v, want admin service token requirement", err)
	}
}

func TestProvisionNamespaceConfig_RequiresAdminURL(t *testing.T) {
	_, err := ProvisionNamespaceConfig(context.Background(), NamespaceProvisionConfig{
		AdminToken:   "admin-service-token",
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
