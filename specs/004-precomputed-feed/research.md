# Research: Precomputed Feed Timeline

## Decision: Use Redis sorted sets as the primary prepared timeline store

**Rationale**: The target read path needs newest-first pagination over post IDs with low latency and bounded per-user storage. Sorted sets provide ordered range reads, idempotent insert by member, bounded trimming, and TTL support without adding PostgreSQL to the feed hot path. Prepared timelines store only post IDs and ordering scores, not rendered feed pages.

**Alternatives considered**:

- PostgreSQL `feed_timeline_items`: stronger durability, but puts another table in the feed read path and creates write amplification that is harder to trim at request latency targets.
- Existing `feed:session` state: helps continuation but does not remove request-time candidate recomputation.
- Fully dynamic feed: simplest storage model but preserves current duplicate/skip/session complexity.

## Decision: Order timeline entries by `created_at` microseconds with post ID tie-breaker in cursor and load filtering

**Rationale**: Current post IDs are UUIDs, not monotonic snowflakes. Using post ID as a sorted-set score would be incorrect. `created_at` microseconds preserves newest-first behavior and matches existing DB cursor semantics. The feed cursor includes the last timeline score and last post ID so the service can exclude the boundary item and handle same-score ties after loading a small over-fetch window.

**Alternatives considered**:

- Add snowflake or `feed_sequence BIGINT`: cleaner long-term ordering, but requires a wider post creation and migration change. It can be introduced later behind the same cursor abstraction.
- Unix nanoseconds as score: double precision in Redis sorted-set scores can lose integer precision at nanosecond scale. Microseconds fit safely for current dates.
- Timestamp-only cursor: simpler but ambiguous when multiple posts share the same microsecond.

## Decision: Cursor v2 is a single opaque base64 JSON token for all feed sources

**Rationale**: Clients should keep one `next_cursor`, while the server tracks positions for primary timeline, recommendation, trending, and fallback sources. The cursor is explicitly not compatible with v1 `FeedPageState` and contains no session ID, seen set, or pending item state.

**Alternatives considered**:

- Separate cursors per source: leaks feed assembly internals to clients and complicates client paging.
- Keep `feed:session`: reduces cursor size but preserves the old mutable continuation model the feature is removing.
- Version-change restart behavior: rejected by user direction; old cursors should be validation errors.

## Decision: Enforce deleted/hidden/unfollowed correctness at response time; clean stale prepared entries lazily

**Rationale**: Eager deletion from all follower timelines is expensive, especially for visibility changes, deletes, and unfollows. The service already batch-loads authoritative posts and following state before returning a response, so it can filter invalid items immediately. Stale prepared IDs are acceptable only if they are not returned to users.

**Alternatives considered**:

- Eager `ZREM` across affected timelines: strict storage correctness but high fanout and scan costs for hot authors and unfollows.
- TTL-only cleanup with no read filter: lower cost but violates user-visible correctness.

## Decision: Lazy timeline refresh on feed request is required; background refresh is optional

**Rationale**: Lazy refresh guarantees recovery from Redis eviction, missed in-process fanout, and users who were not active during backfill. It also avoids requiring a separate scheduler for MVP. Background refresh may be added for active users or rollout backfill but is not required for correctness.

**Alternatives considered**:

- Background-only backfill: can leave long-tail users empty if a job fails or is delayed.
- No refresh path: makes Redis misses user-visible failures or empty feeds.

## Decision: In-process fanout dispatcher for MVP with bounded queue and non-fatal publishing

**Rationale**: Current scale target is <=100k users, and the spec accepts lazy refresh for missed fanout. An in-process dispatcher keeps operational complexity low and can be wired through existing app-layer dependency injection. Post creation must not fail because feed propagation fails.

**Alternatives considered**:

- Persistent queue now: stronger delivery guarantees, but introduces new infrastructure and task semantics before there is evidence the in-process worker is insufficient.
- Synchronous fanout in post creation: simple but risks post creation latency and violates the non-fatal propagation requirement.

## Decision: Cap timeline size at 1,000 primary items and retention at 7 days

**Rationale**: This matches the spec and capacity model. At 100k users, the upper bound is 100M prepared entries, storing only post IDs and scores. Redis memory must be monitored and eviction is recoverable through lazy refresh.

**Alternatives considered**:

- 200 items: lower memory but more frequent fallback refresh and weaker pagination depth.
- Unlimited timelines: violates FR-011 and creates unacceptable memory risk.

## Decision: Roll out in four controlled stages plus cleanup

**Rationale**: The feature changes feed serving and client cursor contracts. A staged rollout allows measuring fanout latency, timeline freshness, fallback rate, and cursor errors before old state is removed.

**Alternatives considered**:

- Single cutover: fastest but unsafe for a feed-critical endpoint.
- Long dual-stack compatibility: conflicts with the user's direction to avoid old client compatibility and preserve a simpler final design.

## Decision: Do not change `/discover` pagination

**Rationale**: The feature scope is personalized feed refactor. Discovery already uses a chronological cursor and must remain stable as a separate endpoint. Feed fallback may use discovery-like source positions inside feed cursor v2, but `/discover` contract remains unchanged.

**Alternatives considered**:

- Fold discovery into prepared timelines: expands scope and changes a public endpoint unrelated to the feed performance problem.
