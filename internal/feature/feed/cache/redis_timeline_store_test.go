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

// timelineSettings builds the snapshot the store reads its retention limits from.
// They are no longer constructor arguments — the store reads them per write, so
// lowering them starts reclaiming memory on the next fanout instead of on the
// next restart.
func timelineSettings(maxItems int, ttl time.Duration) *feed.Settings {
	rs := feed.DefaultRuntimeSettings()
	rs.TimelineMaxItems = maxItems
	rs.TimelineTTL = ttl
	return feed.NewSettings(rs)
}

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
	return NewRedisTimelineStore(client, timelineSettings(3, time.Hour)), client
}

func TestRedisTimelineStore_SetReadTrimAndTTL(t *testing.T) {
	ctx := context.Background()
	store, client := newRedisTimelineStoreForTest(t)
	defer client.Close() //nolint:errcheck

	userID := uuid.New()
	entries := []feed.TimelineEntry{
		{PostID: uuid.New(), Score: 100},
		{PostID: uuid.New(), Score: 200},
		{PostID: uuid.New(), Score: 300},
		{PostID: uuid.New(), Score: 400},
	}

	if err := store.SetPostsBatch(ctx, userID, entries); err != nil {
		t.Fatalf("SetPostsBatch: %v", err)
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
		t.Fatalf("unexpected highest-score-first order after trim: %+v", page.Entries)
	}
	if page.Last == nil || page.Last.PostID != entries[1].PostID.String() {
		t.Fatalf("last position mismatch: %+v", page.Last)
	}
	if ttl := client.TTL(ctx, timelineKey(userID)).Val(); ttl <= 0 || ttl > time.Hour {
		t.Fatalf("ttl = %v, want within configured hour", ttl)
	}
}

func TestRedisTimelineStore_WritesVersionedV2Key(t *testing.T) {
	ctx := context.Background()
	store, client := newRedisTimelineStoreForTest(t)
	defer client.Close() //nolint:errcheck

	userID := uuid.New()
	if err := store.AddPost(ctx, userID, feed.TimelineEntry{PostID: uuid.New(), Score: 1}); err != nil {
		t.Fatalf("AddPost: %v", err)
	}
	// Pin the literal prefix so an accidental revert to the legacy unversioned
	// key (timestamp-scored, overlapping numeric range) fails loudly.
	if n := client.Exists(ctx, "feed:tl:v2:"+userID.String()).Val(); n != 1 {
		t.Fatalf("expected exactly key feed:tl:v2:%s, exists = %d", userID, n)
	}
	if n := client.Exists(ctx, "feed:tl:"+userID.String()).Val(); n != 0 {
		t.Fatal("legacy feed:tl: key must not be written")
	}
}

func TestRedisTimelineStore_AddPostKeepsExistingScore(t *testing.T) {
	ctx := context.Background()
	store, client := newRedisTimelineStoreForTest(t)
	defer client.Close() //nolint:errcheck

	userID := uuid.New()
	postID := uuid.New()
	if err := store.AddPost(ctx, userID, feed.TimelineEntry{PostID: postID, Score: 100}); err != nil {
		t.Fatalf("AddPost initial: %v", err)
	}
	// NX semantics: a late add (e.g. a fan-out event arriving after a re-rank)
	// must not change the stored score.
	if err := store.AddPost(ctx, userID, feed.TimelineEntry{PostID: postID, Score: 999}); err != nil {
		t.Fatalf("AddPost late duplicate: %v", err)
	}
	if got := int64(client.ZScore(ctx, timelineKey(userID), postID.String()).Val()); got != 100 {
		t.Fatalf("score after NX re-add = %d, want original 100", got)
	}
}

func TestRedisTimelineStore_SetPostsBatchOverwritesAndInserts(t *testing.T) {
	ctx := context.Background()
	store, client := newRedisTimelineStoreForTest(t)
	defer client.Close() //nolint:errcheck

	userID := uuid.New()
	existing := uuid.New()
	fresh := uuid.New()
	if err := store.AddPost(ctx, userID, feed.TimelineEntry{PostID: existing, Score: 100}); err != nil {
		t.Fatalf("AddPost: %v", err)
	}
	if err := store.SetPostsBatch(ctx, userID, []feed.TimelineEntry{
		{PostID: existing, Score: 300},
		{PostID: fresh, Score: 200},
	}); err != nil {
		t.Fatalf("SetPostsBatch: %v", err)
	}
	if got := int64(client.ZScore(ctx, timelineKey(userID), existing.String()).Val()); got != 300 {
		t.Fatalf("existing member score = %d, want overwritten 300", got)
	}
	if got := int64(client.ZScore(ctx, timelineKey(userID), fresh.String()).Val()); got != 200 {
		t.Fatalf("inserted member score = %d, want 200", got)
	}

	if err := store.SetPostsBatch(ctx, userID, nil); err != nil {
		t.Fatalf("SetPostsBatch empty must no-op: %v", err)
	}
}

func TestRedisTimelineStore_EqualScoreBlockPagination(t *testing.T) {
	ctx := context.Background()
	_, client := newRedisTimelineStoreForTest(t)
	defer client.Close() //nolint:errcheck
	// Block size must stay <= 2x the ReadPage limit: continuation over a bigger
	// equal-score block exceeds the fetch window and stalls (known, documented
	// limitation — see contracts/timeline-store.md).
	const score = int64(4200)
	userID := uuid.New()
	members := []feed.TimelineEntry{
		{PostID: uuid.New(), Score: score},
		{PostID: uuid.New(), Score: score},
		{PostID: uuid.New(), Score: score},
		{PostID: uuid.New(), Score: score},
	}
	// The shared helper store trims at 3 items; use one with room for 4.
	bigStore := NewRedisTimelineStore(client, timelineSettings(10, time.Hour))
	if err := bigStore.SetPostsBatch(ctx, userID, members); err != nil {
		t.Fatalf("SetPostsBatch: %v", err)
	}

	seen := make(map[uuid.UUID]bool, len(members))
	page1, err := bigStore.ReadPage(ctx, userID, nil, 2)
	if err != nil {
		t.Fatalf("ReadPage page1: %v", err)
	}
	if len(page1.Entries) != 2 || page1.Last == nil {
		t.Fatalf("page1 = %+v, want 2 entries and a continuation", page1)
	}
	for _, e := range page1.Entries {
		seen[e.PostID] = true
	}
	page2, err := bigStore.ReadPage(ctx, userID, page1.Last, 2)
	if err != nil {
		t.Fatalf("ReadPage page2: %v", err)
	}
	if len(page2.Entries) != 2 {
		t.Fatalf("page2 = %+v, want remaining 2 entries", page2)
	}
	for _, e := range page2.Entries {
		if seen[e.PostID] {
			t.Fatalf("duplicate entry across pages: %s", e.PostID)
		}
		seen[e.PostID] = true
	}
	if len(seen) != len(members) {
		t.Fatalf("paged %d distinct entries, want %d (no loss)", len(seen), len(members))
	}
}

func TestRedisTimelineStore_LegacyScalePositionReadsFromTop(t *testing.T) {
	ctx := context.Background()
	store, client := newRedisTimelineStoreForTest(t)
	defer client.Close() //nolint:errcheck

	userID := uuid.New()
	at := time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC)
	entries := []feed.TimelineEntry{
		{PostID: uuid.New(), Score: feed.PackTimelineScore(30, at)},
		{PostID: uuid.New(), Score: feed.PackTimelineScore(20, at)},
	}
	if err := store.SetPostsBatch(ctx, userID, entries); err != nil {
		t.Fatalf("SetPostsBatch: %v", err)
	}

	// A pre-migration cursor carries a UnixMicro-scale score (~1.7e15), above
	// any realistic packed score: the page must degrade to "read from top"
	// without error (accepted one-time deploy-window behavior).
	legacy := &feed.TimelinePosition{Score: time.Now().UTC().UnixMicro(), PostID: uuid.NewString()}
	page, err := store.ReadPage(ctx, userID, legacy, 10)
	if err != nil {
		t.Fatalf("ReadPage with legacy-scale position: %v", err)
	}
	if len(page.Entries) != 2 || page.Entries[0].PostID != entries[0].PostID {
		t.Fatalf("legacy-scale position page = %+v, want full page from top", page.Entries)
	}
}

func TestRedisTimelineStore_TieCursor(t *testing.T) {
	ctx := context.Background()
	store, client := newRedisTimelineStoreForTest(t)
	defer client.Close() //nolint:errcheck

	userID := uuid.New()
	const score = int64(30_000_000_000)
	low := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	high := uuid.MustParse("ffffffff-ffff-ffff-ffff-ffffffffffff")
	if err := store.SetPostsBatch(ctx, userID, []feed.TimelineEntry{
		{PostID: low, Score: score},
		{PostID: high, Score: score},
	}); err != nil {
		t.Fatalf("SetPostsBatch: %v", err)
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

// The retention limits are read per write, so an operator lowering them starts
// reclaiming memory on the next fanout rather than on the next restart. This is
// the property the store gained when maxItems and ttl stopped being constructor
// arguments, and it needs a real Redis to observe: the trim is a ZREMRANGEBYRANK
// in the write pipeline, not a value the store can be asked for.
func TestRedisTimelineStore_TrimBoundFollowsSettingsChange(t *testing.T) {
	ctx := context.Background()
	addr := os.Getenv("REDIS_TEST_ADDR")
	if addr == "" {
		t.Skip("REDIS_TEST_ADDR not set")
	}
	_, client := newRedisTimelineStoreForTest(t)
	defer client.Close() //nolint:errcheck

	settings := timelineSettings(5, time.Hour)
	store := NewRedisTimelineStore(client, settings)
	userID := uuid.New()

	entries := make([]feed.TimelineEntry, 5)
	for i := range entries {
		entries[i] = feed.TimelineEntry{PostID: uuid.New(), Score: int64(100 * (i + 1))}
	}
	if err := store.SetPostsBatch(ctx, userID, entries); err != nil {
		t.Fatalf("SetPostsBatch: %v", err)
	}
	page, err := store.ReadPage(ctx, userID, nil, 10)
	if err != nil {
		t.Fatalf("ReadPage: %v", err)
	}
	if len(page.Entries) != 5 {
		t.Fatalf("entries = %d, want 5 under the initial cap", len(page.Entries))
	}

	rs := feed.DefaultRuntimeSettings()
	rs.TimelineMaxItems = 2
	rs.TimelineTTL = time.Hour
	settings.Set(rs)

	// The next write must trim to the new bound — no new store, no restart.
	if err = store.AddPost(ctx, userID, feed.TimelineEntry{PostID: uuid.New(), Score: 600}); err != nil {
		t.Fatalf("AddPost after lowering the cap: %v", err)
	}
	page, err = store.ReadPage(ctx, userID, nil, 10)
	if err != nil {
		t.Fatalf("ReadPage after lowering the cap: %v", err)
	}
	if len(page.Entries) != 2 {
		t.Fatalf("entries = %d, want 2 — the store is still trimming to the cap it was built with", len(page.Entries))
	}

	// Trim() reads the same live bound rather than a captured one.
	rs.TimelineMaxItems = 1
	settings.Set(rs)
	if err = store.Trim(ctx, userID); err != nil {
		t.Fatalf("Trim: %v", err)
	}
	page, err = store.ReadPage(ctx, userID, nil, 10)
	if err != nil {
		t.Fatalf("ReadPage after Trim: %v", err)
	}
	if len(page.Entries) != 1 {
		t.Fatalf("entries after Trim = %d, want 1", len(page.Entries))
	}
}

// A TTL edit reaches keys written after it. Keys already in Redis keep the expiry
// they were last given — Redis holds one per key — so this asserts the write path,
// not a retroactive sweep.
func TestRedisTimelineStore_TTLFollowsSettingsChange(t *testing.T) {
	ctx := context.Background()
	if os.Getenv("REDIS_TEST_ADDR") == "" {
		t.Skip("REDIS_TEST_ADDR not set")
	}
	_, client := newRedisTimelineStoreForTest(t)
	defer client.Close() //nolint:errcheck

	settings := timelineSettings(10, 24*time.Hour)
	store := NewRedisTimelineStore(client, settings)
	userID := uuid.New()

	if err := store.AddPost(ctx, userID, feed.TimelineEntry{PostID: uuid.New(), Score: 100}); err != nil {
		t.Fatalf("AddPost: %v", err)
	}
	ttl, err := client.TTL(ctx, timelineKey(userID)).Result()
	if err != nil {
		t.Fatalf("TTL: %v", err)
	}
	if ttl <= 23*time.Hour {
		t.Fatalf("initial TTL = %v, want ~24h", ttl)
	}

	rs := feed.DefaultRuntimeSettings()
	rs.TimelineMaxItems = 10
	rs.TimelineTTL = time.Minute
	settings.Set(rs)

	if err = store.AddPost(ctx, userID, feed.TimelineEntry{PostID: uuid.New(), Score: 200}); err != nil {
		t.Fatalf("AddPost after lowering the TTL: %v", err)
	}
	ttl, err = client.TTL(ctx, timelineKey(userID)).Result()
	if err != nil {
		t.Fatalf("TTL after lowering: %v", err)
	}
	if ttl > time.Minute {
		t.Fatalf("TTL after lowering = %v, want at most 1m — the store is still using the TTL it was built with", ttl)
	}
}
