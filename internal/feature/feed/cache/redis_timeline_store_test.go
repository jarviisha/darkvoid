package cache

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/feature/feed"
	pkgredis "github.com/jarviisha/darkvoid/pkg/redis"
	"github.com/redis/go-redis/v9"
)

func newRedisTimelineStoreForTest(t *testing.T) (*RedisTimelineStore, *pkgredis.Client) {
	t.Helper()
	addr := os.Getenv("REDIS_TEST_ADDR")
	if addr == "" {
		t.Skip("REDIS_TEST_ADDR not set")
	}
	client := &pkgredis.Client{Client: redis.NewClient(&redis.Options{Addr: addr, DB: 15})}
	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Skipf("redis unavailable: %v", err)
	}
	if err := client.FlushDB(context.Background()).Err(); err != nil {
		t.Fatalf("flush redis test DB: %v", err)
	}
	return NewRedisTimelineStore(client, 3, time.Hour), client
}

func TestRedisTimelineStore_AddReadTrimAndTTL(t *testing.T) {
	ctx := context.Background()
	store, client := newRedisTimelineStoreForTest(t)
	defer client.Close() //nolint:errcheck

	userID := uuid.New()
	base := time.Date(2026, 5, 2, 10, 0, 0, 123456000, time.UTC)
	entries := []feed.TimelineEntry{
		{PostID: uuid.New(), Score: feed.TimelineScoreFromTime(base)},
		{PostID: uuid.New(), Score: feed.TimelineScoreFromTime(base.Add(time.Second))},
		{PostID: uuid.New(), Score: feed.TimelineScoreFromTime(base.Add(2 * time.Second))},
		{PostID: uuid.New(), Score: feed.TimelineScoreFromTime(base.Add(3 * time.Second))},
	}

	if err := store.AddPostsBatch(ctx, userID, entries); err != nil {
		t.Fatalf("AddPostsBatch: %v", err)
	}
	if err := store.AddPost(ctx, userID, entries[3]); err != nil {
		t.Fatalf("AddPost duplicate: %v", err)
	}

	page, err := store.ReadPage(ctx, userID, nil, 10)
	if err != nil {
		t.Fatalf("ReadPage: %v", err)
	}
	if len(page.Entries) != 3 {
		t.Fatalf("entries len = %d, want trimmed 3", len(page.Entries))
	}
	if page.Entries[0].PostID != entries[3].PostID || page.Entries[2].PostID != entries[1].PostID {
		t.Fatalf("unexpected newest-first order after trim: %+v", page.Entries)
	}
	if page.Last == nil || page.Last.PostID != entries[1].PostID.String() {
		t.Fatalf("last position mismatch: %+v", page.Last)
	}
	if ttl := client.TTL(ctx, timelineKey(userID)).Val(); ttl <= 0 || ttl > time.Hour {
		t.Fatalf("ttl = %v, want within configured hour", ttl)
	}
}

func TestRedisTimelineStore_MicrosecondScoreAndTieCursor(t *testing.T) {
	ctx := context.Background()
	store, client := newRedisTimelineStoreForTest(t)
	defer client.Close() //nolint:errcheck

	userID := uuid.New()
	createdAt := time.Date(2026, 5, 2, 10, 0, 0, 987654000, time.UTC)
	score := feed.TimelineScoreFromTime(createdAt)
	if score%1_000_000 != 987654 {
		t.Fatalf("score = %d, want microsecond precision suffix 987654", score)
	}

	low := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	high := uuid.MustParse("ffffffff-ffff-ffff-ffff-ffffffffffff")
	if err := store.AddPostsBatch(ctx, userID, []feed.TimelineEntry{
		{PostID: low, Score: score},
		{PostID: high, Score: score},
	}); err != nil {
		t.Fatalf("AddPostsBatch: %v", err)
	}

	page, err := store.ReadPage(ctx, userID, nil, 2)
	if err != nil {
		t.Fatalf("ReadPage page1: %v", err)
	}
	if len(page.Entries) != 2 || page.Entries[0].PostID != high || page.Entries[1].PostID != low {
		t.Fatalf("tie order = %+v, want high UUID then low UUID", page.Entries)
	}

	page, err = store.ReadPage(ctx, userID, &feed.TimelinePosition{Score: score, PostID: high.String()}, 2)
	if err != nil {
		t.Fatalf("ReadPage after high: %v", err)
	}
	if len(page.Entries) != 1 || page.Entries[0].PostID != low {
		t.Fatalf("tie cursor page = %+v, want low UUID only", page.Entries)
	}
}

func TestRedisTimelineStore_MissAndRemoveBestEffort(t *testing.T) {
	ctx := context.Background()
	store, client := newRedisTimelineStoreForTest(t)
	defer client.Close() //nolint:errcheck

	userID := uuid.New()
	page, err := store.ReadPage(ctx, userID, nil, 20)
	if err != nil {
		t.Fatalf("ReadPage miss: %v", err)
	}
	if page == nil || len(page.Entries) != 0 {
		t.Fatalf("miss page = %+v, want empty", page)
	}

	postID := uuid.New()
	if addErr := store.AddPost(ctx, userID, feed.TimelineEntry{PostID: postID, Score: 1}); addErr != nil {
		t.Fatalf("AddPost: %v", addErr)
	}
	if removeErr := store.RemovePostBestEffort(ctx, userID, postID); removeErr != nil {
		t.Fatalf("RemovePostBestEffort: %v", removeErr)
	}
	page, err = store.ReadPage(ctx, userID, nil, 20)
	if err != nil {
		t.Fatalf("ReadPage after remove: %v", err)
	}
	if len(page.Entries) != 0 {
		t.Fatalf("entries after remove = %+v, want empty", page.Entries)
	}
}
