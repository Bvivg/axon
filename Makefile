# Axon — single entry point for every routine task.
#
# Everything that needs a dependency runs in Docker. The host is only expected
# to provide Go for formatting and pure unit tests.

SHELL := /bin/bash

COMPOSE       := docker compose -f core/deploy/docker-compose.yml
COMPOSE_TOOLS := docker compose -f core/deploy/docker-compose.tools.yml
# Every module in the workspace, listed explicitly. A pattern has to start at a
# module root in workspace mode, so ./services/... does not work — each service
# is added here as it appears.
GO_MODULES    := ./shared/... ./services/auth/...

# Migration targets need the per-service DSN, which lives in .env. A missing
# file is not an error here: every other target works without it, and `make env`
# is what creates it.
-include core/deploy/.env

# Which service's migrations to act on: `make migrate-up S=auth`. The DSN is
# derived from the name, so a new service needs no change here — only its own
# <NAME>_POSTGRES_DSN entry in .env.
S ?= auth
MIGRATE_SERVICE := $(S)
MIGRATE_DSN := $($(shell echo $(S) | tr '[:lower:]' '[:upper:]')_POSTGRES_DSN)
export MIGRATE_SERVICE

.DEFAULT_GOAL := help

# ---------------------------------------------------------------------------
# Help
# ---------------------------------------------------------------------------

.PHONY: help
help: ## List available targets
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

# ---------------------------------------------------------------------------
# Environment
# ---------------------------------------------------------------------------

.PHONY: env
env: ## Create core/deploy/.env from the example (does not overwrite)
	@if [ -f core/deploy/.env ]; then \
		echo "core/deploy/.env already exists, leaving it alone"; \
	else \
		cp core/deploy/.env.example core/deploy/.env; \
		echo "created core/deploy/.env — fill in the secrets before running services"; \
	fi

# ---------------------------------------------------------------------------
# Stack
# ---------------------------------------------------------------------------

.PHONY: up
up: ## Start the stack and wait until dependencies report healthy
	$(COMPOSE) up -d --wait

.PHONY: down
down: ## Stop the stack, keeping data
	$(COMPOSE) down

.PHONY: reset
reset: ## Stop the stack and delete its volumes
	$(COMPOSE) down -v

.PHONY: ps
ps: ## Show stack status
	$(COMPOSE) ps

.PHONY: logs
logs: ## Follow stack logs
	$(COMPOSE) logs -f

# ---------------------------------------------------------------------------
# Migrations
# ---------------------------------------------------------------------------

# Guard so a missing DSN fails with something readable instead of migrate's
# "error parsing url".
.PHONY: migrate-guard
migrate-guard:
	@if [ -z "$(MIGRATE_DSN)" ]; then \
		echo "no DSN for service '$(S)'."; \
		echo "expected $$(echo $(S) | tr '[:lower:]' '[:upper:]')_POSTGRES_DSN in core/deploy/.env — run 'make env' first"; \
		exit 1; \
	fi

.PHONY: migrate-up
migrate-up: migrate-guard ## Apply pending migrations (make migrate-up S=auth)
	$(COMPOSE) run --rm migrate -path=/migrations -database "$(MIGRATE_DSN)" up

.PHONY: migrate-down
migrate-down: migrate-guard ## Roll back the last migration (make migrate-down S=auth)
	$(COMPOSE) run --rm migrate -path=/migrations -database "$(MIGRATE_DSN)" down 1

.PHONY: migrate-status
migrate-status: migrate-guard ## Show the applied migration version (make migrate-status S=auth)
	$(COMPOSE) run --rm migrate -path=/migrations -database "$(MIGRATE_DSN)" version

.PHONY: migrate-new
migrate-new: ## Create an empty migration pair (make migrate-new S=auth NAME=add_sessions)
	@if [ -z "$(NAME)" ]; then echo "NAME is required: make migrate-new S=$(S) NAME=add_sessions"; exit 1; fi
	$(COMPOSE) run --rm migrate -dir=/migrations create -ext sql -seq $(NAME)

# ---------------------------------------------------------------------------
# Code generation
# ---------------------------------------------------------------------------

.PHONY: proto
proto: ## Generate Go and TypeScript code from the contracts
	$(COMPOSE_TOOLS) run --rm buf generate

.PHONY: proto-lint
proto-lint: ## Lint the contracts
	$(COMPOSE_TOOLS) run --rm buf lint

.PHONY: proto-breaking
proto-breaking: ## Check the contracts for breaking changes against main
	$(COMPOSE_TOOLS) run --rm buf breaking --against '.git#branch=main'

# ---------------------------------------------------------------------------
# Quality
# ---------------------------------------------------------------------------

.PHONY: fmt
fmt: ## Format Go code
	cd core && gofmt -w -l .

.PHONY: fmt-check
fmt-check: ## Fail if any Go file is not gofmt-clean
	@cd core && out=$$(gofmt -l .); \
	if [ -n "$$out" ]; then \
		echo "not gofmt-clean:"; echo "$$out"; exit 1; \
	fi

.PHONY: vet
vet: ## Run go vet over the workspace
	cd core && go vet $(GO_MODULES)

.PHONY: lint
lint: ## Run golangci-lint in a container
	$(COMPOSE_TOOLS) run --rm golangci-lint golangci-lint run $(GO_MODULES)

.PHONY: test
test: ## Run unit tests (no external dependencies)
	cd core && go test -race $(GO_MODULES)

.PHONY: test-cover
test-cover: ## Run unit tests with a coverage summary
	cd core && go test -race -coverprofile=coverage.out $(GO_MODULES) && go tool cover -func=coverage.out | tail -1

.PHONY: tidy
tidy: ## Tidy every module in the workspace
	cd core/shared && go mod tidy

.PHONY: check
check: fmt-check vet test ## Everything CI runs that needs no Docker
