package codohue

import (
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
