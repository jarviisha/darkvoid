# Quickstart: Refactor Feed and Discovery Pagination

## Prerequisites

- Go 1.26.1 toolchain.
- Local PostgreSQL and Redis dependencies available via the existing project setup.
- Codohue service/container running at tag `v0.4.0` when validating rich recommendation behavior.
- Codohue Go SDK modules upgraded together to tag `v0.2.0`.

## Dependency Update

Use the Makefile where possible. For module updates, the implementation task should update all three Codohue modules together:

```bash
go get github.com/jarviisha/codohue/pkg/codohuetypes@v0.2.0
go get github.com/jarviisha/codohue/sdk/go@v0.2.0
go get github.com/jarviisha/codohue/sdk/go/redistream@v0.2.0
go mod tidy
```

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
go test ./internal/feature/feed/... ./internal/feature/post/repository ./pkg/codohue
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

## Manual API Checks

1. Start dependencies and API with Codohue disabled.
2. Request `/feed` with a user who follows authors and verify local fallback works.
3. Enable Codohue service/container `v0.4.0`.
4. Request `/feed`, follow `next_cursor` across at least five pages, and verify no duplicate post IDs.
5. Request `/discover` anonymously and authenticated, follow cursors, and verify identical page membership with only viewer flags differing.
6. Stop Codohue and verify `/feed` still returns valid local results.
