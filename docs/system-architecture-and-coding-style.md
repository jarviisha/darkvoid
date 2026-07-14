# System Architecture and Coding Style

This document describes the architecture and coding style reflected by the
current Go backend. It avoids project-specific branding and can be used as a
portable engineering reference for similar services.

## System Shape

The system is a Go REST API organized as a modular monolith. The codebase uses
feature-oriented bounded contexts while keeping runtime composition centralized.

Top-level responsibilities are separated as follows:

- executable entrypoints live under `cmd/`
- application composition and route wiring live under `internal/app`
- business features live under `internal/feature/<feature>`
- shared HTTP helpers live under `internal/http`
- infrastructure adapters live under `internal/infrastructure`
- reusable libraries live under `pkg/`
- database migrations live under `migrations/`
- generated API documentation lives under `docs/`

Each feature package is split only where the responsibility is real:

- `handler`: HTTP decoding, request context, response writing
- `service`: business rules, validation, orchestration, transactions
- `repository`: persistence access and database error mapping
- `dto`: request and response shapes
- `entity`: domain objects used inside the feature
- `db`: generated query code
- `cache`: optional cache adapters when the feature owns caching concerns

## Runtime Composition

`internal/app` acts as the composition root. It owns dependency creation,
feature context initialization, cross-feature adapters, route registration, and
application lifetime.

The intended startup flow is:

1. Load configuration.
2. Initialize logger and infrastructure clients.
3. Open database and optional cache connections.
4. Initialize security, storage, mail, and external clients.
5. Build feature contexts through `Setup<Feature>Context` constructors.
6. Wire cross-feature dependencies through narrow ports.
7. Register routes and middleware.
8. Start the HTTP server and shut it down gracefully.

Feature contexts should expose only what other contexts need through a `Ports`
method. Internal repositories, services, and handlers remain private fields
unless there is a specific composition reason to expose them.

## Request Flow

The normal request path is:

```text
client
  -> router and middleware
  -> feature handler
  -> feature service
  -> repository or external port
  -> generated database query or infrastructure client
```

Handlers own transport concerns:

- decode JSON bodies and query parameters
- read authenticated identity from request context
- branch on client transport details when required
- call one service method per use case
- write success responses with shared HTTP helpers
- write errors through the shared error package

Services own business behavior:

- validate and normalize input
- enforce domain authorization rules
- coordinate repositories and optional dependencies
- manage transactions for multi-write operations
- emit domain events or invalidate caches after successful mutations
- convert lower-level failures to client-safe application errors

Repositories own persistence translation:

- call generated query interfaces
- map database rows to feature entities
- map database errors to application-level errors
- provide transaction-bound variants when services need atomic writes

## Dependency Boundaries

The codebase favors local interfaces at the consuming side.

Use this dependency direction:

- handlers depend on small service interfaces
- services depend on small repository or external-port interfaces
- repositories depend on generated database code
- feature packages avoid importing sibling feature implementations
- the app layer adapts one feature to another feature's port

When a feature needs data from another feature, define the smallest interface in
the consuming package, then implement the adapter in `internal/app`. This keeps
features independently testable and prevents import cycles.

## Optional Dependencies

Optional infrastructure should be explicit and safe when disabled.

Preferred patterns:

- provide a no-op implementation for optional caches and stores
- attach optional integrations with `With...` methods at wire-up time
- keep constructors focused on required dependencies
- gate optional read paths through configuration or rollout checks
- treat external recommendation, mail, or cache systems as accelerators, not as
  the only source of truth unless the feature contract requires it

## API Conventions

Successful responses return the resource or response DTO directly as JSON.
Error responses use one shared envelope:

```json
{
  "error": {
    "code": "bad_request",
    "message": "Invalid request body"
  }
}
```

Use typed application errors for expected failures:

- bad request
- unauthorized
- forbidden
- not found
- conflict
- internal error

Do not leak raw database, cache, mail, or external-service failures to clients.
Log those details server-side and return a safe error response.

Public API comments should live beside handlers. Regenerate API documentation
whenever handler comments or public contracts change.

## Pagination and Cursors

Use cursor pagination for user-facing timeline or feed endpoints. The feature
that emits a cursor owns the cursor format, validation, and backward-compatibility
policy.

Cursor guidance:

- keep client-facing cursors opaque
- validate malformed cursors before service execution
- include stable tie-breakers for ordered pagination
- reject obsolete cursor shapes intentionally when compatibility is not offered
- keep offset pagination limited to bounded admin or simple list endpoints where
  it is already part of the contract

## Persistence

PostgreSQL is the authoritative store. SQL belongs near the feature that owns
the data. Generated query packages are implementation details behind
repositories.

Persistence rules:

- do not hand-edit generated database code
- return domain entities or DTOs from repositories, not generated row types
- map database errors at the repository boundary
- use batch reads for enrichment paths to avoid N+1 queries
- keep migrations grouped by owning module or bounded context

## Caching and Derived Data

Cache layers are treated as derived state. They should improve latency without
becoming the only correctness path unless explicitly designed as such.

Use no-op cache implementations in disabled or test configurations. Invalidate
or refresh cache entries after successful mutations, not before the mutation is
known to have committed.

Prepared timelines, recommendation results, trending results, and search
enrichment should all be bounded in size and have clear fallback behavior.

## Error Handling and Logging

Use normal Go error returns. Wrap lower-level errors with context when returning
them across package boundaries, and convert them to application errors before
they reach HTTP responses.

Logging should use request-aware context so request IDs and structured fields
flow through the system. Log meaningful domain events and fallback paths rather
than only low-level failures.

Fire-and-forget work should be rare. When it is intentional, detach from request
cancellation only for post-request tasks and set a dedicated timeout.

## Testing Style

Tests live beside the code they cover and use the standard `testing` package.
The codebase favors focused tests over broad integration-heavy coverage for
ordinary business logic.

Recommended patterns:

- table-driven tests for validation, parsing, and cursor behavior
- small in-file mocks for service dependencies
- handler tests that exercise HTTP status, response shape, and auth behavior
- service tests for business rules, transactions, enrichment, fallback paths,
  and cache invalidation
- characterization tests before refactoring old behavior

Every logic change should include a test at the layer where the behavior is
owned.

## Coding Style

The preferred style is explicit, idiomatic Go:

- keep packages small and purpose-named
- pass `context.Context` as the first argument for request-scoped work
- accept narrow interfaces and return concrete types
- prefer early returns for validation and error paths
- use straightforward control flow over clever abstractions
- keep constructors named `New<Type>` or `Setup<Feature>Context`
- attach optional behavior through `With...` methods
- use structured parsers and encoders for structured data
- keep comments for exported contracts, package docs, and non-obvious decisions
- include `docs.go` for packages and update it when responsibilities change

Formatting and linting are part of the implementation standard:

- run `gofmt` and `goimports`
- respect the configured linter set
- handle errors explicitly
- avoid context-less I/O in production code
- keep generated files out of manual edits

## Extending the System

When adding a feature:

1. Create `internal/feature/<feature>` and add `docs.go`.
2. Add only the subpackages that have real responsibilities.
3. Define consumer-owned interfaces for dependencies from other contexts.
4. Implement repositories behind generated database queries if persistence is
   needed.
5. Add services for business behavior.
6. Add handlers for public HTTP use cases.
7. Add a feature context and setup function in `internal/app`.
8. Expose narrow ports only for capabilities other contexts need.
9. Register routes in a feature-specific route file.
10. Add focused service and handler tests.
11. Regenerate generated artifacts when SQL or API contracts change.

When changing an existing feature, preserve the existing package boundary first.
Introduce a new abstraction only when it removes real duplication, prevents an
import cycle, or matches a pattern already used in the codebase.

## Command Reference

Use the Makefile as the primary interface:

```sh
make build
make run
make test
make test-feature feature=<feature>
make test-cover
make lint
make docker-up
make generate
```

