package cache

import (
	"context"
	stderrors "errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type notificationRedisStub struct {
	getValue string
	getErr   error
	setErr   error
	delErr   error
	setKey   string
	setValue any
	setTTL   time.Duration
	delKey   string
}

func (s *notificationRedisStub) Get(context.Context, string) *redis.StringCmd {
	return redis.NewStringResult(s.getValue, s.getErr)
}

func (s *notificationRedisStub) Set(_ context.Context, key string, value any, ttl time.Duration) *redis.StatusCmd {
	s.setKey, s.setValue, s.setTTL = key, value, ttl
	return redis.NewStatusResult("OK", s.setErr)
}

func (s *notificationRedisStub) Del(_ context.Context, keys ...string) *redis.IntCmd {
	if len(keys) > 0 {
		s.delKey = keys[0]
	}
	return redis.NewIntResult(1, s.delErr)
}

func TestRedisNotificationCache_GetUnreadCount(t *testing.T) {
	t.Parallel()

	providerErr := stderrors.New("redis unavailable")
	tests := []struct {
		name      string
		value     string
		err       error
		wantCount int64
		wantErr   bool
	}{
		{name: "hit", value: "42", wantCount: 42},
		{name: "miss", err: redis.Nil, wantCount: -1},
		{name: "provider error", err: providerErr, wantCount: -1, wantErr: true},
		{name: "invalid cached value", value: "not-a-number", wantCount: -1, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cache := &RedisNotificationCache{client: &notificationRedisStub{getValue: tt.value, getErr: tt.err}}
			count, err := cache.GetUnreadCount(context.Background(), uuid.New())
			if (err != nil) != tt.wantErr {
				t.Fatalf("GetUnreadCount() error = %v, wantErr %v", err, tt.wantErr)
			}
			if count != tt.wantCount {
				t.Fatalf("GetUnreadCount() = %d, want %d", count, tt.wantCount)
			}
		})
	}
}

func TestRedisNotificationCache_SetAndInvalidate(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	stub := &notificationRedisStub{}
	cache := &RedisNotificationCache{client: stub}

	if err := cache.SetUnreadCount(context.Background(), userID, 7); err != nil {
		t.Fatalf("SetUnreadCount() error = %v", err)
	}
	wantKey := "notif:unread:" + userID.String()
	if stub.setKey != wantKey || stub.setValue != int64(7) || stub.setTTL != UnreadCountTTL {
		t.Fatalf("Set args = (%q, %v, %v), want (%q, 7, %v)", stub.setKey, stub.setValue, stub.setTTL, wantKey, UnreadCountTTL)
	}
	if err := cache.InvalidateUnreadCount(context.Background(), userID); err != nil {
		t.Fatalf("InvalidateUnreadCount() error = %v", err)
	}
	if stub.delKey != wantKey {
		t.Fatalf("deleted key = %q, want %q", stub.delKey, wantKey)
	}
}

func TestRedisNotificationCache_WriteErrorsAreWrapped(t *testing.T) {
	t.Parallel()

	sentinel := stderrors.New("redis unavailable")
	cache := &RedisNotificationCache{client: &notificationRedisStub{setErr: sentinel, delErr: sentinel}}
	userID := uuid.New()
	if err := cache.SetUnreadCount(context.Background(), userID, 1); !stderrors.Is(err, sentinel) {
		t.Fatalf("SetUnreadCount() error = %v, want wrapped sentinel", err)
	}
	if err := cache.InvalidateUnreadCount(context.Background(), userID); !stderrors.Is(err, sentinel) {
		t.Fatalf("InvalidateUnreadCount() error = %v, want wrapped sentinel", err)
	}
}
