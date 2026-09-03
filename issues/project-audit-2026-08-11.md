# Project Audit — 2026-08-11

## Conclusion

The project has a reasonably clear modular monolith structure. Every P0, P1 and P2 finding has been remediated, covering content authorization, uploads, shared object storage, automated off-host backup, reproducible container selection, destructive-migration gating, error handling, feed/notification consistency, session hardening, order-sensitive queries and enrichment, and test coverage in the high-risk areas. All automated checks recorded in this audit pass.

The project has no open findings and no known security or correctness blockers within the audit's scope. The codebase clears the audit's production-readiness gate; a production rollout still has to complete the documented operational conditions, in particular the protected environment, production-like credentials, and a staging drill for backup/restore against a real webhook.

CI now provisions Redis and MinIO itself, so the timeline/SSE and object-storage integration tests can no longer be skipped on the main branch or on a pull request.

## Remediation status

The P0 findings were fixed in the worktree on 2026-08-11:

| ID | Status | Principal change |
|---|---|---|
| P0-01 | Resolved | A shared visibility policy is applied to posts, comments, replies, post likes and comment likes; read routes use optional auth; the user-post query only accepts the set of visibilities the viewer is authorized for |
| P0-02 | Resolved | Avatars and covers are decoded, bounded by size/pixel count and re-encoded as JPEG/PNG; media sniffs its own bytes; local uploads carry `nosniff` and a CSP sandbox |
| P0-03 | Resolved | The storage factory and config validation fail startup on `s3`, on an empty provider and on any unsupported one; the unimplemented S3 configuration was removed from env and docs |
| P0-04 | Resolved | `WithDetail` is copy-on-write; a panic is only logged internally with its stack and the client always receives a generic `INTERNAL_ERROR` |

The P1 findings were fixed in the worktree on 2026-08-11:

| ID | Status | Principal change |
|---|---|---|
| P1-01 | Resolved | Post and follow mutations write feed events into a transactional PostgreSQL outbox; the consumer has leasing, retry/backoff, a dead-letter path, metrics and idempotent timeline upserts; a full queue falls back to synchronous handling and the emitter returns the error properly |
| P1-02 | Resolved | The follower reader takes its limit from the runtime setting (`cap+1`) instead of a hard-coded 5,000; added followers/attempted/succeeded/failed/capped metrics |
| P1-03 | Resolved | The timeline reads one extra item and uses `HasMore`; an empty or stale continuation ends within the correct cursor family rather than restarting the mixed feed |
| P1-04 | Resolved | The Redis timeline reads in chunks until the page is full or the ZSET is exhausted; covered by an integration test with a 130-element tie-score block |
| P1-05 | Resolved | Refresh uses atomic snapshot replacement while preserving concurrent fanout; delete and visibility events actively evict stale entries |
| P1-06 | Resolved | Private posts are materialized only into the author's own timeline, and batch hydration lets the service's eligibility rule return such a post to its owner |
| P1-07 | Resolved | The cursor only advances past recommendations that were emitted or are invalid; an emitted-but-out-of-order offset is retained in the cursor so an outranked item is never skipped |
| P1-08 | Resolved | The broker synchronizes deliver/cleanup/shutdown under an RW lock, with idempotent cleanup and shutdown; covered by stress and race tests |
| P1-09 | Resolved | Dropped `EXISTS` + `INCR`; every create/upsert/delete/read mutation invalidates the unread cache so it rebuilds from the database |
| P1-10 | Resolved | Refresh tokens are stored only as a SHA-256 hash; consume-and-rotate happens inside one transaction and an old token can be consumed exactly once |
| P1-11 | Resolved | The account-detail endpoint returns `UserResponse` only when the UUID/username key resolves to the authenticated user themselves; added authorization tests |
| P1-12 | Resolved | The auth middleware accepts a bearer token only through `Authorization`; the query-string token and its Swagger parameter were removed |
| P1-13 | Resolved | Post-like and comment-like toggles apply the self-like guard together with a transaction advisory lock keyed on the resource pair, and return committed state before emitting side effects |

The P2 findings were fixed in the worktree through 2026-08-12:

| ID | Status | Principal change |
|---|---|---|
| P2-01 | Resolved | Every JSON handler uses one shared decoder with a 1 MiB body limit, accepts a single JSON document and reports syntax/type/size errors through one contract; rejecting unknown fields is the default, with a lenient variant on the three endpoints whose GET and PUT share a path (`/me`, `/users/{userKey}`, `/posts/{postID}`) so that each endpoint's own response remains a valid request body |
| P2-02 | Resolved | Access tokens accept HS256 only and require the exact issuer, audience and expiration; added the `JWT_AUDIENCE` setting along with security regression tests for both the standard validator and custom claims |
| P2-03 | Resolved | A forwarded client IP is accepted only from `TRUSTED_PROXY_CIDRS`; the proxy chain is walked right to left, a malformed header fails closed, and production Compose binds loopback only by default. `TRUSTED_PROXY_CIDRS` defaults to loopback plus the private ranges a bridge network can occupy — left empty, the middleware rewrites nothing and the whole deployment rate-limits into one bucket keyed on the gateway address |
| P2-04 | Resolved | Common responses carry `nosniff`, anti-frame, no-referrer and a Permissions Policy; API/health/metrics use a CSP that locks down resource contexts, static uploads use their own sandbox CSP, and Swagger keeps a UI-compatible policy |
| P2-05 | Resolved | The access middleware creates synchronized request-scoped state; required and optional auth both update `user_id` through the same pointer so the final access log can always read the identity once the handler has finished |
| P2-06 | Resolved | A JSON response is fully encoded into a buffer before the status is committed; an encode failure returns a clean `INTERNAL_ERROR` contract with HTTP 500 |
| P2-07 | Resolved | The feed service now only coordinates; timeline, mixed blend, trending, discovery, follow resolution and enrichment are separate components, and cursor transitions have table-driven tests |
| P2-08 | Resolved | Post listings batch-check follow relationships in a single query; trending and follower/following listings use the ID as a deterministic tie-breaker |
| P2-09 | Resolved | Added S3-compatible shared storage and a bucket health probe; production rejects local storage, localhost URLs and non-HTTPS URLs. The constraint lives in `pkg/config.validateStorage` rather than in required Compose variables, and moving a running deployment to s3 requires running `scripts/storage/migrate-local-to-s3.sh` first because the database stores bare keys |
| P2-10 | Resolved | PostgreSQL backups run automatically into an encrypted off-host Restic repository, with daily/weekly/monthly retention, a periodic restore drill, health state and failure/recovery webhooks. Configuration is validated by the scheduler rather than by Compose; the restore drill has its own alert and state, does not govern `last-success`, and can be pointed at another instance or turned off |
| P2-11 | Resolved | PostgreSQL, Redis, migrate and every Dockerfile base image use an exact tag plus manifest digest; app and backup deploy by build digest, CD persists the digests into `.env`, and CI rejects floating references |
| P2-12 | Resolved | Bot `000009` was removed from the automatic migration path and gated by a session SQL guard, a protected manual workflow, a handoff reference and fresh backup/restore evidence; the generic down migration no longer pretends to be a data rollback |
| P2-13 | Resolved | Added unit/race/integration tests for notification, search, media, errors/logger/storage; CI provisions digest-pinned Redis and MinIO so the timeline, SSE Pub/Sub and S3 bucket lifecycle are exercised for real |

Deployment impact of P2:

- `JWT_AUDIENCE` defaults to `darkvoid-api`; every API instance must use the same value. Access tokens issued before the change carry no `aud` claim and are rejected after the deploy, so clients have to use their refresh token to obtain a new access token; refresh tokens themselves are unaffected.
- Production Compose changes the `SERVER_BIND` default from `0.0.0.0` to `127.0.0.1`, and since the app is then reachable only through the reverse proxy on the host, the peer the container sees is always the bridge gateway and never the client. `TRUSTED_PROXY_CIDRS` therefore defaults to loopback plus the private ranges a bridge network can occupy, rather than to empty: left empty, `TrustedRealIP` rewrites nothing, `httprate` collapses the whole deployment into one bucket keyed on the gateway address — 429 for everybody once it drains — and the access log records the gateway as the client. Narrow it to the proxy's own address where that is known, and narrow it necessarily if `SERVER_BIND` is opened beyond loopback, because a client arriving directly from one of those ranges could then declare its own `X-Forwarded-For`. The proxy must still overwrite or correctly append `X-Forwarded-For`.
- Production requires `STORAGE_PROVIDER=s3`, a public HTTPS `STORAGE_BASE_URL`, and a shared region and bucket. This is enforced by `pkg/config.validateStorage` — the process refuses to boot when `ENVIRONMENT=production` and the provider is anything but `s3` — rather than by `${VAR:?}` in Compose: a required variable there is evaluated by *every* Compose command, so an unset one takes `dv ps` and `dv logs` down with the deploy, and it fires on the non-production deployments that share the file instead of on the one it is meant to protect. Access key and secret may be left empty to use an IAM role or workload identity; the principal needs permission to probe the bucket, perform multipart uploads and delete objects. Bucket/CDN read policy lives outside the application and must be configured by the operator.
- Moving a running deployment from the local provider to `s3` is a **data migration, not a variable change**. `post.post_media`, `post.comment_media` and `usr.users.avatar_key` store bare keys, and the new provider resolves a key into a URL at read time, so every image and video already posted 404s the moment the provider changes. Run `scripts/storage/migrate-local-to-s3.sh --source <upload dir> --apply` (dry run by default, syncing keys unchanged) first, then change the variable.
- Production requires `BACKUP_RESTIC_REPOSITORY`, `BACKUP_RESTIC_PASSWORD` and an HTTPS `BACKUP_ALERT_WEBHOOK_URL`; a filesystem or same-host repository is rejected. These three are validated by the scheduler — which names everything that is missing at once, alerts if it can, and exits — rather than by `${VAR:?}` in Compose, for the same reason as storage. A deployment that leaves them unset runs `pg-backup` permanently unhealthy; that is the intended signal, not a state to run production in. The backup principal needs read/write/list/delete on its own prefix, and the database role needs `CREATEDB` to restore the drill into an isolated database. CD builds app and backup from the same commit and deploys the exact digest pair the registry returned; operational and recovery guidance lives in [`docs/production-backup-runbook.md`](../docs/production-backup-runbook.md).
- The restore drill creates its isolated database on **the production PostgreSQL instance itself** by default, holding a second copy of the data for the length of the restore — including on first boot, where the drill runs immediately after the first backup. `BACKUP_RESTORE_PGHOST`/`PGPORT`/`PGUSER`/`PGPASSWORD` move the whole drill (the `DROP DATABASE` included) to another instance; `BACKUP_RESTORE_DRILL_ENABLED=false` turns it off entirely and is the last resort, because an untested backup is a hypothesis. A failed drill does **not** withhold `last-success`: that file answers "is there a recent off-host snapshot", and it is what the healthcheck reads — spending an unhealthy `pg-backup` on the drill would leave nothing to signal the failure that actually means data loss. The drill reports separately with `restore_drill_failed` on every cycle until it passes, then `restore_drill_recovered`.
- Production no longer accepts `APP_TAG` or a `latest` fallback. CD takes `APP_DIGEST`/`BACKUP_DIGEST` from the build-push result and persists both into `.env`; a rollback must restore the exact digest pair from one deployment. Upgrading PostgreSQL, Redis, migrate, Go or Alpine means updating the exact tag and the manifest digest together; `make test-production-images` keeps a digest-less reference from coming back.
- Before the one-time bot schema retirement, the GitHub `production` environment must be configured with required reviewers, prevent-self-review, the VPS secrets and `DEPLOY_DIR`; the `Retire legacy bot schema` workflow is then run with a change/handoff reference. A normal deploy and `make migrate-up-bot` stop at `000008`. Data recovery is only from a snapshot, per [`docs/bot-schema-retirement-runbook.md`](../docs/bot-schema-retirement-runbook.md), never from the down migration.
- The CI test job needs Docker service support and free localhost ports `6379`/`9000` on the runner. Redis 7.4.10 and MinIO `RELEASE.2025-09-07T16-13-09Z` are both pinned by manifest digest; the test job sets `REDIS_TEST_ADDR` and `S3_TEST_*` so the integration tests cannot silently skip.

Deployment impact of P1:

- User migrations `000014` and `000015` must run before the new binary. `000014` converts existing tokens to a one-way hash; the down migration only restores the structure using the hashed values and cannot recover the raw tokens.
- SSE clients are no longer sent a JWT in the query string. Clients must use request streaming with `Authorization: Bearer ...`; a native `EventSource`, if used, needs to move to a cookie or short-lived ticket in a separate change.
- `GET /users/{userKey}` is now self-only account detail; another user's public data continues to go through the profile endpoint.

Verification of the P1 changes: `make generate`, `make build`, `make test`, `go test -race ./...`, `go vet ./...` and `golangci-lint run ./...` all pass. The Redis integration tests for atomic replacement, tie-score pagination and SSE Pub/Sub ran under the race detector; the S3-compatible bucket lifecycle ran against a real MinIO.

All changes have passed `make build`, `make test`, `go test -race ./...`, `go vet ./...` and `golangci-lint run ./...`.

## Scope and method

The audit covered:

- Package structure and the `handler/service/repository/entity/dto` boundaries.
- Authentication, authorization and error response flows.
- Posts, comments, likes, follows and user profiles.
- The materialized ranked feed, the Redis timeline and cursor pagination.
- The notification cache, the SSE broker and Redis Pub/Sub.
- Upload/storage and static file serving.
- JWT, refresh tokens and request parsing.
- Docker/Compose, migrations and backup.
- Tests, the race detector, vet and lint.

Verification results:

| Check | Result | Note |
|---|---:|---|
| `make build` | Pass | Builds successfully |
| `make test` | Pass | Run outside the sandbox to allow a localhost listener |
| `go test -race ./...` | Pass | No race detected in the packages that have tests |
| `go vet ./...` | Pass | No findings |
| `golangci-lint` | Pass | 21 linters, 0 issues with a writable cache |
| Package documentation | Pass | No package found without a `docs.go` |

> Measured coverage in the high-risk packages: notification broker 87.1% with the Redis integration enabled, cache 94.1%, service 72.9%; search handler 100%, service 78.8%; storage handler 100%, service 95.5%; `pkg/errors` 100%, JWT 71.9%, logger above 90% and `pkg/storage` above 80%. These are package-level statement coverage figures, not a repository-wide coverage target.

## P0 — must be fixed before production

### P0-01: Private and followers-only posts were publicly readable — Resolved

**Severity:** Critical
**Class:** Broken access control / data exposure

> Fixed with a fail-closed authorization policy at the service layer. The evidence below records the state before the fix.

#### Evidence

- The routes reading a post, post lists, comments and replies used no auth middleware: [`internal/app/post_routes.go:12`](../internal/app/post_routes.go#L12).
- `GetPost` received the viewer but checked neither visibility, ownership nor the follow relationship: [`internal/feature/post/service/post_service.go:200`](../internal/feature/post/service/post_service.go#L200).
- The SQL fetching a post by ID checked only `deleted_at`: [`internal/feature/post/sql/post_queries.sql:8`](../internal/feature/post/sql/post_queries.sql#L8).
- The listing endpoint accepted `?visibility=private` directly: [`internal/feature/post/handler/post_handler.go:248`](../internal/feature/post/handler/post_handler.go#L248).
- With an empty visibility, the SQL returned every visibility: [`internal/feature/post/sql/post_queries.sql:84`](../internal/feature/post/sql/post_queries.sql#L84).

#### Impact

- Knowing the UUID was enough to read a `private` or `followers` post.
- Comments and replies on a non-public post were readable too.
- A logged-in user could like or comment on content they were not allowed to see, because the mutation services only checked that the post existed.

#### Recommendation

- Introduce a shared policy such as `CanViewPost(viewerID, post)`.
- Apply it to get/list/comment/reply/like/comment-like.
- Use optional auth on read routes so the viewer's identity is available.
- For a post the viewer may not see, prefer 404 so the response does not confirm the resource exists.
- Write a test matrix over `public`, `followers`, `private`, owner, follower, non-follower and anonymous.

---

### P0-02: Stored XSS through avatar and cover uploads — Resolved

**Severity:** Critical
**Class:** Unrestricted file upload / stored XSS

> Fixed with content validation, decode/re-encode, and response hardening for user uploads. The evidence below records the state before the fix.

#### Evidence

- The handler bounded only the size and then passed the client-supplied `Content-Type` and extension through: [`internal/feature/user/handler/profile_handler.go:166`](../internal/feature/user/handler/profile_handler.go#L166), [`internal/feature/user/handler/profile_handler.go:214`](../internal/feature/user/handler/profile_handler.go#L214).
- The service kept that extension in the storage key: [`internal/feature/user/service/user_service.go:336`](../internal/feature/user/service/user_service.go#L336), [`internal/feature/user/service/user_service.go:372`](../internal/feature/user/service/user_service.go#L372).
- Local uploads were served publicly from the same origin through `http.FileServer`: [`internal/app/app.go:383`](../internal/app/app.go#L383).
- There was no CSP and no `X-Content-Type-Options: nosniff` in the global middleware.

#### Impact

An attacker could upload an `.html` file or active content and then open it under the API's origin. That could allow same-origin JavaScript execution, calls to the API with the current cookie, or abuse of the refresh flow.

#### Recommendation

- Always sniff the magic bytes; never trust the multipart `Content-Type`.
- Accept only genuine JPEG, PNG or WebP.
- Derive the extension from the verified MIME type.
- Decode and re-encode images to strip payloads and polyglots.
- Serve user uploads from a CDN or separate domain that carries no cookie.
- Add `X-Content-Type-Options: nosniff` and an appropriate CSP.
- Apply the same rules to media uploads; the media endpoint only sniffed when the client sent no `Content-Type`: [`internal/feature/storage/handler/media_handler.go:59`](../internal/feature/storage/handler/media_handler.go#L59).

---

### P0-03: An unsupported storage provider silently discarded files — Resolved

**Severity:** Critical
**Class:** Silent data loss / deployment misconfiguration

> Fixed fail-closed in both config validation and the storage factory. The evidence below records the state before the fix.

#### Evidence

- The config described a `local` or `s3` provider: [`pkg/config/config.go:136`](../pkg/config/config.go#L136).
- `storage.New` supported only `local`; every other value fell through to `nopStorage`: [`pkg/storage/storage.go:17`](../pkg/storage/storage.go#L17).
- `nopStorage.Put` and `Delete` always returned `nil`: [`pkg/storage/storage.go:45`](../pkg/storage/storage.go#L45).
- The application still logged that storage had initialized successfully: [`internal/app/infrastructure_setup.go:28`](../internal/app/infrastructure_setup.go#L28).

#### Impact

With `STORAGE_PROVIDER=s3`, uploads returned success and the database stored the key, while no file was written anywhere.

#### Recommendation

- An unrecognized provider must fail the boot.
- Do not use `nopStorage` outside tests or an explicitly enabled development mode.
- Implement real S3-compatible storage before advertising support for it.
- Add startup validation and a put/get/delete integration test per provider.

---

### P0-04: Mutable global errors caused races and leaked panic contents — Resolved

**Severity:** Critical
**Class:** Information disclosure / shared mutable state / concurrency

> Fixed with copy-on-write error details and a generic panic response. The evidence below records the state before the fix.

#### Evidence

- `WithDetail` mutated `AppError.Details` in place: [`pkg/errors/errors.go:30`](../pkg/errors/errors.go#L30).
- The common errors were global singletons: [`pkg/errors/codes.go:8`](../pkg/errors/codes.go#L8).
- The panic handler wrote the raw panic into `ErrInternal` and returned it to the client: [`pkg/errors/response.go:53`](../pkg/errors/response.go#L53).
- Password validation mutated `user.ErrWeakPassword`: [`internal/feature/user/service/validation.go:76`](../internal/feature/user/service/validation.go#L76).

#### Impact

- Raw panic and internal values were returned to the client.
- Details from an earlier request could survive into a later response.
- Concurrent requests could data-race or hit concurrent map access.
- The outer chi Recoverer lost its chance to log the stack once an inner handler had recovered.

#### Recommendation

- Make `AppError` an immutable value, or have `WithDetail` clone the struct and map.
- Never add the panic value to the response.
- Log the panic and its stack trace internally with the request ID.
- Keep exactly one panic-recovery middleware with a clear responsibility.
- Add concurrent tests for error construction and panic recovery.

## P1 — significant correctness, security and concurrency defects

### P1-01: Feed events could be lost permanently — Resolved

**Severity:** High
**Class:** Reliability / eventual consistency

#### Evidence

- The dispatcher used a non-blocking in-process channel and returned `false` when the queue was full, closed or disabled: [`internal/feature/feed/dispatcher.go:106`](../internal/feature/feed/dispatcher.go#L106).
- The emitters ignored the `Dispatch` result and always returned `nil`: [`internal/feature/feed/dispatcher.go:161`](../internal/feature/feed/dispatcher.go#L161).
- The post service logged only when the emitter returned an error, and the emitter never returned one for an enqueue failure: [`internal/feature/post/service/post_service.go:191`](../internal/feature/post/service/post_service.go#L191).

#### Impact

Events were lost on a full queue, a process crash or restart, a worker timeout, or a Redis failure. An existing timeline is not a cache miss, so refresh-on-miss could not repair it; a new post could simply never appear for some users.

#### Recommendation

- Use a transactional outbox in PostgreSQL.
- Add a durable consumer with retry, dead-lettering and idempotent timeline upserts.
- Failing an outbox, at minimum mark the timeline dirty and emit a metric/alert when an enqueue fails.
- Never return silent success when a required persistence side effect has failed.

---

### P1-02: The fanout follower cap disagreed with runtime settings — Resolved

**Severity:** High
**Class:** Feed correctness / configuration drift

#### Evidence

- Fanout applied the cap from the runtime settings: [`internal/feature/feed/fanout.go:87`](../internal/feature/feed/fanout.go#L87).
- But `GetFollowerIDs` had already hard-capped at 5,000 before that: [`internal/feature/user/service/follow_service.go:178`](../internal/feature/user/service/follow_service.go#L178).

#### Impact

Any configured cap above 5,000 had no effect. Followers beyond the first 5,000 records received no fanout.

#### Recommendation

- Push the limit into the interface/call site, or paginate the whole follower list up to the configured cap.
- Record metrics for `total followers`, `attempted`, `succeeded`, `failed` and `capped`.

---

### P1-03: The timeline cursor could hand off to the mixed feed and duplicate data — Resolved

**Severity:** High
**Class:** Pagination correctness

#### Evidence

- The timeline counted as a hit only if it returned at least one item; an empty page fell through to the mixed path: [`internal/feature/feed/service/feed_service.go:114`](../internal/feature/feed/service/feed_service.go#L114).
- The next cursor was produced whenever exactly `pageSize` items came back, whether or not more data existed: [`internal/feature/feed/service/feed_service.go:307`](../internal/feature/feed/service/feed_service.go#L307).

#### Impact

If the previous page happened to end exactly on the last item, the client still received a cursor. The next request read an empty timeline and switched to the mixed path, but a timeline cursor carries no following or trending position, so the feed could restart and return duplicates.

#### Recommendation

- Fetch `pageSize+1` to determine continuation.
- Represent `timeline exhausted` explicitly in the cursor.
- Never switch cursor family implicitly.
- Add characterization tests for the exact-end case, a stale-only page, and the handoff to discover.

---

### P1-04: Redis timeline pagination skipped a large tie-score block — Resolved

**Severity:** High
**Class:** Pagination correctness / Redis data access

#### Evidence

- The Redis query fetched at most `limit*2` entries by score: [`internal/feature/feed/cache/redis_timeline_store.go:77`](../internal/feature/feed/cache/redis_timeline_store.go#L77).
- The UUID tie-break was then applied in Go, after the fetch: [`internal/feature/feed/cache/redis_timeline_store.go:103`](../internal/feature/feed/cache/redis_timeline_store.go#L103).

#### Impact

With more than `2*limit` members sharing a score ahead of the cursor, the whole chunk could be filtered out and the page returned empty even though valid members remained behind it.

#### Recommendation

- Implement full `(score, member)` tuple continuation in Redis/Lua.
- Or fetch in chunks repeatedly until the limit is reached or the set is genuinely exhausted.
- Add a Redis integration test with a tie block larger than the fetch window.

---

### P1-05: Timeline refresh never removed stale entries — Resolved

**Severity:** High
**Class:** Cache consistency / feed correctness

#### Evidence

- The refresher only upserted, through `SetPostsBatch`: [`internal/feature/feed/refresher.go:72`](../internal/feature/feed/refresher.go#L72).
- Unfollowed authors were deliberately retained: [`internal/feature/feed/fanout.go:61`](../internal/feature/feed/fanout.go#L61).
- `EventPostDeleted`, `EventVisibilityChanged` and `RemovePostBestEffort` were defined but had no production call path: [`internal/feature/feed/dispatcher.go:15`](../internal/feature/feed/dispatcher.go#L15), [`internal/feature/feed/timeline.go:40`](../internal/feature/feed/timeline.go#L40).

#### Impact

Entries from unfollowed authors, deleted posts or changed visibility kept occupying timeline capacity and the read window. When a stale entry ranked highly, a page could come back short or empty and trigger the wrong fallback.

#### Recommendation

- Use an atomic replace with a generation/version so concurrent fanout is not lost.
- Emit and handle delete and visibility events.
- Add scheduled timeline repair/reconciliation.

---

### P1-06: The author's own feed was inconsistent about private posts — Resolved

**Severity:** High
**Class:** Business-rule inconsistency

#### Evidence

- Fanout skipped private posts entirely: [`internal/feature/feed/fanout.go:79`](../internal/feature/feed/fanout.go#L79).
- Batch hydration returned only `public` and `followers`: [`internal/feature/post/sql/post_queries.sql:93`](../internal/feature/post/sql/post_queries.sql#L93).
- Yet the eligibility logic stated a private post is valid for its own author: [`internal/feature/feed/service/feed_service.go:342`](../internal/feature/feed/service/feed_service.go#L342).

#### Impact

The code described authors seeing their own private posts in their feed, while storage and queries made that impossible.

#### Recommendation

- Settle the product rule and align fanout, hydration, eligibility and the tests behind it.

---

### P1-07: The recommendation cursor skipped items that were never shown — Resolved

**Severity:** High
**Class:** Pagination correctness / ranking

#### Evidence

- The recommendation offset advanced by the entire page the provider had fetched: [`internal/feature/feed/service/feed_service.go:393`](../internal/feature/feed/service/feed_service.go#L393).
- Only afterwards were the candidates blended and ranked against following and trending.
- The next cursor kept the advanced offset: [`internal/feature/feed/service/feed_service.go:537`](../internal/feature/feed/service/feed_service.go#L537).

#### Impact

A recommendation that was fetched, outranked by another source, and therefore never returned to the client was skipped permanently on subsequent pages.

#### Recommendation

- Advance only past recommendations that were actually emitted.
- Or keep a buffer or a server-side continuation token from the recommendation provider.

---

### P1-08: The SSE broker raced between deliver, cleanup and shutdown — Resolved

**Severity:** High
**Class:** Concurrency / availability

#### Evidence

- `deliverLocal` took a reference to the map under `RLock`, unlocked, and only then iterated and sent: [`internal/feature/notification/broker/broker.go:110`](../internal/feature/notification/broker/broker.go#L110).
- Cleanup concurrently deleted from the map and then closed the channel: [`internal/feature/notification/broker/broker.go:66`](../internal/feature/notification/broker/broker.go#L66).
- Shutdown closed channels without removing the clients from the map: [`internal/feature/notification/broker/broker.go:79`](../internal/feature/notification/broker/broker.go#L79).

#### Impact

- Concurrent map iteration and write.
- Sends on a closed channel.
- Panics or races in production.

#### Recommendation

- Snapshot the client list safely under the lock, with per-client closed state.
- Or hold the read lock across the iteration with explicit close sequencing.
- Make cleanup idempotent after shutdown.
- Write stress/race tests covering publish + disconnect + shutdown.

---

### P1-09: The unread notification count drifted — Resolved

**Severity:** High
**Class:** Cache consistency

#### Evidence

- The cache performed `EXISTS` and then `INCR` as two non-atomic operations: [`internal/feature/notification/cache/redis_notification_cache.go:49`](../internal/feature/notification/cache/redis_notification_cache.go#L49).
- The create SQL could upsert an existing notification and set `is_read = FALSE`: [`internal/feature/notification/sql/notification_queries.sql:1`](../internal/feature/notification/sql/notification_queries.sql#L1).
- The service always incremented the cache after a create or upsert: [`internal/feature/notification/service/notification_service.go:103`](../internal/feature/notification/service/notification_service.go#L103).
- Deleting a notification neither invalidated nor decremented the cache: [`internal/feature/notification/service/notification_service.go:94`](../internal/feature/notification/service/notification_service.go#L94).

#### Impact

The unread badge could exceed or otherwise diverge from the database until the TTL expired and it rebuilt.

#### Recommendation

- Simple approach: invalidate the unread cache after every mutation.
- Optimal approach: update from database state and `RowsAffected` inside the transaction, and use Lua for Redis atomicity.

---

### P1-10: Refresh-token rotation was not atomic and tokens were stored raw — Resolved

**Severity:** High
**Class:** Session security

#### Evidence

- Refresh tokens were stored and looked up by the raw token: [`internal/feature/user/sql/refresh_token_queries.sql:1`](../internal/feature/user/sql/refresh_token_queries.sql#L1).
- Rotation ran validate, generate access, revoke old and create new as separate steps: [`internal/feature/user/service/auth_service.go:166`](../internal/feature/user/service/auth_service.go#L166).
- A revoke failure was merely logged and the flow continued: [`internal/feature/user/service/auth_service.go:191`](../internal/feature/user/service/auth_service.go#L191).

#### Impact

- A database read compromise handed over immediately usable active refresh tokens.
- Two concurrent requests could use one old token to mint two new ones.
- A failed revoke still returned a new session.

#### Recommendation

- Store a hash of an opaque refresh token.
- Do an atomic consume-and-rotate in a single database transaction.
- Make the revoke conditional on `WHERE is_revoked = false` and require exactly one affected row.
- Detect token reuse and revoke the whole token family where the security bar demands it.

---

### P1-11: A user endpoint leaked email addresses and account status — Resolved

**Severity:** High
**Class:** Privacy / authorization

#### Evidence

- Any logged-in user could call `GET /users/{userKey}/`: [`internal/app/user_routes.go:44`](../internal/app/user_routes.go#L44).
- The handler performed no self/admin check: [`internal/feature/user/handler/user_handler.go:45`](../internal/feature/user/handler/user_handler.go#L45).
- `UserResponse` contains `email` and `is_active`: [`internal/feature/user/dto/user_dto.go:25`](../internal/feature/user/dto/user_dto.go#L25).
- `ProfileResponse` deliberately omits both: [`internal/feature/user/dto/user_dto.go:44`](../internal/feature/user/dto/user_dto.go#L44).

#### Recommendation

- Restrict the account-detail endpoint to self/admin.
- Use `ProfileResponse` for other users.
- Add authorization tests for both the UUID and `?by=username` forms.

---

### P1-12: A bearer token was accepted through the query string on every route — Resolved

**Severity:** High
**Class:** Credential exposure

#### Evidence

- The global middleware fell back to `?token=`: [`internal/app/middleware/auth.go:29`](../internal/app/middleware/auth.go#L29).

#### Impact

The token could turn up in browser history, referrers, reverse-proxy logs, monitoring and analytics.

#### Recommendation

- Accept a bearer token only from the `Authorization` header on ordinary routes.
- For SSE/EventSource, use a short-lived single-use stream ticket or an appropriately scoped cookie.

---

### P1-13: Like toggles were not atomic in their side effects and the self-like rule was inconsistent — Resolved

**Severity:** High
**Class:** Concurrency / business-rule inconsistency

#### Evidence

- `Like()` forbade self-likes: [`internal/feature/post/service/like_service.go:38`](../internal/feature/post/service/like_service.go#L38).
- A comment on `Toggle()` waived the self-like guard: [`internal/feature/post/service/like_service.go:84`](../internal/feature/post/service/like_service.go#L84).
- Toggle used `IsLiked` followed by `Like`/`Unlike`, a check-then-act pattern.

#### Impact

Database state could remain valid thanks to idempotent SQL, but concurrent requests could return the same result and emit duplicate notification and behavior events.

#### Recommendation

- Settle the self-like rule and apply it consistently.
- Use an atomic SQL toggle or transaction that returns the new state.
- Emit side effects only when the database state actually changed.

## P2 — architecture, hardening and operations

### P2-01: JSON handlers had no body limit and no strict decoding — Resolved

**Severity:** Medium

> Fixed with a shared strict JSON request decoder in `internal/http`, applied to every handler that accepts JSON. The decoder bounds the body at 1 MiB, rejects undeclared fields and multiple JSON documents, and normalizes parse failures into `BAD_REQUEST` with a specific reason. Webhooks and multipart uploads keep their own paths because they already have dedicated limits and verification rules. The evidence below records the state before the fix.
>
> Added afterwards: `DecodeJSONLenient` drops exactly one rule — the rejection of unknown fields — and is used on the three endpoints whose GET and PUT share a path. Their responses are far wider than their updates (`/me` eleven fields, `/users/{userKey}` fourteen around the single one it accepts, `/posts/{postID}` ten), so a client that reads the resource, changes one field and sends the whole object back — which was correct until unknown fields started being rejected — would take a 400 for a field it did not add. Every other guarantee holds: the body is still bounded, still required, still exactly one JSON value, and a declared field with the wrong type is still an error. Declaring the extra fields on the request type, the way `UpdateFeedSettingsRequest` does, does not scale here: it would leave `PUT /users/{userKey}` with fourteen ignored fields around one real one. Strict remains the default because it turns a misspelled field into a 400 rather than an edit that silently does nothing.

Before the fix, many handlers called `json.NewDecoder(r.Body).Decode(...)` directly, for example [`internal/feature/post/handler/post_handler.go:57`](../internal/feature/post/handler/post_handler.go#L57) and [`internal/feature/user/handler/auth_handler.go:96`](../internal/feature/user/handler/auth_handler.go#L96).

Recommended shared helper:

- `http.MaxBytesReader`.
- `DisallowUnknownFields`.
- A single JSON document only.
- Normalized syntax/type/size errors.

---

### P2-02: JWT validation accepted any HMAC algorithm — Resolved

**Severity:** Medium

> Fixed with a parser policy that permits exactly HS256 and requires `iss`, `aud` and `exp` to match the configuration. New tokens always carry the audience; `ValidateToken` and `ValidateTokenWithClaims` share the policy. The evidence below records the state before the fix.

Before the fix, JWTs were signed with HS256 but validation only required the token method to belong to the HMAC family: [`pkg/jwt/jwt.go:61`](../pkg/jwt/jwt.go#L61), [`pkg/jwt/jwt.go:83`](../pkg/jwt/jwt.go#L83).

Recommendation:

- Require exactly `jwt.SigningMethodHS256`.
- Require issuer and audience where they form part of the trust boundary.
- Add tests for a wrong algorithm, a wrong issuer and a wrong audience.

---

### P2-03: Rate limiting depended on the reverse proxy's configuration — Resolved

**Severity:** Medium, deployment-dependent

> Fixed with fail-closed trusted-proxy middleware. Forwarded headers are ignored for a peer outside `TRUSTED_PROXY_CIDRS`; a multi-hop chain is walked right to left and a header containing an invalid IP is not used. Production Compose binds `127.0.0.1` by default so the app is not exposed directly. The evidence below records the state before the fix.
>
> Fixed further afterwards: `TRUSTED_PROXY_CIDRS` no longer defaults to empty in production Compose. Binding loopback means only the reverse proxy on the host can reach the app, so the peer the container sees is the bridge gateway; with an empty list the middleware rewrites no `RemoteAddr`, `httprate.LimitByIP` collapses the deployment into a single bucket, and the access log loses the client IP — exactly what `chimiddleware.RealIP` had been doing before it was replaced. The current default covers loopback plus the private ranges a bridge network can occupy; `scripts/ci/deployment-defaults_test.sh` pins it so it cannot revert to empty.

Before the fix, `middleware.RealIP` ran ahead of the rate limiter: [`internal/app/server.go:46`](../internal/app/server.go#L46). If the app was exposed directly, or the proxy did not sanitize forwarded headers, a client could spoof its IP and bypass the rate limit.

Recommendation:

- Trust forwarded headers only from a trusted proxy.
- Do not expose the app container directly.
- Confirm the reverse proxy always overwrites `X-Forwarded-For`/`X-Real-IP`.

---

### P2-04: Missing security headers — Resolved

**Severity:** Medium

> Fixed with a browser policy split by response boundary. The common headers apply `nosniff`, anti-frame, no-referrer and a Permissions Policy; `/api/v1`, `/health` and `/metrics` receive a `default-src 'none'` CSP that also locks base/form/frame; `/static` receives its own sandbox CSP. The Swagger UI does not receive the script/style-locking CSP but still receives the common headers. The evidence below records the state before the fix.

Before the fix, no middleware set a CSP, `X-Content-Type-Options`, `Referrer-Policy` or the related protective headers. The risk was heightened because static user uploads were served from the same origin.

Recommended adding security-header middleware with separate configurations for the API and for static uploads.

---

### P2-05: The access log never saw the auth-enriched context — Resolved

**Severity:** Medium

> Fixed with a request-scoped state pointer created in the access middleware and shared through every derived context. `logger.WithUserID` updates that state under a lock while also enriching the context logger; the final access log reads the state once the handler has finished. Required and optional auth both use the same update path, with integration tests. The evidence below records the state before the fix.

Before the fix, a comment claimed the access logger picked up the most recent logger carrying `user_id`, but the outer middleware still read the request context it was holding at [`pkg/logger/middleware.go:37`](../pkg/logger/middleware.go#L37). An `r.WithContext` in an inner middleware does not change the outer middleware's request.

Impact: access logs could be missing `user_id`, which weakens incident investigation.

Recommended using shared response/request state, or a context carrier with pointer-safe state updated across middleware.

---

### P2-06: JSON responses handled encode errors after the status had been sent — Resolved

**Severity:** Low

> Fixed by encoding the payload completely into a buffer before committing the header and status. A payload that cannot be encoded is discarded and a JSON `INTERNAL_ERROR` with HTTP 500 is returned instead; a regression test confirms the response is never a mix of JSON and plain text. The evidence below records the state before the fix.

Before the fix, `WriteJSON` sent the status first and then called `http.Error` if encoding failed: [`internal/http/response.go:12`](../internal/http/response.go#L12). Once the header and body have started, the status can no longer become 500, and the response could mix JSON with plain text.

Recommended encoding into a buffer before committing the header, or logging only once streaming has begun.

---

### P2-07: The feed service was too large and mixed responsibilities — Resolved

**Severity:** Medium

> `FeedService` was split into a 163-line coordinator plus dedicated components: `timelineReader`, `mixedFeedBuilder`, `trendingSource`, `discoveryReader`, `followingResolver` and `feedEnricher`. The public constructor and API are unchanged; singleflight belongs to each source, cursor transitions are pure functions, and table-driven tests sit alongside the existing characterization suite. The evidence below records the state before the fix.

Before the fix, `internal/feature/feed/service/feed_service.go` ran to 1,031 lines and handled:

- Timeline rollout and caching.
- Timeline refresh and fallback.
- Mixed-source collection.
- Cursor state transitions.
- Ranking and deduplication.
- Enrichment and recommendation integration.

Recommended split:

- Timeline reader / state machine.
- Following, trending and recommendation source adapters.
- Blend/ranking coordinator.
- A cursor transition module with property- or table-driven tests.

Other large files worth revisiting over time: `internal/app/app.go`, `pkg/config/config.go`, `cmd/seed/main.go`.

---

### P2-08: Some queries and enrichment risked N+1 or unstable pagination — Resolved

**Severity:** Medium

> Fixed with a batch relationship lookup covering every unique author in a response, and deterministic `(like_count, id)` / `(created_at, user_id)` ordering. The follower/following API keeps its `limit`/`offset` contract; the unique ID removes the undefined ordering when several records share a timestamp. The evidence below records the state before the fix.

- Post enrichment checked the follow relationship per author on ordinary post listings.
- The trending query only did `ORDER BY like_count DESC`, with no ID tie-break: [`internal/feature/post/sql/post_queries.sql:76`](../internal/feature/post/sql/post_queries.sql#L76).
- Follower/following offset pagination ordered only by timestamp, which can duplicate or skip under concurrent inserts.

Recommended batching the follow lookup and using deterministic `(score/time, id)` ordering for every cursor- or order-sensitive query.

---

### P2-09: Local storage does not suit horizontal scaling — Resolved

**Severity:** Medium/High depending on topology

> Fixed with an AWS SDK v2 S3-compatible provider shared by every instance, supporting the AWS default credential chain and a custom endpoint/path-style for MinIO. Storage is probed at boot and on `/health`; the production config fails closed on the local provider, on a non-public-HTTPS URL, and on a missing region or bucket. The evidence below records the state before the fix.
>
> Fixed further afterwards, on two points. First, the production constraint lives in `pkg/config.validateStorage` rather than in a `${VAR:?}` in Compose, and the upload volume is mounted again: a required variable in Compose is evaluated by every Compose command, so an unset one takes `dv ps`/`dv logs` down with the deploy, and it fires on the non-production deployments that share the file — which still run the local provider and would lose their uploads into a discarded writable layer without the volume. Second, moving a running deployment to `s3` is a data migration: the database stores bare keys and the provider resolves a key into a URL at read time, so all existing media 404s the moment the provider changes. `scripts/storage/migrate-local-to-s3.sh` copies the upload directory into the bucket with keys unchanged, dry-run by default.

Production compose defaulted to a local named volume and `STORAGE_BASE_URL=http://localhost:8080/static`: [`docker-compose.prod.yml:282`](../docker-compose.prod.yml#L282).

Impact:

- Multiple instances or hosts do not see the same files.
- Rolling replacement or failover can leave media inconsistent.
- A localhost URL is unusable by external clients unless the operator overrides it.

Recommended deploying real object storage/CDN, health-checking storage, and requiring a valid public base URL in production.

---

### P2-10: Production backup was opt-in, one-shot and on the same host — Resolved

**Severity:** Medium/High

> The one-shot profile was replaced by a scheduler that runs continuously as part of the production stack. Each cycle streams a PostgreSQL custom dump into an encrypted remote Restic repository, applies daily/weekly/monthly retention, and periodically restores the snapshot it just created into an isolated database to verify it. A failed backup or invalid configuration is sent to an HTTPS webhook, a recovering run emits a recovery event, and the health check detects an overdue cycle. The local volume now holds only reproducible timestamps and cache, never a snapshot. The evidence below records the state before the fix.
>
> Fixed further afterwards, on two points. First, `last-success` is written after the backup and retention rather than waiting for the drill: the healthcheck reads that file to answer "is there a recent off-host snapshot", so the previous version reported "no backup" on a deployment whose snapshots were all uploading successfully — spending the signal reserved for data-loss failures on one that loses no data. The drill now has its own alerts (`restore_drill_failed` / `restore_drill_recovered`) and state file, repeating every cycle until it passes. Second, the drill restores into the production instance itself by default and therefore holds a second copy of the data for the length of the restore; `BACKUP_RESTORE_PGHOST` and the variables beside it move the whole drill to another instance, and `BACKUP_RESTORE_DRILL_ENABLED=false` turns it off. Backup configuration is validated by the scheduler rather than by Compose, for the same reason as storage.

Compose stated plainly that backup ran manually and stored to a local volume: [`docker-compose.prod.yml:333`](../docker-compose.prod.yml#L333).

Recommendation:

- An automated backup schedule.
- Encryption and shipping off-host / to object storage.
- An explicit retention policy.
- A periodic restore drill and alerting when a backup fails.

---

### P2-11: Container images and tags were not fully reproducible — Resolved

**Severity:** Low/Medium

> Public images are pinned by exact patch tag and multi-platform manifest digest: PostgreSQL 16.14, Redis 7.4.10, golang-migrate 4.19.1, the Go 1.26.5/Alpine 3.24 build stage and the Alpine 3.22.5 runtime. App and backup no longer use `latest`; CD publishes a full commit-SHA tag for traceability while Compose deploys by the immutable digest the registry returned and persists it into `.env`. A CI contract test rejects a floating Compose/Dockerfile reference or a CD that drops the digest. The evidence below records the state before the fix.

Production used floating tags such as `migrate/migrate:4`, `postgres:16-alpine`, `redis:7-alpine`, and the application defaulted to `latest`: [`docker-compose.prod.yml:48`](../docker-compose.prod.yml#L48), [`docker-compose.prod.yml:232`](../docker-compose.prod.yml#L232).

Recommended pinning to a patch version or digest, particularly for production deployments.

---

### P2-12: The destructive bot migration sat in the automatic migration chain — Resolved

**Severity:** High operational risk

> Migration `000009` was separated from the ordinary deploy: the safe runner only advances to `000008`, no-ops when the database is already at `000009`, and refuses a downgrade, a newer version or a dirty state. The destructive path is its own Compose profile, invoked from a manual workflow through the GitHub `production` environment; the runner requires exact confirmation, a handoff reference for the external bot, a backup no older than 48 hours and a restore drill no older than 7 days. The `000009` SQL additionally checks a session-only approval before `DROP SCHEMA`. The generic `make migrate-down` no longer runs bot, because its down migration only creates an empty schema; the runbook describes recovery from a snapshot instead of a fake data rollback. The evidence below records the state before the fix.

- The up migration ran `DROP SCHEMA IF EXISTS bot CASCADE`: [`migrations/bot/000009_drop_bot_schema.up.sql:22`](../migrations/bot/000009_drop_bot_schema.up.sql#L22).
- The down migration merely recreated an empty schema and could not restore any data: [`migrations/bot/000009_drop_bot_schema.down.sql:1`](../migrations/bot/000009_drop_bot_schema.down.sql#L1).
- The bot module was still in the production migration chain: [`docker-compose.prod.yml:203`](../docker-compose.prod.yml#L203).

The migration is deliberate and well documented, but needed its own deployment gate:

- Verify the external bot has taken the data over.
- Back up and test the restore before migrating.
- Require manual approval for production.
- Do not treat the down migration as a data rollback.

---

### P2-13: Test coverage was missing in the high-risk areas — Resolved

**Severity:** High engineering risk

> Added table-driven and unit tests for the notification unread cache and for service fallback/enrichment/mutation, for search validation and its focused/all modes together with handler parsing, for the media handler and its type-specific size/storage failures, for the error and logger contracts, and for the local storage lifecycle. The Redis broker has a Pub/Sub round-trip test; S3 storage has a smoke test that creates a real bucket, uploads, reads metadata and body back, health-checks, builds a URL and deletes. CI provisions digest-pinned Redis and MinIO, sets the required test variables, and runs the whole suite under the race detector. Coverage in the targeted packages now ranges from 71.9% to 100%, with the broker at 87.1% when the integration is enabled; the evidence below records the state before the fix.

The packages that had no meaningful tests:

- `internal/feature/notification/broker`
- `internal/feature/notification/cache`
- `internal/feature/notification/service`
- `internal/feature/search/*`
- `internal/feature/storage/*`
- `pkg/errors`
- `pkg/jwt`
- `pkg/logger`

`pkg/storage` now has both local/S3 unit tests and an integration test against a real MinIO bucket in CI.

The backup scheduler has a shell contract test and an end-to-end drill using throwaway PostgreSQL/Restic containers; the path to a real off-host repository and to a real failure/recovery webhook still needs to be verified in a staging environment with production-like credentials.

The Redis timeline integration tests still skip deliberately in a unit-test environment without Redis, but CI always configures `REDIS_TEST_ADDR`; a missing service or an integration failure fails the job.

Recommended test priorities, by risk:

1. The authorization matrix for posts, comments and likes.
2. Upload MIME validation and same-origin active content.
3. A fail-closed storage factory.
4. Error immutability and concurrent panic handling.
5. Feed exact-end cursors, tie-score pagination, a full queue and stale repair.
6. SSE publish/disconnect/shutdown under the race detector.
7. Notification unread-cache consistency.
8. Concurrent refresh-token rotation.

## Suggested implementation order

### Phase 1 — release blockers

1. Fix authorization for posts, comments and likes.
2. Lock down avatar/cover uploads with content validation and separate the upload origin.
3. Make the storage factory fail closed; disable the S3 option until there is a real implementation.
4. Make error values immutable and replace the panic response.

### Phase 2 — feed and notification correctness

1. Add a durable outbox for feed events.
2. Fix the timeline cursor handoff and Redis tie pagination.
3. Add timeline reconciliation and delete/visibility handling.
4. Fix SSE broker concurrency.
5. Fix unread notification cache consistency.

### Phase 3 — session and API hardening

1. Hash refresh tokens and make consume-and-rotate atomic.
2. Remove the global query-string bearer token.
3. Restrict user account detail to self/admin.
4. Strict JSON decoder, body limit and security headers.
5. Pin the exact JWT algorithm and validate issuer and audience.

### Phase 4 — production readiness

1. Shared object storage/CDN for multi-instance deployments.
2. Automated off-host backup and restore drills.
3. Pin container versions and digests.
4. Gate the destructive migration.
5. Add the missing integration, load and race tests.

## Definition of done for an audit finding

A finding should only be closed when:

- There is a regression or characterization test that exhibits the old defect.
- The fix is verified by `make test`, `go test -race ./...`, `go vet ./...` and `make lint`.
- API behavior or deployment impact is recorded in the docs or the PR.
- Any change in package responsibility is reflected in `docs.go`.
- A security-sensitive fix covers both the happy path and the denied/abuse path.
