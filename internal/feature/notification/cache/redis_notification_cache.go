package cache

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/google/uuid"
	pkgredis "github.com/jarviisha/darkvoid/pkg/redis"
	"github.com/redis/go-redis/v9"
)

func unreadKey(userID uuid.UUID) string {
	return fmt.Sprintf("notif:unread:%s", userID)
}

// RedisNotificationCache implements NotificationCache using Redis.
type RedisNotificationCache struct {
	client *pkgredis.Client
}

func NewRedisNotificationCache(client *pkgredis.Client) *RedisNotificationCache {
	return &RedisNotificationCache{client: client}
}

func (c *RedisNotificationCache) GetUnreadCount(ctx context.Context, userID uuid.UUID) (int64, error) {
	raw, err := c.client.Get(ctx, unreadKey(userID)).Result()
	if errors.Is(err, redis.Nil) {
		return -1, nil // cache miss
	}
	if err != nil {
		return -1, fmt.Errorf("redis get unread count: %w", err)
	}
	count, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return -1, fmt.Errorf("parse unread count: %w", err)
	}
	return count, nil
}

func (c *RedisNotificationCache) SetUnreadCount(ctx context.Context, userID uuid.UUID, count int64) error {
	if err := c.client.Set(ctx, unreadKey(userID), count, UnreadCountTTL).Err(); err != nil {
		return fmt.Errorf("redis set unread count: %w", err)
	}
	return nil
}

func (c *RedisNotificationCache) InvalidateUnreadCount(ctx context.Context, userID uuid.UUID) error {
	if err := c.client.Del(ctx, unreadKey(userID)).Err(); err != nil {
		return fmt.Errorf("redis del unread count: %w", err)
	}
	return nil
}
