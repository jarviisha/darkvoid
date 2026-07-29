# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

DarkVoid is a Go 1.26 social-network backend organized as a set of in-process **bounded contexts** (User, Storage, Post, Feed, Notification, Search, Admin, Bot). A single HTTP binary (`cmd/api`) wires everything together; a separate `cmd/seed` handles seed data. Swagger spec serves two filtered UIs from one `docs/swagger.json`: `/swagger/app/` (public API) and `/swagger/admin/` (admin tag, auth-protected).

## Common Commands

Always prefer the `Makefile` — it loads `.env` automatically and scopes migrations per module.

- `make build` / `make run` / `make dev` (air hot reload)
- `make test` — all tests; `make test-v` for verbose; `make test-cover` for coverage
- `make test-feature feature=user` — tests for one feature package only
- `make lint` — `golangci-lint run` (config in `.golangci.yml`)
- `make generate` — runs both `sqlc generate` and `swag` (fmt + init). Equivalent to `make sqlc-generate && make swagger-generate`.
- `make docker-up` / `make docker-down` / `make docker-logs` — full stack (Postgres + Redis + app) via `docker-compose.yml`
- `make bot` — run the content bot against a running API; `make docker-up-bot` / `docker-down-bot` / `docker-logs-bot` for the containerised form (opt-in `bot` compose profile, so a plain `docker compose up` never starts posting)
- `make ctl CTL_ARGS="user roles"` — operator CLI; `user grant-role -username u -role r` is how the bot runner gets the `bot` role
- `make install-tools` — installs `sqlc`, `swag`, `golangci-lint`, `air`, `migrate`

### Migrations

Migrations are **split per module** — each module uses its own `schema_migrations_<module>` table. `DATABASE_URL` must be set (via `.env` or on the command line).

- `make migrate-up` — runs user → post → notification → bot in order (`MIGRATION_MODULES`)
- `make migrate-up-user` / `make migrate-up-post` / `make migrate-up-notification` / `make migrate-up-bot`
- `make migrate-down` — rolls back **one** step per module, in `MIGRATION_MODULES_REVERSED` order. Adding a module means editing both lists.
- `make migrate-create module=post name=add_xxx` — creates a new migration pair (module must be one of `user`, `post`, `notification`, `bot`)
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
- **Cache keys**: `following:ids:{userID}` (5m TTL), `trending:posts` (15m TTL). No per-user feed cache. Redis is optional — when `REDIS_ENABLED=false`, a no-op cache is substituted so feature code keeps working.
- **Scoring**: `score = log(1+likes)*10 + RecencyScale/(1+hours)^decay + RelationshipBonus` with defaults `RelationshipBonus=10, RecencyScale=20, DecayExponent=1.5`. Local ranker is the default; Codohue CF recommender plugs in via `feedSvc.WithRecommender(...)` when `CODOHUE_ENABLED=true`.

### Routing Groups

`app.registerRoutes` splits `/api/v1` into two sibling groups: **Group A** has no request timeout (SSE notifications must keep a plain `http.Flusher`-compatible ResponseWriter), **Group B** applies `RequestTimeout` to all REST endpoints. chi captures the middleware stack at group-creation time, so this split is load-bearing — don't collapse them.

The same property makes route *nesting* load-bearing for authorization. `BotContext.registerAdminRoutes` is called from inside `AdminContext.RegisterRoutes`' `/admin` group so `/admin/bots/*` inherits its `auth.Required` + `RequireRole(admin)`. Declaring `/admin/bots` as a sibling of `/admin` is not equivalent: chi accepts it and resolves the paths correctly, but the subrouter inherits none of the parent group's middleware, leaving that surface unauthenticated. Both behaviours are pinned in `internal/app/bot_routes_test.go` — when adding a route group under an existing guarded prefix, nest it.

`/api/v1/bot` is a separate group guarded by `RequireRole(bot)` instead: an admin token is rejected there and a bot token is rejected on `/admin`.

### Code Generation

- **sqlc** (`sqlc.yaml`): four separate generators (`user`, `post`, `notification`, `bot`), each emitting to `internal/feature/<module>/db/`. Post and notification include the user schema in their `schema:` list because they reference user tables; bot does not, because it holds foreign ids as plain UUIDs and never joins across schemas. **Do not hand-edit files under `internal/feature/*/db/`** — regenerate via `make sqlc-generate`. Exception: some cursor queries in `internal/feature/post/db/post_queries.sql.go` are hand-patched additions; check the `sql/` source before regenerating, or the edits will be lost.
- **swag** (`swag init -g cmd/api/main.go`): comments on handlers drive `docs/swagger.json`. The two Swagger UIs are produced by `swaggerFilterHandler` at serve-time based on the `admin` / `auth` tags — not two separate generations.

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

## Configuration

All config is loaded from `.env` via `pkg/config`. Update `.env.example` whenever a new variable is added. Keys of note:

- `REDIS_ENABLED` — when false, feed cache becomes no-op (feature still works).
- `CODOHUE_ENABLED` — when false, feed uses local scoring only; CF recommender is off. When true, the client is wired whether or not Codohue answers at startup: an unreachable Codohue is already handled per call (the feed falls back to local scoring, ingest logs and moves on), so gating the wiring on a boot probe only meant a brief outage disabled the feature for the whole process. The probe now just classifies, `GET /health` reports `codohue: off|active|degraded`, and a background monitor re-probes every 2 minutes for the life of the process — not only after a failed boot probe, or `/health` would assert `active` through an outage that started a minute after boot. Nothing is rewired at runtime, so no service field is written once the server is serving. `pkg/codohue` carries a circuit breaker over the HTTP surface: 3 consecutive availability failures open it for 30s, so an outage costs one timeout rather than 2-3s added to every feed request. A 4xx counts as *available* — the service answered, and opening the circuit because one call site sent something malformed would disable the integration for everyone. `Ping` bypasses the gate (a health probe answered from a cached "circuit open" measures nothing) but reports its outcome, so a successful probe closes the circuit at once. `/health` reads the breaker as well as the last probe: the breaker learns of an outage from real traffic within a few requests, where the probe would not notice until its next tick, so an open circuit overrides a stale `active`.
- `ROOT_EMAIL` + `ROOT_PASSWORD` — auto-bootstraps a root user on first boot if `ROOT_USERNAME` is not taken, then grants it the `admin` role on **every** boot so the admin API/Swagger stays reachable (`bootstrapRootUser` in `app.go` → `AdminContext.GrantAdminRole`). No-op when either var is empty.
- `STORAGE_PROVIDER` — `local` (default, serves `/static/*` from `STORAGE_LOCAL_DIR`) or `s3` (S3/MinIO/GCS).
- `MAILER_PROVIDER` — `nop` (logs only) or `smtp`.
- `BOT_*` / `GEMINI_API_KEY` — read only by `cmd/bot`, never by the API. They cover credentials and an address and nothing else: interval, persona count, model chain, personas, and topics live in the `bot` schema and are edited through `/admin/bots`, so the bot re-reads them on every tick. `BOT_ACCOUNTS`, `BOT_POST_INTERVAL`, and `GEMINI_MODELS` are no longer read.

### Bot control plane

`cmd/bot` stays an external HTTP client — it dogfoods the public API rather than running in-process. It polls `GET /bot/plan` for its desired state and reports each attempt to `POST /bot/runs`, which is the only reason its activity is visible outside the bot process's own logs.

Two accounts, deliberately: the **runner** holds the `bot` role and is the only one allowed on `/bot/*`; the **personas** hold no role and only publish their own posts. The bot registers a persona on first use but never the runner — an auto-created runner would lack the role, and the 403 on every plan fetch would read as a server fault. Grant it with `make ctl CTL_ARGS="user grant-role -username bot_runner -role bot"`.

Run-now is the fiddly part — the plan is a filtered, capped view, so a request for a persona outside it silently never fires. Three rules keep that from happening, all of them load-bearing:

- `ListEnabledBots` sorts pending run requests ahead of username order, so a request for a persona sorting past the `accounts` cap still reaches the plan.
- `RequestRun` rejects a disabled persona with 409 instead of recording a request the plan can never carry.
- `UpdateBot` clears the flag when a persona is disabled, so re-enabling it later doesn't fire a request nobody remembers making.

Separately, `ReportRun` clears the flag only when the bot sets `honored_run_request`. Clearing on every report drops a request that arrived after the bot's last plan fetch.

**Activity-log retention.** `entity.RunRetention` (30 days) is the single definition of the window, and both the prune and the per-persona summary on `GET /admin/bots` take it as a parameter — so the summary can never describe a period the log has dropped. `last_success_at` / `last_error_at` therefore mean "within the window", not "ever". Pruning rides along with each `ReportRun` rather than a scheduler: in steady state the indexed probe deletes nothing and costs ~0.1ms, cheaper than owning a cron for a table only that method writes to. Measured before changing anything — at 210k rows the unbounded aggregate took 45ms and grew linearly; a per-persona subquery rewrite looked faster and measured 250ms because the planner walks the whole `created_at` index for a persona with no rows. Bounding the data, not restructuring the query, was the fix.

<!-- SPECKIT START -->
For additional context about technologies to be used, project structure,
shell commands, and other important information, read the current plan
at `specs/006-materialized-ranked-feed/plan.md`.
<!-- SPECKIT END -->
