# Repository Guidelines

## Project Structure & Module Organization
`cmd/api` and `cmd/seed` contain executable entrypoints. Core application wiring lives in `internal/app`. Business logic is organized by feature under `internal/feature/<feature>` with subpackages such as `handler`, `service`, `repository`, `dto`, and `entity`. Shared infrastructure is in `internal/http`, `internal/infrastructure`, `internal/validation`, and `pkg/*`. SQL migrations live in `migrations/*`; generated Swagger files are in `docs/`.

Keep package boundaries explicit. When adding or changing a package, include a `docs.go` file that states the package purpose. If a package’s responsibilities or behavior change, update its `docs.go` in the same change.

## Build, Test, and Development Commands
Use the `Makefile` as the primary interface:

- `make build`: build the API binary into `bin/api`
- `make run`: run `./cmd/api`
- `make test`: run all Go tests with `go test ./...`
- `make test-feature feature=user`: run one feature’s tests
- `make test-cover`: generate terminal coverage output
- `make lint`: run `golangci-lint`
- `make docker-up`: start local dependencies with Docker Compose

## Coding Style & Naming Conventions
This is a Go 1.26 project. Follow standard Go formatting with tabs and run `gofmt`/`goimports`; linting is configured in `.golangci.yml`. Prefer small packages with clear single-purpose responsibilities. Use lowercase package names, `CamelCase` for exported identifiers, and feature-local naming such as `AuthHandler`, `UserService`, `RefreshTokenRepository`.

Treat `.golangci.yml` as an enforced constraint for all changes. Do not introduce new lint violations, and make code changes in a way that respects the configured linters even when a task does not explicitly mention linting.

Do not hand-edit generated files under `internal/feature/*/db`.

## Testing Guidelines
Tests sit next to the code they cover and use the standard `testing` package. Name files `*_test.go` and tests like `TestLogin_Success` or `TestCreatePost_ValidationError`. Add or update tests for every logic change, especially in `service` and `handler` packages. Before refactoring old code, write a characterization test first.

## Commit & Pull Request Guidelines
Recent history is inconsistent, so prefer a clear convention going forward: short imperative subjects, ideally with a type prefix, for example `fix: handle refresh token cookie` or `test: cover post mention parsing`.

PRs should describe the behavior change, note config or migration impact, list test coverage, and include example requests/responses when API behavior changes.

## Security & Configuration Tips
Configuration is loaded from `.env`; keep secrets out of git and update `.env.example` when required variables change. For auth, cookie, storage, mail, or database changes, document any operational impact in the PR.
