# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

DarkVoid is a Go 1.26 social-network backend organized as a set of in-process **bounded contexts** (User, Storage, Post, Feed, Notification, Search, Admin, Settings). A single HTTP binary (`cmd/api`) wires everything together; a separate `cmd/seed` handles seed data. Swagger spec serves two filtered UIs from one `docs/swagger.json`: `/swagger/app/` (public API) and `/swagger/admin/` (admin tag, auth-protected).

## Common Commands

Always prefer the `Makefile` — it loads `.env` automatically and scopes migrations per module.

- `make build` / `make run` / `make dev` (air hot reload)
- `make test` — all tests; `make test-v` for verbose; `make test-cover` for coverage
- `make test-feature feature=user` — tests for one feature package only
- `make lint` — `golangci-lint run` (config in `.golangci.yml`)
- `make generate` — runs both `sqlc generate` and `swag` (fmt + init). Equivalent to `make sqlc-generate && make swagger-generate`.
- `make docker-up` / `make docker-down` / `make docker-logs` — full stack (Postgres + Redis + app) via `docker-compose.yml`

- `make ctl CTL_ARGS="user roles"` — operator CLI; `user grant-role -username u -role r` is the only way to assign a role no API endpoint hands out, such as `bot`, `mail suppressions` / `mail unsuppress -email x` manage the bounce suppression list (nothing in the API takes an address back off it), and `codohue reindex` re-sends existing posts to the recommendation index (posts are indexed once at creation with no retry queue, so an outage leaves them missing until this is run)
- `make install-tools` — installs `sqlc`, `swag`, `golangci-lint`, `air`, `migrate`

### Migrations

Migrations are **split per module** — each module uses its own `schema_migrations_<module>` table. Connection comes from the same `DB_*` variables the app uses, so only `DB_PASSWORD` has no default and is what the targets check for. There is deliberately no `DATABASE_URL`: golang-migrate wants one URL where the app wants discrete fields, and holding a second copy of the connection details meant a stale one silently migrated the wrong database and still exited 0. The Makefile exports `DB_*` as `PG*` instead and passes `postgres:///?x-migrations-table=…`, letting lib/pq fill in the rest — which also keeps the password out of the migrate process's argv.

- `make migrate-up` — runs user → post → notification → bot → settings in order (`MIGRATION_MODULES`). `bot` has no Go code behind it any more: the content bot moved to its own project and `migrations/bot/000009` drops the schema. The module stays in the list until every environment has run that migration — removing it sooner strands the schema in each deployed database with nothing left here to clean it up.
- `make migrate-up-user` / `make migrate-up-post` / `make migrate-up-notification` / `make migrate-up-bot` / `make migrate-up-settings`
- `make migrate-down` — rolls back **one** step per module, in `MIGRATION_MODULES_REVERSED` order. Adding a module means editing both lists.
- `make migrate-create module=post name=add_xxx` — creates a new migration pair (module must be one of `user`, `post`, `notification`, `bot`, `settings`)
- `make migrate-status` — shows current version for each module
- `make migrate-force module=user version=N` — recover from dirty state
- `make db-reset` — destroys docker volumes (prompts for confirmation)

## Architecture

### Bounded Contexts

Each feature under `internal/feature/<feature>/` owns its own `handler`, `service`, `repository`, `dto`, `entity`, `sql`, and `db` (sqlc-generated). Features do **not import each other directly**. Cross-context access goes through narrow reader interfaces defined on the *consumer* side (e.g. `feed.PostReader`, `feed.FollowReader`) and implemented by small adapters in `internal/app/*_adapters.go` that call into the owning feature's service.

`Application` (`internal/app/app.go`) constructs and owns all contexts. `context_setup.go` orchestrates init order; each `<feature>_wiring.go` file does the dependency injection, and `<feature>.go` defines the context struct + `Ports()` method used by other contexts.

### Deferred Wiring Pattern

Because contexts can't depend on each other at construction time, cross-context dependencies are injected *after* setup via `With...` methods. Example: `FollowService.WithFeedInvalidator(...)` is called in `wireFeedDependencies()` so that follow/unfollow can evict `following:ids:{userID}` in the feed cache. Do the same when adding new cross-context wiring — don't introduce direct feature imports.

### Feed Subsystem (recently refactored — see `memory/` notes)

- **DB cursor pagination** via `(created_at, id) < (cursor_ts, cursor_id)` row value comparison (see `migrations/post/000007_add_feed_cursor_index.up.sql` for the composite partial index).
- **Page 1**: merge ~60 following posts with cached trending, score+sort, return top 20. **Page 2+**: pure following in DB order, no trending injection. **Discover fallback**: when a user has an empty following feed, cursor hands off seamlessly to `GetDiscoverWithCursor` because `FollowingCursor` and `DiscoverCursor` share fields.
- **Cache keys**: `following:ids:{userID}` (5m TTL), `trending:posts` (15m TTL). No per-user feed cache. Redis is a **hard dependency** — see Configuration below.
- **Scoring**: `score = log(1+likes)*10 + RecencyScale/(1+hours)^decay + RelationshipBonus`, defaults `RelationshipBonus=10, RecencyScale=20, DecayExponent=1.5`. The three weights are stored in `settings.feed`, not compiled in — see Runtime settings below. Local ranker is the default; Codohue CF recommender plugs in via `feedSvc.WithRecommender(...)` when `CODOHUE_ENABLED=true`.

### Routing Groups

`app.registerRoutes` splits `/api/v1` into two sibling groups: **Group A** has no request timeout (SSE notifications must keep a plain `http.Flusher`-compatible ResponseWriter), **Group B** applies `RequestTimeout` to all REST endpoints. chi captures the middleware stack at group-creation time, so this split is load-bearing — don't collapse them.

The same property makes route *nesting* load-bearing for authorization. `SettingsContext.registerAdminRoutes` is called from inside `AdminContext.RegisterRoutes`' `/admin` group so `/admin/settings/*` inherits its `auth.Required` + `RequireRole(admin)`. Declaring `/admin/settings` as a sibling of `/admin` is not equivalent: chi accepts it and resolves the paths correctly, but the subrouter inherits none of the parent group's middleware, leaving that surface unauthenticated. Both behaviours are pinned in `internal/app/settings_routes_test.go` and `internal/app/routes_test.go` — when adding a route group under an existing guarded prefix, nest it.


### Code Generation

- **sqlc** (`sqlc.yaml`): four separate generators (`user`, `post`, `notification`, `settings`), each emitting to `internal/feature/<module>/db/`. Post and notification include the user schema in their `schema:` list because they reference user tables; settings does not, because it holds foreign ids as plain UUIDs and never joins across schemas. **Do not hand-edit files under `internal/feature/*/db/`** — regenerate via `make sqlc-generate`. Exception: some cursor queries in `internal/feature/post/db/post_queries.sql.go` are hand-patched additions; check the `sql/` source before regenerating, or the edits will be lost.
- **swag** (`swag init -g cmd/api/main.go`): comments on handlers drive `docs/swagger.json`. The two Swagger UIs are produced by `swaggerFilterHandler` at serve-time based on the `admin` / `auth` tags — not two separate generations.

### Mail Delivery

`mailer.Mailer.Send` returns the provider's message id alongside its error. SMTP has no server-assigned id, so it generates its own `Message-ID` header and returns that. Provider selection and the Resend specifics are under Configuration below.

Every send is recorded in `usr.email_deliveries` keyed by that id — `AccountMailService.deliver` is the single choke point all three flows go through, so a flow added later cannot forget to record. A recording failure is logged and swallowed: the mail has already left, and reporting failure would only make the caller retry a send that worked.

**Delivery reports.** `POST /api/v1/webhooks/resend` receives them. It sits outside every auth group — the caller is Resend, not a user, and the Svix signature (HMAC-SHA256 over `{svix-id}.{svix-timestamp}.{body}`) is the authentication, which is why the handler verifies the *raw* bytes before anything decodes them. With no `RESEND_WEBHOOK_SECRET` the route is not registered at all rather than served unverified: it writes to the suppression list, so an open endpoint would let anyone block any address. The 5-minute timestamp tolerance is what expires a replay — a captured request stays validly signed forever.

Status codes are the retry contract: 401 for a bad signature, 400 for a signed-but-unparseable payload (it will fail every retry too), 500 only for our own faults so Svix retries, and 200 for an event type we do not model or one naming a send we never recorded — neither becomes acceptable on a retry. Idempotency needs no dedup table: `ApplyEmailDeliveryEvent` is guarded on `last_event_at` so a replayed or late event cannot overwrite a newer outcome, and suppression is an upsert.

**Suppression.** `mailer.SuppressionGate` wraps the Mailer and drops recipients on the list; it is a decorator rather than a check at each call site for the same reason as above, and its checker is injected by `wireMailDependencies` after the user context exists. A checker error fails *open* — the list is a reputation guard, and blocking password resets on a database blip is the worse outcome. Only **permanent** bounces and complaints suppress: `Transient` covers a full mailbox or greylisting, and suppressing on that would lock a real user out of password resets over a problem that fixes itself. `SendPasswordReset` maps `ErrSuppressed` to a success response, since a 500 for suppressed addresses only would leak which addresses exist and have bounced. Nothing in the API un-suppresses — that is `make ctl CTL_ARGS="mail unsuppress -email x"`, alongside `mail suppressions` to see the list.

### Logging

Always use `pkg/logger` (wraps `slog`) — never raw `log/slog`. The root logger is set via `logger.SetDefault` in `app.setupLogger`; package-level helpers use that default.

### Docs Convention

Every package should have a `docs.go` stating its purpose. Update it in the same change when a package's responsibilities shift.

## Testing

- `testing` package only; files colocated as `*_test.go`.
- Name tests like `TestLogin_Success` / `TestCreatePost_ValidationError`.
- Service and handler packages must have tests for every logic change.
- Before refactoring legacy code, write a characterization test first.

## Linting

Respect `.golangci.yml` — all production and test code must pass `make lint` before committing. If a linter fires, fix the root cause rather than silencing it. Only suppress with a **targeted** `//nolint:<linter> // <reason>` on the specific line, with a real reason. Do **not** add blanket `//nolint` directives, and do **not** relax `.golangci.yml` to clear a failure unless the rule itself is genuinely wrong for this codebase — in which case justify it in the commit message.

## Commits

A commit message describes **only the change in that commit** — what it does and why, in terms of the code itself. Do **not** reference specs, plans, task ids, or docs paths (`specs/006-…`, `plan.md`, `T-042`, "per the spec"): those live outside the history and rot independently of it, so a reader six months out follows the pointer to a file that has moved, changed meaning, or is no longer there. Anything the reader needs in order to understand the change belongs in the message body, spelled out.

## Configuration

Config splits in two. `pkg/config` loads from `.env` everything a process needs before it can reach anything else — the database, the port, the signing keys, the mail and storage providers. Everything an operator changes while watching a graph lives in the database instead; see **Runtime settings** below. Update `.env.example` whenever a new variable is added. Keys of note:

- `SETTINGS_REFRESH_INTERVAL` (default `30s`) — how often each instance re-reads `settings.feed`. It cannot itself be a stored setting: it is the setting that says how often to read the settings.
- `FEED_FANOUT_WORKERS` / `FEED_FANOUT_QUEUE_SIZE` — all that is left of the old `FEED_*` group. They allocate a goroutine pool and a channel at construction, so a stored value could not take effect without rebuilding the dispatcher, and a knob that quietly does nothing until the next restart is a worse deal than one that admits it needs one. `FEED_TIMELINE_ENABLED`, `FEED_TIMELINE_ROLLOUT_PERCENT`, `FEED_TIMELINE_MAX_ITEMS`, `FEED_TIMELINE_TTL`, `FEED_TIMELINE_REFRESH_ON_MISS`, `FEED_FANOUT_ENABLED` and `FEED_FANOUT_MAX_FOLLOWERS` are no longer read.

- `REDIS_*` — **required, and there is no `REDIS_ENABLED`.** Redis holds the feed cache, the materialized timeline and the cross-instance notification Pub/Sub, so an instance without one does not serve a slower feed — it serves a different one, from a different code path, with SSE fan-out silently confined to whichever instance the producer happened to hit. The old no-op caches made that difference invisible, which is why they are gone (`NopNotificationCache` and `NopHashtagCache` deleted; `NopFeedCache`/`NopTimelineStore` kept only as test doubles). `setupRedis` therefore uses `pkgredis.New` (which pings) rather than `NewLazy`: an unreachable Redis fails boot. `GET /health` probes it too and reports `redis: up|down`, returning 503 on down — it probes Postgres *and* Redis before responding rather than returning on the first failure, so a 503 says which one broke. Note this is the opposite call from `CODOHUE_EVENTS_REDIS_*`, which stays lazy and never fails boot: dropping behavior events while an optional integration's bus is down is worth serving through, an empty timeline is not.
- `CODOHUE_ENABLED` — **Codohue is never started from this repo.** It has no containers, no Qdrant and no schema here; `docker-compose.yml` carries no `codohue` profile and there is no `make docker-up-codohue`. Deploy it from its own compose project and point `CODOHUE_BASE_URL` / `CODOHUE_ADMIN_URL` at it — this project only ever *reaches* it, over HTTP and over the events Redis Stream. Because `.env`'s URLs are written from the host's point of view and `localhost` inside a container is the container, the `app`, `seed` and `ctl` services override both, defaulting to `host.docker.internal`; `CODOHUE_DOCKER_BASE_URL` / `CODOHUE_DOCKER_ADMIN_URL` override that in turn. When false, feed uses local scoring only; CF recommender is off. When true, the client is wired whether or not Codohue answers at startup: an unreachable Codohue is already handled per call (the feed falls back to local scoring, ingest logs and moves on), so gating the wiring on a boot probe only meant a brief outage disabled the feature for the whole process. The probe now just classifies, `GET /health` reports `codohue: off|active|degraded`, and a background monitor re-probes every 2 minutes for the life of the process — not only after a failed boot probe, or `/health` would assert `active` through an outage that started a minute after boot. Nothing is rewired at runtime, so no service field is written once the server is serving. `pkg/codohue` carries a circuit breaker over the HTTP surface: 3 consecutive availability failures open it for 30s, so an outage costs one timeout rather than 2-3s added to every feed request. A 4xx counts as *available* — the service answered, and opening the circuit because one call site sent something malformed would disable the integration for everyone. `Ping` bypasses the gate (a health probe answered from a cached "circuit open" measures nothing) but reports its outcome, so a successful probe closes the circuit at once. `/health` reads the breaker as well as the last probe: the breaker learns of an outage from real traffic within a few requests, where the probe would not notice until its next tick, so an open circuit overrides a stale `active`.
- `CODOHUE_EVENTS_REDIS_*` — behavior events are the one part of the integration that does not travel over HTTP. `PublishBehaviorEvent` XADDs to the `codohue:events` Redis Stream, and Codohue's consumer reads that stream from whichever Redis Codohue owns, so events published anywhere else are never seen. Leave `CODOHUE_EVENTS_REDIS_HOST` unset when one Redis serves both sides — the producer then shares `app.redis`. Set it when Codohue owns a separate instance: `app.codohueEventsClient()` hands only the producer to Codohue's Redis, leaving the feed cache, timeline store, and notification pub/sub on ours. Pointing `REDIS_HOST` at Codohue's instance instead also delivers events, but it drags that darkvoid-owned state onto a stack darkvoid does not own, so an outage in an integration meant to be optional takes core features down with it. The events client is built with `pkgredis.NewLazy` and **kept after a failed boot probe** — discarding it would repeat the mistake already fixed for the HTTP client, disabling events for the life of the process rather than the length of the outage. It never fails boot.
- **Hostname pinning under `docker-compose.codohue.yml`.** A container on two compose networks sees both projects' service names, so bare `postgres` and `redis` each resolve to two servers with no guarantee which wins — measured on the dev box, `redis` from the app container resolved to Codohue's instance. The override therefore gives our datastores network-unique aliases (`darkvoid-postgres`, `darkvoid-redis`) and addresses Codohue's Redis by container name via `CODOHUE_STACK`. Using a bare service name for either is the failure that hides: the app boots, the health check passes, and the cache or the event stream is quietly pointed at the wrong server.
- `COOKIE_SAMESITE` / `COOKIE_DOMAIN` / `COOKIE_SECURE` — the refresh token cookie's deployment-dependent attributes, resolved in `pkg/config` and carried to `handler.NewAuthHandler` as a `CookieOptions` struct rather than as three positional bools. `Path` and `HttpOnly` are deliberately **not** configurable: `Path` is coupled to where the auth routes are mounted rather than to the deployment, so a variable for it would be a second source of truth that disagrees silently — as a cookie the browser never sends back — and `HttpOnly` has one safe value, since a refresh token readable from JavaScript turns any XSS into long-lived session theft and the clients that need the token in hand already ask with `X-Client-Type: mobile`. `COOKIE_SAMESITE` defaults to `lax`, which is correct only while the frontend and the API share a registrable domain; a frontend on a different domain needs `none`, because the browser withholds a Lax cookie on cross-site fetches and `/auth/refresh` then answers 401 in a way that reads as an expired session. CORS does not cover this — `AllowCredentials` governs whether the response is readable, not whether the cookie was attached. `COOKIE_SECURE` is a tri-state: unset means "true unless `ENVIRONMENT=development`", and it is settable because a staging box behind TLS termination that still calls itself development gets the derived value wrong. Two combinations fail the boot instead of failing in a browser: an unrecognised SameSite name (the zero `http.SameSite` emits no attribute at all, so a typo would otherwise change behaviour without saying so) and `none` without `Secure` (which the browser drops outright). A `COOKIE_DOMAIN` carrying a scheme, port or path is rejected for the same reason; empty means host-only, which is what a single API host wants. Set cookie attributes are logged at boot by `setupUserContext` — resolved values, not the raw variables, since a wrong one surfaces as a missing cookie rather than an error.
- `ROOT_EMAIL` + `ROOT_PASSWORD` — auto-bootstraps a root user on first boot if `ROOT_USERNAME` is not taken, then grants it the `admin` role on **every** boot so the admin API/Swagger stays reachable (`bootstrapRootUser` in `app.go` → `AdminContext.GrantAdminRole`). No-op when either var is empty.
- `STORAGE_PROVIDER` — only `local` is currently supported and serves `/static/*` from `STORAGE_LOCAL_DIR`. Any other value fails startup instead of silently discarding uploads.
- `MAILER_PROVIDER` — `nop` (logs only), `resend`, or `smtp`. A named-but-unconfigured provider (`resend` with no `RESEND_API_KEY`) fails at startup instead of falling back to `nop`: a mailer that only logs looks like a healthy boot while every verification and reset email silently disappears. `resend` talks to `POST /emails` over `net/http` rather than through `resend-go` — the API is one call, and owning it keeps the timeout, retry policy and logging consistent with `pkg/codohue`. Retries cover 429/5xx/transport only; a 4xx is permanent (an unverified sending domain does not become verified on the second try) and returns at once. One `Idempotency-Key` is generated per `Send` and reused across its retries, so a delivered-but-timed-out attempt is not re-sent. No circuit breaker, unlike Codohue: two of the three flows are fire-and-forget goroutines and volume is low, so `http.Client.Timeout` plus bounded retries is enough — but note `SendPasswordReset` *is* in the request path and propagates its error, which is why the per-attempt timeout is load-bearing. The Resend sending domain must be verified (SPF + DKIM) or every send returns 403.

### Runtime settings

`settings.feed` holds the feed knobs an operator changes without a redeploy: timeline serving and its rollout percent, retention (`timeline_max_items`, `timeline_ttl_seconds`), `timeline_refresh_on_miss`, the fanout kill switch and follower cap, and the three ranking weights — which were never environment variables at all, just literals in `feed.DefaultScorerConfig`. `GET`/`PATCH /admin/settings/feed` reads and edits them, nested inside the `/admin` group so it inherits that group's auth and admin role check, the same load-bearing nesting described under Routing Groups.

These settings are consumed in-process, so they land in one `feed.Settings` holder (`atomic.Pointer`) that the read path, the ranker, the timeline store, the refresher and the dispatcher all read. **Nothing captures a value at construction any more** — that was the bug the whole change fixes: five components each held their own copy from `SetupFeedContext`, so no value could change without rebuilding all five.

Three properties are load-bearing:

- **The service publishes on read as well as on write.** The instance serving a `PATCH` applies the snapshot at once; its siblings pick it up from their refresh loop within `SETTINGS_REFRESH_INTERVAL`. Publish-on-write alone would leave a two-instance deployment serving two different rollout percentages until the next restart — the exact failure this replaced.
- **What is published is the row the database returned, not the request.** A partial update names some fields and inherits the rest, so only the round trip knows the whole new state.
- **A failed read never fails boot and never clears the snapshot.** The defaults are a working configuration and the last known values stand through an outage; the alternative is taking the API down over a ranking weight. `entity.DefaultFeedSettings`, `feed.DefaultRuntimeSettings` and the column `DEFAULT`s are three copies of the same numbers, pinned together by tests in `settings/entity` and `internal/app` — if they drift, every restart spends a moment ranking on numbers the admin API does not report.

Validation lives in `entity.FeedSettingsUpdate.Validate` and mirrors the `CHECK`s, so a bad value is a 400 naming the field rather than a 500 naming a constraint. It validates rather than clamps: a silently clamped rollout percent reads back as a number the operator did not type. `decay_exponent` rejects 0 specifically — at 0 the recency term is the same constant for every post, which removes recency from the formula instead of flattening it.

<!-- SPECKIT START -->
For additional context about technologies to be used, project structure,
shell commands, and other important information, read the current plan
at `specs/006-materialized-ranked-feed/plan.md`.
<!-- SPECKIT END -->
