# Contract: /feed API — Invariance Statement

P1 makes **no client-visible contract change**. This file exists so the invariance is
explicit, reviewed, and testable rather than assumed.

## What MUST NOT change

- **Envelope**: success `{"data": ..., "next_cursor": ...}`, error envelope and status
  codes — exactly as specified in `specs/005-feed-cursor-contract/contracts/feed.md`.
- **Cursor shape**: the opaque no-version cursor from 005 (`tl_score`, `tl_post_id`,
  `tl_user`, recommendation/trending continuation fields). `tl_score` remains an opaque
  non-negative int64; clients never interpreted it, and its internal meaning change
  (UnixMicro → packed rank score) is invisible by construction.
- **Cursor validation**: rejection rules for obsolete/invalid cursors, `tl_score ≥ 0`,
  `tl_post_id` UUID parsing — untouched.
- **`/discover`**: untouched (out of P1 scope per design.md §2).
- **Swagger**: no handler comment changes expected → `docs/swagger.json` unchanged. If any
  wording does change, `make swagger-generate` output ships in the same commit.

## What changes behind the contract

- Ordering within a timeline-served page now comes from the materialized ZSET score
  (write-time constant or refreshed local score) instead of request-time re-scoring. Under
  local-only P1 both orders derive from the same formula; drift between them is bounded by
  refresh staleness, which the design accepts (soft feed, design.md §11/Q1).
- The `score` field VALUE on timeline-served items is now the unpacked materialized rank
  (`rank bucket / 1000` via `UnpackTimelineRank` — e.g. a fresh fan-out post serves
  exactly `30.0` under default config) instead of a per-request realtime computation.
  Field type and presence are unchanged; only the value source is. Mixed/discover pages
  keep realtime scores. Clients were never given value semantics for `score`, so this is
  documented for transparency, not as a breaking change.

## One-time deploy-window behavior (accepted, documented)

- A cursor issued before the P1 deploy carries a v1-scale `tl_score` (~1.7e15). Read against
  the v2 timeline it filters out nothing (all packed scores are smaller), so the client
  effectively restarts from the top of the feed once. No error is returned; pagination
  proceeds normally afterward. This is the accepted trade-off from design.md §5 — do NOT
  add cursor versioning to "fix" it.

## Verification

- Existing 005 cursor/handler/service contract tests MUST pass **unmodified** after P1.
  Any needed edit to those tests is a red flag that the client contract drifted — stop and
  re-review instead of editing the test.
