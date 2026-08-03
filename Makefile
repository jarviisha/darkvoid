SHELL := /bin/bash
.DEFAULT_GOAL := help

.PHONY: help \
	sqlc-generate sqlc-clean swagger-init swagger-generate swagger-serve generate \
	build run dev bot ctl clean \
	test test-v test-cover test-cover-html test-feature lint deps \
	docker-up docker-up-app docker-up-codohue docker-up-bot docker-seed docker-seed-reset \
	docker-down docker-down-app docker-down-bot docker-logs docker-logs-app docker-logs-bot \
	migrate-up migrate-down migrate-up-user migrate-up-post migrate-up-notification migrate-up-bot migrate-up-settings migrate-down-notification migrate-create migrate-status migrate-force \
	db-reset install-tools

# Load .env if it exists.
-include .env
export

GO ?= go
DOCKER_COMPOSE ?= docker compose

BIN_DIR := bin
APP_BIN := $(BIN_DIR)/api
COVERAGE_FILE := coverage.out
MIGRATE := $(shell $(GO) env GOPATH)/bin/migrate

# Connection settings for the migrate CLI.
#
# golang-migrate takes a single -database URL where the app takes discrete DB_*
# fields, which is the only reason a DATABASE_URL ever existed here. It was a
# second copy of the same connection details, hand-kept in sync: point it at the
# wrong database and `make migrate-up` migrates that one and still exits 0, so
# the app runs on an unmigrated schema with nothing reporting a problem.
#
# migrate's postgres driver is lib/pq, which reads any field missing from the URL
# out of PG*. So DB_* stays the single source of truth, and three things follow:
# the password leaves argv (it was visible to `ps` on every migrate run — the
# same reason docker-compose passes PGPASSWORD out of band), a password holding
# URL-reserved characters needs no encoding, and x-migrations-table becomes the
# only query parameter, so the DSN no longer has to already contain a '?'.
#
# Assigned with := rather than ?= deliberately: a PGDATABASE left over in the
# shell from a psql session must not silently redirect a migration.
DB_HOST    ?= localhost
DB_PORT    ?= 5432
DB_USER    ?= postgres
DB_NAME    ?= darkvoid
DB_SSLMODE ?= disable

export PGHOST     := $(DB_HOST)
export PGPORT     := $(DB_PORT)
export PGUSER     := $(DB_USER)
export PGPASSWORD := $(DB_PASSWORD)
export PGDATABASE := $(DB_NAME)
export PGSSLMODE  := $(DB_SSLMODE)

# migrate-up walks MIGRATION_MODULES forward and migrate-down walks the reversed
# list, so the two must stay mirror images — adding a module means editing both.
MIGRATION_MODULES          := user post notification bot settings
MIGRATION_MODULES_REVERSED := settings bot notification post user
SQLC_DB_DIRS := internal/feature/user/db internal/feature/post/db internal/feature/notification/db internal/feature/bot/db internal/feature/settings/db

# Tests the variable through the recipe's environment ($${NAME}) rather than
# expanding its value into the shell command ($(NAME)). Same result for the
# plain arguments this guards, but DB_PASSWORD would otherwise be pasted
# verbatim into a `[ -z "..." ]` that is visible in `ps` while it runs. Relies
# on the blanket `export` above, which puts every variable in the environment.
define require_var
	@if [ -z "$${$(1)}" ]; then \
		echo "Error: $(1) is required.$(if $2, Usage: $2)"; \
		exit 1; \
	fi
endef

define require_module
	@if [[ ! " $(MIGRATION_MODULES) " =~ " $(1) " ]]; then \
		echo "Error: invalid module '$(1)'. Expected one of: $(MIGRATION_MODULES)"; \
		exit 1; \
	fi
endef

define migrate_cmd
	$(MIGRATE) -path migrations/$(1) -database "postgres:///?x-migrations-table=schema_migrations_$(1)" $(2)
endef

define run_migrations
	@set -e; \
	for module in $(1); do \
		echo "$(2) $$module migrations..."; \
		$(call migrate_cmd,$$module,$(3)); \
	done
endef

help: ## Show this help message
	@echo "Usage: make [target]"
	@echo ""
	@echo "Available targets:"
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z0-9_.-]+:.*?## / {printf "  %-24s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

sqlc-generate: ## Generate SQLC code from SQL files
	sqlc generate

sqlc-clean: ## Clean generated SQLC code
	rm -f $(addsuffix /*.go,$(SQLC_DB_DIRS))

swagger-init: ## Initialize Swagger in the project (run once)
	swag init -g cmd/api/main.go -o docs --parseInternal

swagger-generate: ## Generate/update Swagger documentation
	swag fmt
	swag init -g cmd/api/main.go -o docs --parseInternal

swagger-serve: ## Print the local Swagger UI URL
	@echo "Swagger docs generated at: docs/swagger.json"
	@echo "View at: http://localhost:8080/swagger/app/index.html"

generate: sqlc-generate swagger-generate ## Generate all derived code

build: ## Build the application
	$(GO) build -o $(APP_BIN) ./cmd/api

run: ## Run the application
	$(GO) run ./cmd/api

dev: ## Run in development mode with hot reload (requires air)
	air

bot: ## Run the Gemini content bot against a running API (usage: make bot BOT_ARGS="--max-posts=1")
	$(GO) run ./cmd/bot $(BOT_ARGS)

ctl: ## Run the operator CLI (usage: make ctl CTL_ARGS="user list")
	$(GO) run ./cmd/darkvoidctl $(CTL_ARGS)

clean: ## Clean build artifacts
	rm -rf $(BIN_DIR) $(COVERAGE_FILE)

test: ## Run all tests
	$(GO) test ./...

test-v: ## Run all tests with verbose output
	$(GO) test -v ./...

test-cover: ## Run all tests with coverage report
	$(GO) test ./... -coverprofile=$(COVERAGE_FILE)
	$(GO) tool cover -func=$(COVERAGE_FILE)
	@rm -f $(COVERAGE_FILE)

test-cover-html: ## Run all tests and open HTML coverage report
	$(GO) test ./... -coverprofile=$(COVERAGE_FILE)
	$(GO) tool cover -html=$(COVERAGE_FILE)
	@rm -f $(COVERAGE_FILE)

test-feature: ## Run tests for a specific feature (usage: make test-feature feature=user)
	$(call require_var,feature,make test-feature feature=user)
	$(GO) test -v ./internal/feature/$(feature)/...

lint: ## Run golangci-lint
	golangci-lint run

deps: ## Download and tidy Go dependencies
	$(GO) mod download
	$(GO) mod tidy

docker-up: ## Start Docker containers (PostgreSQL, Redis, app)
	$(DOCKER_COMPOSE) up -d

docker-up-app: ## Start only the app container and connect to external/local infra
	$(DOCKER_COMPOSE) up -d app-external

docker-up-codohue: ## Start Docker containers including Codohue CF recommender (requires CODOHUE_NAMESPACE_KEY)
	$(DOCKER_COMPOSE) --profile codohue up -d

docker-up-bot: ## Start the content bot container (needs GEMINI_API_KEY + BOT_RUNNER_PASSWORD + BOT_PASSWORD in .env)
	$(DOCKER_COMPOSE) --profile bot up -d bot

docker-down-bot: ## Stop the content bot container
	$(DOCKER_COMPOSE) --profile bot down bot

docker-logs-bot: ## View content bot logs
	$(DOCKER_COMPOSE) --profile bot logs -f bot

docker-seed: ## Seed data inside Docker (usage: make docker-seed SEED_POSTS=500)
	$(DOCKER_COMPOSE) --profile tools run --rm seed

docker-seed-reset: ## Reset seeded data and seed again inside Docker
	$(DOCKER_COMPOSE) --profile tools run --rm seed --reset --posts=$${SEED_POSTS:-500} --likes-per-post=$${SEED_LIKES_PER_POST:-40} --comments-per-post=$${SEED_COMMENTS_PER_POST:-5}

docker-down: ## Stop Docker containers (all profiles)
	$(DOCKER_COMPOSE) --profile codohue --profile external --profile bot down

docker-down-app: ## Stop the app-only container
	$(DOCKER_COMPOSE) --profile external down app-external

docker-logs: ## View Docker container logs
	$(DOCKER_COMPOSE) logs -f

docker-logs-app: ## View app-only Docker container logs
	$(DOCKER_COMPOSE) logs -f app-external

migrate-up: ## Run all pending migrations (user, post, notification, bot, settings)
	$(call require_var,DB_PASSWORD,set DB_* in .env or: make migrate-up DB_PASSWORD=secret)
	$(call run_migrations,$(MIGRATION_MODULES),Running,up)

migrate-down: ## Roll back the last migration for all modules (settings, bot, notification, post, user)
	$(call require_var,DB_PASSWORD,set DB_* in .env or: make migrate-down DB_PASSWORD=secret)
	$(call run_migrations,$(MIGRATION_MODULES_REVERSED),Rolling back,down 1)

migrate-up-user: ## Run pending migrations for user module only
	$(call require_var,DB_PASSWORD,set DB_* in .env or: make migrate-up-user DB_PASSWORD=secret)
	$(call migrate_cmd,user,up)

migrate-up-post: ## Run pending migrations for post module only
	$(call require_var,DB_PASSWORD,set DB_* in .env or: make migrate-up-post DB_PASSWORD=secret)
	$(call migrate_cmd,post,up)

migrate-up-notification: ## Run pending migrations for notification module only
	$(call require_var,DB_PASSWORD,set DB_* in .env or: make migrate-up-notification DB_PASSWORD=secret)
	$(call migrate_cmd,notification,up)

migrate-up-bot: ## Run pending migrations for bot module only
	$(call require_var,DB_PASSWORD,set DB_* in .env or: make migrate-up-bot DB_PASSWORD=secret)
	$(call migrate_cmd,bot,up)

migrate-up-settings: ## Run pending migrations for settings module only
	$(call require_var,DB_PASSWORD,set DB_* in .env or: make migrate-up-settings DB_PASSWORD=secret)
	$(call migrate_cmd,settings,up)

migrate-down-notification: ## Roll back the last migration for notification module
	$(call require_var,DB_PASSWORD,set DB_* in .env or: make migrate-down-notification DB_PASSWORD=secret)
	$(call migrate_cmd,notification,down 1)

migrate-create: ## Create a new migration (usage: make migrate-create module=post name=add_example_field)
	$(call require_var,module,make migrate-create module=post name=add_example_field)
	$(call require_var,name,make migrate-create module=post name=add_example_field)
	$(call require_module,$(module))
	@echo "Creating migration '$(name)' in module '$(module)'..."
	$(MIGRATE) create -ext sql -dir migrations/$(module) -seq $(name)

migrate-status: ## Show current migration status for all modules
	$(call require_var,DB_PASSWORD,set DB_* in .env or: make migrate-status DB_PASSWORD=secret)
	@set -e; \
	for module in $(MIGRATION_MODULES); do \
		echo "=== $$module migration status ==="; \
		$(call migrate_cmd,$$module,version) || true; \
	done

migrate-force: ## Force migration to a specific version (usage: make migrate-force module=user version=1)
	$(call require_var,module,make migrate-force module=user version=1)
	$(call require_var,version,make migrate-force module=user version=1)
	$(call require_var,DB_PASSWORD,set DB_* in .env or: make migrate-force module=user version=1 DB_PASSWORD=secret)
	$(call require_module,$(module))
	@echo "WARNING: forcing $(module) migrations to version $(version)"
	$(call migrate_cmd,$(module),force $(version))

db-reset: ## Reset dockerized database volumes after confirmation
	@echo "WARNING: This will delete Docker volumes and all local data."
	@read -r -p "Are you sure? [y/N] " reply; \
	if [[ "$$reply" =~ ^[Yy]$$ ]]; then \
		$(DOCKER_COMPOSE) --profile codohue down -v; \
		$(DOCKER_COMPOSE) up -d; \
	else \
		echo "Aborted."; \
	fi

install-tools: ## Install development tools
	@echo "Installing development tools..."
	$(GO) install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
	$(GO) install github.com/swaggo/swag/cmd/swag@latest
	$(GO) install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	$(GO) install github.com/air-verse/air@latest
	$(GO) install -tags postgres github.com/golang-migrate/migrate/v4/cmd/migrate@latest
	@echo "Done."
