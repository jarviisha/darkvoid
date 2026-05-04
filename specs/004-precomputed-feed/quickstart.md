# Quickstart: Precomputed Feed Timeline

## Prerequisites

- PostgreSQL and Redis available locally.
- `.env` configured from `.env.example`.
- Redis enabled for timeline development.
- Codohue optional; feed must still work with `CODOHUE_ENABLED=false`.

## Configuration To Add Or Confirm

```text
REDIS_ENABLED=true

FEED_TIMELINE_ENABLED=false
FEED_TIMELINE_ROLLOUT_PERCENT=0
FEED_TIMELINE_MAX_ITEMS=1000
FEED_TIMELINE_TTL=168h
FEED_FANOUT_ENABLED=true
FEED_FANOUT_WORKERS=10
FEED_FANOUT_QUEUE_SIZE=10000
FEED_FANOUT_MAX_FOLLOWERS=10000
FEED_TIMELINE_REFRESH_ON_MISS=true
```

Exact names may be adjusted during implementation, but new variables must be documented in `.env.example`.

## Development Flow

1. Start dependencies:

   ```bash
   make docker-up
   ```

2. Run tests before changes to establish baseline:

   ```bash
   make test
   ```

3. Implement timeline store and cursor v2 behind disabled serving flag.

4. Run targeted feed tests:

   ```bash
   go test ./internal/feature/feed/...
   ```

5. Implement fanout hooks from post creation and follow mutations.

6. Run targeted post/user tests:

   ```bash
   go test ./internal/feature/post/... ./internal/feature/user/...
   ```

7. Enable timeline preparation but keep serving disabled:

   ```text
   FEED_FANOUT_ENABLED=true
   FEED_TIMELINE_ENABLED=false
   ```

8. Create posts from followed authors and verify prepared timeline entries are populated without changing `/feed` responses.

9. Enable limited timeline serving:

   ```text
   FEED_TIMELINE_ENABLED=true
   FEED_TIMELINE_ROLLOUT_PERCENT=5
   ```

10. Verify `/feed` behavior:

    - No cursor returns page 1 with `data`.
    - Valid v2 cursor returns the next page.
    - Old session-based cursor returns `400`.
    - Empty or missing timeline triggers lazy refresh/fallback.
    - Deleted/hidden/unfollowed posts are not returned.

11. Regenerate Swagger after handler contract changes:

    ```bash
    make swagger-generate
    ```

12. Run full verification:

    ```bash
    make test
    make lint
    ```

## Rollout Checklist

- Stage 1: Fanout enabled, timeline serving disabled (`FEED_TIMELINE_ENABLED=false`, `FEED_FANOUT_ENABLED=true`). Confirm fanout latency, queue depth, capped fanout count, and Redis memory.
- Stage 2: Timeline serving enabled for a limited audience (`FEED_TIMELINE_ENABLED=true`, `FEED_TIMELINE_ROLLOUT_PERCENT=5`). Confirm feed p95, timeline hit/miss rate, lazy refresh rate, cursor rejection rate, and fallback rate.
- Stage 3: Increase rollout percentage in small steps after p95, stale-filtered count, and fallback rate are stable.
- Stage 4: Timeline serving default (`FEED_TIMELINE_ROLLOUT_PERCENT=100`). Keep `FEED_TIMELINE_ENABLED=false` as the serving rollback switch.
- Stage 5: Stable observation period. Session cursor/cache code is removed; clients must keep using the v2 cursor contract.

## Rollback

Set timeline serving disabled while keeping feed access available through fallback behavior:

```text
FEED_TIMELINE_ENABLED=false
```

For a partial rollback without fully disabling the path, reduce:

```text
FEED_TIMELINE_ROLLOUT_PERCENT=0
```

Post creation and follow mutations must remain successful even if fanout workers are disabled or failing.
