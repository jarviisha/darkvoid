# Contract: Packed Timeline Score

Producers and consumers of `TimelineEntry.Score` / `TimelinePosition.Score` MUST honor this
encoding. It is internal to the feed context (the wire cursor treats the value as opaque),
but background writers and the read path must agree on it exactly.

## Encoding

```go
const (
    timelineEpoch     = 1577836800 // 2020-01-01T00:00:00Z
    timelineRankMax   = 1<<20 - 1
    timelineRankScale = 1000
)

packed = clamp(int64(math.Round(rankScore*timelineRankScale)), 0, timelineRankMax) << 32
       | clamp(createdAt.UTC().Unix()-timelineEpoch, 0, 1<<32-1)
```

`math.Round` on the bucket is load-bearing: plain truncation puts e.g. `10.001` into
bucket `10000` (`10.001×1000` floats to `10000.999…8`), silently breaking guarantee 3.

## Guarantees (MUST hold, tested)

1. **Non-negative**: `packed ≥ 0` for all inputs, including negative rank scores and
   pre-2020 timestamps (both clamp to 0). Keeps 005 cursor validation (`tl_score ≥ 0`) safe.
2. **Float64-exact**: `packed ≤ (2^20-1)<<32 + 2^32-1 < 2^53`. Round-trip
   `int64 → float64 (Redis ZSET) → int64` is lossless.
3. **Rank-major ordering**: `rankA ≥ rankB + 0.001` ⇒ `packedA > packedB` regardless of
   timestamps.
4. **Time-minor ordering**: equal rank buckets ⇒ strictly newer `createdAt` (second
   granularity) yields strictly greater packed value.
5. **Sub-resolution ties**: rank scores differing by < 0.001 MAY share a bucket — then time
   ordering (guarantee 4) applies. This is intended behavior, not a defect.
6. **Timezone-independent**: input times are normalized to UTC before packing.

## Producer rules

- Fan-out (`EmitPostCreated`): `rankScore = ScorerConfig.RecencyScale +
  ScorerConfig.RelationshipBonus` (the exact degenerate value of the local formula for a
  fresh post), `createdAt` = the post's creation time — NOT `time.Now()`.
- Refresher: `rankScore` = `LocalRanker.RankPosts` output for that post; `createdAt` = the
  post's creation time.
- No other producer may exist in P1. Codohue-blended scores (P2) reuse this same encoding.

## Consumer rules

- For ordering and pagination, the read path only compares packed values (the ZSET does
  this) and threads them through `TimelinePosition`/cursor opaquely — ordering logic MUST
  NOT branch on decoded components.
- Decoding the RANK component via `UnpackTimelineRank` is allowed for display and
  observability only: the serialized `FeedItem.Score` field is populated this way
  (amended post-implementation — the `score` field in the client response made this
  necessary; see contracts/feed-api.md).
- Nothing may reconstruct `createdAt` from a packed score for display or logic (seconds
  truncation + clamping make it lossy by design).

## Explicit non-goals

- No version bits inside the score. Score-semantics migration is handled by the Redis key
  version (`feed:tl:v2`), not by the value.
- No negative-score range reserved. Rank floor is 0.
