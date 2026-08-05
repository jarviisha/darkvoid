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
	"github.com/redis/go-redis/v9"
)

const (
	// recommendTimeout caps one recommendations call. It sits far below the
	// SDK's 5s transport timeout on purpose: GetRecommendations runs
	// synchronously inside every mixed feed page and the feed has a complete
	// local fallback, so this deadline is the worst case a slow Codohue may
	// impose on a feed request. It is also what makes the circuit breaker
	// latency-aware — the breaker counts failures, and a Codohue answering in
	// 2.9s under a 3s deadline never fails, taxing every page forever, while
	// under this deadline it times out and three timeouts open the circuit.
	recommendTimeout = 800 * time.Millisecond
	// trendingTimeout stays looser: trending sits behind a 15-minute shared
	// cache and a single-flight rebuild, so per cache window exactly one
	// caller pays this worst case rather than every request.
	trendingTimeout = 3 * time.Second

	// eventsStreamMaxLen bounds the behavior-events stream at the producer
	// (XADD MAXLEN ~, approximate so it costs nothing per publish). The
	// consumer is Codohue's; when it stops or lags, an uncapped stream grows
	// without limit on a Redis that may also hold the feed cache, the prepared
	// timelines and the notification pub/sub — and an optional integration's
	// backlog must never be able to evict core state or stop core writes.
	// Events beyond the cap drop oldest-first, which is this integration's
	// documented trade: behavior events are lossy whenever the bus is
	// unhealthy. At roughly 300 bytes per event the stream stays near 30 MB.
	eventsStreamMaxLen = 100_000
)

// cappedXAdder injects the MAXLEN bound into every stream publish. It wraps
// the Redis client handed to the events producer only — catalog ingest goes
// over HTTP, so no producer-trimmed-catalog concern applies here.
type cappedXAdder struct {
	inner  redistream.XAdder
	maxLen int64
}

// countIndexErr counts a failed index-maintenance call and passes the error
// through, so the four call sites stay one-liners.
func countIndexErr(err error) error {
	if err != nil {
		countIndexError()
	}
	return err
}

func (c cappedXAdder) XAdd(ctx context.Context, a *redis.XAddArgs) *redis.StringCmd {
	if a.MaxLen == 0 && a.MinID == "" {
		a.MaxLen = c.maxLen
		a.Approx = true
	}
	return c.inner.XAdd(ctx, a)
}

// Action represents a user behavior action recognized by Codohue.
type Action string

const (
	ActionView    Action = "VIEW"
	ActionLike    Action = "LIKE"
	ActionComment Action = "COMMENT"
	ActionShare   Action = "SHARE"
	ActionSkip    Action = "SKIP" // negative signal, e.g. unlike
)

// RankedItem holds an object ID and its CF relevance score returned by the
// rankings endpoint.
//
// Read Scored before Score. Since Codohue v0.8.0 a returned candidate can mean
// two different things: Scored true is a real relevance verdict (a low or zero
// score says the engine looked and found little), Scored false means the item
// came back unscored so the candidate list stays whole — the subject has no
// vector, the item was never indexed, or an eligibility filter (recently seen,
// exclude_authored) took it out. Treating those as score 0 would blend
// "excluded" and "irrelevant" into the same number.
//
// Score is no longer per-request min-max normalized either: v0.8.0 maps it with
// x/(x+k), which is batch-independent and comparable across calls, but not
// comparable with values recorded before the upgrade.
type RankedItem struct {
	ObjectID string
	Score    float64
	Scored   bool
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
		producer = redistream.NewProducer(cappedXAdder{inner: redisClient, maxLen: eventsStreamMaxLen})
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

// CircuitOpen reports whether the client is currently short-circuiting calls.
//
// This is the live availability signal: the breaker learns Codohue is down from
// real traffic within a few requests, where a periodic health probe only finds
// out on its next tick. Reporting health from the probe alone would leave
// /health claiming "active" for most of a short outage.
func (c *Client) CircuitOpen() bool {
	if c == nil || c.breaker == nil {
		return false
	}
	return c.breaker.isOpen()
}

// GetRecommendations returns ordered post IDs.
func (c *Client) GetRecommendations(ctx context.Context, userID string, limit int, offset int) (*feed.RecommendationPage, error) {
	reqCtx, cancel := context.WithTimeout(ctx, recommendTimeout)
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

// Rank calls the rankings endpoint and returns hybrid-scored candidates sorted
// by relevance.
//
// resp.Source is logged rather than returned: "hybrid_rank" means the subject
// was scored, "no_subject_vector" means it had no vector at all and every item
// comes back Scored=false in the original request order. Callers that need to
// tell those apart can read Scored per item, which covers the per-item
// exclusions (seen, authored) the response source does not.
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
	scored := 0
	for i, item := range resp.Items {
		ranked[i] = RankedItem{ObjectID: item.ObjectID, Score: item.Score, Scored: item.Scored}
		if item.Scored {
			scored++
		}
	}

	logger.Debug(ctx, "codohue rank completed",
		"source", resp.Source, "candidates", len(candidates), "returned", len(ranked), "scored", scored, "subject_id", subjectID)
	return ranked, nil
}

// GetTrending returns trending object IDs.
func (c *Client) GetTrending(ctx context.Context, limit int, offset int) (*feed.TrendingPage, error) {
	reqCtx, cancel := context.WithTimeout(ctx, trendingTimeout)
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

	return countIndexErr(guardErr(c, func() error {
		return c.ns.StoreObjectEmbedding(reqCtx, objectID, converted, opts...)
	}))
}

// UpsertSubjectEmbedding pushes a dense embedding vector for a user (subject) to Codohue.
func (c *Client) UpsertSubjectEmbedding(ctx context.Context, subjectID string, vector []float64) error {
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	converted := make([]float32, len(vector))
	for i, v := range vector {
		converted[i] = float32(v)
	}

	return countIndexErr(guardErr(c, func() error {
		return c.ns.StoreSubjectEmbedding(reqCtx, subjectID, converted)
	}))
}

// IngestCatalogItem publishes post content to Codohue's catalog auto-embedding
// pipeline (dense_source "catalog"). The server embeds the content
// asynchronously; authorSubjectID is stored as ownership metadata only.
func (c *Client) IngestCatalogItem(ctx context.Context, objectID, content, authorSubjectID string) error {
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return countIndexErr(guardErr(c, func() error {
		return c.ns.IngestCatalog(reqCtx, codohuetypes.CatalogIngestRequest{
			ObjectID:        objectID,
			Content:         content,
			AuthorSubjectID: authorSubjectID,
		})
	}))
}

// DeleteObject removes an item from the recommendation index.
func (c *Client) DeleteObject(ctx context.Context, objectID string) error {
	reqCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	return countIndexErr(guardErr(c, func() error {
		return c.ns.DeleteObject(reqCtx, objectID)
	}))
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
		countEventPublishError()
		logger.LogError(ctx, err, "codohue: failed to publish event",
			"action", action, "subject", subjectID, "object", objectID)
		return err
	}
	return nil
}
