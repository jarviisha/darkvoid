package cache

import (
	"context"
	"errors"

	"github.com/jarviisha/darkvoid/internal/feature/feed"
)

// ErrFeedSessionMiss indicates the feed continuation state is not cached.
var ErrFeedSessionMiss = errors.New("feed session cache miss")

// FeedSessionCache stores short-lived feed continuation state.
// It is not a reusable per-user feed cache; it only preserves one active
// browsing sequence's cursor state.
type FeedSessionCache interface {
	GetFeedState(ctx context.Context, key string) (*feed.FeedPageState, error)
	SetFeedState(ctx context.Context, key string, state *feed.FeedPageState) error
	DeleteFeedState(ctx context.Context, key string) error
}

// NopFeedSessionCache is a no-op implementation used when Redis is unavailable.
type NopFeedSessionCache struct{}

func NewNopFeedSessionCache() *NopFeedSessionCache { return &NopFeedSessionCache{} }

func (n *NopFeedSessionCache) GetFeedState(_ context.Context, _ string) (*feed.FeedPageState, error) {
	return nil, ErrFeedSessionMiss
}

func (n *NopFeedSessionCache) SetFeedState(_ context.Context, _ string, _ *feed.FeedPageState) error {
	return nil
}

func (n *NopFeedSessionCache) DeleteFeedState(_ context.Context, _ string) error { return nil }
