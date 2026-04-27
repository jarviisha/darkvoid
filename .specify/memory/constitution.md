<!--
## Sync Impact Report
- Version change: (unpopulated template) → 1.0.0
- Added sections: Core Principles (I–IV), Architecture Constraints, Development Workflow, Governance
- Removed sections: N/A — first population of template placeholders
- Templates requiring updates:
  - ✅ .specify/templates/plan-template.md — Constitution Check gate placeholder is dynamic; aligns with principles
  - ✅ .specify/templates/spec-template.md — Performance Goals + Constraints fields align with Principle IV; no structural change required
  - ✅ .specify/templates/tasks-template.md — Foundation phase tasks (linting, logging, error handling) align with Principles I & II; no structural change required
  - ⚠ .specify/templates/commands/ — Directory does not exist; command file validation skipped
- Deferred TODOs: None
-->

# DarkVoid Constitution

## Core Principles

### I. Code Quality

All production code MUST pass `make lint` (golangci-lint) before commit. Linter failures MUST be
fixed at root cause. Targeted `//nolint:<linter> // <reason>` suppressions are permitted only on
the specific offending line — blanket suppressions and `.golangci.yml` relaxations are prohibited
unless the rule is genuinely wrong for this codebase (justify in the commit message).

- Logging MUST use `pkg/logger` (the project's `slog` wrapper). Raw `log`, `fmt.Print*`, or
  direct `log/slog` usage is forbidden in production code.
- Code generation artifacts under `internal/feature/*/db/` MUST NOT be hand-edited except where
  explicitly documented (e.g., hand-patched cursor queries in `post/db/post_queries.sql.go`).
  Regenerate via `make sqlc-generate`.
- Cross-context imports are forbidden. Features MUST communicate through narrow reader interfaces
  defined on the consumer side, implemented as adapters in `internal/app/`.
- Every package MUST have a `docs.go` stating its purpose; update it in the same commit whenever
  a package's responsibilities shift.
- No premature abstractions, feature flags, backwards-compat shims, or half-finished
  implementations. Three similar lines are preferable to a premature helper.

### II. Testing Standards

Every logic change in a service or handler package MUST be accompanied by a test. Tests are
the primary mechanism for verifying correctness — type checking and CI alone are insufficient.

- Test files are colocated as `*_test.go`; use the `testing` package only (no third-party
  test frameworks).
- Test names MUST follow `Test<Subject>_<Scenario>` (e.g., `TestLogin_Success`,
  `TestCreatePost_ValidationError`).
- Before refactoring legacy code, a characterization test MUST be written first.
- `make test` MUST pass (zero failures) before any commit. `make test-cover` MUST be reviewed
  when adding significant new logic.
- Tests MUST NOT mock the database. Integration tests MUST exercise real DB behavior; mock/prod
  divergence has caused production incidents.
- Service and handler packages MUST have tests for every logic change — not just happy paths.

### III. API & User Experience Consistency

All REST endpoints MUST present a uniform, predictable interface to clients. Inconsistent
response shapes, status codes, or pagination patterns are defects, not style choices.

- Response envelopes MUST be consistent: JSON with a stable top-level shape
  (`data` on success, `error` with a machine-readable code and human-readable message on failure).
- HTTP status codes MUST be semantically correct: 200 (success), 201 (creation), 400 (client
  error), 401 (unauthenticated), 403 (unauthorized), 404 (not found), 500 (unexpected server
  error). Using 200 for error responses is forbidden.
- Pagination MUST use DB cursor pagination (`(created_at, id)` row-value comparison). Offset
  pagination is prohibited for feed and list endpoints.
- Error responses MUST NOT expose stack traces, internal package paths, or raw DB errors to
  clients.
- All authenticated endpoints MUST validate and document their auth requirements in Swagger
  handler comments. The Swagger spec is the client contract; `make swagger-generate` MUST be run
  and its output committed before merging any handler change.
- SSE endpoints MUST NOT apply `RequestTimeout` middleware. All other REST endpoints MUST be
  wrapped by `RequestTimeout` (the routing group split in `app.registerRoutes` is load-bearing).

### IV. Performance Requirements

Performance targets are non-negotiable constraints, not aspirational benchmarks. Each target
applies at p95 under normal production load.

- Feed endpoint page 1 (following + trending merge): MUST complete ≤ 200ms p95.
- Feed endpoint page 2+ (pure following): MUST complete ≤ 100ms p95.
- Database queries on the posts table MUST use the composite partial index on `(created_at, id)`
  for cursor pagination. Full-table scans on the posts table are prohibited.
- Redis caching MUST be used for `following:ids:{userID}` (5m TTL) and `trending:posts`
  (15m TTL). Per-user feed caching is prohibited without documented capacity justification.
- The external CF recommender (Codohue) MUST be gated by `CODOHUE_ENABLED=true` and MUST NOT
  block the critical feed-serving path. When disabled, local scoring MUST apply.
- Feed scoring formula: `score = log(1+likes)×10 + RecencyScale/(1+hours)^decay + RelationshipBonus`
  with defaults `RelationshipBonus=10`, `RecencyScale=20`, `DecayExponent=1.5`. Changes to
  defaults MUST include a measured justification in the commit message.

## Architecture Constraints

The application follows a bounded-context model. Each context (User, Storage, Post, Feed,
Notification, Search, Admin) owns its schema, SQL queries, repository, service, and handlers.

- Cross-context dependencies MUST be wired via the deferred wiring pattern (`With...` methods
  called in `internal/app/`) — never at construction time, never via direct feature-to-feature
  imports.
- All configuration MUST be loaded from `.env` via `pkg/config`. Every new config variable MUST
  be added to `.env.example` in the same commit.
- The two Swagger UIs (`/swagger/app/` and `/swagger/admin/`) are produced at serve-time from a
  single `docs/swagger.json`. Two separate generated specs MUST NOT be maintained.
- The routing group split (Group A for SSE without timeout, Group B for REST with `RequestTimeout`)
  is load-bearing and MUST NOT be collapsed.

## Development Workflow

The `Makefile` is the canonical entry point. Raw CLI invocations are permitted only when no
`make` target exists for the operation.

- Migrations MUST use per-module schema tracking tables (`schema_migrations_<module>`) and MUST
  be created via `make migrate-create` and applied via `make migrate-up[-<module>]`.
- `make generate` (sqlc + swag) MUST be run and its output committed whenever SQL queries or
  handler Swagger comments change. Stale generated files MUST NOT be committed.
- `make lint` and `make test` MUST both pass locally before opening a PR.
- Seed data is managed via `cmd/seed`; the root user is auto-bootstrapped by `bootstrapRootUser`
  in `app.go` on first boot when `ROOT_EMAIL` + `ROOT_PASSWORD` are set. No manual seeding of
  the root user is permitted.

## Governance

This constitution supersedes all other project practices. When a practice conflicts with a
principle here, the practice MUST be updated — not the constitution — unless a formal amendment
is made.

**Amendment procedure**:
1. Open a PR with the proposed change to this file and the Sync Impact Report updated.
2. Bump `CONSTITUTION_VERSION` per semantic versioning: MAJOR for breaking principle removal
   or redefinition; MINOR for new principle or section; PATCH for clarification or wording fix.
3. Update `LAST_AMENDED_DATE` to the date of the merge commit.
4. Propagate changes to dependent templates (plan, spec, tasks) in the same PR.

**Compliance**: Every PR MUST pass the Constitution Check gate in `plan.md`. Code review MUST
verify adherence to Principles I–IV before approval. Repeated violations of a principle require
a root-cause note in the PR, not a constitution relaxation.

**Version**: 1.0.0 | **Ratified**: 2026-04-27 | **Last Amended**: 2026-04-27