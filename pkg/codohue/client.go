// Package codohue provides the application-facing Codohue adapter.
// It keeps the existing darkvoid interfaces stable while delegating HTTP
// operations to the official Codohue Go SDK.
package codohue

import (
	"context"
	"fmt"
	"time"

	"github.com/jarviisha/codohue/pkg/codohuetypes"
	sdk "github.com/jarviisha/codohue/sdk/go"
	"github.com/jarviisha/codohue/sdk/go/redistream"
	"github.com/jarviisha/darkvoid/internal/feature/feed"
	"github.com/jarviisha/darkvoid/pkg/logger"
	pkgredis "github.com/jarviisha/darkvoid/pkg/redis"
)

// Action represents a user behavior action recognized by Codohue.
type Action string

const (
	ActionView    Action = "VIEW"
	ActionLike    Action = "LIKE"
	ActionComment Action = "COMMENT"
	ActionShare   Action = "SHARE"
	ActionSkip    Action = "SKIP" // negative signal, e.g. unlike
)

// RankedItem holds an object ID and its CF relevance score returned by /v1/rank.
// Score is in the range [0, 1]; 0 means no interaction history for the item.
type RankedItem struct {
	ObjectID string
	Score    float64
}

// Client communicates with the Codohue recommendation service.
// It is safe for concurrent use.
type Client struct {
	http      *sdk.Client
	ns        *sdk.Namespace
	namespace string
	producer  *redistream.Producer // nil when Redis is unavailable
	// breaker short-circuits the HTTP surface while Codohue is down, so an
	// outage costs one timeout rather than one per call. It does not cover
	// PublishBehaviorEvent, which goes to Redis and fails independently.
	breaker *breaker
}

// NewClient creates a Codohue client.
// nsKey is the namespace key returned when the namespace was first created.
// redisClient may be nil — in that case event publishing is disabled.
func NewClient(baseURL, nsKey, namespace string, redisClient *pkgredis.Client) *Client {
	httpClient, err := sdk.New(
		baseURL,
		sdk.WithTimeout(5*time.Second),
		sdk.WithRetries(2),
	)
	if err != nil {
		return nil
	}

	var producer *redistream.Producer
	if redisClient != nil {
		producer = redistream.NewProducer(redisClient)
	}

	return &Client{
		http:      httpClient,
		ns:        httpClient.Namespace(namespace, nsKey),
		namespace: namespace,
		producer:  producer,
		breaker:   newBreaker(),
	}
}

// Ping checks whether the Codohue service is reachable via the official SDK.
//
// It deliberately bypasses the circuit breaker: this is the health probe, and a
// probe answered from a cached "circuit is open" tells you nothing about whether
// the service came back. It does report its outcome to the breaker, so a probe
// that succeeds closes the circuit immediately instead of waiting for the next
// cooldown to expire.
func (c *Client) Ping(ctx context.Context) error {
	if c == nil || c.http == nil {
		return fmt.Errorf("codohue client is not configured")
	}

	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	err := c.http.Ping(reqCtx)
	c.breaker.observe(err)
	return err
}

// GetRecommendations returns ordered post IDs.
func (c *Client) GetRecommendations(ctx context.Context, userID string, limit int, offset int) (*feed.RecommendationPage, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	resp, err := guard(c, func() (*codohuetypes.Response, error) {
		return c.ns.Recommend(reqCtx, userID, sdk.WithLimit(limit), sdk.WithOffset(offset))
	})
	if err != nil {
		return nil, err
	}

	logger.Info(ctx, "codohue recommendations fetched", "source", resp.Source, "count", len(resp.Items), "user_id", userID)
	return recommendationPageFromResponse(resp), nil
}

func recommendationPageFromResponse(resp *codohuetypes.Response) *feed.RecommendationPage {
	items := make([]feed.RecommendedItem, len(resp.Items))
	for i, item := range resp.Items {
		items[i] = feed.RecommendedItem{ObjectID: item.ObjectID, Score: item.Score, Rank: item.Rank}
	}
	return &feed.RecommendationPage{
		Items:  items,
		Limit:  resp.Limit,
		Offset: resp.Offset,
		Total:  resp.Total,
		Source: resp.Source,
	}
}

// Rank calls POST /rank and returns CF-scored candidates sorted by relevance.
func (c *Client) Rank(ctx context.Context, subjectID string, candidates []string) ([]RankedItem, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	resp, err := guard(c, func() (*codohuetypes.RankResponse, error) {
		return c.ns.Rank(reqCtx, subjectID, candidates)
	})
	if err != nil {
		return nil, err
	}

	ranked := make([]RankedItem, len(resp.Items))
	for i, item := range resp.Items {
		ranked[i] = RankedItem{ObjectID: item.ObjectID, Score: item.Score}
	}
	return ranked, nil
}

// GetTrending returns trending object IDs.
func (c *Client) GetTrending(ctx context.Context, limit int, offset int) (*feed.TrendingPage, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	resp, err := guard(c, func() (*codohuetypes.TrendingResponse, error) {
		return c.ns.Trending(reqCtx, sdk.WithLimit(limit), sdk.WithOffset(offset))
	})
	if err != nil {
		return nil, err
	}

	return trendingPageFromResponse(resp), nil
}

func trendingPageFromResponse(resp *codohuetypes.TrendingResponse) *feed.TrendingPage {
	items := make([]feed.TrendingItem, len(resp.Items))
	for i, item := range resp.Items {
		items[i] = feed.TrendingItem{ObjectID: item.ObjectID, Score: item.Score, Rank: resp.Offset + i + 1}
	}
	return &feed.TrendingPage{Items: items, Limit: resp.Limit, Offset: resp.Offset, Total: resp.Total}
}

// UpsertObjectEmbedding pushes a dense embedding vector for an item (post) to Codohue.
// A non-zero createdAt feeds Codohue's γ-based object-freshness rerank for BYOE vectors.
func (c *Client) UpsertObjectEmbedding(ctx context.Context, objectID string, vector []float64, createdAt time.Time) error {
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	converted := make([]float32, len(vector))
	for i, v := range vector {
		converted[i] = float32(v)
	}

	var opts []sdk.EmbeddingOption
	if !createdAt.IsZero() {
		opts = append(opts, sdk.WithObjectCreatedAt(createdAt.UTC()))
	}

	return guardErr(c, func() error {
		return c.ns.StoreObjectEmbedding(reqCtx, objectID, converted, opts...)
	})
}

// UpsertSubjectEmbedding pushes a dense embedding vector for a user (subject) to Codohue.
func (c *Client) UpsertSubjectEmbedding(ctx context.Context, subjectID string, vector []float64) error {
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	converted := make([]float32, len(vector))
	for i, v := range vector {
		converted[i] = float32(v)
	}

	return guardErr(c, func() error {
		return c.ns.StoreSubjectEmbedding(reqCtx, subjectID, converted)
	})
}

// IngestCatalogItem publishes post content to Codohue's catalog auto-embedding
// pipeline (dense_source "catalog"). The server embeds the content
// asynchronously; authorSubjectID is stored as ownership metadata only.
func (c *Client) IngestCatalogItem(ctx context.Context, objectID, content, authorSubjectID string) error {
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return guardErr(c, func() error {
		return c.ns.IngestCatalog(reqCtx, codohuetypes.CatalogIngestRequest{
			ObjectID:        objectID,
			Content:         content,
			AuthorSubjectID: authorSubjectID,
		})
	})
}

// DeleteObject removes an item from the recommendation index.
func (c *Client) DeleteObject(ctx context.Context, objectID string) error {
	reqCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	return guardErr(c, func() error {
		return c.ns.DeleteObject(reqCtx, objectID)
	})
}

// PublishBehaviorEvent publishes a user behavior event to the codohue:events Redis Stream.
// Phase 2 delegates event publishing to the official Codohue Redis Streams SDK.
func (c *Client) PublishBehaviorEvent(ctx context.Context, subjectID, objectID, action string, objectCreatedAt *time.Time) error {
	if c.producer == nil {
		return nil
	}

	event := codohuetypes.EventPayload{
		Namespace:  c.namespace,
		SubjectID:  subjectID,
		ObjectID:   objectID,
		Action:     codohuetypes.Action(action),
		OccurredAt: time.Now().UTC(),
	}
	if objectCreatedAt != nil {
		t := objectCreatedAt.UTC()
		event.ObjectCreatedAt = &t
	}

	if _, err := c.producer.Publish(ctx, event); err != nil {
		logger.LogError(ctx, err, "codohue: failed to publish event",
			"action", action, "subject", subjectID, "object", objectID)
		return err
	}
	return nil
}
