# Implementation Plan: Refactor Feed and Discovery Pagination

**Branch**: `003-refactor-feed-discovery` | **Date**: 2026-04-28 | **Spec**: [spec.md](./spec.md)
**Input**: Feature specification from `specs/003-refactor-feed-discovery/spec.md`

**Note**: This template is filled in by the `/speckit.plan` command. See `.specify/templates/plan-template.md` for the execution workflow.

## Summary

Refactor `/feed` so mixed following, recommendation, trending, and discovery fallback pages advance from a stable opaque cursor instead of using the last shown following post as the only continuation point. Upgrade all three Codohue Go SDK modules to tag `v0.2.0` to match Codohue service/container `v0.4.0`, consume recommendation items with `object_id`, `score`, `rank`, `limit`, `offset`, and `total`, and preserve `/discover` as chronological DB-cursor pagination.

The technical approach is to introduce a versioned feed cursor/session model that tracks active source cursors, Codohue recommendation offset, bounded seen IDs, and pending candidates fetched but not yet shown. This prevents duplicate and skipped posts when ranking mixes multiple sources. The public API keeps a single opaque `next_cursor`; Codohue offset is internal provider state, not client-facing list pagination.

## Technical Context

**Language/Version**: Go 1.26.1  
**Primary Dependencies**: chi HTTP routing, pgx/PostgreSQL, Redis, Codohue Go SDK modules `github.com/jarviisha/codohue/pkg/codohuetypes`, `github.com/jarviisha/codohue/sdk/go`, `github.com/jarviisha/codohue/sdk/go/redistream` upgraded to tag `v0.2.0`, local `pkg/logger`, local `pkg/errors`  
**Storage**: PostgreSQL for posts/follows/likes; Redis for existing following/trending caches and short-lived feed continuation state when enabled  
**Testing**: Standard Go `testing` package; `make test`, targeted `go test ./internal/feature/feed/...`, and integration coverage for real DB cursor behavior where repository SQL changes are involved  
**Target Platform**: Linux web-service API  
**Project Type**: Go REST API service with bounded feature contexts  
**Performance Goals**: Feed page 1 <= 200ms p95, feed page 2+ <= 100ms p95, Codohue unavailable path still returns local results without user-visible request failure  
**Constraints**: DB list pagination must use `(created_at, id)` cursor comparisons; Codohue must be gated by `CODOHUE_ENABLED`; no hand-edits under `internal/feature/*/db`; generated Swagger must be refreshed if handler contracts change; raw production logging is forbidden  
**Scale/Scope**: Existing `/feed` and `/discover` behavior only; no frontend changes; no unrelated recommendation-event or embedding refactor beyond SDK compatibility required for the new response shapes

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

- **Code Quality**: PASS. Changes stay within feed, app adapters, Codohue adapter, config/docs, generated artifacts, and tests. New or changed packages must include/update `docs.go`.
- **Testing Standards**: PASS. Plan requires characterization tests before refactor, service tests for cursor/session/ranking behavior, handler tests for cursor validation/response shape, and repository/integration tests for DB cursor changes.
- **API & User Experience Consistency**: PASS WITH NOTE. `/feed` and `/discover` keep `data` plus optional `next_cursor`. Cursor internals may change because cursors are opaque. Any response-field additions require Swagger regeneration.
- **Pagination Rule**: PASS. Darkvoid list endpoints keep opaque cursor and DB `(created_at, id)` pagination for local post lists. Codohue recommendation `offset` is internal provider state stored inside Darkvoid's opaque cursor/session, not client-facing offset pagination.
- **Performance Requirements**: PASS WITH CAPACITY JUSTIFICATION. Short-lived feed continuation state is allowed only as bounded cursor/session state, not a reusable per-user feed cache. State must have TTL, max pending IDs, max seen IDs, and no cached fully-rendered feed pages.
- **Architecture Constraints**: PASS. Feed continues to depend on Post/User/Like through feed-owned reader interfaces and app-layer adapters. Codohue remains behind feed-owned recommender/trending interfaces.

**Post-Design Recheck**: PASS. `research.md`, `data-model.md`, `contracts/`, and `quickstart.md` preserve the same constraints: endpoint pagination stays cursor-based, Codohue offset remains internal provider state, session state is bounded continuation state, and tests/generation requirements are explicit.

## Project Structure

### Documentation (this feature)

```text
specs/003-refactor-feed-discovery/
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/
│   ├── feed.md
│   ├── discover.md
│   └── codohue-provider.md
└── tasks.md
```

### Source Code (repository root)

```text
go.mod
go.sum
pkg/codohue/
├── client.go
└── docs.go
internal/feature/feed/
├── cursor.go
├── recommender.go
├── post_reader.go
├── docs.go
├── entity/
├── cache/
├── handler/
│   ├── feed_handler.go
│   └── feed_handler_test.go
└── service/
    ├── feed_service.go
    ├── feed_service_test.go
    └── docs.go
internal/app/
├── feed.go
└── feed_adapters.go
internal/feature/post/
├── sql/post_queries.sql
└── repository/post_repository.go
docs/
├── docs.go
├── swagger.json
└── swagger.yaml
```

**Structure Decision**: Use the existing bounded-context layout. Feed owns cursor/session, merge, ranking, and provider interfaces. App adapters remain the only layer that translates Post/User data into Feed data. Codohue SDK details remain isolated in `pkg/codohue`.

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

No constitution violations require justification.
