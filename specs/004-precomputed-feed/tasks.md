# Tasks: Precomputed Feed Timeline

**Input**: Design documents from `/specs/004-precomputed-feed/`
**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md

**Tests**: Test tasks are included because the project constitution requires tests for service/handler logic changes, and this feature explicitly requires characterization tests before removing the old feed session path.

**Organization**: Tasks are grouped by user story to enable independent implementation and testing. Foundational tasks cover shared prerequisites that must be completed before user story work.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel with other marked tasks in the same phase because it touches different files and has no dependency on incomplete tasks.
- **[Story]**: User story label from `specs/004-precomputed-feed/spec.md`.
- Every task includes an exact file path.

## Phase 1: Setup

**Purpose**: Establish feature configuration and documentation scaffolding without changing feed behavior.

- [X] T001 Update feature context reference in `AGENTS.md` to keep `specs/004-precomputed-feed/plan.md` between the SPECKIT markers
- [X] T002 Add `FeedTimelineConfig` and fanout rollout config structs to `pkg/config/config.go`
- [X] T003 Add FEED_* environment loading for timeline serving, rollout percent, max items, TTL, fanout enabled, workers, queue size, max followers, and refresh-on-miss in `pkg/config/load.go`
- [X] T004 Add validation for FEED_* config bounds in `pkg/config/config.go`
- [X] T005 Document new FEED_* variables and defaults in `.env.example`
- [X] T006 Update config package purpose text if responsibilities change in `pkg/config/docs.go`

---

## Phase 2: Foundational

**Purpose**: Shared prerequisites that block all user stories: characterization tests, cursor/timeline contracts, and timeline store foundation.

**CRITICAL**: No user story implementation should begin until this phase is complete.

### Characterization Tests Before Refactor

- [X] T007 [P] Add characterization tests for current `FeedPageState.Validate` version mismatch, expired cursor, invalid mode, too many pending items, too many seen IDs, invalid following cursor, invalid discovery cursor, invalid pending post ID, and invalid seen post ID in `internal/feature/feed/cursor_test.go`
- [X] T008 [P] Add characterization tests for current `DecodeFeedCursor` empty cursor, malformed base64, malformed JSON, and valid v1 cursor acceptance in `internal/feature/feed/cursor_test.go`
- [X] T009 [P] Add characterization tests for current empty/missing candidate behavior entering discovery fallback in `internal/feature/feed/service/feed_service_test.go`
- [X] T010 [P] Add characterization tests proving current session cache misses do not fail feed reads and invalid cursor state returns bad request in `internal/feature/feed/service/feed_service_test.go`
- [X] T011 [P] Add characterization tests for current `/feed` invalid cursor response status and shape in `internal/feature/feed/handler/feed_handler_test.go`

### Timeline Store Foundation

- [X] T012 Create feed timeline entity and `TimelineStore` interfaces for add, batch add, read page, trim, and best-effort remove in `internal/feature/feed/timeline.go`
- [X] T013 Create no-op timeline store for Redis-disabled mode in `internal/feature/feed/cache/nop_timeline_store.go`
- [X] T014 [P] Add unit tests for no-op timeline store miss and non-fatal behavior in `internal/feature/feed/cache/nop_timeline_store_test.go`
- [X] T015 Create Redis sorted-set timeline store with key naming, add, batch add, read page, trim, TTL, and best-effort remove in `internal/feature/feed/cache/redis_timeline_store.go`
- [X] T016 Add Redis timeline store tests for idempotent add, newest-first read, created_at microsecond score precision, same-microsecond post ID tie handling, max 1,000 trim, 7-day TTL, miss behavior, and Redis error wrapping in `internal/feature/feed/cache/redis_timeline_store_test.go`
- [X] T017 Update feed cache package documentation for timeline store responsibilities in `internal/feature/feed/cache/docs.go`
- [X] T018 Update feed package documentation for cursor, timeline, refresh, dispatcher, and fanout ownership in `internal/feature/feed/docs.go`

**Checkpoint**: Characterization tests and timeline store foundation pass with no production feed behavior changed.

---

## Phase 3: User Story 1 - Browse A Fast Personalized Feed (Priority: P1) MVP

**Goal**: Authenticated users can browse a prepared followed-author timeline with stable newest-first pagination and no duplicates across pages.

**Independent Test**: Prepare a user with followed authors and recent visible posts, request `/feed` first page and next page, and verify ordered non-duplicated items with a new cursor.

### Tests for User Story 1

- [X] T019 [P] [US1] Add cursor v2 encode/decode tests for timeline score, timeline post ID tie-breaker, recommendation offset, trending cursor, fallback cursor, issued_at, and empty cursor behavior in `internal/feature/feed/cursor_test.go`
- [X] T020 [P] [US1] Add feed service tests for timeline-first reads, newest-first ordering, no duplicate posts across cursor pages, and omitted next cursor at completion in `internal/feature/feed/service/feed_service_test.go`
- [X] T021 [P] [US1] Add feed service tests for response-time post visibility filtering and stale prepared entry suppression in `internal/feature/feed/service/feed_service_test.go`
- [X] T022 [P] [US1] Add feed service tests for lazy refresh when the prepared timeline is missing, expired, or incomplete in `internal/feature/feed/service/feed_service_test.go`
- [X] T023 [P] [US1] Add handler tests for authenticated `/feed` success with v2 cursor and `next_cursor` response in `internal/feature/feed/handler/feed_handler_test.go`

### Implementation for User Story 1

- [X] T024 [US1] Replace v1 `FeedPageState` cursor model with v2 feed cursor fields and validation in `internal/feature/feed/cursor.go`
- [X] T025 [US1] Add feed post reader methods needed for lazy refresh and prepared ID loading in `internal/feature/feed/post_reader.go`
- [X] T026 [US1] Add feed follow reader methods needed for response-time follow-state filtering in `internal/feature/feed/follow_reader.go`
- [X] T027 [US1] Create timeline refresher for lazy refresh from current following authors and recent eligible posts in `internal/feature/feed/refresher.go`
- [X] T028 [US1] Implement timeline-first read path, over-fetching, batch load, visibility filtering, follow-state filtering, and v2 next cursor advancement in `internal/feature/feed/service/feed_service.go`
- [X] T029 [US1] Integrate timeline store and refresher dependencies into `FeedService` construction without using `FeedSessionCache` in `internal/feature/feed/service/feed_service.go`
- [X] T030 [US1] Wire Redis/no-op timeline store and refresher into feed setup in `internal/app/feed.go`
- [X] T031 [US1] Extend app feed adapters for any new feed post/follow reader methods in `internal/app/feed_adapters.go`
- [X] T032 [US1] Update `/feed` handler success path to emit v2 cursors while preserving `data` and optional `next_cursor` envelope in `internal/feature/feed/handler/feed_handler.go`
- [X] T033 [US1] Run targeted feed tests for cursor, cache, handler, and service packages under `internal/feature/feed/...`

**Checkpoint**: User Story 1 works independently with timeline-backed feed pages and lazy refresh, before fanout is required.

---

## Phase 4: User Story 2 - See New Followed Posts Quickly (Priority: P2)

**Goal**: Newly published visible posts become eligible for followers' prepared feeds within the freshness target without making post creation depend on feed propagation success.

**Independent Test**: Create a post from a followed author, wait within the freshness target, and verify the follower's feed includes the post; force propagation failure and verify post creation still succeeds.

### Tests for User Story 2

- [X] T034 [P] [US2] Add dispatcher tests for enqueue success, full queue behavior, disabled dispatcher behavior, and sanitized error logging expectations in `internal/feature/feed/dispatcher_test.go`
- [X] T035 [P] [US2] Add fanout worker tests for follower lookup, max fanout cap, idempotent timeline writes, trim calls, and non-fatal timeline errors in `internal/feature/feed/fanout_test.go`
- [X] T036 [P] [US2] Add post service tests proving post-created feed events emit only after successful commit, do not emit on failed create, and emitter failure is non-fatal in `internal/feature/post/service/post_service_test.go`
- [X] T037 [P] [US2] Add follow service tests proving follow/unfollow feed events emit after success and emitter failure is non-fatal in `internal/feature/user/service/follow_service_test.go`

### Implementation for User Story 2

- [X] T038 [US2] Create feed event dispatcher with bounded queue, enabled flag, worker lifecycle, and non-blocking dispatch in `internal/feature/feed/dispatcher.go`
- [X] T039 [US2] Create fanout worker for post-created events, follower lookup, max follower cap, timeline writes, trim, TTL refresh, and error logging in `internal/feature/feed/fanout.go`
- [X] T040 [US2] Add feed-owned event emitter interfaces and event types for post-created, follow-created, follow-deleted, post-deleted, and visibility-changed events in `internal/feature/feed/dispatcher.go`
- [X] T041 [US2] Add post-created feed event emitter port to post service interfaces in `internal/feature/post/service/interfaces.go`
- [X] T042 [US2] Emit post-created feed event after `CreatePost` transaction commit and after post entity exists in `internal/feature/post/service/post_service.go`
- [X] T043 [US2] Add follow feed event emitter port to follow service and keep existing following-ID invalidation behavior in `internal/feature/user/service/follow_service.go`
- [X] T044 [US2] Wire feed dispatcher into post and user contexts in `internal/app/feed_wiring.go`
- [X] T045 [US2] Wire post-created emitter dependency into `PostContext` in `internal/app/post.go`
- [X] T046 [US2] Wire follow event emitter dependency into `UserContext` in `internal/app/user.go`
- [X] T047 [US2] Run targeted post and user service tests under `internal/feature/post/...` and `internal/feature/user/...`

**Checkpoint**: User Story 2 works independently with prepared timeline fanout, while source mutations remain successful during propagation failures.

---

## Phase 5: User Story 3 - Use The New Feed Contract Explicitly (Priority: P3)

**Goal**: Clients use the new feed cursor contract only; old session-based cursors are rejected and session state is removed from normal feed browsing.

**Independent Test**: Send no cursor, a valid v2 cursor, an old v1 session cursor, and malformed cursors to `/feed`; verify only the new contract is accepted.

### Tests for User Story 3

- [X] T048 [P] [US3] Add cursor tests rejecting old `FeedPageState` JSON, unsupported cursor versions, negative numeric positions, malformed base64, malformed JSON, and tampered payloads in `internal/feature/feed/cursor_test.go`
- [X] T049 [P] [US3] Add handler tests for old session-based cursor rejection with `400` and stable error response in `internal/feature/feed/handler/feed_handler_test.go`
- [X] T050 [P] [US3] Add feed service tests proving no `SessionID`, `PendingItems`, or `SeenPostIDs` state is required across v2 pages in `internal/feature/feed/service/feed_service_test.go`

### Implementation for User Story 3

- [X] T051 [US3] Remove remaining `SessionID`, `PendingItems`, `SeenPostIDs`, and `FeedSessionTTL` usage from feed cursor implementation in `internal/feature/feed/cursor.go`
- [X] T052 [US3] Remove `FeedSessionCache` dependency from feed service state and constructor in `internal/feature/feed/service/feed_service.go`
- [X] T053 [US3] Remove `FeedSessionCache` fields and constructor wiring from feed app context in `internal/app/feed.go`
- [X] T054 [US3] Delete old feed session cache interface and `NopFeedSessionCache` implementation file `internal/feature/feed/cache/feed_session_cache.go`
- [X] T055 [US3] Delete old Redis feed session cache implementation file `internal/feature/feed/cache/redis_feed_session_cache.go`
- [X] T056 [US3] Update feed handler Swagger comments to document v2 cursor and old cursor rejection in `internal/feature/feed/handler/feed_handler.go`
- [X] T057 [US3] Regenerate Swagger with `make swagger-generate` after handler comment changes and verify `docs/swagger.json` and `docs/swagger.yaml`
- [X] T058 [US3] Review regenerated Swagger diff for the `/feed` cursor contract and old cursor rejection in `docs/swagger.json` and `docs/swagger.yaml`
- [X] T059 [US3] Run targeted feed cursor and handler tests under `internal/feature/feed/...`

**Checkpoint**: User Story 3 works independently with explicit breaking cursor behavior and no normal-path session state.

---

## Phase 6: User Story 4 - Preserve Discovery And Supplemental Sources (Priority: P4)

**Goal**: Users with sparse or missing prepared timelines still receive valid feed responses with bounded supplemental content, and `/discover` remains unchanged.

**Independent Test**: Use a user with no prepared timeline and few followed posts; verify feed returns fallback/supplemental content with v2 continuation while `/discover` chronological pagination remains stable.

### Tests for User Story 4

- [X] T060 [P] [US4] Add feed service tests for recommendation/trending/fallback merge with duplicate collapse and bounded supplemental count in `internal/feature/feed/service/feed_service_test.go`
- [X] T061 [P] [US4] Add feed service tests for supplemental provider unavailable, slow, or partial results returning a valid feed response in `internal/feature/feed/service/feed_service_test.go`
- [X] T062 [P] [US4] Add feed service tests for fallback source continuation through v2 cursor without `feed:session` in `internal/feature/feed/service/feed_service_test.go`
- [X] T063 [P] [US4] Add discovery regression tests proving `/discover` cursor behavior remains unchanged in `internal/feature/feed/handler/feed_handler_test.go`

### Implementation for User Story 4

- [X] T064 [US4] Update supplemental source merge in feed service to use v2 recommendation, trending, and fallback cursor positions in `internal/feature/feed/service/feed_service.go`
- [X] T065 [US4] Ensure supplemental candidate visibility filtering and duplicate collapse use the same response-time rules as primary timeline items in `internal/feature/feed/service/feed_service.go`
- [X] T066 [US4] Ensure `/discover` handler and discover cursor code are not changed except for regression-test compatibility in `internal/feature/feed/handler/feed_handler.go`
- [X] T067 [US4] Run targeted discovery and feed fallback tests under `internal/feature/feed/...`

**Checkpoint**: User Story 4 works independently with sparse timelines, supplemental content, and unchanged `/discover` behavior.

---

## Phase 7: Rollout, Observability, And Backfill

**Purpose**: Enable controlled rollout and measurement before final cleanup.

- [X] T068 Add rollout gating for timeline preparation enabled, timeline serving enabled, rollout percent, fanout enabled, and refresh-on-miss in `internal/feature/feed/service/feed_service.go`
- [X] T069 Add deterministic rollout eligibility helper tests for percentage gates in `internal/feature/feed/service/feed_service_test.go`
- [X] T070 Add operational logging and counters for fanout latency, enqueue failures, timeline hit/miss, lazy refresh, fallback usage, cursor rejection, stale filtered count, and Redis memory pressure in `internal/feature/feed/service/feed_service.go`
- [X] T071 Add operational logging and counters for queue depth, fanout processing duration, capped fanout, and fanout worker errors in `internal/feature/feed/fanout.go`
- [X] T072 Add backfill/warm method for active users or manual rollout support to existing `internal/feature/feed/refresher.go`
- [X] T073 Add backfill/warm helper tests for bounded candidate reads and timeline writes in `internal/feature/feed/service/feed_service_test.go`
- [X] T074 Update rollout and rollback steps in `specs/004-precomputed-feed/quickstart.md`

---

## Phase 8: Polish & Cross-Cutting Concerns

**Purpose**: Final cleanup, verification, and documentation across all stories.

- [X] T075 Update feed package documentation after session removal and timeline ownership changes in `internal/feature/feed/docs.go`
- [X] T076 Update feed service package documentation after timeline-first read changes in `internal/feature/feed/service/docs.go`
- [X] T077 Update feed handler package documentation after cursor contract changes in `internal/feature/feed/handler/docs.go`
- [X] T078 Update feed cache package documentation after deleting session cache files in `internal/feature/feed/cache/docs.go`
- [X] T079 Run `gofmt`/`goimports` on changed Go files under `pkg/config`, `internal/app`, `internal/feature/feed`, `internal/feature/post/service`, and `internal/feature/user/service`
- [X] T080 Run targeted tests for feed, post, and user packages under `internal/feature/feed/...`, `internal/feature/post/...`, and `internal/feature/user/...`
- [X] T081 Run full project tests with `make test`
- [X] T082 Run lint with `make lint`
- [X] T083 Run Swagger generation with `make swagger-generate` and verify `docs/swagger.json` and `docs/swagger.yaml`
- [X] T084 Review `.env.example` and `specs/004-precomputed-feed/quickstart.md` for matching FEED_* defaults and rollback instructions

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies; start immediately.
- **Foundational (Phase 2)**: Depends on Setup; blocks all user story phases.
- **US1 (Phase 3)**: Depends on Foundational; MVP feed read path.
- **US2 (Phase 4)**: Depends on Foundational and should use US1 timeline store/read semantics; can begin after timeline store is ready, but final validation depends on US1.
- **US3 (Phase 5)**: Depends on Foundational and US1 cursor/read tests; session deletion must wait until v2 path works.
- **US4 (Phase 6)**: Depends on US1 cursor/read path; can proceed before US2 if supplemental work is staffed separately.
- **Rollout (Phase 7)**: Depends on US1, US2, US3, and US4.
- **Polish (Phase 8)**: Depends on all selected implementation phases.

### User Story Dependencies

- **US1 Browse A Fast Personalized Feed (P1)**: MVP. Requires Setup + Foundational only.
- **US2 See New Followed Posts Quickly (P2)**: Requires timeline store foundation and should integrate with US1 timeline semantics.
- **US3 Use The New Feed Contract Explicitly (P3)**: Requires cursor v2 tests and feed read path; completes old session removal.
- **US4 Preserve Discovery And Supplemental Sources (P4)**: Requires v2 cursor and feed service read path; `/discover` remains independently regression-tested.

### Within Each User Story

- Tests must be written first and fail for new behavior before implementation.
- Contracts/interfaces before services.
- Services before handlers/wiring.
- App wiring after service interfaces are stable.
- Cleanup/delete tasks only after replacement tests pass.

## Parallel Opportunities

- T007-T011 characterization tests can be written in parallel across cursor, service, and handler test files.
- T013-T014 can proceed while T015-T016 are implemented if the `TimelineStore` interface from T012 is stable.
- T019-T023 US1 tests can be written in parallel before US1 implementation.
- T034-T037 US2 tests can be written in parallel across feed, post, and user packages.
- T048-T050 US3 tests can be written in parallel across cursor, handler, and service tests.
- T060-T063 US4 tests can be written in parallel across service and handler tests.
- Documentation tasks T075-T078 can run in parallel after code cleanup.

## Parallel Example: User Story 1

```text
Task: "T019 [US1] Add cursor v2 encode/decode tests in internal/feature/feed/cursor_test.go"
Task: "T020 [US1] Add feed service timeline pagination tests in internal/feature/feed/service/feed_service_test.go"
Task: "T023 [US1] Add handler success tests in internal/feature/feed/handler/feed_handler_test.go"
```

## Parallel Example: User Story 2

```text
Task: "T034 [US2] Add dispatcher tests in internal/feature/feed/dispatcher_test.go"
Task: "T036 [US2] Add post service event tests in internal/feature/post/service/post_service_test.go"
Task: "T037 [US2] Add follow service event tests in internal/feature/user/service/follow_service_test.go"
```

## Parallel Example: User Story 3

```text
Task: "T048 [US3] Add old cursor rejection tests in internal/feature/feed/cursor_test.go"
Task: "T049 [US3] Add handler invalid cursor tests in internal/feature/feed/handler/feed_handler_test.go"
Task: "T050 [US3] Add session-free paging tests in internal/feature/feed/service/feed_service_test.go"
```

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1 Setup.
2. Complete Phase 2 Foundational, especially characterization tests and timeline store.
3. Complete Phase 3 US1 timeline-first feed read.
4. Stop and validate with targeted feed tests under `internal/feature/feed/...`.

### Incremental Delivery

1. US1: Deliver timeline-backed feed read and lazy refresh behind disabled or limited rollout.
2. US2: Add propagation and fanout so new posts become eligible quickly.
3. US3: Enforce the breaking cursor contract and delete old session state.
4. US4: Complete supplemental/fallback continuation and discovery regression coverage.
5. Rollout: Enable measurement, backfill/warm behavior, and rollback switches.

### Parallel Team Strategy

1. One engineer owns timeline store and cursor foundation.
2. One engineer owns feed service read path and lazy refresh after foundation.
3. One engineer owns post/user event emitters and fanout after the dispatcher interface stabilizes.
4. One engineer owns handler contract, Swagger, and discovery regression after cursor v2 stabilizes.

## Notes

- `[P]` tasks touch different files or independent tests and can run in parallel.
- `[US1]`, `[US2]`, `[US3]`, and `[US4]` map directly to spec user stories.
- Do not delete session cache files until v2 cursor and timeline-first service tests pass.
- Do not hand-edit generated files under `internal/feature/*/db`.
- Use `pkg/logger`; do not introduce raw `log`, `fmt.Print*`, or direct `log/slog` production logging.
