package codohue

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxProvisionErrorBodyBytes = 1 << 20

// Dense-source modes selectable via CODOHUE_DENSE_SOURCE.
const (
	// DenseSourceBYOE — darkvoid computes TF-IDF vectors locally and pushes
	// them through the BYOE embedding endpoints.
	DenseSourceBYOE = "byoe"
	// DenseSourceCatalog — darkvoid publishes raw post content to Codohue's
	// catalog pipeline; the server embeds it asynchronously.
	DenseSourceCatalog = "catalog"
)

// catalogStrategyID/Version pin Codohue's built-in deterministic hashing
// n-grams embedder. Its dim param must equal the namespace embedding_dim
// (valid dims: 64, 128, 256, 512).
const (
	catalogStrategyID      = "internal-hashing-ngrams"
	catalogStrategyVersion = "v1"
)

var provisionHTTPClient = http.DefaultClient

// NamespaceProvisionConfig contains the admin-plane configuration sent to Codohue.
// AdminBaseURL points at the Codohue admin server (cmd/admin, default port 2002) —
// a separate binary from the data-plane API the runtime SDK talks to.
// DenseSource selects who produces object vectors: DenseSourceBYOE (default
// when empty) or DenseSourceCatalog.
type NamespaceProvisionConfig struct {
	AdminBaseURL string
	AdminKey     string
	Namespace    string
	EmbeddingDim int
	DenseSource  string
}

// NamespaceProvisionResult contains the relevant response fields from Codohue.
type NamespaceProvisionResult struct {
	Namespace string    `json:"namespace"`
	UpdatedAt time.Time `json:"updated_at"`
	APIKey    string    `json:"api_key,omitempty"`
}

type namespaceProvisionPayload struct {
	ActionWeights map[string]float64 `json:"action_weights"`
	Lambda        float64            `json:"lambda"`
	Gamma         float64            `json:"gamma"`
	MaxResults    int                `json:"max_results"`
	SeenItemsDays int                `json:"seen_items_days"`
	Alpha         float64            `json:"alpha"`
	// DenseSource is omitted in catalog mode: "catalog" is rejected by the
	// namespace upsert route (it must be set via the catalog endpoint), and
	// an omitted field leaves the current value untouched (PATCH semantics).
	DenseSource    string  `json:"dense_source,omitempty"`
	EmbeddingDim   int     `json:"embedding_dim"`
	DenseDistance  string  `json:"dense_distance"`
	TrendingWindow int     `json:"trending_window"`
	TrendingTTL    int     `json:"trending_ttl"`
	LambdaTrending float64 `json:"lambda_trending"`
}

// catalogConfigPayload is the body for PUT /api/admin/v1/namespaces/{ns}/catalog.
type catalogConfigPayload struct {
	Enabled         bool           `json:"enabled"`
	StrategyID      string         `json:"strategy_id"`
	StrategyVersion string         `json:"strategy_version"`
	Params          map[string]any `json:"params"`
}

// ProvisionNamespaceConfig upserts Darkvoid's Codohue namespace config through
// Codohue's admin plane. The admin API is session-authenticated: the admin key
// is exchanged for a session cookie (POST /api/v1/auth/sessions), which then
// authorizes the namespace upsert (PUT /api/admin/v1/namespaces/{ns}). The
// official runtime SDK intentionally does not wrap these operator-facing routes.
func ProvisionNamespaceConfig(ctx context.Context, cfg NamespaceProvisionConfig) (*NamespaceProvisionResult, error) {
	if cfg.AdminBaseURL == "" {
		return nil, fmt.Errorf("codohue: admin base URL is required")
	}
	if cfg.AdminKey == "" {
		return nil, fmt.Errorf("codohue: admin key is required")
	}
	if cfg.Namespace == "" {
		return nil, fmt.Errorf("codohue: namespace is required")
	}
	if cfg.EmbeddingDim <= 0 {
		return nil, fmt.Errorf("codohue: embedding dimension must be positive")
	}
	denseSource := cfg.DenseSource
	if denseSource == "" {
		denseSource = DenseSourceBYOE
	}
	if denseSource != DenseSourceBYOE && denseSource != DenseSourceCatalog {
		return nil, fmt.Errorf("codohue: dense source must be %q or %q, got %q", DenseSourceBYOE, DenseSourceCatalog, cfg.DenseSource)
	}

	base := strings.TrimRight(cfg.AdminBaseURL, "/")

	session, err := createAdminSession(ctx, base, cfg.AdminKey)
	if err != nil {
		return nil, err
	}

	payload := defaultNamespaceProvisionPayload(cfg.EmbeddingDim)
	if denseSource == DenseSourceCatalog {
		payload.DenseSource = "" // omitted — set server-side by the catalog endpoint below
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("codohue: marshal namespace config: %w", err)
	}

	endpoint := base + "/api/admin/v1/namespaces/" + url.PathEscape(cfg.Namespace)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("codohue: build namespace config request: %w", err)
	}
	req.AddCookie(session)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := provisionHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("codohue: send namespace config request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// 200 (update) and 201 (create) are both success.
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxProvisionErrorBodyBytes))
		return nil, fmt.Errorf("codohue: namespace config upsert failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var result NamespaceProvisionResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("codohue: decode namespace config response: %w", err)
	}
	if result.Namespace != cfg.Namespace {
		return nil, fmt.Errorf("codohue: namespace config response namespace %q does not match %q", result.Namespace, cfg.Namespace)
	}

	if denseSource == DenseSourceCatalog {
		if err := enableCatalogAutoEmbedding(ctx, base, session, cfg.Namespace, cfg.EmbeddingDim); err != nil {
			return nil, err
		}
	}

	return &result, nil
}

// enableCatalogAutoEmbedding turns on Codohue's catalog auto-embedding for the
// namespace. Server-side this flips dense_source to "catalog" and validates
// that the strategy's dim matches the namespace embedding_dim.
func enableCatalogAutoEmbedding(ctx context.Context, base string, session *http.Cookie, namespace string, embeddingDim int) error {
	payload := catalogConfigPayload{
		Enabled:         true,
		StrategyID:      catalogStrategyID,
		StrategyVersion: catalogStrategyVersion,
		Params:          map[string]any{"dim": embeddingDim},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("codohue: marshal catalog config: %w", err)
	}

	endpoint := base + "/api/admin/v1/namespaces/" + url.PathEscape(namespace) + "/catalog"
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("codohue: build catalog config request: %w", err)
	}
	req.AddCookie(session)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := provisionHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("codohue: send catalog config request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxProvisionErrorBodyBytes))
		return fmt.Errorf("codohue: enable catalog auto-embedding failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

// createAdminSession exchanges the admin key for a session cookie via the
// admin plane's login endpoint.
func createAdminSession(ctx context.Context, base, adminKey string) (*http.Cookie, error) {
	body, err := json.Marshal(map[string]string{"api_key": adminKey})
	if err != nil {
		return nil, fmt.Errorf("codohue: marshal session request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/v1/auth/sessions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("codohue: build session request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := provisionHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("codohue: send session request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxProvisionErrorBodyBytes))
		return nil, fmt.Errorf("codohue: admin session login failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	for _, cookie := range resp.Cookies() {
		if cookie.Name == "codohue_admin_session" {
			return cookie, nil
		}
	}
	return nil, fmt.Errorf("codohue: admin session login response did not set a session cookie")
}

func defaultNamespaceProvisionPayload(embeddingDim int) namespaceProvisionPayload {
	return namespaceProvisionPayload{
		ActionWeights: map[string]float64{
			string(ActionView):    1,
			string(ActionLike):    5,
			string(ActionComment): 8,
			string(ActionShare):   10,
			string(ActionSkip):    -2,
		},
		Lambda:         0.01,
		Gamma:          0.5,
		MaxResults:     20,
		SeenItemsDays:  30,
		Alpha:          0.7,
		DenseSource:    "byoe",
		EmbeddingDim:   embeddingDim,
		DenseDistance:  "cosine",
		TrendingWindow: 24,
		TrendingTTL:    600,
		LambdaTrending: 0.1,
	}
}
