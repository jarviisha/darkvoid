# Research: Materialized Ranked Feed — P1

All decisions below were settled in design review round 1 (2026-07-24) against the live
codebase; design.md (DRAFT v2) §4, §5, §11 is the authoritative write-up. This file records
them in Decision / Rationale / Alternatives form for the plan workflow. No NEEDS
CLARIFICATION items remain for P1.

## R1. Timeline ZSET score encoding

- **Decision**: `PackTimelineScore(rankScore float64, createdAt time.Time) int64` =
  `clamp(int64(rankScore*1000), 0, 2^20-1) << 32 | clamp(createdAt.Unix()-epoch2020, 0, 2^32-1)`.
  Primary order: rank bucket (0.001 resolution). Secondary: createdAt seconds. Final
  tie-break: postID (already implemented in `ReadPage`).
- **Rationale**: The v1 draft (`int64(rankScore*1e6)`) has a fatal tie block: every fan-out
  write scores the constant `RecencyScale + RelationshipBonus` (engagement=0, age≈0), so all
  fresh posts tie and order falls to postID — which is `gen_random_uuid()` (UUIDv4, random).
  Packing createdAt into the low bits restores newest-first inside equal-rank blocks. Max
  packed value ≈ 4.5e15 < 2^53, so the int64 survives Redis's float64 ZSET scores exactly,
  and `TimelinePosition.Score`/cursor `tl_score` keep their `int64` type — the 005 cursor
  contract is untouched.
- **Alternatives considered**:
  - *Plain `rank*1e6` scaling* (v1 draft) — rejected: constant-score tie block above; also
    sat closer to precision-loss anxiety that packing eliminates by construction.
  - *Switch ZSET to native float scores* — rejected: forces `TimelinePosition.Score` and the
    wire cursor `tl_score` to change type; contract churn for zero ordering benefit.
  - *Rely on UUIDv7 post IDs for time-ordered tie-break* — rejected: posts use DB-side
    `gen_random_uuid()`; changing ID generation is a cross-cutting change far beyond feed.

## R2. Score overwrite path for the refresher / re-rank job

- **Decision**: Add `SetPostsBatch(ctx, userID, entries)` to `TimelineStore` — plain `ZADD`
  upsert + trim + expire, mirroring `AddPostsBatch` minus the `NX` flag. Refresher switches
  to it. `AddPost`/`AddPostsBatch` keep `NX`.
- **Rationale**: `AddPostsBatch` uses `ZAddArgs{NX: true}` — it never updates an existing
  member's score, so the v1 draft's "overwrite via AddPostsBatch" wrote into the void. NX is
  still *correct* for fan-out: a post-created event that lands after a refresh must not
  clobber a fresher refreshed score with the write-time constant. Two methods, two semantics.
- **Alternatives considered**:
  - *Drop NX from `AddPostsBatch`* — rejected: loses the late-event protection above.
  - *DEL + rebuild the key in the refresher* — rejected: races concurrent fan-out (a post
    written between DEL and ZADD is silently lost); stale members are already handled by
    follow/unfollow removal events plus `isEligibleTimelinePost` read-side filtering.

## R3. Migration of score semantics

- **Decision**: Bump the Redis key prefix `feed:tl` → `feed:tl:v2`. v2 timelines build via
  the existing lazy `RefreshOnMiss`; v1 keys expire via their TTL (default 7d). No cursor
  version field. No Postgres changes.
- **Rationale**: Old scores (UnixMicro, ~1.7e15) and new packed scores (max ≈ 4.5e15)
  overlap numerically — they cannot be told apart by value, and NX means refresh could never
  repair old entries in place. A fresh key sidesteps both. In-flight old cursors read on v2
  behave as "start from top" exactly once during the deploy window — acceptable under the
  soft-cursor decision (R4). Rollback = revert; v1 keys likely still alive under TTL,
  otherwise lazily rebuilt.
- **Alternatives considered**:
  - *FLUSH/rewrite in place on deploy* — rejected: needs a coordinated migration step for
    derived data that rebuilds itself for free.
  - *Cursor version field* — rejected: permanent contract complexity to smooth a one-time,
    self-healing wrinkle.

## R4. Cursor stability under re-ranking (design Q1)

- **Decision**: Accept a "soft" cursor — scores may change between page fetches; items can
  shift across page boundaries. No session snapshotting.
- **Rationale**: Snapshotting the score into the cursor stabilizes nothing: `ReadPage`
  queries the ZSET by *current* score. True stability requires a per-session copy of the
  timeline — Redis cost plus cleanup machinery, unjustified for a social feed. The existing
  `(score, postID)` continuation filter already prevents intra-page loops; in P1 the only
  score rewrites come from lazy refresh on empty reads, so cross-page drift is rare by
  construction (and in later phases, re-rank cadence ≫ scroll-session length).
- **Alternatives considered**: score snapshot in cursor (stabilizes nothing — see above);
  per-session timeline snapshot (cost/complexity).

## R5. CF blend formula (design Q2 — recorded for P2, not implemented in P1)

- **Decision**: `final = local + FEED_RANK_CF_WEIGHT × cf` (additive, W default 1.0).
- **Rationale**: local carries the recency floor; full CF replacement buries new posts (CF
  has no signal for them). Keeps the "local is the floor, codohue is the overlay" principle.
  CF score scale is unsurveyed — W must be tunable with metrics before tuning (P4).
- **Alternatives considered**: CF replaces local when present — rejected (recency loss).

## R6. Fan-out write-time score

- **Decision**: `EmitPostCreated` writes `PackTimelineScore(RecencyScale + RelationshipBonus,
  createdAt)`. The dispatcher receives `ScorerConfig` (or the precomputed constant) at
  construction in `internal/app/feed.go`.
- **Rationale**: For a brand-new post the local formula degenerates to exactly
  `RecencyScale + RelationshipBonus` (likes=0 → engagement 0; age≈0 → recency = scale;
  author is followed by every fan-out recipient by definition). Using the constant openly is
  honest and avoids pretending to "score" — the createdAt component carries the ordering.
  Relative behavior vs refreshed posts is intended ranked-feed behavior: fresh beats decayed
  zero-like posts (30 > ~13.85 at 2h), heavily-liked older posts beat fresh (~53 for 50
  likes) — by design.
- **Alternatives considered**: calling `Scorer.Score` on a synthetic post at emit time —
  rejected as ceremony that computes the same constant with more coupling.

## R7. Hot-path ordering after dropping `rankCandidates`

- **Decision**: `getFeedFromTimeline` must explicitly reorder hydrated posts to the ZSET
  entry order (index map over `page.Entries`) before eligibility filtering and enrichment.
  The mixed/discover path keeps `rankCandidates` — P1 does not touch it.
- **Rationale**: `GetPostsByIDs` (`WHERE id = ANY(...)`) does not guarantee result order;
  today the hot path is accidentally correct because `rankCandidates` re-sorts. Removing it
  without an explicit sort would return DB-arbitrary order. This is the single subtlest step
  of P1 and gets a characterization test *before* the refactor (Constitution II).
- **Alternatives considered**: `ORDER BY array_position(...)` in SQL — rejected: touches
  hand-patched generated query files for a problem trivially solved in Go at page size.

## R8. Config surface in P1

- **Decision**: No new env vars in P1. `FEED_RANK_SOURCE`, `FEED_RANK_CF_WEIGHT`,
  `FEED_RERANK_*`, `CODOHUE_RANK_TIMEOUT` all arrive with P2/P3. Existing
  `FEED_TIMELINE_*`/`FEED_FANOUT_*` gates keep guarding the timeline path.
- **Rationale**: P1 is behavior-preserving local-only; a config knob with one valid value is
  noise (Constitution I: no premature flags). `.env.example` stays untouched, satisfying the
  same-commit rule vacuously.
- **Alternatives considered**: adding `FEED_RANK_SOURCE=local` early — rejected as a dead
  flag until P2 exists.
