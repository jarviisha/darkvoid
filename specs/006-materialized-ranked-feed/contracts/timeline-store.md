# Contract: TimelineStore

Interface owned by `internal/feature/feed` (`timeline.go`), implemented by
`cache.RedisTimelineStore` and `cache.NopTimelineStore`. P1 adds one method and freezes the
semantics of the rest.

## Methods

### `AddPost(ctx, userID, entry)` / `AddPostsBatch(ctx, userID, entries)` — NX add

- MUST NOT modify the score of an existing member (`ZADD NX`).
- Rationale (load-bearing): a post-created event processed *after* a refresh has already
  scored that post must not replace the refreshed score with the write-time constant.
- Callers: fan-out worker (`AddPost`).
- After adding: trim to `maxItems` by rank (lowest scores evicted), refresh key TTL.

### `SetPostsBatch(ctx, userID, entries)` — upsert (NEW in P1)

- MUST insert new members and MUST overwrite scores of existing members (plain `ZADD`).
- Caller: `PreparedTimelineRefresher` (and P3 re-rank triggers later).
- MUST NOT delete members absent from `entries` (no DEL+rebuild — it races concurrent
  fan-out; stale members are handled by removal events + read-side eligibility filtering).
- After setting: same trim + TTL refresh as adds.
- Empty `entries` is a no-op returning nil.

### `ReadPage(ctx, userID, after, limit)` — frozen semantics

- Descending by score. With `after` set: return entries strictly "after" the position in
  `(score desc, postID)` order — skip `score > after.Score`, and at `score == after.Score`
  skip `postID >= after.PostID`. Unchanged from current implementation; the 005 cursor
  contract depends on it.
- Returns at most `limit` entries plus `Last` position.

### `Trim` / `RemovePostBestEffort` — unchanged

## Key schema (Redis implementation)

- P1 key: `feed:tl:v2:{userID}`. The v1 prefix `feed:tl:` MUST NOT be written or read after
  P1 ships; v1 keys expire via their TTL.
- Trim bound `maxItems` and TTL come from `FEED_TIMELINE_MAX_ITEMS` / `FEED_TIMELINE_TTL`
  as today.

## Nop implementation

- `SetPostsBatch` returns nil, like every other method. `ReadPage` keeps returning an empty
  page (timeline path effectively disabled without Redis).

## Required tests (real Redis via `REDIS_TEST_ADDR`, DB 15)

1. `AddPostsBatch` then `AddPostsBatch` same member, new score → score unchanged (NX
   characterization — this pins the bug the v1 design missed).
2. `AddPostsBatch` then `SetPostsBatch` same member, new score → score overwritten.
3. `SetPostsBatch` inserts members that did not exist.
4. `SetPostsBatch` respects trim (`maxItems`) and sets TTL.
5. Keys are written under `feed:tl:v2:` (assert exact key, so an accidental prefix revert
   fails loudly).
6. `ReadPage` continuation across equal-score members (the fan-out constant-bucket case)
   pages without loss or duplication.
