# Research: Post Feature Test Coverage

**Branch**: `002-post-test-coverage` | **Date**: 2026-04-27

## Coverage Baseline

| Package | Before | Target |
|---|---|---|
| `post/handler` | 67.2% | ≥ 90% |
| `post/service` | 50.7% | ≥ 75% |
| `post/repository` | 0.0% | (see decision below) |
| `post/db` (sqlc) | — | excluded by spec |

## Decision 1: Repository Unit Tests Are Descoped

**Decision**: Do not write unit tests for `repository/*.go` that mock `db.DBTX`.

**Rationale**: The project constitution (Principle II) forbids mocking the database: "Tests MUST NOT mock
the database. Integration tests MUST exercise real DB behavior; mock/prod divergence has caused
production incidents." Each repository method is a 3-line pass-through:
`MapDBError(r.queries.<Generated>(ctx, params))`. The only testable unit is the error-mapping
function, which lives in `internal/infrastructure/database/error.go` and is already a pure
function callable without any DB. Writing tests that mock `DBTX` to exercise the 3-line
repository wrapper would violate the constitution and add no meaningful protection.

**Alternative considered**: Integration tests against a real Postgres instance. These are the correct
long-term answer but are out of scope for this feature (no test DB container in CI configuration).

**Implication for spec User Story 5**: Descoped. The spec's FR-003 requirement to test repositories
via mocked DBTX is overridden by the constitution. Instead, `database.MapDBError` can be tested
as a standalone pure-function test under `internal/infrastructure/database/` if desired, but that
falls outside `feature/post` and is also out of scope here.

## Decision 2: Mock Pattern — Reuse Existing Helpers

**Decision**: All new service tests reuse the mock structs in `post_test_helpers.go`
(`mockPostRepo`, `mockLikeRepo`, `mockCommentRepo`, etc.) and helper constructors
(`newLikeService`, `newCommentService`). New tests add to existing `*_test.go` files where
the subject already has tests; new files are opened only for subjects with zero coverage.

**Rationale**: The existing mock infrastructure is comprehensive. Adding a parallel mock library
(gomock, testify) would violate the "no third-party test frameworks" constraint and the "no
premature abstractions" principle. Three similar lines are better than a new dependency.

**New mocks required** (not yet in `post_test_helpers.go`):
- `mockCommentLikeRepo` — needed for `CommentLikeService.Toggle` tests
- `mockHashtagCache` — needed for `HashtagService` tests  
- `newCommentLikeService()` constructor — needed for `CommentLikeService` tests

## Decision 3: Comment Enrichment Tests via Exported Paths

**Decision**: Test `enrichLikes`, `enrichAuthors`, and `enrichCommentMentions` indirectly through
`GetComments` and `GetReplies` (the exported entry points that call them). Do not use `_test`
package tricks or reflection to call unexported methods directly.

**Rationale**: These helpers are implementation details of `CommentService`. Calling them only
through the exported API surface means tests stay valid if the enrichment is ever refactored.
The key behavioral assertions are: (a) enrichment errors are non-fatal, (b) nil-guard conditions
are correct (e.g., nil commentLikeRepo means IsLiked is skipped). Both can be verified through
`GetComments` with suitably configured mock repos.

## Decision 4: Handler Mock Pattern

**Decision**: Follow the exact pattern established in `like_handler_test.go`:
one file per handler under test, with an inline `mock<Service>` struct at the top, a small
`newXxxHandler` constructor, and flat test functions using `withAuth`/`withChiParam`/`assertStatus`
helpers already defined in the handler package.

**Rationale**: Consistency with existing test files reduces cognitive overhead and avoids
introducing a shared test helper package. The `assertStatus`, `withAuth`, and `withChiParam`
helpers are already defined in `post_handler_test.go` and are accessible to all files in the
same package.

## Decision 5: HashtagService Cache Mock

**Decision**: The `mockHashtagCache` for service tests implements the `hashtagCache` interface
from `service/interfaces.go`. For "cache hit" scenarios, configure the mock to return a value
from `GetTrendingHashtags`/`GetSearchResults`/`GetHashtagPostsPage1`. For "cache miss" scenarios,
return an error (simulating a Redis miss). `SetXxx` methods are no-ops by default with an optional
callback to assert that caching was attempted.

**Rationale**: This is the minimal mock needed to test the two code paths in each HashtagService
method: (a) cache hit → skip repo call, (b) cache miss → call repo → attempt cache write.
A real Redis or a nop cache cannot distinguish these paths.
