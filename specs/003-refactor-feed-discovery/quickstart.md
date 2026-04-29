# Quickstart: Refactor Feed and Discovery Pagination

## Prerequisites

- Go 1.26.1 toolchain.
- Local PostgreSQL and Redis dependencies available via the existing project setup.
- Codohue service/container running at tag `v0.4.0` when validating rich recommendation behavior.
- Codohue Go SDK modules upgraded together to tag `v0.2.0`.
- Use `GOCACHE=/home/jarviisha/development/darkvoid/tmp/gocache` for local Go commands in this workspace when the default home cache is not writable.

## Dependency Update

Use the Makefile where possible. For module updates, the implementation task should update all three Codohue modules together:

```bash
go get github.com/jarviisha/codohue/pkg/codohuetypes@v0.2.0
go get github.com/jarviisha/codohue/sdk/go@v0.2.0
go get github.com/jarviisha/codohue/sdk/go/redistream@v0.2.0
go mod tidy
```

Expected module versions after setup:

- `github.com/jarviisha/codohue/pkg/codohuetypes v0.2.0`
- `github.com/jarviisha/codohue/sdk/go v0.2.0`
- `github.com/jarviisha/codohue/sdk/go/redistream v0.2.0`

## Characterization Tests Before Refactor

Add tests that capture current behavior before changing service logic:

```bash
go test ./internal/feature/feed/...
```

Required scenarios:

- Current page 1 mixed feed can produce duplicate/skip risk when ranked following items are not chronological.
- Current discovery cursor remains deterministic for timestamp ties.
- Current Codohue adapter expects ID-only recommendations and must change for SDK `v0.2.0`.

## Implementation Validation

After implementation:

```bash
GOCACHE=/home/jarviisha/development/darkvoid/tmp/gocache go test ./internal/feature/feed/... ./internal/feature/post/repository ./pkg/codohue
make test
make lint
```

If handler response fields or Swagger comments change:

```bash
make swagger-generate
```

If SQL changes:

```bash
make generate
```

Validation run on 2026-04-28:

- `GOCACHE=/home/jarviisha/development/darkvoid/tmp/gocache go test ./internal/feature/feed/... ./internal/feature/post/repository ./pkg/codohue` passed.
- `GOCACHE=/home/jarviisha/development/darkvoid/tmp/gocache go test ./...` passed.
- `GOCACHE=/home/jarviisha/development/darkvoid/tmp/gocache GOLANGCI_LINT_CACHE=/home/jarviisha/development/darkvoid/tmp/golangci-lint-cache make lint` passed with 0 issues.

## Manual API Checks

1. Start dependencies and API with Codohue disabled.
2. Request `/feed` with a user who follows authors and verify local fallback works.
3. Enable Codohue service/container `v0.4.0`.
4. Request `/feed`, follow `next_cursor` across at least five pages, and verify no duplicate post IDs.
5. Request `/discover` anonymously and authenticated, follow cursors, and verify identical page membership with only viewer flags differing.
6. Stop Codohue and verify `/feed` still returns valid local results.
