# Tasks: Feed Cursor Contract

**Input**: Design documents from `/specs/005-feed-cursor-contract/`
**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md

**Tests**: Test tasks are included because the project constitution requires tests for service/handler logic changes, and this feature changes the client-facing `/feed` cursor contract.

**Organization**: Tasks are grouped by user story to enable independently testable increments where possible. Cursor model changes are shared, so strict obsolete-cursor rejection (US2) should run after the new cursor contract (US1) is in place.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel with other marked tasks in the same phase because it touches different files and has no dependency on incomplete tasks.
- **[Story]**: User story label from `specs/005-feed-cursor-contract/spec.md`.
- Every task includes an exact file path.

## Phase 1: Setup

**Purpose**: Confirm feature context and establish the current cursor behavior before implementation tasks begin.

- [X] T001 Confirm the SPECKIT context reference points to `specs/005-feed-cursor-contract/plan.md` in `AGENTS.md`
- [X] T002 [P] Review the no-version cursor contract and field semantics in `specs/005-feed-cursor-contract/contracts/cursor.md`
- [X] T003 [P] Review the `/feed` contract and invalid cursor cases in `specs/005-feed-cursor-contract/contracts/feed.md`

---

## Phase 2: Foundational

**Purpose**: Characterize current behavior and define shared cursor helpers that block all user-story implementation.

**CRITICAL**: No user story implementation should begin until this phase is complete.

- [X] T004 [P] Add characterization tests for current `FeedCursor` encode/decode shape, empty cursor behavior, malformed base64, malformed JSON, and current nested `timeline` acceptance in `internal/feature/feed/cursor_test.go`
- [X] T005 [P] Add characterization tests for current `/feed` invalid cursor response shape and current valid cursor forwarding behavior in `internal/feature/feed/handler/feed_handler_test.go`
- [X] T006 [P] Add characterization tests for current page-1-only trending behavior and recommendation offset continuation in `internal/feature/feed/service/feed_service_test.go`
- [X] T007 Define shared cursor position helpers for score plus post ID tie-breaker semantics in `internal/feature/feed/cursor.go`
- [X] T008 Update feed package documentation to mention no-version cursor ownership and source continuation in `internal/feature/feed/docs.go`

**Checkpoint**: Current behavior is characterized, and shared cursor primitives are ready for story implementation.

---

## Phase 3: User Story 1 - Continue Feed With New Cursor (Priority: P1) MVP

**Goal**: `/feed` emits and accepts the new no-version cursor with flat continuation fields and can continue timeline, recommendation, and trending sources without duplicates.

**Independent Test**: Request page 1, decode `next_cursor`, verify it has no `v` and contains flat continuation fields for active sources, then request page 2 and verify no duplicate post IDs.

### Tests for User Story 1

- [X] T009 [P] [US1] Add cursor tests for no-version encode/decode with `tl_score`, `tl_post_id`, `tl_user`, `rec_offset`, `trend_score`, `trend_post_id`, missing optional fields, and nil cursor behavior in `internal/feature/feed/cursor_test.go`
- [X] T010 [P] [US1] Add service tests for timeline continuation using `tl_score` and `tl_post_id` without duplicate posts across pages in `internal/feature/feed/service/feed_service_test.go`
- [X] T011 [P] [US1] Add service tests for recommendation continuation using `rec_offset` while preserving the no-version cursor shape in `internal/feature/feed/service/feed_service_test.go`
- [X] T012 [P] [US1] Add service tests for trending continuation using `trend_score` and `trend_post_id`, including same-score tie handling in `internal/feature/feed/service/feed_service_test.go`
- [X] T013 [P] [US1] Add handler success tests proving `/feed` response `next_cursor` decodes to the no-version contract and omits `next_cursor` at completion in `internal/feature/feed/handler/feed_handler_test.go`

### Implementation for User Story 1

- [X] T014 [US1] Replace `FeedCursor` fields with no-version flat fields and remove emitted `v` from `Encode` in `internal/feature/feed/cursor.go`
- [X] T015 [US1] Update `DecodeFeedCursor` and `FeedCursor.Validate` to parse the no-version flat shape and validate `tl_user`, numeric positions, and tie-breaker requirements in `internal/feature/feed/cursor.go`
- [X] T016 [US1] Update timeline-first read path to consume and emit `tl_score`, `tl_post_id`, and `tl_user` in `internal/feature/feed/service/feed_service.go`
- [X] T017 [US1] Update mixed-source feed path to emit `rec_offset` in the no-version cursor only when recommendation continuation remains active in `internal/feature/feed/service/feed_service.go`
- [X] T018 [US1] Implement trending continuation filtering and next-cursor advancement with `trend_score` and `trend_post_id` in `internal/feature/feed/service/feed_service.go`
- [X] T019 [US1] Update feed service tests and mocks for the new cursor fields after implementation changes in `internal/feature/feed/service/feed_service_test.go`
- [X] T020 [US1] Update feed handler tests and mocks for the new cursor fields after implementation changes in `internal/feature/feed/handler/feed_handler_test.go`
- [X] T021 [US1] Run targeted feed cursor, service, and handler tests for `internal/feature/feed/...` with `go test ./internal/feature/feed/...`

**Checkpoint**: User Story 1 works independently with the new no-version cursor emitted and accepted for normal pagination.

---

## Phase 4: User Story 2 - Reject Obsolete Cursor Shapes (Priority: P2)

**Goal**: `/feed` rejects obsolete feed cursor payloads clearly, including versioned cursors, nested timeline cursors, and old session-style cursor state.

**Independent Test**: Send cursors containing `v`, nested `timeline`, old session fields, malformed payloads, negative positions, and mismatched `tl_user`; verify each returns the standard invalid cursor response.

### Tests for User Story 2

- [X] T022 [P] [US2] Add cursor rejection tests for payloads containing `v`, nested `timeline`, old session fields, negative positions, missing tie-breaker IDs, invalid UUIDs, and mismatched `tl_user` in `internal/feature/feed/cursor_test.go`
- [X] T023 [P] [US2] Add handler tests for obsolete cursor rejection with HTTP `400` and stable `BAD_REQUEST` error body in `internal/feature/feed/handler/feed_handler_test.go`
- [X] T024 [P] [US2] Add service tests proving invalid cursor validation returns bad request before source reads execute in `internal/feature/feed/service/feed_service_test.go`

### Implementation for User Story 2

- [X] T025 [US2] Add strict obsolete-field detection for `v`, nested `timeline`, session ID, pending items, and seen IDs in `internal/feature/feed/cursor.go`
- [X] T026 [US2] Update `/feed` handler cursor error path only as needed to preserve the standard invalid cursor response in `internal/feature/feed/handler/feed_handler.go`
- [X] T027 [US2] Ensure `FeedService.GetFeed` validates `tl_user` against the authenticated user before reading timeline, recommendation, trending, or fallback sources in `internal/feature/feed/service/feed_service.go`
- [X] T028 [US2] Run targeted obsolete cursor rejection tests for `internal/feature/feed/...` with `go test ./internal/feature/feed/...`

**Checkpoint**: User Story 2 rejects all obsolete and inconsistent cursor shapes while preserving missing-cursor startup.

---

## Phase 5: User Story 3 - Document Cursor Field Meaning (Priority: P3)

**Goal**: Cursor fields and `/feed` behavior are documented for client/backend coordination, including opacity, field purpose, validation, and unchanged `/discover` scope.

**Independent Test**: Review docs and Swagger output to confirm each cursor field is explained, old cursor rejection is documented, and `/discover` is unchanged.

### Tests for User Story 3

- [X] T029 [P] [US3] Add or update handler tests asserting Swagger-facing response types still use `data` and optional `next_cursor` in `internal/feature/feed/handler/feed_handler_test.go`
- [X] T030 [P] [US3] Add documentation consistency checks in feed cursor tests for field names expected by the no-version contract in `internal/feature/feed/cursor_test.go`

### Implementation for User Story 3

- [X] T031 [US3] Update `/feed` Swagger comments to describe the no-version opaque cursor and obsolete cursor rejection in `internal/feature/feed/handler/feed_handler.go`
- [X] T032 [US3] Update feed handler package documentation for the no-version cursor contract in `internal/feature/feed/handler/docs.go`
- [X] T033 [US3] Update `specs/005-feed-cursor-contract/contracts/cursor.md` if implementation chooses any final field-name adjustment during development
- [X] T034 [US3] Regenerate Swagger with `make swagger-generate` and verify `docs/docs.go`, `docs/swagger.json`, and `docs/swagger.yaml`
- [X] T035 [US3] Review Swagger diff for `/feed` and confirm `/discover` remains unchanged in `docs/swagger.json`

**Checkpoint**: User Story 3 provides clear cursor documentation and updated generated API docs.

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Final cleanup, verification, and project-wide checks.

- [X] T036 [P] Run `gofmt`/`goimports` on changed Go files under `internal/feature/feed/`
- [X] T037 [P] Review `internal/feature/feed/docs.go` and `internal/feature/feed/service/docs.go` for responsibility changes and update if needed
- [X] T038 Run targeted feed tests with `go test ./internal/feature/feed/...`
- [X] T039 Run full project tests through `Makefile` with `make test`
- [X] T040 Run lint through `Makefile` with `make lint`
- [X] T041 Execute quickstart verification steps from `specs/005-feed-cursor-contract/quickstart.md`
- [X] T042 Review final diffs for generated docs, `specs/005-feed-cursor-contract/`, and `internal/feature/feed/` before commit

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies; start immediately.
- **Foundational (Phase 2)**: Depends on Setup; blocks all user story implementation.
- **US1 (Phase 3)**: Depends on Foundational; MVP for new cursor emission and normal continuation.
- **US2 (Phase 4)**: Depends on US1 cursor model because strict obsolete-shape rejection is implemented in the same cursor parser.
- **US3 (Phase 5)**: Depends on US1 and US2 so docs describe the final accepted and rejected behavior.
- **Polish (Phase 6)**: Depends on all desired user stories being complete.

### User Story Dependencies

- **US1 Continue Feed With New Cursor (P1)**: Required MVP. Delivers the new contract for normal pagination.
- **US2 Reject Obsolete Cursor Shapes (P2)**: Depends on US1. Tightens validation and error behavior after the new parser exists.
- **US3 Document Cursor Field Meaning (P3)**: Depends on US1 and US2. Documents final behavior after implementation stabilizes.

### Within Each User Story

- Tests must be written first and fail for the new behavior before implementation.
- Cursor model changes before service and handler behavior.
- Service continuation behavior before handler success contract updates.
- Swagger regeneration after handler comment changes.
- Story checkpoint validation before moving to the next priority.

## Parallel Opportunities

- T002 and T003 can run in parallel during Setup.
- T004, T005, and T006 can run in parallel during Foundational characterization.
- T009 through T013 can run in parallel because they target different test concerns before implementation.
- T022 through T024 can run in parallel after US1 lands.
- T029 and T030 can run in parallel before documentation updates.
- T036 and T037 can run in parallel during Polish.

## Parallel Example: User Story 1

```text
Task: "Add cursor tests for no-version encode/decode with flat fields in internal/feature/feed/cursor_test.go"
Task: "Add service tests for timeline continuation in internal/feature/feed/service/feed_service_test.go"
Task: "Add service tests for recommendation continuation in internal/feature/feed/service/feed_service_test.go"
Task: "Add service tests for trending continuation in internal/feature/feed/service/feed_service_test.go"
Task: "Add handler success tests for no-version next_cursor in internal/feature/feed/handler/feed_handler_test.go"
```

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1 and Phase 2.
2. Implement US1 tests and cursor/service/handler changes.
3. Stop and validate with `go test ./internal/feature/feed/...`.
4. At this point, normal clients can use the new no-version cursor contract for pagination.

### Incremental Delivery

1. US1: emit and accept the new cursor for normal pagination.
2. US2: reject obsolete or inconsistent cursor shapes.
3. US3: update docs and Swagger for client coordination.
4. Polish: run full test, lint, quickstart verification, and final diff review.

### Team Parallelization

After Foundational tasks:

- Developer A can write cursor and handler tests for US1.
- Developer B can write service continuation tests for US1.
- Implementation should converge in `cursor.go` first, then `feed_service.go`, then handler/docs.

## Notes

- `[P]` tasks touch different files or independent test concerns and can run in parallel.
- `[US1]`, `[US2]`, and `[US3]` labels map to the user stories in `specs/005-feed-cursor-contract/spec.md`.
- Do not hand-edit generated DB files under `internal/feature/*/db`.
- Keep `/discover` behavior unchanged unless a future spec explicitly changes it.
