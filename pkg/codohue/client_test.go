package codohue

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jarviisha/codohue/pkg/codohuetypes"
)

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

	client := NewClient(server.URL, "key", "ns", nil)
	if client == nil {
		t.Fatal("NewClient returned nil")
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
