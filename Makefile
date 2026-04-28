SHELL := /bin/bash
.DEFAULT_GOAL := help

.PHONY: help \
	sqlc-generate sqlc-clean swagger-init swagger-generate swagger-serve generate \
	build run dev clean \
	test test-v test-cover test-cover-html test-feature lint deps \
	docker-up docker-up-app docker-up-codohue docker-seed docker-seed-reset docker-down docker-down-app docker-logs docker-logs-app \
	migrate-up migrate-down migrate-up-user migrate-up-post migrate-up-notification migrate-down-notification migrate-create migrate-status migrate-force \
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

MIGRATION_MODULES := user post notification
SQLC_DB_DIRS := internal/feature/user/db internal/feature/post/db internal/feature/notification/db

define require_var
	@if [ -z "$($1)" ]; then \
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
	$(MIGRATE) -path migrations/$(1) -database "$(DATABASE_URL)&x-migrations-table=schema_migrations_$(1)" $(2)
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
	$(DOCKER_COMPOSE) -f docker-compose.external.yml up -d

docker-up-codohue: ## Start Docker containers including Codohue CF recommender (requires CODOHUE_NAMESPACE_KEY)
	$(DOCKER_COMPOSE) --profile codohue up -d

docker-seed: ## Seed data inside Docker (usage: make docker-seed SEED_POSTS=500)
	$(DOCKER_COMPOSE) --profile tools run --rm seed

docker-seed-reset: ## Reset seeded data and seed again inside Docker
	$(DOCKER_COMPOSE) --profile tools run --rm seed --reset --posts=$${SEED_POSTS:-500} --likes-per-post=$${SEED_LIKES_PER_POST:-40} --comments-per-post=$${SEED_COMMENTS_PER_POST:-5}

docker-down: ## Stop Docker containers (all profiles)
	$(DOCKER_COMPOSE) --profile codohue down

docker-down-app: ## Stop the app-only Docker compose stack
	$(DOCKER_COMPOSE) -f docker-compose.external.yml down

docker-logs: ## View Docker container logs
	$(DOCKER_COMPOSE) logs -f

docker-logs-app: ## View app-only Docker container logs
	$(DOCKER_COMPOSE) -f docker-compose.external.yml logs -f

migrate-up: ## Run all pending migrations (user, post, notification)
	$(call require_var,DATABASE_URL,make migrate-up DATABASE_URL=postgres://...)
	$(call run_migrations,$(MIGRATION_MODULES),Running,up)

migrate-down: ## Roll back the last migration for all modules (notification, post, user)
	$(call require_var,DATABASE_URL,make migrate-down DATABASE_URL=postgres://...)
	$(call run_migrations,notification post user,Rolling back,down 1)

migrate-up-user: ## Run pending migrations for user module only
	$(call require_var,DATABASE_URL,make migrate-up-user DATABASE_URL=postgres://...)
	$(call migrate_cmd,user,up)

migrate-up-post: ## Run pending migrations for post module only
	$(call require_var,DATABASE_URL,make migrate-up-post DATABASE_URL=postgres://...)
	$(call migrate_cmd,post,up)

migrate-up-notification: ## Run pending migrations for notification module only
	$(call require_var,DATABASE_URL,make migrate-up-notification DATABASE_URL=postgres://...)
	$(call migrate_cmd,notification,up)

migrate-down-notification: ## Roll back the last migration for notification module
	$(call require_var,DATABASE_URL,make migrate-down-notification DATABASE_URL=postgres://...)
	$(call migrate_cmd,notification,down 1)

migrate-create: ## Create a new migration (usage: make migrate-create module=post name=add_example_field)
	$(call require_var,module,make migrate-create module=post name=add_example_field)
	$(call require_var,name,make migrate-create module=post name=add_example_field)
	$(call require_module,$(module))
	@echo "Creating migration '$(name)' in module '$(module)'..."
	$(MIGRATE) create -ext sql -dir migrations/$(module) -seq $(name)

migrate-status: ## Show current migration status for all modules
	$(call require_var,DATABASE_URL,make migrate-status DATABASE_URL=postgres://...)
	@set -e; \
	for module in $(MIGRATION_MODULES); do \
		echo "=== $$module migration status ==="; \
		$(call migrate_cmd,$$module,version) || true; \
	done

migrate-force: ## Force migration to a specific version (usage: make migrate-force module=user version=1)
	$(call require_var,module,make migrate-force module=user version=1)
	$(call require_var,version,make migrate-force module=user version=1)
	$(call require_var,DATABASE_URL,make migrate-force module=user version=1 DATABASE_URL=postgres://...)
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
