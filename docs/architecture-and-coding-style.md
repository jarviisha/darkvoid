# DarkVoid Architecture and Coding Style

This document summarizes the architecture and coding style expressed by the
current codebase. It is descriptive first: follow these patterns when extending
the system unless a feature has a clear reason to introduce a different shape.

## System Shape

DarkVoid is a Go REST API organized as a modular monolith with explicit bounded
contexts. The executable entrypoints live in `cmd/api` and `cmd/seed`; runtime
composition lives in `internal/app`; feature code lives under
`internal/feature/<feature>`.

The main feature contexts are:

- `user`: accounts, auth, profiles, follows, refresh tokens, email tokens
- `post`: posts, media, comments, likes, hashtags, mentions, post search
- `feed`: ranked feeds, discover feed, prepared timelines, fanout, cursors,
  recommendations, trending integration
- `notification`: notification persistence, caching, brokers, SSE delivery
- `search`: cross-feature search orchestration
- `storage`: media upload and retrieval integration
- `admin`: administrative user operations

Shared runtime and infrastructure concerns are kept outside feature packages:

- `internal/app`: composition root, route registration, cross-context adapters
- `internal/app/middleware`: auth, role, and rate-limit middleware
- `internal/http`: success response helpers and request context helpers
- `internal/infrastructure`: infrastructure adapters such as database and mailer
- `internal/pagination`, `internal/validation`: shared request utilities
- `pkg/*`: reusable packages for config, logger, errors, jwt, redis, storage,
  database, Codohue integration, and local TF-IDF embeddings

## Runtime Composition

`internal/app.Application` owns application lifetime and dependency setup:

1. Load configuration outside the app and pass it into `app.New`.
2. Initialize logger, PostgreSQL pool, optional Redis, JWT service, storage, and
   infrastructure clients.
3. Initialize each bounded context through `Setup<Feature>Context`.
4. Wire cross-context dependencies after contexts exist.
5. Register routes on the Chi router.
6. Start and gracefully shut down the HTTP server.

Feature contexts are structs such as `UserContext`, `PostContext`, and
`FeedContext`. They hold repositories, services, and handlers for one feature.
Constructors should keep this order visible:

1. repositories
2. local adapters/cache choices
3. services
4. handlers
5. returned context struct

Cross-context dependencies should not be imported directly by feature services.
Instead, `internal/app` exposes narrow ports from each context and builds
adapters between them. For example, post services need author and follow data,
but they depend on local `userReader` and `followChecker` interfaces; the app
layer adapts the user context to those interfaces.

## Request Flow

The normal HTTP flow is:

```text
client
  -> chi route and middleware
  -> feature handler
  -> feature service
  -> feature repository or external port
  -> sqlc-generated query / infrastructure client
```

Handlers are responsible for HTTP concerns:

- decode JSON or query parameters
- read authenticated user IDs from request context
- handle web/mobile transport differences, such as refresh-token cookie vs body
- call services
- write success responses with `internal/http.WriteJSON`
- write failures through `pkg/errors.WriteJSON`

Services own business logic:

- validation and normalization
- authorization checks that are domain-specific
- orchestration across repositories and optional ports
- transactions where multiple writes must commit together
- logging business events with request context
- converting infrastructure failures to application errors

Repositories own persistence translation:

- call sqlc generated query interfaces
- map database rows to feature entities
- map database errors through `internal/infrastructure/database`
- expose `WithTx` where services need transactional composition

Generated sqlc code under `internal/feature/*/db` is not hand-edited.

## Dependency Direction

Keep dependencies pointing inward to local domain contracts:

- handlers depend on service interfaces local to the handler package
- services depend on small interfaces local to the service package
- repositories depend on generated db packages and database infrastructure
- feature packages do not import sibling feature implementations
- the app layer is allowed to know about multiple contexts and adapt them

This keeps packages testable and prevents import cycles. When a feature needs
data from another feature, define the smallest interface at the consuming side,
then implement the adapter in `internal/app`.

## Optional Integrations

Optional infrastructure should degrade to a no-op implementation when disabled.
Current examples:

- Redis-backed caches fall back to `Nop*Cache` or `NopTimelineStore`.
- Codohue recommendation/trending integration is wired only when enabled.
- Email sending after registration is attached with `WithEmailSender`.
- Feed timeline serving is gated by config and rollout percentage.

Prefer `With...` methods for optional dependencies attached at wire-up time.
They make the core constructor small and keep optional behavior explicit.

## API Conventions

Success responses are written directly as JSON at the top level. Errors use the
standard shape from `pkg/errors`:

```json
{
  "error": {
    "code": "bad_request",
    "message": "Invalid request body"
  }
}
```

Use typed application errors such as bad request, unauthorized, forbidden, not
found, conflict, and internal errors. Do not leak raw database or infrastructure
errors to clients.

Pagination style depends on endpoint semantics:

- Feed-style endpoints use opaque cursor tokens.
- Cursor contents are owned by the feature package that emits them.
- Cursor validation should reject malformed or obsolete shapes explicitly.
- Offset pagination is acceptable for bounded admin or simple list operations
  where the contract already uses it.

Swagger comments live beside handlers. Regenerate Swagger when handler comments
or public API contracts change.

## Persistence and Data Modeling

PostgreSQL is the authoritative store. SQL lives in feature-local `sql/`
directories and generated Go lives in feature-local `db/` directories. Migrations
are split by module under `migrations/user`, `migrations/post`, and
`migrations/notification`.

Repository methods should return feature entities, not sqlc row types. Keep
mapping functions near repositories so services do not learn database details.

Use batch repository methods for enrichment paths to avoid N+1 reads. Existing
examples include batch author lookup, media lookup, liked-post lookup, mention
lookup, and hashtag lookup.

## Feed Architecture

Feed is intentionally more orchestration-heavy than other contexts. It combines:

- following graph reads
- post reads
- like enrichment
- local ranking/scoring
- Redis prepared timelines
- fanout workers
- optional Codohue recommendations and trending
- cursor continuation across timeline, recommendation, and trending sources

Feed owns its cursor contract in `internal/feature/feed/cursor.go`. The cursor is
opaque to clients, encoded as base64 JSON, and validated against the
authenticated feed owner when needed.

Prepared timeline reads are attempted first when rollout gates allow them. On
miss or error, the service falls back to the mixed feed path. This pattern keeps
fast-path infrastructure optional without making it the source of truth.

## Error Handling and Logging

Use normal Go error returns. Add context when wrapping lower-level errors, and
convert errors to client-safe application errors at service or handler
boundaries.

Use `logger.Info`, `logger.Warn`, and `logger.LogError` with `context.Context`
so request IDs and contextual fields flow through logs. Log meaningful domain
events such as registration, login, feed timeline hit/miss, and fallback paths.

Fire-and-forget goroutines should detach from request cancellation only when the
work is intentionally post-request, and they must set their own timeout. The
registration email flow is the model for this.

## Testing Style

Tests live next to the code they cover and use the standard `testing` package.
The codebase favors focused mocks and table-driven tests:

- service tests define small in-file mocks for the ports the service consumes
- validation and parsing tests use table cases
- feed tests cover cursor behavior, duplicates, timeline fallback, stale data,
  rollout gates, and source continuation
- handler tests use interfaces so HTTP behavior can be tested without real
  repositories

Add tests for every logic change, especially in service and handler packages.
Before refactoring old behavior, add characterization tests that describe the
current contract.

## Coding Style

The dominant style is idiomatic, explicit Go:

- keep packages small and purpose-named
- pass `context.Context` as the first argument for request-scoped work
- accept narrow interfaces at package boundaries and return concrete structs
- prefer constructor functions named `New<Type>` or `Setup<Feature>Context`
- keep optional dependencies explicit through `With...` methods
- use early returns for validation and error paths
- avoid clever control flow when straight-line code is clearer
- use standard library encoders/parsers instead of ad hoc string handling where
  structured data is involved
- keep comments for exported types, package docs, contracts, and non-obvious
  decisions
- include a `docs.go` for every package, and update it when package
  responsibility changes

Formatting and linting are part of the contract:

- run `gofmt` and `goimports`
- respect `.golangci.yml`
- do not introduce ignored errors unless there is a documented best-effort case
- avoid context-less I/O in production code
- do not hand-edit generated sqlc files

## How to Extend the Codebase

When adding a new feature:

1. Create `internal/feature/<feature>` with `docs.go`.
2. Split into `entity`, `dto`, `repository`, `service`, and `handler` only when
   each package has real responsibility.
3. Define service-side ports for dependencies owned by other contexts.
4. Add a `<Feature>Context` and `Setup<Feature>Context` in `internal/app`.
5. Expose narrow `Ports()` only for capabilities other contexts actually need.
6. Add app-layer adapters for cross-context dependencies.
7. Register routes in a feature route file.
8. Add tests at service and handler boundaries.
9. Add migrations and sqlc queries if persistent state is needed.
10. Regenerate generated artifacts through `make generate` when contracts or SQL
    change.

When changing an existing feature, preserve the boundary shape first. Reach for a
new abstraction only when it removes real duplication, prevents a dependency
cycle, or matches an established local pattern.

## Command Reference

Use the Makefile as the primary interface:

```sh
make build
make run
make test
make test-feature feature=feed
make test-cover
make lint
make docker-up
make generate
```

