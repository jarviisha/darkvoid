# Feature Specification: Post Feature Meaningful Test Coverage

**Feature Branch**: `002-post-test-coverage`  
**Created**: 2026-04-27  
**Status**: Draft  

## User Scenarios & Testing *(mandatory)*

<!--
  Each story is a self-contained coverage increment. Each can be written and verified
  independently without depending on other stories.
-->

### User Story 1 – Complete Handler Coverage for CommentLike and Hashtag (Priority: P1)

`ToggleCommentLike`, `GetTrendingHashtags`, `SearchHashtags`, and `GetPostsByHashtag` are live HTTP
handlers with zero test coverage today. These handlers contain authentication gates, input validation,
and error-to-HTTP-status mapping that must be exercised to catch regressions.

**Why this priority**: Handler behaviour is the outermost contract — a broken handler silently returns
wrong status codes to clients. These are also the easiest to test with the existing mock infrastructure
already in place for other handlers.

**Independent Test**: Running `go test ./internal/feature/post/handler/...` passes and handler
package coverage rises above 90%.

**Acceptance Scenarios**:

1. **Given** an unauthenticated request, **When** `POST /posts/{id}/comments/{id}/like` is called, **Then** HTTP 401 is returned.
2. **Given** a malformed comment UUID in the URL, **When** `ToggleCommentLike` is called, **Then** HTTP 400 is returned.
3. **Given** a valid authenticated request, **When** `ToggleCommentLike` succeeds, **Then** HTTP 200 with `{"liked": true/false}` is returned.
4. **Given** a GET request to `/hashtags/trending`, **When** the service succeeds, **Then** HTTP 200 with trending list is returned.
5. **Given** a search query shorter than 2 characters, **When** `SearchHashtags` is called, **Then** HTTP 400 is returned.
6. **Given** a valid search query, **When** `SearchHashtags` succeeds, **Then** HTTP 200 with prefix + results is returned.
7. **Given** a valid hashtag name, **When** `GetPostsByHashtag` is called, **Then** HTTP 200 with paginated posts is returned.
8. **Given** an invalid cursor, **When** `GetPostsByHashtag` is called, **Then** HTTP 400 is returned.

---

### User Story 2 – LikeService and CommentLikeService Toggle Logic (Priority: P2)

`LikeService.Toggle` and `CommentLikeService.Toggle` are stateful operations that branch on current
like state. They have 0% coverage. The comment-like service additionally enforces a self-like
prohibition that has no test path today.

**Why this priority**: Toggle is a read-then-write operation; bugs here can cause double-likes or
missed unlikes. The self-like guard is security-relevant and must be verified.

**Independent Test**: Running `go test ./internal/feature/post/service/... -run Toggle` passes and
covers the full toggle state machine for both services.

**Acceptance Scenarios**:

1. **Given** a post that the user has not yet liked, **When** `LikeService.Toggle` is called, **Then** it returns `(true, nil)` and the like is persisted.
2. **Given** a post that the user already likes, **When** `LikeService.Toggle` is called, **Then** it returns `(false, nil)` and the like is removed.
3. **Given** a non-existent post, **When** `LikeService.Toggle` is called, **Then** `ErrPostNotFound` is returned.
4. **Given** a comment that the user has not yet liked, **When** `CommentLikeService.Toggle` is called, **Then** it returns `(true, nil)`.
5. **Given** a comment that the user already likes, **When** `CommentLikeService.Toggle` is called, **Then** it returns `(false, nil)`.
6. **Given** the user is the comment author, **When** `CommentLikeService.Toggle` is called, **Then** `ErrSelfLike` is returned.
7. **Given** a non-existent comment, **When** `CommentLikeService.Toggle` is called, **Then** `ErrCommentNotFound` is returned.

---

### User Story 3 – HashtagService Business Logic (Priority: P3)

`HashtagService` (GetTrending, SearchHashtags, GetPostsByHashtag) has 0% coverage. It has
non-trivial cache hit/miss branching and input validation logic that is worth exercising.

**Why this priority**: Hashtag search and trending are user-visible features. The cache path silently
swallows errors; tests that confirm non-fatal cache failures still return correct data are valuable.

**Independent Test**: Running `go test ./internal/feature/post/service/... -run Hashtag` passes.

**Acceptance Scenarios**:

1. **Given** cached trending tags exist, **When** `GetTrending` is called, **Then** the cached value is returned without hitting the repository.
2. **Given** no cache entry, **When** `GetTrending` is called, **Then** the repository is queried and the result is cached.
3. **Given** a prefix shorter than 2 characters, **When** `SearchHashtags` is called, **Then** a bad-request error is returned.
4. **Given** a valid prefix with a cache hit, **When** `SearchHashtags` is called, **Then** cached results are returned.
5. **Given** a valid prefix with no cache, **When** `SearchHashtags` is called, **Then** repository results are returned and cached.
6. **Given** a hashtag name with page 1 cache hit, **When** `GetPostsByHashtag` is called with no cursor, **Then** the cached page is returned.
7. **Given** a cache set failure, **When** `GetTrending` completes, **Then** the repository result is still returned (non-fatal cache error).

---

### User Story 4 – CommentService Enrichment Functions (Priority: P4)

`enrichLikes`, `enrichAuthors`, and `enrichCommentMentions` within `CommentService` sit below 15%
coverage. These helpers run on every comment list response and their error-tolerance branches
(non-fatal failures) are completely untested.

**Why this priority**: These functions are called on every `GetComments` and `GetReplies` response.
The enrichment pattern (best-effort, non-fatal errors) is a contract relied on by callers; bugs
silently degrade comment data without surfacing errors.

**Independent Test**: Running `go test ./internal/feature/post/service/... -run Enrich` includes the
comment enrichment paths and comment service coverage rises above 80%.

**Acceptance Scenarios**:

1. **Given** an empty comment slice, **When** `enrichLikes` is called, **Then** it returns without error and no repository calls are made.
2. **Given** a nil like repository, **When** `enrichLikes` is called, **Then** it returns without error.
3. **Given** a like repository error, **When** `enrichLikes` is called, **Then** the error is swallowed (non-fatal) and comments are returned unchanged.
4. **Given** an empty comment slice, **When** `enrichAuthors` is called, **Then** it returns without error.
5. **Given** a user-reader error, **When** `enrichAuthors` is called, **Then** the error is swallowed and comments have no author info.
6. **Given** comments with mentions, **When** `enrichCommentMentions` is called, **Then** mention user data is attached to each comment.
7. **Given** a mention repository error, **When** `enrichCommentMentions` is called, **Then** the error is swallowed and comments are returned.

---

### User Story 5 – Repository Critical-Path Error Mapping (Priority: P5)

`PostRepository` and `LikeRepository` have 0% test coverage. Only the error-mapping paths are worth
unit-testing: verifying that database not-found and unique-constraint violations are correctly
translated to domain errors. The sqlc-generated code under `db/` is explicitly out of scope.

**Why this priority**: Error mapping is the repository's primary responsibility beyond data transfer.
A misconfigured `MapDBError` silently propagates raw Postgres errors to service callers.

**Independent Test**: Running `go test ./internal/feature/post/repository/...` passes without
requiring a live database (using mock `DBTX`).

**Acceptance Scenarios**:

1. **Given** a `pgx.ErrNoRows` result from the DB, **When** `PostRepository.GetByID` is called, **Then** `errors.ErrNotFound` is returned.
2. **Given** a unique-constraint violation, **When** `LikeRepository.Like` is called, **Then** a domain-appropriate conflict error is returned.
3. **Given** a generic DB error, **When** any repository method is called, **Then** the raw error is wrapped (not returned as-is) so internal details are not leaked.

---

### Edge Cases

- What happens when `GetPostsByHashtag` receives a negative or zero limit?
- What happens when toggle is called concurrently for the same user/post — is idempotency guaranteed by the repository layer?
- What happens when enrichment is called with posts whose author IDs no longer exist in the user table?

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: Every test MUST verify observable behavior (return values, HTTP status, error type) — not internal call counts alone.
- **FR-002**: Tests for `db/` (sqlc-generated files) MUST NOT be written.
- **FR-003**: Repository tests MUST NOT require a live database; they MUST use mock or stub implementations of `DBTX`.
- **FR-004**: All new tests MUST pass `make lint` without suppression directives.
- **FR-005**: Each new test file MUST follow the existing naming convention `<subject>_test.go` colocated with the file under test.
- **FR-006**: Service tests MUST use the existing mock infrastructure in `post_test_helpers.go` where applicable rather than introducing new mock types.
- **FR-007**: Handler tests MUST reuse the test helper and mock pattern established in `post_handler_test.go` and `comment_handler_test.go`.

### Key Entities

- **PostRepository**: wraps sqlc `Queries`, maps `pgx.ErrNoRows` → `errors.ErrNotFound` and unique violations → conflict errors.
- **LikeRepository**: like/unlike/isLiked operations, conflict on duplicate like.
- **LikeService / CommentLikeService**: toggle state machine (read-then-write), self-like guard.
- **HashtagService**: cache-first reads with non-fatal cache write failures.
- **CommentService enrichment**: best-effort decoration of comment slices (likes, authors, mentions).
- **CommentLikeHandler / HashtagHandler**: HTTP adapters with auth guards and input validation.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Handler package coverage rises from 67.2% to ≥ 90% after Story 1 is complete.
- **SC-002**: Service package coverage rises from 50.7% to ≥ 75% after Stories 2, 3, and 4 are complete.
- **SC-003**: Repository package gains tests for ≥ 3 critical error-mapping paths (Story 5).
- **SC-004**: All new tests pass `make test` and `make lint` with zero new linter suppressions.
- **SC-005**: No test is added whose only purpose is incrementing a percentage counter — every test must assert a specific behavior that could catch a real regression.

## Assumptions

- The existing `post_test_helpers.go` mock stubs are sufficient to cover new service tests; no new third-party mock library is needed.
- Repository tests use an in-process mock of `DBTX` (already partially present in helpers) — no Postgres test container is required.
- `db/` files are sqlc-generated and maintained upstream; they are excluded from all new test targets.
- Cache behaviour in HashtagService tests uses a simple in-memory stub (nil-safe no-op is acceptable for miss branches).
- Comment enrichment functions are unexported (`enrichLikes`, `enrichAuthors`, etc.) but are reachable through the exported `GetComments` / `GetReplies` paths, so they do not require `_test` package tricks to exercise.
