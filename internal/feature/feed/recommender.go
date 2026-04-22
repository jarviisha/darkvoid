package feed

import "context"

// Recommender fetches personalized post recommendations for a user.
// The returned post IDs are already sorted by relevance (best match first).
// Implementations: *codohue.Client (HTTP), nil-safe (optional dependency).
type Recommender interface {
	GetRecommendations(ctx context.Context, userID string, limit int) ([]string, error)
}

// TrendingFetcher fetches trending post IDs from an external recommendation engine.
// The returned IDs are ordered by trending score (highest first).
// Implementations: *codohue.Client (GET /v1/trending/{ns}), nil-safe (optional dependency).
type TrendingFetcher interface {
	GetTrending(ctx context.Context, limit int) ([]string, error)
}
