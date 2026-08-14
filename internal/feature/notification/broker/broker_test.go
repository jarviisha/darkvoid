package broker

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	pkgredis "github.com/jarviisha/darkvoid/pkg/redis"
	"github.com/redis/go-redis/v9"
)

func TestBroker_DeliverCleanupShutdownConcurrent(t *testing.T) {
	b := NewBroker(nil)
	userID := uuid.New()
	cleanups := make([]func(), 0, 100)
	for range 100 {
		_, cleanup := b.Subscribe(context.Background(), userID)
		cleanups = append(cleanups, cleanup)
	}

	var wg sync.WaitGroup
	for i := range cleanups {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for range 100 {
				b.deliverLocal(userID, Event{Type: "notification"})
			}
		}()
		go func(cleanup func()) {
			defer wg.Done()
			cleanup()
			cleanup()
		}(cleanups[i])
	}
	b.Shutdown()
	b.Shutdown()
	wg.Wait()

	ch, cleanup := b.Subscribe(context.Background(), userID)
	cleanup()
	if _, ok := <-ch; ok {
		t.Fatal("subscription after shutdown must be closed")
	}
}

func TestBroker_RedisPublishRoundTrip(t *testing.T) {
	addr := os.Getenv("REDIS_TEST_ADDR")
	if addr == "" {
		t.Skip("REDIS_TEST_ADDR not set")
	}
	client := &pkgredis.Client{Client: redis.NewClient(&redis.Options{Addr: addr, DB: 14})}
	t.Cleanup(func() { _ = client.Close() })
	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Skipf("redis unavailable: %v", err)
	}

	b := NewBroker(client)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	t.Cleanup(cancel)
	go b.StartRedisSubscriber(ctx)

	userID := uuid.New()
	events, cleanup := b.Subscribe(ctx, userID)
	t.Cleanup(cleanup)
	want := Event{Type: "notification", Data: json.RawMessage(`{"id":"notification-1"}`)}
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			t.Fatal("timed out waiting for Redis pub/sub round trip")
		case <-ticker.C:
			b.Publish(ctx, userID, want)
		case got := <-events:
			if got.Type != want.Type || string(got.Data) != string(want.Data) {
				t.Fatalf("event = %#v, want %#v", got, want)
			}
			return
		}
	}
}
