# Tasks: Refactor Feed and Discovery Pagination

**Input**: Design documents from `specs/003-refactor-feed-discovery/`
**Prerequisites**: `plan.md`, `spec.md`, `research.md`, `data-model.md`, `contracts/`, `quickstart.md`

**Tests**: Required by FR-015 and the project constitution. Characterization tests must be written before refactoring legacy behavior; regression tests must cover duplicate, skip, fallback, cursor, and provider-version scenarios.

**Organization**: Tasks are grouped by user story to enable independent implementation and testing.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel because it touches different files or has no dependency on incomplete tasks
- **[Story]**: User story label for story phases only
- Every task includes a concrete file path

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Prepare dependencies and document the exact target integration versions.

- [x] T001 Update the three Codohue Go modules to `v0.2.0` in `go.mod`
- [x] T002 Run module tidy after SDK upgrade and update `go.sum`
- [x] T003 [P] Record Codohue service `v0.4.0` and SDK `v0.2.0` compatibility notes in `pkg/codohue/docs.go`
- [x] T004 [P] Add feed/discovery refactor notes and validation commands to `specs/003-refactor-feed-discovery/quickstart.md`

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Shared contracts, data structures, and adapters needed before any user story implementation.

**Critical**: No user story work can begin until this phase is complete.

- [x] T005 Create characterization scaffolding and feed service mocks in `internal/feature/feed/service/feed_service_test.go`
- [x] T006 Define recommendation item/page types with object ID, score, rank, limit, offset, and total in `internal/feature/feed/recommender.go`
- [x] T007 Update feed recommender and trending interfaces for paginated recommendation metadata in `internal/feature/feed/recommender.go`
- [x] T008 Add versioned feed page state, feed candidate, and cursor encode/decode types in `internal/feature/feed/cursor.go`
- [x] T009 Add bounded feed continuation cache interface and no-op implementation in `internal/feature/feed/cache/feed_session_cache.go`
- [x] T010 Implement Redis-backed feed continuation cache with TTL and size caps in `internal/feature/feed/cache/redis_feed_session_cache.go`
- [x] T011 Wire feed continuation cache through feed context setup in `internal/app/feed.go`
- [x] T012 Update feed handler service interface to accept and return the new feed cursor type in `internal/feature/feed/handler/feed_handler.go`
- [x] T013 Update `docs.go` package descriptions for feed cursor/session responsibilities in `internal/feature/feed/docs.go`
- [x] T014 Update feed service package description for mixed-source pagination responsibilities in `internal/feature/feed/service/docs.go`

**Checkpoint**: Foundation ready; user story implementation can begin.

---

## Phase 3: User Story 1 - Stable Personalized Feed Scrolling (Priority: P1) MVP

**Goal**: Authenticated users can scroll mixed personalized feed pages without duplicate posts or skipped eligible posts.

**Independent Test**: Prepare followed, recommended, and trending posts; request consecutive `/feed` pages with returned cursors; verify no duplicate post IDs and no unreachable followed posts caused by ranked mixing.

### Tests for User Story 1

- [x] T015 [P] [US1] Add failing characterization test for current ranked page-1 duplicate/skip risk in `internal/feature/feed/service/feed_service_test.go`
- [x] T016 [P] [US1] Add failing regression test for pending followed posts remaining reachable after recommended/trending items outrank them in `internal/feature/feed/service/feed_service_test.go`
- [x] T017 [P] [US1] Add failing regression test for feed fallback to discovery excluding already-seen posts in `internal/feature/feed/service/feed_service_test.go`
- [x] T018 [P] [US1] Add cursor validation tests for versioned feed page state in `internal/feature/feed/cursor_test.go`
- [x] T019 [P] [US1] Add handler tests for malformed, expired, and valid opaque feed cursors in `internal/feature/feed/handler/feed_handler_test.go`

### Implementation for User Story 1

- [x] T020 [US1] Implement feed page state validation, seen-set bounding, and pending-item bounding in `internal/feature/feed/cursor.go`
- [x] T021 [US1] Implement feed candidate merge and duplicate collapse helpers in `internal/feature/feed/service/feed_service.go`
- [x] T022 [US1] Refactor `GetFeed` to initialize mixed feed state from empty cursor in `internal/feature/feed/service/feed_service.go`
- [x] T023 [US1] Refactor `GetFeed` to consume pending candidates before fetching more source candidates in `internal/feature/feed/service/feed_service.go`
- [x] T024 [US1] Refactor following lane cursor advancement so unshown fetched candidates remain pending in `internal/feature/feed/service/feed_service.go`
- [x] T025 [US1] Add discovery fallback state transition using seen IDs in `internal/feature/feed/service/feed_service.go`
- [x] T026 [US1] Persist and restore feed continuation state through the feed session cache in `internal/feature/feed/service/feed_service.go`
- [x] T027 [US1] Preserve existing response envelope while returning the new opaque feed cursor in `internal/feature/feed/handler/feed_handler.go`
- [x] T028 [US1] Add structured logs for feed session creation, fallback use, filtered candidates, and cursor errors in `internal/feature/feed/service/feed_service.go`
- [x] T029 [US1] Run targeted feed tests and fix failures in `internal/feature/feed/service/feed_service.go`

**Checkpoint**: User Story 1 is independently functional and testable as the MVP.

---

## Phase 4: User Story 2 - Reliable Public Discovery (Priority: P2)

**Goal**: Public discovery remains chronological, deterministic, and stable for anonymous and authenticated users, and feed fallback does not repeat seen posts.

**Independent Test**: Request `/discover` pages with timestamp ties as anonymous and authenticated users; verify identical page membership, deterministic order, viewer-specific enrichment only, and no duplicates.

### Tests for User Story 2

- [x] T030 [P] [US2] Add discovery cursor tie-order regression tests in `internal/feature/feed/service/feed_service_test.go`
- [x] T031 [P] [US2] Add authenticated discovery enrichment tests that verify page membership is unchanged in `internal/feature/feed/service/feed_service_test.go`
- [x] T032 [P] [US2] Add handler limit and malformed discover cursor tests in `internal/feature/feed/handler/feed_handler_test.go`
- [x] T033 [P] [US2] Add repository-level cursor test for public discovery timestamp ties in `internal/feature/post/repository/repository_test.go`

### Implementation for User Story 2

- [x] T034 [US2] Keep discover cursor decode/encode stable and add validation hardening in `internal/feature/feed/cursor.go`
- [x] T035 [US2] Update `GetDiscover` to preserve chronological ordering while applying viewer enrichment after page selection in `internal/feature/feed/service/feed_service.go`
- [x] T036 [US2] Ensure feed discovery fallback passes seen IDs into discovery candidate filtering in `internal/feature/feed/service/feed_service.go`
- [x] T037 [US2] Review and adjust public discovery SQL cursor query without hand-editing generated DB code in `internal/feature/post/sql/post_queries.sql`
- [x] T038 [US2] Regenerate sqlc output for post query changes in `internal/feature/post/db/post_queries.sql.go`
- [x] T039 [US2] Update post repository adapter for any regenerated discover query signature changes in `internal/feature/post/repository/post_repository.go`
- [x] T040 [US2] Run targeted discovery and repository tests and fix failures in `internal/feature/feed/service/feed_service.go`

**Checkpoint**: User Story 2 is independently functional and `/discover` remains stable.

---

## Phase 5: User Story 3 - Fully Compatible Recommendation Integration (Priority: P3)

**Goal**: Darkvoid consumes Codohue service `v0.4.0` paginated recommendation items through the three Go SDK modules at `v0.2.0`.

**Independent Test**: Run feed with provider responses containing object ID, score, rank, limit, offset, and total; verify those values drive recommendation continuation and ranking, while provider failures fall back locally.

### Tests for User Story 3

- [x] T041 [P] [US3] Add Codohue adapter tests for paginated recommendation item mapping in `pkg/codohue/client_test.go`
- [x] T042 [P] [US3] Add feed service tests for provider score/rank blended ordering in `internal/feature/feed/service/feed_service_test.go`
- [x] T043 [P] [US3] Add feed service tests for recommendation offset continuation and provider total exhaustion in `internal/feature/feed/service/feed_service_test.go`
- [x] T044 [P] [US3] Add feed service tests for invalid provider IDs, deleted posts, private posts, and duplicate provider items in `internal/feature/feed/service/feed_service_test.go`
- [x] T045 [P] [US3] Add feed service tests for Codohue unavailable fallback behavior in `internal/feature/feed/service/feed_service_test.go`

### Implementation for User Story 3

- [x] T046 [US3] Update `pkg/codohue.Client.GetRecommendations` to return paginated recommendation items from SDK `v0.2.0` in `pkg/codohue/client.go`
- [x] T047 [US3] Update Codohue trending mapping for SDK `v0.2.0` response metadata in `pkg/codohue/client.go`
- [x] T048 [US3] Update feed service recommendation fetching to request and persist Codohue offset in `internal/feature/feed/service/feed_service.go`
- [x] T049 [US3] Replace flat Codohue bonus with score/rank-aware blended scoring in `internal/feature/feed/service/feed_service.go`
- [x] T050 [US3] Preserve provider ordering when resolving recommended post IDs in `internal/app/feed_adapters.go`
- [x] T051 [US3] Filter invalid, inaccessible, deleted, private, and duplicate provider items before ranking in `internal/feature/feed/service/feed_service.go`
- [x] T052 [US3] Add optional recommendation score/rank response fields in `internal/feature/feed/handler/feed_handler.go`
- [x] T053 [US3] Update feed handler tests for optional recommendation score/rank response fields in `internal/feature/feed/handler/feed_handler_test.go`
- [x] T054 [US3] Update Swagger comments for new optional feed metadata fields in `internal/feature/feed/handler/feed_handler.go`
- [x] T055 [US3] Regenerate Swagger artifacts after handler contract changes in `docs/docs.go`
- [x] T056 [US3] Regenerate Swagger JSON and YAML artifacts after handler contract changes in `docs/swagger.json`
- [x] T057 [US3] Update generated Swagger YAML after handler contract changes in `docs/swagger.yaml`
- [x] T058 [US3] Run targeted Codohue and feed tests and fix failures in `pkg/codohue/client.go`

**Checkpoint**: User Story 3 is independently functional with Codohue service `v0.4.0` and SDK modules `v0.2.0`.

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Validate the whole feature, update documentation, and prepare for implementation review.

- [x] T059 [P] Update quickstart validation results and operational notes in `specs/003-refactor-feed-discovery/quickstart.md`
- [x] T060 [P] Update contract documentation for final response fields in `specs/003-refactor-feed-discovery/contracts/feed.md`
- [x] T061 [P] Update provider contract documentation for final SDK method names in `specs/003-refactor-feed-discovery/contracts/codohue-provider.md`
- [x] T062 Run `go test ./internal/feature/feed/... ./internal/feature/post/repository ./pkg/codohue` and document any required fixes in `specs/003-refactor-feed-discovery/quickstart.md`
- [x] T063 Run `make test` and fix feature-related failures in `internal/feature/feed/service/feed_service.go`
- [x] T064 Run `make lint` and fix feature-related lint findings in `internal/feature/feed/service/feed_service.go`
- [x] T065 Review changed package documentation and update any remaining `docs.go` files in `internal/feature/feed/handler/docs.go`
- [x] T066 Review final git diff for generated files, docs, and feature scope in `specs/003-refactor-feed-discovery/tasks.md`

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1 Setup**: No dependencies.
- **Phase 2 Foundational**: Depends on Phase 1, blocks all user stories.
- **Phase 3 US1**: Depends on Phase 2 and is the MVP.
- **Phase 4 US2**: Depends on Phase 2; can run alongside US1 after shared cursor/session contracts are stable, but final fallback filtering depends on US1 state semantics.
- **Phase 5 US3**: Depends on Phase 2; can run alongside US1 after recommender interfaces are defined, but final blended ranking integrates with US1 merge logic.
- **Phase 6 Polish**: Depends on selected user stories being complete.

### User Story Dependencies

- **US1 Stable Personalized Feed Scrolling**: MVP, no dependency on US2 or US3 after foundation.
- **US2 Reliable Public Discovery**: Independent `/discover` path after foundation; fallback duplicate prevention integrates with US1.
- **US3 Fully Compatible Recommendation Integration**: Independent provider adapter path after foundation; final ranking integrates with US1.

### Within Each User Story

- Tests must be written first and should fail before implementation.
- Cursor/data model tasks precede service tasks.
- Service tasks precede handler/contract tasks.
- Generated artifacts are updated only after source comments or SQL change.
- Targeted tests run before moving to the next story checkpoint.

## Parallel Opportunities

- T003 and T004 can run in parallel with dependency update review.
- T015 through T019 can be written in parallel because they cover different test surfaces.
- T030 through T033 can be written in parallel because they cover service, handler, and repository behavior.
- T041 through T045 can be written in parallel because they cover adapter and independent service scenarios.
- T059 through T061 can be completed in parallel after implementation stabilizes.

## Parallel Example: User Story 1

```text
Task: "T015 [P] [US1] Add failing characterization test for current ranked page-1 duplicate/skip risk in internal/feature/feed/service/feed_service_test.go"
Task: "T018 [P] [US1] Add cursor validation tests for versioned feed page state in internal/feature/feed/cursor_test.go"
Task: "T019 [P] [US1] Add handler tests for malformed, expired, and valid opaque feed cursors in internal/feature/feed/handler/feed_handler_test.go"
```

## Parallel Example: User Story 2

```text
Task: "T030 [P] [US2] Add discovery cursor tie-order regression tests in internal/feature/feed/service/feed_service_test.go"
Task: "T032 [P] [US2] Add handler limit and malformed discover cursor tests in internal/feature/feed/handler/feed_handler_test.go"
Task: "T033 [P] [US2] Add repository-level cursor test for public discovery timestamp ties in internal/feature/post/repository/repository_test.go"
```

## Parallel Example: User Story 3

```text
Task: "T041 [P] [US3] Add Codohue adapter tests for paginated recommendation item mapping in pkg/codohue/client_test.go"
Task: "T042 [P] [US3] Add feed service tests for provider score/rank blended ordering in internal/feature/feed/service/feed_service_test.go"
Task: "T045 [P] [US3] Add feed service tests for Codohue unavailable fallback behavior in internal/feature/feed/service/feed_service_test.go"
```

## Implementation Strategy

### MVP First

1. Complete Phase 1 and Phase 2.
2. Complete Phase 3 only.
3. Validate `/feed` across multiple cursor pages without duplicate or skipped posts using local/provider mocks.
4. Stop and review before expanding discovery and full provider compatibility.

### Incremental Delivery

1. Deliver US1 to stabilize mixed feed cursor semantics.
2. Deliver US2 to preserve public discovery and feed fallback behavior.
3. Deliver US3 to consume Codohue SDK `v0.2.0` metadata from service `v0.4.0`.
4. Complete Phase 6 validation and documentation updates.

### Team Parallel Strategy

After Phase 2:

- Developer A: US1 feed service and cursor/session behavior.
- Developer B: US2 discovery service, repository cursor, and handler tests.
- Developer C: US3 Codohue adapter and provider metadata tests.

## Notes

- Do not hand-edit generated files under `internal/feature/*/db`; regenerate them.
- Keep client-facing `/feed` pagination cursor-based; Codohue offset remains internal state.
- Use `pkg/logger` for production logs.
- Update Swagger artifacts if handler response fields or comments change.
- Keep session state bounded and short-lived; do not create a reusable per-user feed cache.
