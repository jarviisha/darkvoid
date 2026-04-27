# Tasks: user/handler Meaningful Test Coverage

**Input**: `specs/001-handler-tests/plan.md`, `specs/001-handler-tests/research.md`
**Prerequisites**: plan.md ✅, research.md ✅ (no spec.md — tasks derived directly from gap analysis)

**Organization**: Tasks grouped by handler file. Each group is independently implementable
and testable. All groups can be worked in parallel once Phase 1 is confirmed.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Handler group (US1–US5 map to the five files touched)

---

## Phase 1: Setup

**Purpose**: Confirm baseline passes before adding tests.

- [x] T001 Run `go test ./internal/feature/user/handler/... -count=1` — must pass at 68.4%

**Checkpoint**: Baseline green → all story phases can begin in parallel.

---

## Phase 2: US1 — EmailHandler (new file, 0% → ~100%) 🎯 Most Critical

**Goal**: Create `email_handler_test.go` with a `mockEmailService`, helper `newEmailHandler`,
and 12 tests covering the four EmailHandler methods.

**Independent Test**: `go test ./internal/feature/user/handler/... -run TestVerify -v`

**Why first**: Security-critical flows (token expiry, password reset, account-existence
obscurity) — highest risk of regressions going undetected.

- [x] T002 [US1] Create `internal/feature/user/handler/email_handler_test.go` with `mockEmailService` struct (fields: `verifyEmail`, `resendVerification`, `sendPasswordReset`, `resetPassword` func fields) and `newEmailHandler` helper
- [x] T003 [P] [US1] Add `TestVerifyEmail_Success` — valid token → 200 with success message in `internal/feature/user/handler/email_handler_test.go`
- [x] T004 [P] [US1] Add `TestVerifyEmail_InvalidJSON` — malformed body → 400 in `internal/feature/user/handler/email_handler_test.go`
- [x] T005 [P] [US1] Add `TestVerifyEmail_ServiceError` — service returns error → error propagated in `internal/feature/user/handler/email_handler_test.go`
- [x] T006 [P] [US1] Add `TestResendVerification_Success` — assert 200 regardless of service outcome (account-existence obscurity preserved) in `internal/feature/user/handler/email_handler_test.go`
- [x] T007 [P] [US1] Add `TestResendVerification_InvalidJSON` → 400 in `internal/feature/user/handler/email_handler_test.go`
- [x] T008 [P] [US1] Add `TestResendVerification_ServiceError` — service errors are propagated (see handler: `errors.WriteJSON(w, err)`) in `internal/feature/user/handler/email_handler_test.go`
- [x] T009 [P] [US1] Add `TestForgotPassword_Success` — assert 200 (account-existence obscurity) in `internal/feature/user/handler/email_handler_test.go`
- [x] T010 [P] [US1] Add `TestForgotPassword_InvalidJSON` → 400 in `internal/feature/user/handler/email_handler_test.go`
- [x] T011 [P] [US1] Add `TestForgotPassword_ServiceError` — error propagated in `internal/feature/user/handler/email_handler_test.go`
- [x] T012 [P] [US1] Add `TestResetPassword_Success` — token + new password → 200 in `internal/feature/user/handler/email_handler_test.go`
- [x] T013 [P] [US1] Add `TestResetPassword_InvalidJSON` → 400 in `internal/feature/user/handler/email_handler_test.go`
- [x] T014 [P] [US1] Add `TestResetPassword_ServiceError` — expired/invalid token error propagated in `internal/feature/user/handler/email_handler_test.go`

**Checkpoint**: `go test ./internal/feature/user/handler/... -run "TestVerify|TestResend|TestForgot|TestReset"` — all 12 pass.

---

## Phase 3: US2 — AuthHandler mobile/web divergence (75–86% → ~100%)

**Goal**: Cover the untested mobile success paths and missing service-error paths in
`RefreshToken`, `Logout`, and `LogoutAllSessions`.

**Independent Test**: `go test ./internal/feature/user/handler/... -run TestRefresh -v`

- [x] T015 [P] [US2] Add `TestRefreshToken_MobileSuccess` in `internal/feature/user/handler/auth_handler_test.go` — set `X-Client-Type: mobile`, send token in body → new token appears in JSON body, **no** `Set-Cookie` header
- [x] T016 [P] [US2] Add `TestRefreshToken_WebMissingCookie` in `internal/feature/user/handler/auth_handler_test.go` — no cookie, no mobile header → 401
- [x] T017 [P] [US2] Add `TestLogout_MobileSuccess` in `internal/feature/user/handler/auth_handler_test.go` — `X-Client-Type: mobile`, token in body → service called, **no** `Set-Cookie` header in response
- [x] T018 [P] [US2] Add `TestLogout_ServiceError` in `internal/feature/user/handler/auth_handler_test.go` — service returns error → error response propagated (web cookie path)
- [x] T019 [P] [US2] Add `TestLogoutAllSessions_ServiceError` in `internal/feature/user/handler/auth_handler_test.go` — service error → error propagated
- [x] T020 [P] [US2] Add `TestLogoutAllSessions_MobileNoCookieClear` in `internal/feature/user/handler/auth_handler_test.go` — `X-Client-Type: mobile` → 200, assert no `Set-Cookie` header

**Checkpoint**: `go test ./internal/feature/user/handler/... -run "TestRefresh|TestLogout"` — all pass.

---

## Phase 4: US3 — UserHandler authorization gates (89/85% → 100%)

**Goal**: Verify the 403 guard in `UpdateUser` and `DeactivateUser` — the most security-
sensitive untested path: a user attempting to modify another user's account.

**Independent Test**: `go test ./internal/feature/user/handler/... -run TestUpdateUser_Forbidden -v`

- [x] T021 [P] [US3] Add `TestUpdateUser_Forbidden` in `internal/feature/user/handler/user_handler_test.go` — authenticate as user A, provide user B's UUID in path → 403 FORBIDDEN
- [x] T022 [P] [US3] Add `TestDeactivateUser_Forbidden` in `internal/feature/user/handler/user_handler_test.go` — authenticate as user A, provide user B's UUID in path → 403 FORBIDDEN

**Checkpoint**: `go test ./internal/feature/user/handler/... -run "TestUpdateUser_Forbidden|TestDeactivateUser_Forbidden"` — both pass.

---

## Phase 5: US4 — ProfileHandler uploads + username resolution (0/81% → ~95%)

**Goal**: Test `UploadAvatar`, `UploadCover` (0%), the `?by=username` resolution path in
`GetUserProfile`, and the `is_following` enrichment. Requires adding `mockFollowChecker`
and an extended `newProfileHandlerWithChecker` helper.

**Independent Test**: `go test ./internal/feature/user/handler/... -run TestUpload -v`

- [x] T023 [US4] Add `mockFollowChecker` struct and `newProfileHandlerWithChecker(svc profileService, checker followChecker) *ProfileHandler` helper in `internal/feature/user/handler/profile_handler_test.go`
- [x] T024 [P] [US4] Add `multipartRequest` test helper (creates a `multipart/form-data` request with a named file field) in `internal/feature/user/handler/profile_handler_test.go`
- [x] T025 [P] [US4] Add `TestUploadAvatar_Success` — authenticated, valid multipart with `file` field → 200 with user response in `internal/feature/user/handler/profile_handler_test.go`
- [x] T026 [P] [US4] Add `TestUploadAvatar_NotAuthenticated` — no user in context → 401 in `internal/feature/user/handler/profile_handler_test.go`
- [x] T027 [P] [US4] Add `TestUploadAvatar_MissingFile` — multipart without `file` field → 400 in `internal/feature/user/handler/profile_handler_test.go`
- [x] T028 [P] [US4] Add `TestUploadAvatar_ServiceError` — service returns error → error propagated in `internal/feature/user/handler/profile_handler_test.go`
- [x] T029 [P] [US4] Add `TestUploadCover_Success` — same flow as avatar → 200 in `internal/feature/user/handler/profile_handler_test.go`
- [x] T030 [P] [US4] Add `TestUploadCover_NotAuthenticated` → 401 in `internal/feature/user/handler/profile_handler_test.go`
- [x] T031 [P] [US4] Add `TestUploadCover_MissingFile` → 400 in `internal/feature/user/handler/profile_handler_test.go`
- [x] T032 [P] [US4] Add `TestUploadCover_ServiceError` → error propagated in `internal/feature/user/handler/profile_handler_test.go`
- [x] T033 [P] [US4] Add `TestGetUserProfile_ByUsername` — `?by=username` query param → resolver called, `GetProfileByUserID` NOT called (assert mock call count), 200 in `internal/feature/user/handler/profile_handler_test.go`
- [x] T034 [P] [US4] Add `TestGetUserProfile_ByUsernameNotFound` — `?by=username` + resolver error → 404 in `internal/feature/user/handler/profile_handler_test.go`
- [x] T035 [P] [US4] Add `TestGetUserProfile_IsFollowingEnriched` — authenticated viewer (not profile owner) → `followChecker.IsFollowing` called, `is_following` field set in response in `internal/feature/user/handler/profile_handler_test.go`

**Checkpoint**: `go test ./internal/feature/user/handler/... -run "TestUpload|TestGetUserProfile"` — all pass.

---

## Phase 6: US5 — FollowHandler service error gap (85% → 100%)

**Goal**: Add the one missing service-error case for `GetFollowing`.

**Independent Test**: `go test ./internal/feature/user/handler/... -run TestGetFollowing_ServiceError -v`

- [x] T036 [US5] Add `TestGetFollowing_ServiceError` in `internal/feature/user/handler/follow_handler_test.go` — service returns error → error propagated (mirrors existing `TestGetFollowers_ServiceError`)

**Checkpoint**: `go test ./internal/feature/user/handler/... -run TestGetFollowing` — all pass.

---

## Phase 7: Polish & Validation

**Purpose**: Confirm the full suite is clean and the coverage goal is met.

- [x] T037 Run `make lint` — all new test code must pass golangci-lint with zero suppressions
- [x] T038 Run `go test ./internal/feature/user/handler/... -coverprofile=/tmp/handler.out` and confirm coverage is ≥ 85% (target; email_handler + upload handlers drive most of the gain)
- [x] T039 Run `make test` — full suite must pass with zero failures

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1 (Setup)**: No dependencies — start immediately
- **Phases 2–6 (US1–US5)**: All depend only on Phase 1 passing — can run in parallel
- **Phase 7 (Polish)**: Depends on all US phases complete

### Within Each User Story

- T002 (mock + helper) must complete before T003–T014 in US1
- T023 (mock + helper) must complete before T025–T035 in US4
- T024 (multipart helper) must complete before T025–T032 in US4
- All other tasks within a story are independent ([P])

### Parallel Opportunities

All five US phases touch different files — a team of two could split US1+US4 (largest) and
US2+US3+US5 (smaller) in parallel.

---

## Parallel Example: US4 (after T023 + T024 complete)

```text
Task: TestUploadAvatar_Success       → profile_handler_test.go
Task: TestUploadAvatar_NotAuthenticated → profile_handler_test.go [sequential within file]
Task: TestUploadCover_Success        → profile_handler_test.go
Task: TestGetUserProfile_ByUsername  → profile_handler_test.go
Task: TestGetUserProfile_IsFollowingEnriched → profile_handler_test.go
```

(Within a single file, tasks are sequential; across files they are truly parallel.)

---

## Implementation Strategy

### MVP Scope (single session)

1. Phase 1 — confirm baseline (1 command)
2. Phase 2 — `email_handler_test.go` (T002–T014): highest value, self-contained new file
3. Phase 4 — `user_handler_test.go` auth gates (T021–T022): security risk, 2 tests
4. Phase 7 — lint + coverage check

Remaining phases (US2, US4, US5) add depth but don't carry the same security urgency.

### Full Delivery Order

Phase 1 → Phases 2–6 in parallel → Phase 7

---

## Notes

- [P] marks tasks that touch no shared state with sibling tasks — safe to implement
  concurrently within a phase
- `multipartRequest` helper (T024) must be defined before upload tests (T025–T032) but is
  itself a simple stdlib function — no external deps needed
- `mockFollowChecker` (T023) is a new mock not present in any existing test file — it belongs
  in `profile_handler_test.go` alongside `mockProfileService`
- `mockEmailService` (T002) is a new mock not present in any existing test file
- Do **not** add `//nolint` directives — fix any lint issues at the root cause per
  Constitution Principle I
