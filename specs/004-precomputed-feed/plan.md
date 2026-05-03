# Implementation Plan: Precomputed Feed Timeline

**Branch**: `004-precomputed-feed` | **Date**: 2026-05-02 | **Spec**: [spec.md](./spec.md)
**Input**: Feature specification from `specs/004-precomputed-feed/spec.md`

**Note**: This template is filled in by the `/speckit.plan` command. See `.specify/templates/plan-template.md` for the execution workflow.

## Summary

Refactor `/feed` from request-time mixed candidate collection plus `feed:session` continuation state to a prepared per-user followed-author timeline. Redis sorted sets become the primary read model for followed-author feed items, while PostgreSQL remains the source of truth for posts, visibility, follows, and enrichment. Feed propagation is event-driven after successful post creation, with lazy refresh on feed requests when a user's timeline is missing, expired, or incomplete.

The new feed contract is intentionally breaking: old session-based feed cursors and unsupported cursor versions are rejected with `400`. Cursor v2 is still opaque to clients but carries continuation positions for primary timeline, recommendation, trending, and fallback sources. Deleted, hidden, or unfollowed content is filtered at response time; physical cleanup of stale prepared entries is lazy or best-effort.

## Technical Context

**Language/Version**: Go 1.26.1  
**Primary Dependencies**: chi HTTP routing, pgx/PostgreSQL, Redis via `github.com/redis/go-redis/v9`, local `pkg/logger`, local `pkg/errors`, optional Codohue Go client for recommendations/trending  
**Storage**: PostgreSQL for authoritative post/follow/visibility data and optional fanout audit log; Redis sorted sets for prepared per-user feed timelines; existing Redis keys for `following:ids:{userID}` and `trending:posts` remain  
**Testing**: Standard Go `testing` package; `make test`; targeted `go test ./internal/feature/feed/...`, `go test ./internal/feature/post/...`, and `go test ./internal/feature/user/...`; integration coverage where SQL behavior changes  
**Target Platform**: Linux REST API service  
**Project Type**: Go web-service backend with bounded feature contexts  
**Performance Goals**: Feed page 1 <= 200ms p95 for users with prepared timeline items; feed page 2+ <= 100ms p95; 99% of followed-author posts available to eligible followers within 30s under normal operation  
**Constraints**: New `/feed` cursor contract is breaking; old feed session cursors rejected; response-time visibility/follow correctness required; each user's prepared timeline max 1,000 primary items and 7-day retention; Redis failure must not fail post creation; Codohue must remain gated by `CODOHUE_ENABLED`; no hand edits under `internal/feature/*/db`; generated Swagger refreshed if handler contract changes  
**Scale/Scope**: Existing `/feed` behavior only plus feed-impacting hooks from post/follow mutations; `/discover` chronological cursor behavior must remain unchanged; target planning scale is <=100k users, 1,000 prepared primary items/user, and bounded fanout for hot authors

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

- **Code Quality**: PASS. Changes stay within feed, app adapters, post/user service ports for event hooks, config/docs, generated docs, and tests. New/changed packages require `docs.go`, logging uses `pkg/logger`, and generated DB files are not hand-edited.
- **Testing Standards**: PASS. Plan requires characterization tests for current cursor rejection/fallback boundaries before refactor, unit tests for cursor/timeline/fanout behavior, handler tests for the breaking cursor contract, service tests for response-time eligibility filtering, and targeted integration tests where SQL query behavior changes.
- **API & User Experience Consistency**: PASS WITH BREAKING-CONTRACT NOTE. `/feed` keeps a stable JSON success envelope with `data` and optional `next_cursor`, but the cursor internals and validation behavior change intentionally. Old feed cursors return a semantic 400 validation error. Swagger must be regenerated.
- **Pagination Rule**: PASS WITH JUSTIFICATION. `/discover` and DB-backed post lists continue using `(created_at, id)` cursor pagination. `/feed` uses an opaque feed cursor over prepared timeline positions plus supplemental-source cursors; this is not DB offset pagination.
- **Performance Requirements**: PASS WITH CAPACITY JUSTIFICATION. The constitution previously prohibited per-user feed caching without capacity justification. This feature explicitly introduces bounded prepared timelines to meet feed p95 and freshness goals: 100k users x 1,000 items/user is capped, 7-day retention limits stale storage, Redis eviction is recoverable by lazy refresh, and the timeline stores only IDs/scores rather than rendered feed pages.
- **Architecture Constraints**: PASS. Feed owns timeline/cursor/fanout contracts. Post and User contexts expose feed-impacting events through narrow interfaces wired in `internal/app`; no cross-context feature imports are introduced.

**Post-Design Recheck**: PASS. `research.md`, `data-model.md`, `contracts/`, and `quickstart.md` preserve the same constraints: response-time correctness, bounded prepared timelines, breaking cursor contract, lazy refresh requirement, and controlled rollout.

## Project Structure

### Documentation (this feature)

```text
specs/004-precomputed-feed/
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/
│   ├── feed.md
│   ├── cursor.md
│   ├── timeline-store.md
│   └── propagation.md
├── checklists/
│   └── requirements.md
└── tasks.md
```

### Source Code (repository root)

```text
pkg/config/
├── config.go                 # MODIFY: add FeedTimeline/Fanout config structs
├── load.go                   # MODIFY: load and validate FEED_* variables
└── docs.go                   # MODIFY if package purpose text changes

.env.example                  # MODIFY: document new FEED_* rollout and fanout variables

internal/app/
├── feed.go                   # MODIFY: wire timeline store, dispatcher, remove session cache field
├── feed_adapters.go          # MODIFY: add refresh/fanout reader methods if needed
├── feed_wiring.go            # MODIFY: expose feed propagation ports to post/user contexts
├── post.go                   # MODIFY: wire post-created emitter into PostService
└── user.go                   # MODIFY: wire follow event emitter into FollowService

internal/feature/feed/
├── cursor.go                 # MODIFY: replace v1 FeedPageState/session cursor with v2 cursor
├── docs.go
├── follow_reader.go          # MODIFY: add follower/following capabilities required by fanout/filtering
├── post_reader.go            # MODIFY: add refresh/load methods if current ports are insufficient
├── timeline.go               # CREATE: timeline entry/store contracts
├── refresher.go              # CREATE: lazy timeline refresh contract and implementation
├── dispatcher.go             # CREATE: feed event dispatcher
├── fanout.go                 # CREATE: fanout worker and event handling
├── cache/
│   ├── docs.go
│   ├── feed_cache.go
│   ├── feed_session_cache.go         # DELETE after replacement path is covered
│   ├── redis_feed_session_cache.go   # DELETE after replacement path is covered
│   ├── nop_feed_cache.go             # MODIFY: no-op timeline-related behavior if interface changes
│   ├── nop_timeline_store.go         # CREATE: no-op timeline store for Redis-disabled mode
│   └── redis_timeline_store.go       # CREATE: Redis sorted-set timeline store
├── handler/
│   ├── docs.go
│   ├── feed_handler.go       # MODIFY: document breaking cursor behavior, response contract
│   └── feed_handler_test.go  # MODIFY: handler tests for v2 cursor and old cursor rejection
└── service/
    ├── docs.go
    ├── feed_service.go       # MODIFY: timeline-first read path, fallback warm path, remove session path
    └── feed_service_test.go  # MODIFY: characterization, timeline, fallback, filtering tests

internal/feature/post/
├── service/
│   ├── interfaces.go         # MODIFY: add post-created feed event emitter port
│   ├── post_service.go       # MODIFY: emit post-created event after commit
│   └── post_service_test.go  # MODIFY: event emitted after success, not on failure, emitter failure non-fatal
└── repository/
    └── post_repository.go    # MODIFY only if refresh query support is insufficient

internal/feature/user/
└── service/
    ├── follow_service.go     # MODIFY: emit follow-created/follow-deleted feed events
    └── follow_service_test.go # MODIFY: event behavior and non-fatal emitter failure

docs/
├── docs.go
├── swagger.json              # REGENERATE if handler comments/contract change
└── swagger.yaml              # REGENERATE if handler comments/contract change
```

**Structure Decision**: Use the existing bounded-context layout. Feed owns cursor, timeline store, timeline refresh, and fanout orchestration. Post/User services expose feed events through consumer-owned ports and app-layer wiring. No new top-level service or worker binary is required for the initial in-process implementation.

## Implementation Phases

### Phase 0 - Characterization And Safety Baseline

Purpose: lock down current behavior before deleting the session-based feed path.

Required tests before refactor:

- `internal/feature/feed/cursor_test.go`: characterize current v1 `FeedPageState.Validate` boundaries for version mismatch, expired cursor, invalid mode, too many pending items, too many seen IDs, invalid following cursor, invalid discovery cursor, invalid pending post ID, and invalid seen post ID.
- `internal/feature/feed/cursor_test.go`: characterize current `DecodeFeedCursor` handling for empty cursor, malformed base64, malformed JSON, and current v1 cursor acceptance.
- `internal/feature/feed/service/feed_service_test.go`: characterize current empty/missing candidate behavior that enters discovery fallback.
- `internal/feature/feed/service/feed_service_test.go`: characterize that current session cache misses do not fail feed reads, while invalid cursor state returns bad request.
- `internal/feature/feed/handler/feed_handler_test.go`: characterize existing invalid cursor response shape/status for `/feed`.

Exit criteria:

- Baseline characterization tests pass.
- No production behavior is changed in this phase.

### Phase 1 - Config And Timeline Store Foundation

Purpose: introduce bounded timeline infrastructure behind disabled serving behavior.

Work:

- Add `FeedTimelineConfig`/fanout config loading and validation in `pkg/config`.
- Document new variables in `.env.example`.
- Create feed-owned `TimelineStore` contracts and no-op implementation.
- Create Redis sorted-set timeline store with add, batch add, read page, trim, TTL, and best-effort remove operations.
- Add unit tests for idempotent add, newest-first read, tie handling, max 1,000 trim, 7-day TTL, miss behavior, and Redis error behavior.

Dependencies:

- Must complete before feed service can read timeline or fanout can write timeline.

### Phase 2 - Cursor V2 And Handler Contract

Purpose: switch `/feed` validation to the new opaque cursor contract while old serving can still be behind flags.

Work:

- Replace `FeedCursorVersion = 1` state model with v2 cursor fields for timeline, recommendation, trending, and fallback source positions.
- Reject old `FeedPageState`/session-style cursor payloads and unsupported versions with `400`.
- Remove cursor dependence on `SessionID`, `PendingItems`, and `SeenPostIDs`.
- Update handler Swagger comments and tests for breaking cursor behavior.

Dependencies:

- Phase 0 tests must exist first so removed behavior is intentional.
- Phase 2 can run in parallel with Phase 1 only if tests isolate cursor from timeline store.

### Phase 3 - Timeline-First Feed Read And Lazy Refresh

Purpose: make feed service read from prepared timeline when enabled and recover with lazy refresh/fallback when missing.

Work:

- Add timeline-first read path in `FeedService`.
- Batch load posts by prepared IDs and enforce response-time visibility and follow-state filtering.
- Use over-fetch to compensate for stale prepared entries.
- Implement lazy refresh on feed request when timeline is missing, expired, or incomplete.
- Keep `/discover` endpoint behavior unchanged.
- Merge bounded recommendation/trending/fallback candidates using cursor v2 continuation, without `feed:session`.

Dependencies:

- Requires Phase 1 timeline store and Phase 2 cursor contract.
- Must complete before fanout rollout can serve timeline reads.

### Phase 4 - Event Propagation And Fanout

Purpose: prepare timelines after feed-impacting mutations without blocking source mutations.

Work:

- Create feed event dispatcher and in-process worker pool.
- Implement post-created fanout to followers with max fanout cap and non-fatal errors.
- Wire `PostService` post-created event after transaction commit.
- Wire `FollowService` follow/unfollow events for following cache invalidation and lazy refresh signals.
- Add tests proving source mutations succeed when feed propagation fails.

Dependencies:

- Requires Phase 1 timeline store.
- Should land after Phase 3 tests define response-time filtering semantics.

### Phase 5 - Rollout, Observability, And Backfill

Purpose: enable controlled rollout and measurement before deleting old session code.

Work:

- Add rollout gates for timeline preparation enabled, timeline serving enabled, and rollout percentage.
- Add operational signals for fanout latency, enqueue failures, timeline hit/miss, lazy refresh, fallback usage, cursor rejection, stale filtered count, and Redis memory pressure where available.
- Add a backfill/warm mechanism for active users or manual rollout support.
- Document rollback by disabling timeline serving while preserving fallback feed access.

Dependencies:

- Requires Phases 1, 3, and 4.

### Phase 6 - Session Path Removal And Cleanup

Purpose: complete FR-005 and FR-019 after the new path is covered.

Work:

- Delete `internal/feature/feed/cache/feed_session_cache.go`.
- Delete `internal/feature/feed/cache/redis_feed_session_cache.go`.
- Remove `FeedSessionCache` from `FeedService`, app wiring, tests, and docs.
- Remove old `FeedPageState`, pending item, and seen ID code.
- Remove old session Redis key usage from docs and tests.
- Regenerate Swagger if handler comments changed.

Dependencies:

- Requires Phase 2 cursor tests and Phase 3 feed service tests passing.
- Should happen after rollout controls exist so old code is not the rollback mechanism.

### Phase 7 - Verification

Purpose: confirm the plan satisfies code quality, API contract, and performance constraints.

Work:

- Run targeted tests for feed, post, and user services.
- Run `make test`.
- Run `make lint`.
- Run `make swagger-generate` if handler comments changed and verify generated docs.
- Review Redis capacity assumptions and `.env.example` defaults before PR.

Dependencies:

- Final phase before task completion and PR.

## Complexity Tracking

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| Bounded per-user prepared timeline despite constitution caution against per-user feed caching | Required to meet feed p95 targets and eliminate request-time candidate recomputation. Storage is bounded to 1,000 IDs/user and 7 days, and rendered pages are not cached. | Continuing dynamic collection keeps the existing duplicate/skip/session complexity and cannot reliably hit page 2+ p95 under mixed sources. |
| In-process fanout dispatcher and lazy refresh alongside read path during rollout | Required to deliver near real-time followed posts while preserving feed availability during Redis misses, evictions, and missed in-process jobs. | A persistent external queue would increase operational complexity beyond current scale; pure request-time refresh would miss the 30s freshness target for active followers. |
