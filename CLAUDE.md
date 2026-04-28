# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

DarkVoid is a Go 1.26 social-network backend organized as a set of in-process **bounded contexts** (User, Storage, Post, Feed, Notification, Search, Admin). A single HTTP binary (`cmd/api`) wires everything together; a separate `cmd/seed` handles seed data. Swagger spec serves two filtered UIs from one `docs/swagger.json`: `/swagger/app/` (public API) and `/swagger/admin/` (admin tag, auth-protected).

## Common Commands

Always prefer the `Makefile` — it loads `.env` automatically and scopes migrations per module.

- `make build` / `make run` / `make dev` (air hot reload)
- `make test` — all tests; `make test-v` for verbose; `make test-cover` for coverage
- `make test-feature feature=user` — tests for one feature package only
- `make lint` — `golangci-lint run` (config in `.golangci.yml`)
- `make generate` — runs both `sqlc generate` and `swag` (fmt + init). Equivalent to `make sqlc-generate && make swagger-generate`.
- `make docker-up` / `make docker-down` / `make docker-logs` — full stack (Postgres + Redis + app) via `docker-compose.yml`
- `make install-tools` — installs `sqlc`, `swag`, `golangci-lint`, `air`, `migrate`

### Migrations

Migrations are **split per module** — each module uses its own `schema_migrations_<module>` table. `DATABASE_URL` must be set (via `.env` or on the command line).

- `make migrate-up` — runs user → post → notification in order
- `make migrate-up-user` / `make migrate-up-post` / `make migrate-up-notification`
- `make migrate-down` — rolls back **one** step per module (reverse order)
- `make migrate-create module=post name=add_xxx` — creates a new migration pair (module must be one of `user`, `post`, `notification`)
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

### Code Generation

- **sqlc** (`sqlc.yaml`): three separate generators (`user`, `post`, `notification`), each emitting to `internal/feature/<module>/db/`. Post and notification include the user schema in their `schema:` list because they reference user tables. **Do not hand-edit files under `internal/feature/*/db/`** — regenerate via `make sqlc-generate`. Exception: some cursor queries in `internal/feature/post/db/post_queries.sql.go` are hand-patched additions; check the `sql/` source before regenerating, or the edits will be lost.
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
- `CODOHUE_ENABLED` — when false, feed uses local scoring only; CF recommender is off.
- `ROOT_EMAIL` + `ROOT_PASSWORD` — auto-bootstraps a root user on first boot if no users exist (`bootstrapRootUser` in `app.go`). No-op otherwise.
- `STORAGE_PROVIDER` — `local` (default, serves `/static/*` from `STORAGE_LOCAL_DIR`) or `s3` (S3/MinIO/GCS).
- `MAILER_PROVIDER` — `nop` (logs only) or `smtp`.

<!-- SPECKIT START -->
For additional context about technologies to be used, project structure,
shell commands, and other important information, read the current plan
at `specs/002-post-test-coverage/plan.md`.
<!-- SPECKIT END -->
