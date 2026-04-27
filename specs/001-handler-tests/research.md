# Research: user/handler Test Coverage Gaps

**Branch**: `001-handler-tests` | **Date**: 2026-04-27

## Coverage Baseline

Current: **68.4%** (`go test ./internal/feature/user/handler/... -coverprofile`)

Per-function breakdown of meaningful gaps (constructors excluded — 0% but trivial):

| Function | Coverage | Gap type |
|---|---|---|
| `EmailHandler.*` (all 4) | 0% | Entire handler file untested |
| `UploadAvatar` | 0% | Auth, multipart, service |
| `UploadCover` | 0% | Auth, multipart, service |
| `Logout` | 75% | Mobile success path + service error |
| `RefreshToken` | 86.4% | Mobile success path |
| `LogoutAllSessions` | 81.8% | Service error + mobile no-cookie path |
| `GetUserProfile` | 81.2% | `?by=username` path + IsFollowing enrichment |
| `resolveUser` | 60% | Username resolution path |
| `UpdateUser` | 89.5% | Authorization gate (403 path) |
| `DeactivateUser` | 85.7% | Authorization gate (403 path) |
| `GetFollowing` | 85.7% | Service error case |

---

## Decision Log

### D1: email_handler_test.go — new file, full coverage

**Decision**: Write tests for all four `EmailHandler` methods.

**Rationale**: `VerifyEmail`, `ResendVerification`, `ForgotPassword`, `ResetPassword` are security-critical flows (token expiry, password reset). Zero coverage is a real risk. Each handler has 3 distinct branches: invalid JSON, service error, success.

**Alternatives considered**: Skip as "thin proxy to service" — rejected because the handlers implement the security-by-obscurity pattern (`ResendVerification` / `ForgotPassword` always return 200 to avoid leaking account existence). That behavior must be explicitly verified.

---

### D2: Authorization gate tests (UpdateUser, DeactivateUser)

**Decision**: Add `TestUpdateUser_Forbidden` and `TestDeactivateUser_Forbidden`.

**Rationale**: Both handlers contain an explicit authorization check:
```go
if currentUserID == nil || resolved.ID != *currentUserID {
    errors.WriteJSON(w, errors.NewForbiddenError("..."))
```
This 403 path is entirely untested. A regression here would silently allow users to mutate other accounts.

**Alternatives considered**: Cover via `resolveUser` unit tests — rejected because the auth check is *after* resolution, in the handler body, not in `resolveUser`.

---

### D3: Mobile/web client divergence (RefreshToken, Logout, LogoutAllSessions)

**Decision**: Add mobile success paths for `RefreshToken` and `Logout`; add service error path for `Logout` and `LogoutAllSessions`; add mobile no-cookie-clear assertion for `LogoutAllSessions`.

**Rationale**: The `isMobileClient(r)` branch controls whether the refresh token travels in the response body or an HttpOnly cookie. Existing tests cover the web-cookie path for `RefreshToken_Success` and `Logout_Success`, and the mobile path for parse errors. The mobile *success* paths — where the token should appear in the body and no cookie should be set/cleared — are unverified.

**Alternatives considered**: Skip as "the mock service just returns a value" — rejected because the test needs to assert the *absence* of a Set-Cookie header and the *presence* of the token in the JSON body. Both are behavioral contracts.

---

### D4: UploadAvatar / UploadCover

**Decision**: Add tests for both upload handlers covering: unauthenticated, missing file field, service error, success.

**Rationale**: File upload handlers have non-trivial request parsing (`ParseMultipartForm`, `FormFile`). The auth check, missing file, and service error paths are all untested at 0%.

**Alternatives considered**: Skip file-upload tests as "hard to test" — rejected because Go's `mime/multipart` in tests is straightforward and the auth/parse paths are the same as other handlers.

---

### D5: GetUserProfile username resolution + IsFollowing enrichment

**Decision**: Add tests for the `?by=username` lookup path and the authenticated viewer `is_following` enrichment.

**Rationale**: `resolveUser` returns a populated `User` when called with `?by=username`, which skips the second `GetProfileByUserID` call. This optimization is completely untested. Similarly, when a logged-in viewer looks at another user's profile, `IsFollowing` enriches the response — the `followChecker` mock is already stubbed in the test setup.

**Alternatives considered**: Test `resolveUser` in isolation — not done because it's an unexported helper; testing through the handler is the idiomatic approach.

---

### D6: Deliberate omissions

| Skipped | Reason |
|---|---|
| `New*Handler` constructors (0%) | Trivial, no logic |
| `Unfollow` service error | Single missing branch in an otherwise well-tested function — low signal |
| `GetFollowers` service error | Already covered; existing `GetFollowers_ServiceError` test exists |
| Large file upload (`MaxBytesReader`) | Sending 10MB+ in a unit test has no meaningful value over testing `ParseMultipartForm` failure |

---

## Test Infrastructure Assessment

All mocks needed already exist in test files:
- `mockEmailService` — does **not** exist yet; must be defined in `email_handler_test.go`
- `mockFollowChecker` — does **not** exist yet; must be defined in `profile_handler_test.go`
- All other mocks (`mockAuthService`, `mockUserService`, `mockProfileService`, `mockFollowService`, `mockUserResolver`) — already in place

No new dependencies required. `mime/multipart` and `bytes` are stdlib.
