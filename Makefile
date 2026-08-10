# Axon — single entry point for every routine task.
#
# Everything that needs a dependency runs in Docker. The host is only expected
# to provide Go for formatting and pure unit tests.

SHELL := /bin/bash

COMPOSE       := docker compose -f core/deploy/docker-compose.yml
COMPOSE_TOOLS := docker compose -f core/deploy/docker-compose.tools.yml
COMPOSE_E2E   := docker compose -f core/deploy/docker-compose.e2e.yml
# Every module in the workspace, listed explicitly. A pattern has to start at a
# module root in workspace mode, so ./services/... does not work — each service
# is added here as it appears.
GO_MODULES    := ./shared/... ./services/auth/... ./services/gateway/... ./services/game/...

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
	@grep -hE '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) \
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
# Secrets
# ---------------------------------------------------------------------------

ENV_FILE := core/deploy/.env

.PHONY: jwt-key
jwt-key: env ## Generate a development RS256 signing key into core/deploy/.env
	@if grep -qE '^JWT_PRIVATE_KEY_DEV_1=.+' $(ENV_FILE); then \
		echo "JWT_PRIVATE_KEY_DEV_1 is already set in $(ENV_FILE); delete the line to regenerate"; \
		exit 0; \
	fi; \
	echo "generating a 2048-bit RSA key in a container..."; \
	key=$$(docker run --rm alpine/openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 2>/dev/null | base64 | tr -d '\n'); \
	if [ -z "$$key" ]; then echo "key generation failed"; exit 1; fi; \
	tmp=$$(mktemp); \
	grep -v '^JWT_PRIVATE_KEY_DEV_1=' $(ENV_FILE) > "$$tmp"; \
	echo "JWT_PRIVATE_KEY_DEV_1=$$key" >> "$$tmp"; \
	mv "$$tmp" $(ENV_FILE); \
	echo "wrote JWT_PRIVATE_KEY_DEV_1 to $(ENV_FILE) (git-ignored)"

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

# Integration and e2e sit behind build tags, so `make test` above never compiles
# them and stays runnable on a machine with nothing but Go.

.PHONY: test-integration
test-integration: ## Run integration tests (testcontainers spins up Postgres)
	cd core && go test -tags integration -count=1 ./services/auth/integration/...

E2E_ENV_FILE := core/deploy/.env.e2e

# `run` rather than `up --abort-on-container-exit`: the latter tears the stack
# down the moment any container exits, and the migration container is supposed
# to exit. `run` starts the dependency chain, waits for each depends_on
# condition, and returns the runner's own exit code — which is what CI needs.
#
# The teardown is a trap so a failing run, an interrupt, or a crash all leave the
# machine clean.
E2E_LOG_FILE := e2e-logs.txt

.PHONY: test-e2e
test-e2e: e2e-key ## Run e2e against the full stack on the shipping images
	@trap '$(COMPOSE_E2E) --env-file $(E2E_ENV_FILE) logs --no-color > $(E2E_LOG_FILE) 2>&1; \
	       $(COMPOSE_E2E) --env-file $(E2E_ENV_FILE) down -v --remove-orphans >/dev/null 2>&1' EXIT; \
	$(COMPOSE_E2E) --env-file $(E2E_ENV_FILE) run --rm --build e2e

# A throwaway signing key per machine, never committed. Regenerating it costs
# nothing: the stack is rebuilt from empty on every run, so no token outlives it.
.PHONY: e2e-key
e2e-key:
	@if [ -s $(E2E_ENV_FILE) ] && grep -qE '^JWT_PRIVATE_KEY_DEV_1=.+' $(E2E_ENV_FILE); then exit 0; fi; \
	echo "generating a throwaway e2e signing key..."; \
	key=$$(docker run --rm alpine/openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 2>/dev/null | base64 | tr -d '\n'); \
	if [ -z "$$key" ]; then echo "key generation failed"; exit 1; fi; \
	printf 'JWT_PRIVATE_KEY_DEV_1=%s\n' "$$key" > $(E2E_ENV_FILE); \
	echo "wrote $(E2E_ENV_FILE) (git-ignored)"

# ---------------------------------------------------------------------------
# Web client
# ---------------------------------------------------------------------------

# All of these run in a container: npm is not expected on the host. node_modules
# lands in ui/web on the host through the bind mount, which is what the editor
# reads for type information — the one place the host does see it.

.PHONY: web-install
web-install: ## Install the web client's dependencies (in a container)
	$(COMPOSE_TOOLS) run --rm node npm ci

.PHONY: web-lint
web-lint: ## Lint the web client
	$(COMPOSE_TOOLS) run --rm node npm run lint

.PHONY: web-typecheck
web-typecheck: ## Type-check the web client
	$(COMPOSE_TOOLS) run --rm node npm run typecheck

.PHONY: web-build
web-build: ## Build the web client's production bundle
	$(COMPOSE_TOOLS) run --rm node npm run build

.PHONY: web-check
web-check: web-lint web-typecheck web-build ## Everything CI runs for the web client

.PHONY: tidy
tidy: ## Tidy every module in the workspace
	@# GOWORK=off deliberately: the service images build each module on its own,
	@# so every go.sum has to be complete by itself. Tidying inside the workspace
	@# lets go.work.sum cover the gaps, and the omission only surfaces as a
	@# failed image build — which is exactly how it surfaced the first time.
	@for module in shared services/auth services/gateway services/game; do \
		echo "tidy $$module"; \
		(cd core/$$module && GOWORK=off go mod tidy) || exit 1; \
	done

.PHONY: check
check: fmt-check vet test ## Everything CI runs that needs no Docker

# ---------------------------------------------------------------------------
# Browser end-to-end (Playwright)
# ---------------------------------------------------------------------------

COMPOSE_WEB_E2E := docker compose -f core/deploy/docker-compose.web-e2e.yml

# Its own stack rather than the Go suite's: the browser needs a different
# origin, a different CORS allow-list and a refresh cookie without Secure, and
# threading that through docker-compose.e2e.yml would put a working suite at
# risk. The signing key is shared, because one throwaway key per machine is
# enough.
#
# Logs are printed rather than written to a file, and only when the run is red:
# the report itself is on stdout, and a CI job shows stdout without an artifact
# step. The teardown is a trap so a failure, an interrupt or a crash all leave
# the machine clean.
.PHONY: test-web-e2e
test-web-e2e: e2e-key ## Run the browser e2e suite (Playwright) against the full stack
	@trap '$(COMPOSE_WEB_E2E) --env-file $(E2E_ENV_FILE) down -v --remove-orphans >/dev/null 2>&1' EXIT; \
	if ! $(COMPOSE_WEB_E2E) --env-file $(E2E_ENV_FILE) run --rm --build playwright; then \
		echo "--- stack logs ------------------------------------------------"; \
		$(COMPOSE_WEB_E2E) --env-file $(E2E_ENV_FILE) logs --no-color; \
		exit 1; \
	fi
