package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jarviisha/darkvoid/internal/feature/feed"
	pkgredis "github.com/jarviisha/darkvoid/pkg/redis"
	"github.com/redis/go-redis/v9"
)

const feedSessionPrefix = "feed:session:"

// RedisFeedSessionCache stores feed continuation state in Redis with a short TTL.
type RedisFeedSessionCache struct {
	client *pkgredis.Client
}

func NewRedisFeedSessionCache(client *pkgredis.Client) *RedisFeedSessionCache {
	return &RedisFeedSessionCache{client: client}
}

func feedSessionKey(key string) string {
	return feedSessionPrefix + key
}

func (c *RedisFeedSessionCache) GetFeedState(ctx context.Context, key string) (*feed.FeedPageState, error) {
	raw, err := c.client.Get(ctx, feedSessionKey(key)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, ErrFeedSessionMiss
	}
	if err != nil {
		return nil, fmt.Errorf("redis get feed session: %w", err)
	}
	var state feed.FeedPageState
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, fmt.Errorf("unmarshal feed session: %w", err)
	}
	return &state, nil
}

func (c *RedisFeedSessionCache) SetFeedState(ctx context.Context, key string, state *feed.FeedPageState) error {
	raw, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal feed session: %w", err)
	}
	if err := c.client.Set(ctx, feedSessionKey(key), raw, feed.FeedSessionTTL).Err(); err != nil {
		return fmt.Errorf("redis set feed session: %w", err)
	}
	return nil
}

func (c *RedisFeedSessionCache) DeleteFeedState(ctx context.Context, key string) error {
	if err := c.client.Del(ctx, feedSessionKey(key)).Err(); err != nil {
		return fmt.Errorf("redis del feed session: %w", err)
	}
	return nil
}
