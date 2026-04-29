package feed

import "context"

// RecommendedItem is one item from an external recommendation provider.
type RecommendedItem struct {
	ObjectID string
	Score    float64
	Rank     int
}

// RecommendationPage is a paginated external recommendation result.
type RecommendationPage struct {
	Items  []RecommendedItem
	Limit  int
	Offset int
	Total  int
	Source string
}

// TrendingItem is one item from an external trending provider.
type TrendingItem struct {
	ObjectID string
	Score    float64
	Rank     int
}

// TrendingPage is a paginated external trending result.
type TrendingPage struct {
	Items  []TrendingItem
	Limit  int
	Offset int
	Total  int
}

// Recommender fetches personalized post recommendations for a user.
// The returned items are already sorted by relevance (best match first).
// Implementations: *codohue.Client (HTTP), nil-safe (optional dependency).
type Recommender interface {
	GetRecommendations(ctx context.Context, userID string, limit int, offset int) (*RecommendationPage, error)
}

// TrendingFetcher fetches trending post IDs from an external recommendation engine.
// The returned IDs are ordered by trending score (highest first).
// Implementations: *codohue.Client (GET /v1/trending/{ns}), nil-safe (optional dependency).
type TrendingFetcher interface {
	GetTrending(ctx context.Context, limit int, offset int) (*TrendingPage, error)
}
