# Tasks: Post Feature Meaningful Test Coverage

**Input**: Design documents from `specs/002-post-test-coverage/`
**Prerequisites**: plan.md ✅, spec.md ✅, research.md ✅

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no incomplete dependencies)
- **[Story]**: Which user story this maps to (US1–US4 from spec.md)
- Exact file paths are included in every description

---

## Phase 1: Setup — Extend Test Helpers (Foundational for US2/US3/US4)

**Purpose**: Add the three mock types and five helper constructors that US2, US3, and US4 depend on.
US1 (handler tests) is **independent** of this phase and can start immediately in parallel.

**⚠️ US2, US3, US4 cannot start until this phase is complete.**

- [x] T001 Add `mockCommentLikeRepo` struct implementing `commentLikeRepo` interface to `internal/feature/post/service/post_test_helpers.go` — fields: `like`, `unlike`, `isLiked`, `getLikedCommentIDs` funcs; defaults: `IsLiked` returns `(false, nil)`, others return nil/empty
- [x] T002 Add `mockHashtagCache` struct implementing `hashtagCache` interface to `internal/feature/post/service/post_test_helpers.go` — all `Get*` fields default to returning `(nil, errors.New("cache miss"))`; all `Set*`/`Invalidate*` fields default to returning `nil`
- [x] T003 Add helper constructors `newCommentLikeService(clr commentLikeRepo, cr commentRepo) *CommentLikeService`, `commentExists(authorID uuid.UUID) *mockCommentRepo`, and `newHashtagService(hr hashtagRepo, hc hashtagCache, pr postRepo) *HashtagService` to `internal/feature/post/service/post_test_helpers.go`

**Checkpoint**: `go build ./internal/feature/post/service/...` passes — US2, US3, US4 can now proceed

---

## Phase 2: User Story 1 — Handler Coverage (Priority: P1) 🎯 MVP

**Goal**: Bring `post/handler` package from 67.2% to ≥ 90% by testing `ToggleCommentLike`, all three hashtag handlers, and missing `GetUserPosts` branches.

**Independent Test**: `go test ./internal/feature/post/handler/... -cover` reports ≥ 90%

> **Note**: This phase is fully independent of Phase 1. Tasks T004–T006 can run in parallel with T001–T003 and with each other.

- [x] T004 [P] [US1] Create `internal/feature/post/handler/comment_like_handler_test.go` with inline `mockCommentLikeService` and these 7 tests: `TestToggleCommentLike_Success_Liked` (HTTP 200 `{"liked":true}`), `TestToggleCommentLike_Success_Unliked` (HTTP 200 `{"liked":false}`), `TestToggleCommentLike_Unauthenticated` (HTTP 401), `TestToggleCommentLike_InvalidCommentID` (commentID="bad" → HTTP 400), `TestToggleCommentLike_CommentNotFound` (service returns `post.ErrCommentNotFound` → HTTP 404), `TestToggleCommentLike_SelfLike` (service returns `post.ErrSelfLike` → HTTP 400, assert error code "SELF_LIKE"), `TestToggleCommentLike_ServiceError` (generic error propagated); use `withAuth`, `withChiParam`, `assertStatus`, `assertErrCode` helpers from `post_handler_test.go`
- [x] T005 [P] [US1] Create `internal/feature/post/handler/hashtag_handler_test.go` with inline `mockHashtagSvc` (implementing `hashtagSvc` interface) and `newHashtagHandler` that passes `storage.NewNop("")` as the store; add these 10 tests — `GetTrendingHashtags`: `TestGetTrendingHashtags_Success` (HTTP 200 with list), `TestGetTrendingHashtags_ServiceError` (error propagated); `SearchHashtags`: `TestSearchHashtags_Success` (q="go" → HTTP 200, body has prefix="go"), `TestSearchHashtags_QueryTooShort` (q="a" → HTTP 400), `TestSearchHashtags_ServiceError`; `GetPostsByHashtag`: `TestGetPostsByHashtag_Success` (name="golang" → HTTP 200), `TestGetPostsByHashtag_MissingName` (chi param="" → HTTP 400), `TestGetPostsByHashtag_InvalidCursor` (cursor="!!bad" → HTTP 400), `TestGetPostsByHashtag_ServiceError`, `TestGetPostsByHashtag_LimitCappedAt100` (limit=200 in query → service called with limit=100, assert via mock capture)
- [x] T006 [P] [US1] Extend `internal/feature/post/handler/post_handler_test.go` with 4 `GetUserPosts` tests: `TestGetUserPosts_ServiceError` (service returns error → error JSON propagated), `TestGetUserPosts_AuthenticatedViewer` (authenticated request → viewerID forwarded, assert service receives non-nil viewerID), `TestGetUserPosts_CustomLimit` (limit=50 in query → service receives 50), `TestGetUserPosts_WithCursor` (valid base64 cursor in query → service receives non-nil cursor); use existing `newPostHandler` helper and `assertStatus`

**Checkpoint**: `go test ./internal/feature/post/handler/... -cover` ≥ 90%

---

## Phase 3: User Story 2 — Toggle State Machine Tests (Priority: P2)

**Goal**: Test `LikeService.Toggle` and `CommentLikeService.Toggle` — the read-then-write state machines and their self-like/not-found guards (currently 0% covered).

**Independent Test**: `go test ./internal/feature/post/service/... -run Toggle -v` passes with ≥ 10 cases

**Depends on**: Phase 1 complete (T001–T003)

- [x] T007 [US2] Extend `internal/feature/post/service/comment_like_service_test.go` — add section `// --- CommentLikeService.Toggle ---` with 6 tests using `newCommentLikeService` and `commentExists`: `TestToggleCommentLike_Like_WhenNotLiked` (isLiked=false → Like called, returns true), `TestToggleCommentLike_Unlike_WhenAlreadyLiked` (isLiked=true → Unlike called, returns false), `TestToggleCommentLike_CommentNotFound` (GetByID returns ErrNotFound → ErrCommentNotFound), `TestToggleCommentLike_SelfLike` (comment.AuthorID == userID → ErrSelfLike), `TestToggleCommentLike_IsLikedError` (IsLiked returns error → internal error), `TestToggleCommentLike_LikeRepoError` (Like returns error → internal error)
- [x] T008 [US2] Extend `internal/feature/post/service/comment_like_service_test.go` — add section `// --- LikeService.Toggle ---` with 4 tests using `newLikeService` and `mockLikeRepo`: `TestToggle_Like_WhenNotLiked` (isLiked=false → Like called, returns (true,nil)), `TestToggle_Unlike_WhenAlreadyLiked` (isLiked=true → Unlike called, returns (false,nil)), `TestToggle_PostNotFound` (postRepo.GetByID returns ErrNotFound → ErrPostNotFound), `TestToggle_IsLikedError` (IsLiked returns error → internal error propagated)

**Checkpoint**: `go test ./internal/feature/post/service/... -run "TestToggle"` — all 10 pass

---

## Phase 4: User Story 3 — HashtagService Tests (Priority: P3)

**Goal**: Test cache-hit/miss branching and input validation in `HashtagService.GetTrending`, `SearchHashtags`, and `GetPostsByHashtag` (currently 0% covered).

**Independent Test**: `go test ./internal/feature/post/service/... -run Hashtag -v` passes with ≥ 14 cases

**Depends on**: Phase 1 complete (T001–T003)

- [x] T009 [US3] Create `internal/feature/post/service/hashtag_service_test.go` with `package service` — use `newHashtagService` from helpers and build 14 tests: `GetTrending` (4): `TestGetTrending_CacheHit` (cache returns tags → repo NOT called, assert with a `getTrending` func that calls `t.Fatal`), `TestGetTrending_CacheMiss_RepoSuccess` (cache miss → repo returns tags → SetTrendingHashtags called), `TestGetTrending_CacheMiss_RepoError` (cache miss + repo error → internal error), `TestGetTrending_CacheSetError_NonFatal` (repo success + set error → tags still returned); `SearchHashtags` (5): `TestSearchHashtags_PrefixTooShort` (prefix="a" → bad-request error), `TestSearchHashtags_CacheHit`, `TestSearchHashtags_CacheMiss_RepoSuccess`, `TestSearchHashtags_CacheMiss_RepoError`, `TestSearchHashtags_CacheSetError_NonFatal`; `GetPostsByHashtag` (5): `TestGetPostsByHashtag_CacheHit_Page1` (cursor=nil + cache returns page → repo NOT called), `TestGetPostsByHashtag_CacheMiss_RepoSuccess` (cursor=nil + cache miss → repo called), `TestGetPostsByHashtag_Page2_SkipsCache` (cursor non-nil → `GetHashtagPostsPage1` NOT called), `TestGetPostsByHashtag_RepoError`, `TestGetPostsByHashtag_DefaultLimit` (limit=0 → repo called with limit=20)

**Checkpoint**: `go test ./internal/feature/post/service/... -run "TestGetTrending|TestSearch|TestGetPostsByHashtag"` — all 14 pass

---

## Phase 5: User Story 4 — CommentService Enrichment Tests (Priority: P4)

**Goal**: Test the non-fatal enrichment branches (`enrichLikes`, `enrichAuthors`, `enrichCommentMentions`) indirectly through `GetComments`, verifying nil-guard and error-tolerance contracts (currently 6–14% covered).

**Independent Test**: `go test ./internal/feature/post/service/... -run "EnrichLikes|EnrichAuthors|Enrich"` passes

**Depends on**: Phase 1 complete (T001–T003)

- [x] T010 [US4] Extend `internal/feature/post/service/comment_like_service_test.go` — add section `// --- CommentService enrichment ---` with 6 tests that call `GetComments` after setting fields directly on the service (same package): `TestGetComments_EnrichLikes_WithViewer` (commentLikeRepo wired, GetLikedCommentIDs returns commentID → comment.IsLiked=true), `TestGetComments_EnrichLikes_NilCommentLikeRepo` (commentLikeRepo=nil → IsLiked stays false, no panic), `TestGetComments_EnrichLikes_RepoError_NonFatal` (GetLikedCommentIDs returns error → error swallowed, comments returned), `TestGetComments_EnrichAuthors_Success` (userReader wired, GetAuthorsByIDs returns author → comment.Author populated), `TestGetComments_EnrichAuthors_NilUserReader` (userReader=nil → Author stays nil, no panic), `TestGetComments_EnrichAuthors_RepoError_NonFatal` (GetAuthorsByIDs returns error → error swallowed, comments returned); for each test construct a service via `newCommentService`, then set `svc.commentLikeRepo`/`svc.userReader` directly before calling `GetComments`

**Checkpoint**: `go test ./internal/feature/post/service/... -run "TestGetComments_Enrich"` — all 6 pass

---

## Phase 6: Polish & Verification

**Purpose**: Confirm coverage targets are met and no linter regressions introduced.

- [x] T011 Run `make test` from repo root and confirm zero test failures across all packages in `internal/feature/post/`
- [x] T012 Run `make lint` from repo root and fix any linter issues in new test files (common: unused imports, `require` replaceable by `if err != nil { t.Fatal(...) }`, `errcheck` on unchecked returns)
- [x] T013 Run `go test ./internal/feature/post/handler/... -coverprofile=/tmp/h.out && go tool cover -func=/tmp/h.out | tail -1` and confirm total ≥ 90%; if below, identify uncovered lines with `go tool cover -func` and add targeted tests
- [x] T014 Run `go test ./internal/feature/post/service/... -coverprofile=/tmp/s.out && go tool cover -func=/tmp/s.out | tail -1` and confirm total ≥ 75%; if below, identify gaps in `hashtag_service.go` or `comment_like_service.go` and add targeted tests

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1 (Setup)**: No dependencies — start immediately
- **US1 (Phase 2)**: **Independent of Phase 1** — can start immediately and in parallel with Phase 1
- **US2 (Phase 3)**: Depends on Phase 1 (needs `mockCommentLikeRepo`, `newCommentLikeService`)
- **US3 (Phase 4)**: Depends on Phase 1 (needs `mockHashtagCache`, `newHashtagService`)
- **US4 (Phase 5)**: Depends on Phase 1 (needs helpers, but set fields directly on service)
- **Polish (Phase 6)**: Depends on all previous phases

### User Story Dependencies

- **US1 (P1)**: Independent — no story dependencies
- **US2 (P2)**: Depends on Phase 1 setup only
- **US3 (P3)**: Depends on Phase 1 setup only
- **US4 (P4)**: Depends on Phase 1 setup only

### Within Each Phase

- T004, T005, T006 (handler test files) — different files, fully parallel
- T007, T008 share `comment_like_service_test.go` — write sequentially to avoid conflicts
- T009 is a single new file — no concurrency concern
- T010 appends to `comment_like_service_test.go` — write after T007/T008 complete
- T011–T014 are sequential validation steps

### Parallel Opportunities

```bash
# Start immediately (no dependencies):
Phase 1:  T001, T002, T003  (extend post_test_helpers.go — sequential, same file)
US1:      T004, T005, T006  (handler tests — fully parallel, different files)

# After Phase 1 completes:
US2: T007 → T008  (same file, sequential)
US3: T009         (new file)
US4: T010         (same file as T007/T008, add after US2)
# US2, US3 can run in parallel with each other once Phase 1 is done
```

---

## Implementation Strategy

### MVP First (US1 Only — handler coverage)

1. Complete Phase 1 (T001–T003) to unlock service tests later
2. **Immediately**: T004, T005, T006 in parallel — these are independent
3. Run `make test` and `go test ./handler/... -cover` → handler ≥ 90%
4. **Stop and validate** — handler coverage target met

### Full Delivery (All Stories)

1. Phase 1 (T001–T003) — extend helpers
2. US1 (T004–T006) in parallel — handler tests
3. US2 (T007–T008) + US3 (T009) in parallel — service toggle + hashtag
4. US4 (T010) — enrichment tests
5. Polish (T011–T014) — verify and lint

---

## Notes

- `mockCommentLikeRepo` and `mockHashtagCache` live in `post_test_helpers.go` (same `package service`) — no new files needed for helpers
- All handler tests use `storage.NewNop("")` (exported from `pkg/storage`) — do NOT pass nil storage, `dto.ToPostResponse` calls `store.URL()` unconditionally
- `enrichLikes`/`enrichAuthors` are unexported; test them through `GetComments` (same package, no reflection needed)
- For `TestToggleCommentLike_*` in the handler: use `assertErrCode` from `post_handler_test.go` to verify `"SELF_LIKE"` error code
- Repository tests (originally US5) are **descoped** — constitution Principle II prohibits mocking the database; repository methods are 3-line sqlc pass-throughs
