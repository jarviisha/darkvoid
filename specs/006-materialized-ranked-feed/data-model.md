# Data Model: Materialized Ranked Feed — P1

No Postgres schema changes. Everything below is Redis-derived data and in-process types
inside the `feed` bounded context.

## Packed timeline score (the core change)

An `int64` that is simultaneously the Redis ZSET score (as float64 — exact, see bounds) and
the cursor continuation value (`tl_score`).

```text
 bit 63 ......... 52 51 ................. 32 31 ...................... 0
┌───────────────────┬───────────────────────┬──────────────────────────┐
│ 0 (unused, ≥ 0)   │ rank bucket (20 bit)  │ createdAt seconds (32bit)│
└───────────────────┴───────────────────────┴──────────────────────────┘
```

| Component | Definition | Range | Notes |
|---|---|---|---|
| rank bucket | `int64(rankScore × 1000)`, clamped to `[0, 2^20-1]` | 0 … 1,048,575 (= rank 0 … 1048.575) | 0.001 rank resolution; local formula tops out < 200 in practice |
| ts component | `createdAt.UTC().Unix() − 1577836800`, clamped to `[0, 2^32-1]` | 2020-01-01 … ~2156 | seconds precision is enough below 0.001-rank ties |

**Invariants**

- Packed value ∈ `[0, (2^20-1)<<32 + 2^32-1]` ≈ 4.5036e15 < 2^53 → survives Redis float64
  ZSET scores without precision loss, round-trips through `int64(row.Score)` exactly.
- Always ≥ 0 → existing `FeedCursor` validation (`tl_score >= 0`) holds unchanged.
- Ordering: higher rank bucket wins; equal bucket → newer `createdAt` wins; equal both →
  `ReadPage`'s existing postID comparison breaks the tie (stable, arbitrary).
- Monotonic in `createdAt` for fixed rank — the fan-out constant-score block sorts
  newest-first.

**Producers** (the only two places a packed score is created):

| Producer | rankScore input | When |
|---|---|---|
| `EmitPostCreated` (dispatcher) | constant `RecencyScale + RelationshipBonus` from `ScorerConfig` | post creation → fan-out |
| `PreparedTimelineRefresher.refreshOne` | `LocalRanker.RankPosts` output per post | timeline miss (lazy refresh); later phases add periodic/event triggers |

## Changed / touched types

### `feed.TimelineEntry` (unchanged shape, changed meaning)

| Field | Type | P0 meaning (today) | P1 meaning |
|---|---|---|---|
| `PostID` | `uuid.UUID` | member | member (unchanged) |
| `Score` | `int64` | `UnixMicro(createdAt)` | packed rank score |

`TimelineScoreFromTime` is deleted; `PackTimelineScore` replaces it. All call sites
(dispatcher, refresher, tests) migrate in the same change — no dual-meaning window in code.

### `feed.TimelineStore` (interface — one added method)

| Method | ZADD mode | Caller | Semantics |
|---|---|---|---|
| `AddPost` | NX | fan-out worker | insert-if-absent; never downgrades a refreshed score |
| `SetPostsBatch` **(new)** | plain (upsert) | refresher | overwrite scores; the materialized re-rank write |
| `ReadPage` | — | hot path | unchanged `(score, postID)` continuation |
| `Trim`, `RemovePostBestEffort` | — | maintenance/events | unchanged |

`AddPostsBatch` was removed from the interface (analysis U1): no production caller after
the refresher switched to `SetPostsBatch`. The Redis implementation shares a private
`writeBatch(nx bool)` helper between `AddPost` and `SetPostsBatch`.

Implementations: `RedisTimelineStore` (real), `NopTimelineStore` (Redis disabled — returns
nil). Both gain `SetPostsBatch`.

### `feed.PreparedTimelineRefresher` (new dependency)

Gains a `feed.Ranker` (wired with the existing `LocalRanker` in `internal/app/feed.go`).
`refreshOne` pipeline: fetch following posts → build followingSet from `authorIDs` (which
already includes self) → `RankPosts` → pack each score with the post's `createdAt` →
`SetPostsBatch`.

### `feed.EventDispatcher` (new dependency)

Gains `ScorerConfig` (or the precomputed write-time constant) at construction.
`EmitPostCreated` sets `Event.Score = PackTimelineScore(RecencyScale+RelationshipBonus,
createdAt)`. `FanoutWorker` continues to write `Event.Score` verbatim via `AddPost`.

### `feedservice.getFeedFromTimeline` (behavior change, no type change)

Drops the `rankCandidates` call. New explicit step: reorder hydrated posts by ZSET entry
index (`map[postID]int` built from `page.Entries`) before eligibility filter → enrich →
page-cut. `FeedItem.Source` remains `SourceFollowing`. The mixed/discover path keeps
`rankCandidates` untouched in P1.

## Redis key schema

| Key | Type | P0 | P1 |
|---|---|---|---|
| `feed:tl:{userID}` | ZSET | member=postID, score=UnixMicro | **dead** — expires via TTL (default 7d), never written again |
| `feed:tl:v2:{userID}` | ZSET | — | member=postID, score=packed rank score; same maxItems trim + TTL as before |
| `following:ids:{userID}`, `trending:posts` | — | unchanged | unchanged |

## Score lifecycle (state view)

```text
 post created ──fan-out──▶ [write-time score]      constant bucket (RecencyScale+Bonus),
                              │                     ordered by createdAt within the block
                              │ lazy refresh (P1) / periodic re-rank (P3)
                              ▼
                          [refreshed score]        LocalRanker bucket at refresh time
                              │                     (engagement + decayed recency + bonus)
                              │ every later refresh (SetPostsBatch upsert)
                              ▼
                          [refreshed score']       overwritten; NX adds cannot regress it
```

Removal paths unchanged: unfollow/visibility/delete events → `RemovePostBestEffort`;
read-side `isEligibleTimelinePost` filters anything the events missed.

## Validation rules

- `PackTimelineScore` clamps rank to `[0, 2^20-1]` and ts to `[0, 2^32-1]` — never
  panics, never returns negative.
- `FeedCursor` validation is intentionally untouched (`tl_score ≥ 0`, `tl_post_id` must
  parse as UUID when score present) — contract-frozen by 005.
- `ReadPage` continuation filter (`score > after.Score` skip; equal-score postID
  comparison) is intentionally untouched — contract-frozen in contracts/timeline-store.md.
