package codohue

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jarviisha/codohue/pkg/codohuetypes"
	"github.com/jarviisha/codohue/sdk/go/redistream"
	"github.com/redis/go-redis/v9"
)

// recordingXAdder captures the args of the last XAdd call.
type recordingXAdder struct {
	lastArgs *redis.XAddArgs
}

func (r *recordingXAdder) XAdd(_ context.Context, a *redis.XAddArgs) *redis.StringCmd {
	r.lastArgs = a
	return redis.NewStringResult("1-1", nil)
}

// TestCappedXAdder_BoundsTheStream pins the producer-side MAXLEN: without it a
// stalled Codohue consumer grows the events stream without limit on a Redis
// that also holds the feed cache and prepared timelines.
func TestCappedXAdder_BoundsTheStream(t *testing.T) {
	inner := &recordingXAdder{}
	capped := cappedXAdder{inner: inner, maxLen: eventsStreamMaxLen}

	if err := capped.XAdd(context.Background(), &redis.XAddArgs{Stream: "codohue:events"}).Err(); err != nil {
		t.Fatalf("XAdd: %v", err)
	}
	if inner.lastArgs == nil {
		t.Fatal("inner XAdd not called")
	}
	if inner.lastArgs.MaxLen != eventsStreamMaxLen || !inner.lastArgs.Approx {
		t.Fatalf("args = MaxLen %d, Approx %v — want approximate cap at %d",
			inner.lastArgs.MaxLen, inner.lastArgs.Approx, int64(eventsStreamMaxLen))
	}
}

func TestCappedXAdder_RespectsExplicitTrim(t *testing.T) {
	inner := &recordingXAdder{}
	capped := cappedXAdder{inner: inner, maxLen: eventsStreamMaxLen}

	_ = capped.XAdd(context.Background(), &redis.XAddArgs{Stream: "s", MaxLen: 5}).Err()
	if inner.lastArgs.MaxLen != 5 || inner.lastArgs.Approx {
		t.Fatalf("explicit MaxLen overridden: %+v", inner.lastArgs)
	}
}

type failingXAdder struct{}

func (failingXAdder) XAdd(_ context.Context, _ *redis.XAddArgs) *redis.StringCmd {
	return redis.NewStringResult("", context.DeadlineExceeded)
}

// The two counters below are the whole point of the silent-degradation
// metrics: every failure they observe is logged-and-continued by design, so
// without them nothing measurable says the index is drifting or events are
// being dropped.

func TestPublishBehaviorEvent_CountsDroppedEvents(t *testing.T) {
	client := &Client{
		namespace: "ns",
		producer:  redistream.NewProducer(failingXAdder{}),
	}

	before := SnapshotMetrics().EventPublishErrors
	if err := client.PublishBehaviorEvent(context.Background(), "user", "post", "LIKE", nil); err == nil {
		t.Fatal("expected publish error")
	}
	if got := SnapshotMetrics().EventPublishErrors - before; got != 1 {
		t.Fatalf("event publish errors delta = %d, want 1", got)
	}
}

func TestDeleteObject_CountsIndexErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "key", "ns", 0, nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	before := SnapshotMetrics().IndexErrors
	if err := client.DeleteObject(context.Background(), "post-1"); err == nil {
		t.Fatal("expected delete error")
	}
	if got := SnapshotMetrics().IndexErrors - before; got != 1 {
		t.Fatalf("index errors delta = %d, want 1", got)
	}
}

// TestGetRecommendations_SlowProviderFailsFast pins the per-call deadline: a
// Codohue that is slow but alive must turn into a fast failure (which the
// breaker counts), not a tax on every feed page.
func TestGetRecommendations_SlowProviderFailsFast(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done(): // client gave up — return so Close() does not hang
		case <-time.After(5 * time.Second):
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "key", "ns", 0, nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	start := time.Now()
	_, err = client.GetRecommendations(context.Background(), "user-1", 20, 0)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected timeout error from a hanging provider")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("call took %v, want it bounded near recommendTimeout (%v)", elapsed, recommendTimeout)
	}
}

func TestRecommendationPageFromResponse_MapsPaginatedItems(t *testing.T) {
	page := recommendationPageFromResponse(&codohuetypes.Response{
		Items: []codohuetypes.RecommendedItem{
			{ObjectID: "post-1", Score: 0.91, Rank: 6},
		},
		Limit:  10,
		Offset: 5,
		Total:  20,
		Source: "cf",
	})
	if page.Total != 20 || page.Limit != 10 || page.Offset != 5 || page.Source != "cf" {
		t.Fatalf("page mismatch: %+v", page)
	}
	if len(page.Items) != 1 || page.Items[0].ObjectID != "post-1" || page.Items[0].Score != 0.91 || page.Items[0].Rank != 6 {
		t.Fatalf("item mismatch: %+v", page.Items)
	}
}

// Rank must carry Codohue v0.8.0's per-item scored flag through, not flatten it
// into the score. An excluded item (recently seen, authored by the subject) and
// an item the engine judged irrelevant both arrive as score 0; only the flag
// tells them apart, and a caller that blends on score alone would treat a
// deliberate exclusion as a relevance verdict.
func TestRank_PropagatesScoredFlag(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/namespaces/ns/rankings" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(codohuetypes.RankResponse{
			Source: "hybrid_rank",
			Items: []codohuetypes.RankedItem{
				{ObjectID: "post-1", Score: 0.42, Rank: 1, Scored: true},
				{ObjectID: "post-2", Score: 0, Rank: 2, Scored: true},  // judged, no signal
				{ObjectID: "post-3", Score: 0, Rank: 3, Scored: false}, // excluded, never judged
			},
			Total: 3,
		})
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "key", "ns", 0, nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	ranked, err := client.Rank(context.Background(), "user-1", []string{"post-1", "post-2", "post-3"})
	if err != nil {
		t.Fatalf("Rank: %v", err)
	}

	want := []RankedItem{
		{ObjectID: "post-1", Score: 0.42, Scored: true},
		{ObjectID: "post-2", Score: 0, Scored: true},
		{ObjectID: "post-3", Score: 0, Scored: false},
	}
	if len(ranked) != len(want) {
		t.Fatalf("got %d items, want %d", len(ranked), len(want))
	}
	for i, w := range want {
		if ranked[i] != w {
			t.Errorf("item %d = %+v, want %+v", i, ranked[i], w)
		}
	}
}

func TestTrendingPageFromResponse_MapsPaginatedItems(t *testing.T) {
	page := trendingPageFromResponse(&codohuetypes.TrendingResponse{
		Items: []codohuetypes.TrendingItem{
			{ObjectID: "post-2", Score: 12.5},
		},
		Limit:  10,
		Offset: 5,
		Total:  20,
	})
	if page.Total != 20 || page.Limit != 10 || page.Offset != 5 {
		t.Fatalf("page mismatch: %+v", page)
	}
	if len(page.Items) != 1 || page.Items[0].ObjectID != "post-2" || page.Items[0].Score != 12.5 || page.Items[0].Rank != 6 {
		t.Fatalf("item mismatch: %+v", page.Items)
	}
}

// TestNewClient_EmptyBaseURLIsAnError pins the one way client construction can
// fail. CODOHUE_BASE_URL defaults to "" and Config.Validate never checks it, so
// enabling Codohue without naming a URL is a plausible misconfiguration. Handing
// back a nil *Client for it is worse than useless: the caller stores it in a
// feed.Recommender field, which makes a non-nil interface holding a nil pointer,
// and the first feed request dereferences the breaker. A caller can only refuse
// to boot on this if it is told about it.
func TestNewClient_EmptyBaseURLIsAnError(t *testing.T) {
	client, err := NewClient("", "key", "ns", 0, nil)

	if err == nil {
		t.Fatal(`NewClient("") returned a nil error — want an error naming the missing base URL`)
	}
	if client != nil {
		t.Fatalf(`NewClient("") returned client %v — want nil alongside the error`, client)
	}
}

// The failure this exists to catch, reproduced exactly: Codohue is up and /ping
// answers 200 without credentials, while every namespace-scoped call is rejected.
// The old probe called the SDK's Ping and reported the deployment healthy for six
// days, through a window in which not one recommendation, rank or ingest
// succeeded.
func TestPing_FailsWhenTheNamespaceRejectsTheCredential(t *testing.T) {
	var authenticatedCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ping" {
			w.WriteHeader(http.StatusOK)
			return
		}
		authenticatedCalls++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"code":"unauthorized","message":"invalid or missing bearer token"}}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "stale-key", "ns", 0, nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	if err := client.Ping(context.Background()); err == nil {
		t.Fatal("Ping reported success against a namespace that rejects every call")
	}
	if authenticatedCalls == 0 {
		t.Fatal("Ping never touched the authenticated surface, so it cannot see a dead credential")
	}
}

// Consequence of the above for /health: the probe's verdict must reach the
// breaker, because an open circuit is what overrides a stale "active".
func TestPing_RepeatedAuthFailuresOpenTheCircuit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"code":"unauthorized","message":"nope"}}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "stale-key", "ns", 0, nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	for range breakerThreshold {
		_ = client.Ping(context.Background())
	}

	if !client.CircuitOpen() {
		t.Error("the circuit stayed closed after repeated rejected probes, so /health would still read active")
	}
}

// The probe must still be able to report recovery: once the credential works
// again, one successful probe closes the circuit rather than waiting out a
// cooldown.
func TestPing_SuccessAfterAuthFailureClosesTheCircuit(t *testing.T) {
	var reject atomic.Bool
	reject.Store(true)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if reject.Load() {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"code":"unauthorized","message":"nope"}}`))
			return
		}
		_ = json.NewEncoder(w).Encode(codohuetypes.TrendingResponse{})
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "key", "ns", 0, nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	for range breakerThreshold {
		_ = client.Ping(context.Background())
	}
	if !client.CircuitOpen() {
		t.Fatal("precondition: circuit should be open")
	}

	reject.Store(false)
	if err := client.Ping(context.Background()); err != nil {
		t.Fatalf("Ping after recovery: %v", err)
	}
	if client.CircuitOpen() {
		t.Error("a successful probe must close the circuit immediately")
	}
}

// Codohue accepts an unstamped envelope only for a generation-1 namespace with
// the legacy gate still open. Once that gate closes, or once the namespace has
// been deleted and recreated, an unstamped event is dropped on read — and
// PublishBehaviorEvent logs and returns, so nothing would report it. The stamp is
// therefore the difference between events arriving and events vanishing quietly.
func TestPublishBehaviorEvent_StampsTheNamespaceGeneration(t *testing.T) {
	recorder := &recordingXAdder{}
	client := &Client{namespace: "ns", producer: newEventProducer(recorder, 7)}

	if err := client.PublishBehaviorEvent(context.Background(), "user-1", "post-1", "LIKE", nil); err != nil {
		t.Fatalf("PublishBehaviorEvent: %v", err)
	}

	event, _ := decodeEnvelope(t, recorder)
	if event.NamespaceGeneration != 7 {
		t.Errorf("namespace_generation = %d, want 7 — an unstamped event is dropped once the legacy gate closes", event.NamespaceGeneration)
	}
}

// Generation 0 means "not reported": a caller that does not provision, or a
// Codohue predating the lifecycle work. The field must then be absent rather than
// sent as a literal 0, which is not a generation any namespace has.
func TestPublishBehaviorEvent_OmitsAnUnknownGeneration(t *testing.T) {
	recorder := &recordingXAdder{}
	client := &Client{namespace: "ns", producer: newEventProducer(recorder, 0)}

	if err := client.PublishBehaviorEvent(context.Background(), "user-1", "post-1", "LIKE", nil); err != nil {
		t.Fatalf("PublishBehaviorEvent: %v", err)
	}

	_, payload := decodeEnvelope(t, recorder)
	if strings.Contains(payload, "namespace_generation") {
		t.Errorf("envelope carries namespace_generation with nothing to report: %s", payload)
	}
}

// decodeEnvelope pulls the event back out of the recorded XAdd. XAddArgs.Values is
// an interface{} in go-redis v9, and the producer puts a map there.
func decodeEnvelope(t *testing.T, recorder *recordingXAdder) (codohuetypes.EventPayload, string) {
	t.Helper()
	if recorder.lastArgs == nil {
		t.Fatal("no XAdd was recorded")
	}
	values, ok := recorder.lastArgs.Values.(map[string]any)
	if !ok {
		t.Fatalf("XAdd values are %T, want map[string]any", recorder.lastArgs.Values)
	}
	payload, ok := values[codohuetypes.PayloadField].(string)
	if !ok {
		t.Fatalf("stream entry carried no %q field: %#v", codohuetypes.PayloadField, values)
	}
	var event codohuetypes.EventPayload
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	return event, payload
}
