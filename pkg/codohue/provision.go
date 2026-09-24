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
)

const maxProvisionErrorBodyBytes = 1 << 20

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
//
// There is no dense-source choice: Codohue produces every object vector from the
// content darkvoid ingests. EmbeddingDim is still ours to declare because it must
// match the catalog strategy's dim, which the server validates.
type NamespaceProvisionConfig struct {
	AdminBaseURL string
	// AdminToken is an admin-plane service token, sent as a bearer. Codohue
	// v0.12.0 stopped accepting the global admin key it replaced: that key now
	// authenticates nothing unless the server is started with
	// CODOHUE_LEGACY_ADMIN_AUTH, which its own release notes mark as migration
	// only.
	AdminToken string
	// NamespaceKey is the key this deployment will use for runtime calls. We
	// supply it rather than reading one back: provisioning used to mint a key
	// and return it, so a retried call produced a second key and whichever
	// response landed last won. Sending ours makes the call idempotent.
	NamespaceKey string
	Namespace    string
	EmbeddingDim int
}

// NamespaceProvisionResult contains the relevant response fields from Codohue.
// The response's api_key is deliberately not read: the caller supplied the key.
type NamespaceProvisionResult struct {
	Namespace string `json:"namespace"`
}

type namespaceProvisionPayload struct {
	// ProvisionAPIKey is the namespace key we want this namespace to carry. Always
	// sent: validateCodohue refuses to boot without one, and provisioning runs on
	// every start, so there is no path here with an empty key. Resending it on an
	// update is not a rotation — the server treats the value as immutable, which
	// is what makes a retried call safe.
	ProvisionAPIKey string             `json:"provision_api_key"`
	ActionWeights   map[string]float64 `json:"action_weights"`
	Lambda          float64            `json:"lambda"`
	Gamma           float64            `json:"gamma"`
	MaxResults      int                `json:"max_results"`
	SeenItemsDays   int                `json:"seen_items_days"`
	Alpha           float64            `json:"alpha"`
	// dense_source is deliberately absent from this payload: "catalog" is
	// rejected by the namespace upsert route — it is set by the catalog
	// endpoint below — and an omitted field leaves the current value
	// untouched (PATCH semantics).
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
// Codohue's admin plane, authenticated with an admin-plane service token sent as
// a bearer on each request (PUT /api/admin/v1/namespaces/{ns}).
//
// The login round trip it used to make is gone. Codohue v0.12.0 replaced the
// global admin key with named operator accounts and scoped service tokens, so
// POST /api/v1/auth/sessions with {"api_key": ...} answers 403 — measured against
// v0.12.1, not inferred from the notes. Session cookies would also have been the
// wrong thing to keep: the admin server sets them Secure, and darkvoid reaches it
// over plain HTTP inside a compose network.
//
// It always provisions catalog auto-embedding: darkvoid ships raw post content
// and Codohue embeds it. That takes two requests here, because this payload omits
// dense_source — recorded when the namespace upsert was measured to reject
// "catalog" on that route.
//
// That may no longer hold. sdk/go/admin's ProvisionCatalogNamespace sends
// dense_source "catalog" in the same PUT and documents it as "validated
// server-side ... exactly like the dedicated catalog endpoint", which would
// collapse this to one request. Left as two until it can be exercised against a
// server: the namespace this provisions does not currently exist, so the single
// request cannot be tried, and guessing wrong here fails provisioning outright.
//
// Still hand-rolled rather than using sdk/go/admin, for the reason recorded when
// that package first appeared and re-checked against v0.7.0: its
// ProvisionCatalogRequest carries action_weights, alpha and dense_distance and
// nothing else, so adopting it would silently leave lambda, gamma, max_results,
// seen_items_days and the three trending knobs below at whatever the server
// defaults to — a config regression disguised as a dependency upgrade. Only the
// auth mechanism moved to the SDK's model; the payload stays ours until the admin
// SDK covers all of it.
func ProvisionNamespaceConfig(ctx context.Context, cfg NamespaceProvisionConfig) (*NamespaceProvisionResult, error) {
	if cfg.AdminBaseURL == "" {
		return nil, fmt.Errorf("codohue: admin base URL is required")
	}
	if cfg.AdminToken == "" {
		return nil, fmt.Errorf("codohue: admin service token is required")
	}
	if cfg.Namespace == "" {
		return nil, fmt.Errorf("codohue: namespace is required")
	}
	if cfg.EmbeddingDim <= 0 {
		return nil, fmt.Errorf("codohue: embedding dimension must be positive")
	}
	base := strings.TrimRight(cfg.AdminBaseURL, "/")

	body, err := json.Marshal(defaultNamespaceProvisionPayload(cfg.EmbeddingDim, cfg.NamespaceKey))
	if err != nil {
		return nil, fmt.Errorf("codohue: marshal namespace config: %w", err)
	}

	endpoint := base + "/api/admin/v1/namespaces/" + url.PathEscape(cfg.Namespace)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("codohue: build namespace config request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+cfg.AdminToken)
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

	if err := enableCatalogAutoEmbedding(ctx, base, cfg.AdminToken, cfg.Namespace, cfg.EmbeddingDim); err != nil {
		return nil, err
	}

	return &result, nil
}

// enableCatalogAutoEmbedding turns on Codohue's catalog auto-embedding for the
// namespace. Server-side this flips dense_source to "catalog" and validates
// that the strategy's dim matches the namespace embedding_dim.
func enableCatalogAutoEmbedding(ctx context.Context, base, adminToken, namespace string, embeddingDim int) error {
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
	req.Header.Set("Authorization", "Bearer "+adminToken)
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

func defaultNamespaceProvisionPayload(embeddingDim int, namespaceKey string) namespaceProvisionPayload {
	return namespaceProvisionPayload{
		ProvisionAPIKey: namespaceKey,
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
		EmbeddingDim:   embeddingDim,
		DenseDistance:  "cosine",
		TrendingWindow: 24,
		TrendingTTL:    600,
		LambdaTrending: 0.1,
	}
}
