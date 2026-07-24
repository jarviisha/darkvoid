# Tasks: Materialized Ranked Feed — Phase P1 (local-only)

**Input**: Design documents from `/specs/006-materialized-ranked-feed/`
**Prerequisites**: plan.md, design.md (v2 — replaces spec.md for this feature), research.md, data-model.md, contracts/, quickstart.md

**Tests**: INCLUDED — not optional here. Constitution II mandates a test for every service/handler logic change and a characterization test before refactoring legacy code; contracts/timeline-store.md and contracts/timeline-score.md enumerate required test cases explicitly.

**Organization**: No spec.md exists; "user stories" are the two independently shippable increments from design.md §9/P1, in dependency order:

- **US1 — Background writers materialize rank scores**: fan-out + refresher write packed local rank scores into `feed:tl:v2`. Shippable alone with **zero user-visible change** (hot path still re-ranks realtime and ignores ZSET order beyond membership).
- **US2 — Hot path serves the materialized order**: `getFeedFromTimeline` stops re-ranking and trusts ZSET order. Delivers the actual latency win. Depends on US1.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies on incomplete tasks)
- **[Story]**: US1 or US2 (user story phases only)

## Phase 1: Setup

**Purpose**: Confirm a green baseline so refactor regressions are attributable.

- [X] T001 Verify baseline on branch `006-materialized-ranked-feed`: run `make test` and `make lint`; both must be green before any change. Optionally start Redis and confirm the store test harness runs: `REDIS_TEST_ADDR=localhost:6379 go test ./internal/feature/feed/cache/...` (note: the shared docker Redis requires AUTH which the harness does not support — tests run against an ephemeral `redis:alpine` on port 16379 instead)

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Characterization guard + the two primitives every story needs (score encoding, store upsert). No story work until this phase is done.

**⚠️ Compile note**: T004–T006 change one interface and its two implementations — the repo only compiles when all three land. Execute sequentially, commit as one unit.

- [X] T002 [P] Characterization test in `internal/feature/feed/service/feed_service_test.go`: pin the CURRENT timeline-path output order (`TestGetFeed_TimelineOrderCharacterization`). IMPLEMENTATION FINDING: the timeline path today does NOT sort at all — `rankCandidates` only computes display scores and `sortFeedItems` runs only on the mixed path, so today's order = `GetPostsByIDs` return order (a latent ordering bug US2 fixes). The characterization therefore pins the stable case (hydrate order == entry order, rank-aligned fixture) and the shuffled-hydrate assertion moved to the US2 behavior test (T015).
- [X] T003 [P] Encoding unit tests in `internal/feature/feed/timeline_test.go` for `PackTimelineScore`, covering all six guarantees of `contracts/timeline-score.md` + `UnpackTimelineRank` round-trip. Existing `TimelineScoreFromTime` tests kept until T013.
- [X] T004 Implemented `PackTimelineScore` (with `math.Round` on the rank bucket so the 0.001-step guarantee survives float truncation) + `UnpackTimelineRank` (needed because `FeedItem.Score` is serialized to clients — see T016) + constants in `internal/feature/feed/timeline.go`; `TimelineStore` interface gains `SetPostsBatch` and DROPS `AddPostsBatch` (analysis finding U1 — no production caller post-refactor; refresher call mechanically switched to `SetPostsBatch` in this same compile unit, semantics unchanged until T011).
- [X] T005 `SetPostsBatch` no-op replaces `AddPostsBatch` in `internal/feature/feed/cache/nop_timeline_store.go` + conformance test updated.
- [X] T006 `internal/feature/feed/cache/redis_timeline_store.go`: private `writeBatch(nx bool)` shared by `AddPost` (NX) and `SetPostsBatch` (upsert); `timelineKeyPrefix` bumped to `feed:tl:v2`.
- [X] T007 Real-Redis tests per contracts/timeline-store.md required tests 1–7 (NX keeps score; Set overwrites+inserts+empty no-op; trim+TTL; v2 key literal + legacy key absence; equal-score block ≤ 2×limit pagination; legacy-scale position reads from top). 8/8 pass against `redis:alpine`.

**Checkpoint**: Encoding + store primitives exist and are pinned by tests; full suite green (writers still write old-style scores — harmless, everything ships as one PR).

---

## Phase 3: User Story 1 — Background writers materialize rank scores (Priority: P1) 🎯 MVP

**Goal**: Dispatcher fan-out and refresher write packed local rank scores into `feed:tl:v2`; hot path untouched, so user-visible behavior is provably unchanged.

**Independent Test**: `make test-feature feature=feed` green with updated writer tests; full `make test` green with NO service/handler test edits (proves no visible change); optional: `redis-cli ZREVRANGE feed:tl:v2:<uid> 0 5 WITHSCORES` shows packed scores (`score>>32` = rank×1000, low 32 bits = seconds since 2020).

- [X] T008 [P] [US1] Writer tests (write first, fail) in `internal/feature/feed/dispatcher_test.go`: `EmitPostCreated` sets `Event.Score = PackTimelineScore(cfg.RecencyScale+cfg.RelationshipBonus, createdAt)` — use `feed.DefaultScorerConfig()` (constant 30 → bucket 30000) and the post's createdAt, not time.Now().
- [X] T009 [P] [US1] Refresher tests (write first, fail) in `internal/feature/feed/refresher_test.go`: fake TimelineStore records method calls — refresher must call `SetPostsBatch` (NOT `AddPostsBatch`); entries carry `PackTimelineScore(localScore, post.CreatedAt)` where localScore comes from the injected Ranker; followingSet passed to the Ranker is built from authorIDs (following + self).
- [X] T010 [P] [US1] Implement in `internal/feature/feed/dispatcher.go`: `NewEventDispatcher` accepts `ScorerConfig` (store the precomputed write-time constant); `EmitPostCreated` packs it with `createdAt`. T008 green.
- [X] T011 [P] [US1] Implement in `internal/feature/feed/refresher.go`: `NewPreparedTimelineRefresher` accepts a `feed.Ranker`; `refreshOne` builds followingSet from authorIDs, calls `RankPosts`, packs each score with that post's CreatedAt, writes via `SetPostsBatch`. T009 green.
- [X] T012 [US1] Update wiring in `internal/app/feed.go`: create one shared `feed.DefaultScorerConfig()` value and one `LocalRanker`; pass config into `NewEventDispatcher` and the ranker into `NewPreparedTimelineRefresher` (reuse the instance already given to `NewFeedService`).
- [X] T013 [US1] Delete `TimelineScoreFromTime` from `internal/feature/feed/timeline.go`; compiler-sweep every remaining reference: drop its tests in `internal/feature/feed/timeline_test.go`, migrate helpers in `internal/feature/feed/cache/redis_timeline_store_test.go` and `internal/feature/feed/service/feed_service_test.go` to `PackTimelineScore` (service fixtures: pack a plausible rank with each post's CreatedAt so relative order is explicit in the fixture).
- [X] T014 [US1] Gate: `make test-feature feature=feed`, then full `make test` — green with zero edits to service/handler behavior tests (T002 characterization untouched and passing: hot path still re-ranks realtime).

**Checkpoint**: Shippable increment — materialized rank scores in Redis, identical feed output.

---

## Phase 4: User Story 2 — Hot path serves the materialized order (Priority: P2)

**Goal**: `getFeedFromTimeline` stops calling `rankCandidates`; page order comes from the ZSET, posts reordered explicitly after hydrate. Mixed/discover paths untouched.

**Independent Test**: T002 characterization still green UNMODIFIED; new ZSET-order test proves DB return order no longer leaks through; 005 contract tests (`cursor_test.go`, `handler/feed_handler_test.go`) pass without edits; reading the diff confirms no `RankPosts` call remains on the timeline path.

- [X] T015 [US2] Service tests (write first, fail) in `internal/feature/feed/service/feed_service_test.go`: timeline page preserves ZSET entry order when the fake postReader returns posts shuffled (this is the trap — `GetPostsByIDs` does not preserve order, `rankCandidates` currently masks it); eligibility filtering, `SourceFollowing`, isLiked/isFollowing enrichment, page-cut, and next-cursor-from-last-shown-entry all preserved.
- [X] T016 [US2] Implement in `internal/feature/feed/service/feed_service.go`: in `getFeedFromTimeline` remove the `rankCandidates`/`postsToTimelineCandidates` step; build `map[postID]int` from `page.Entries` order and reorder hydrated posts by it; keep `isEligibleTimelinePost`, `CountStaleFiltered`, enrichment, page-size cut, cursor construction. DO NOT touch the mixed/discover path (`rankCandidates` at its other call site stays).
- [X] T017 [US2] Contract invariance gate: run 005 tests — `go test ./internal/feature/feed/... -run 'Cursor|Handler'` plus full `make test`. Per contracts/feed-api.md: if any 005 cursor/handler test needs editing, STOP — the client contract drifted; re-review instead of editing the test. T002 characterization must also pass unmodified.

**Checkpoint**: Hot path is read → hydrate → reorder → enrich → return. P1 architecture complete.

---

## Phase 5: Polish & Cross-Cutting Concerns

- [ ] T018 [P] Update `internal/feature/feed/docs.go` and `internal/feature/feed/cache/docs.go` (create if absent): ZSET score is now a packed rank score (reference contracts/timeline-score.md); store has NX-add vs upsert-set semantics (Constitution I: docs.go updated when responsibilities shift).
- [ ] T019 [P] Hygiene greps across repo: `grep -rn TimelineScoreFromTime` returns nothing; `grep -rn '"feed:tl'` shows only the v2 constant (no stray v1 literals in code or tests).
- [ ] T020 Manual verification per `specs/006-materialized-ranked-feed/quickstart.md`: `make docker-up` with timeline flags on; trigger lazy refresh; verify packed score sanity via `redis-cli` (`score>>32 ∈ [0,1048575]`, low bits = seconds since 2020); create a post from a followed author → appears immediately at bucket 30000, newest-first within the block; paginate across the equal-score block with `next_cursor` (no dup/gap); confirm no new `feed:tl:` v1 keys are written.
- [ ] T021 Final gates: `make test` and `make lint` green. `make generate` only if any Swagger handler comment changed (none expected — contracts/feed-api.md). Commit message notes the scoring formula/defaults are unchanged (Constitution IV — no measured-justification clause triggered).

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: none.
- **Foundational (Phase 2)**: after Setup. T002/T003 parallel; T004 → T005 → T006 sequential (one compiling unit); T007 after T006. Blocks both stories.
- **US1 (Phase 3)**: after Phase 2. T008/T009 parallel → T010/T011 (each unblocks after its test task; different files, parallelizable) → T012 → T013 → T014.
- **US2 (Phase 4)**: after US1 (serving ZSET order is only correct once writers produce rank-meaningful scores — this story-dependency is inherent, not accidental). T015 → T016 → T017.
- **Polish (Phase 5)**: after US2. T018/T019 parallel; T020 → T021 last.

### Parallel Opportunities

```text
Phase 2: T002 ∥ T003 (different test files)
Phase 3: T008 ∥ T009 (dispatcher vs refresher tests), then T010 ∥ T011
Phase 5: T018 ∥ T019
```

## Implementation Strategy

**MVP = Phase 1–3 (US1)**. It is deliberately shippable with zero visible change: if anything smells wrong in Redis after deploy, stop before US2 and nothing user-facing regressed. US2 is the payoff (hot path drops per-request scoring) and rides on evidence US1 produced sane scores. Everything is expected to land as one PR on `006-materialized-ranked-feed`, but each checkpoint is a safe pause/rollback point (`git revert` granularity per phase).

**Format validation**: 21 tasks; all follow `- [ ] Txxx [P?] [Story?] description + file path`; story labels only in Phases 3–4; [P] only on genuinely independent files.
