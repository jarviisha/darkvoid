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

var provisionHTTPClient = http.DefaultClient

// NamespaceProvisionConfig contains the admin-plane configuration sent to Codohue.
type NamespaceProvisionConfig struct {
	BaseURL      string
	AdminKey     string
	Namespace    string
	EmbeddingDim int
}

// NamespaceProvisionResult contains the relevant response fields from Codohue.
type NamespaceProvisionResult struct {
	Namespace string    `json:"namespace"`
	UpdatedAt time.Time `json:"updated_at"`
	APIKey    string    `json:"api_key,omitempty"`
}

type namespaceProvisionPayload struct {
	ActionWeights  map[string]float64 `json:"action_weights"`
	Lambda         float64            `json:"lambda"`
	Gamma          float64            `json:"gamma"`
	MaxResults     int                `json:"max_results"`
	SeenItemsDays  int                `json:"seen_items_days"`
	Alpha          float64            `json:"alpha"`
	DenseStrategy  string             `json:"dense_strategy"`
	EmbeddingDim   int                `json:"embedding_dim"`
	DenseDistance  string             `json:"dense_distance"`
	TrendingWindow int                `json:"trending_window"`
	TrendingTTL    int                `json:"trending_ttl"`
	LambdaTrending float64            `json:"lambda_trending"`
}

// ProvisionNamespaceConfig upserts Darkvoid's Codohue namespace config through
// Codohue's admin endpoint. The official runtime SDK intentionally does not
// wrap this operator-facing route.
func ProvisionNamespaceConfig(ctx context.Context, cfg NamespaceProvisionConfig) (*NamespaceProvisionResult, error) {
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("codohue: base URL is required")
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

	payload := defaultNamespaceProvisionPayload(cfg.EmbeddingDim)
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("codohue: marshal namespace config: %w", err)
	}

	endpoint := strings.TrimRight(cfg.BaseURL, "/") + "/v1/config/namespaces/" + url.PathEscape(cfg.Namespace)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("codohue: build namespace config request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+cfg.AdminKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := provisionHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("codohue: send namespace config request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
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

	return &result, nil
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
		DenseStrategy:  "byoe",
		EmbeddingDim:   embeddingDim,
		DenseDistance:  "cosine",
		TrendingWindow: 24,
		TrendingTTL:    600,
		LambdaTrending: 0.1,
	}
}
