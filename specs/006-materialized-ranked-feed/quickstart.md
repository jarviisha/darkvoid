# Quickstart: Materialized Ranked Feed — P1 Verification

## Order of work (Constitution II: characterize before refactoring)

1. **Characterization first** — before touching `getFeedFromTimeline`, add/verify a service
   test that pins today's timeline-path output order for a fixed set of posts (distinct
   like-counts and ages, timeline entries deliberately shuffled vs DB return order). This
   test documents that ordering currently comes from `rankCandidates`, and catches the
   `GetPostsByIDs`-does-not-preserve-order trap when the refactor lands.
2. Encoding: `PackTimelineScore` + tests (`timeline.go`, `timeline_test.go`); delete
   `TimelineScoreFromTime` and let the compiler enumerate every call site.
3. Store: `SetPostsBatch` in interface + both implementations + key prefix `feed:tl:v2`;
   tests per contracts/timeline-store.md.
4. Writers: refresher (Ranker + pack + `SetPostsBatch`), dispatcher (`ScorerConfig`,
   packed write-time score); update wiring in `internal/app/feed.go`.
5. Hot path: drop `rankCandidates` from `getFeedFromTimeline`, add explicit reorder to
   ZSET entry order; the characterization test from step 1 must still pass (same formula,
   now materialized — with entries scored by the refresher the visible order is preserved).
6. `docs.go` updates for `feed` and `feed/cache`.

## Test commands

```bash
# Feature-scoped, fast loop
make test-feature feature=feed

# Redis-backed store tests (skipped without this)
docker compose up -d redis            # or any local redis
REDIS_TEST_ADDR=localhost:6379 go test ./internal/feature/feed/cache/...

# Full gates before commit
make test
make lint
```

## Manual walkthrough (docker stack)

```bash
make docker-up
# .env: ensure REDIS_ENABLED=true. The timeline and fanout switches are no longer
#        environment variables — set them through the admin API instead:
#   curl -X PATCH -H "Authorization: Bearer $ADMIN_TOKEN" \
#        -H 'Content-Type: application/json' \
#        -d '{"timeline_enabled":true,"timeline_rollout_percent":100,"fanout_enabled":true}' \
#        localhost:8080/api/v1/admin/settings/feed

# 1. Log in as a user following at least one author; call feed once to trigger lazy refresh:
curl -s -H "Authorization: Bearer $TOKEN" 'localhost:8080/api/v1/feed' | jq '.data[].id'

# 2. Inspect the materialized scores:
redis-cli --scan --pattern 'feed:tl:v2:*'
redis-cli ZREVRANGE feed:tl:v2:<userID> 0 5 WITHSCORES
#    sanity: score>>32 ∈ [0, 1048575] (= rank×1000); score & 0xFFFFFFFF = seconds
#    since 2020-01-01. Example: default fan-out writes rank 30 → bucket 30000 →
#    packed ≈ 1.29e14. Any score ≥ 4.51e15 or with bucket > 1048575 is wrong.

# 3. Create a post from a followed author → verify it appears at the write-time bucket
#    (RecencyScale+RelationshipBonus = 30 by default → bucket 30000) and shows in feed
#    immediately, newest-first among same-bucket posts.

# 4. Paginate: request page 2 with next_cursor; verify no duplicates/gaps across the
#    equal-score block.

# 5. Legacy keys: confirm nothing writes feed:tl:<userID> (v1) anymore:
redis-cli --scan --pattern 'feed:tl:*' | grep -v ':v2:'   # only pre-existing, TTL-bound
```

## Acceptance checklist

- [x] `PackTimelineScore` guarantees 1–6 of contracts/timeline-score.md covered by unit tests
- [x] NX-vs-upsert behavior pinned by real-Redis tests (contracts/timeline-store.md tests 1–6)
- [x] Characterization test written BEFORE hot-path refactor, still green after
- [x] 005 cursor/handler contract tests pass **unmodified**
- [x] No new env vars; `.env.example` untouched
- [x] `docs.go` updated for packages whose responsibilities shifted
- [x] `make test` and `make lint` green
- [x] Timeline hot path no longer calls `RankPosts` (verify by reading the final diff, and
      optionally via a test asserting the service's Ranker is not invoked on the timeline path)
