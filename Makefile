SHELL := /bin/bash

COMPOSE       := docker compose -f core/deploy/docker-compose.yml
COMPOSE_TOOLS := docker compose -f core/deploy/docker-compose.tools.yml
COMPOSE_E2E   := docker compose -f core/deploy/docker-compose.e2e.yml
GO_MODULES    := ./shared/... ./services/auth/... ./services/chat/... ./services/gateway/... ./services/game/... ./services/rating/... ./services/analysis/... ./services/tournament/...

-include core/deploy/.env

S ?= auth
MIGRATE_SERVICE := $(S)
MIGRATE_DSN := $($(shell echo $(S) | tr '[:lower:]' '[:upper:]')_POSTGRES_DSN)
export MIGRATE_SERVICE

.DEFAULT_GOAL := help

.PHONY: help
help: ## List available targets
	@grep -hE '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

.PHONY: env
env: ## Create core/deploy/.env from the example (does not overwrite)
	@if [ -f core/deploy/.env ]; then \
		echo "core/deploy/.env already exists, leaving it alone"; \
	else \
		cp core/deploy/.env.example core/deploy/.env; \
		echo "created core/deploy/.env — fill in the secrets before running services"; \
	fi

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

ENV_FILE := core/deploy/.env

.PHONY: jwt-key
jwt-key: env ## Generate a development RS256 signing key and the key-encryption key into core/deploy/.env
	@if ! grep -qE '^JWT_KEY_ENCRYPTION_KEY=.+' $(ENV_FILE); then \
		tmp=$$(mktemp); \
		grep -v '^JWT_KEY_ENCRYPTION_KEY=' $(ENV_FILE) > "$$tmp"; \
		echo "JWT_KEY_ENCRYPTION_KEY=$$(head -c 32 /dev/urandom | base64 | tr -d '\n')" >> "$$tmp"; \
		mv "$$tmp" $(ENV_FILE); \
		echo "wrote JWT_KEY_ENCRYPTION_KEY to $(ENV_FILE) (git-ignored)"; \
	fi
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

VAPID_KEYGEN = docker run --rm --entrypoint sh alpine/openssl -c ' \
	openssl ecparam -name prime256v1 -genkey -noout -out /tmp/k.pem 2>/dev/null && \
	b64url() { base64 | tr -d "\n=" | tr "/+" "_-"; } && \
	pub=$$(openssl ec -in /tmp/k.pem -pubout -outform DER 2>/dev/null | tail -c 65 | b64url) && \
	priv=$$(openssl ec -in /tmp/k.pem -outform DER 2>/dev/null | tail -c +8 | head -c 32 | b64url) && \
	[ $${\#pub} -eq 87 ] && [ $${\#priv} -eq 43 ] && echo "$$pub $$priv"'

.PHONY: vapid-key
vapid-key: env ## Generate a Web Push (VAPID) key pair for chat into core/deploy/.env
	@if grep -qE '^CHAT_VAPID_PRIVATE_KEY=.+' $(ENV_FILE); then \
		echo "CHAT_VAPID_PRIVATE_KEY is already set in $(ENV_FILE); delete the line to regenerate"; \
		exit 0; \
	fi; \
	echo "generating a P-256 key in a container..."; \
	pair=$$($(VAPID_KEYGEN)); \
	if [ -z "$$pair" ]; then echo "key generation failed"; exit 1; fi; \
	tmp=$$(mktemp); \
	grep -vE '^CHAT_VAPID_(PUBLIC|PRIVATE)_KEY=' $(ENV_FILE) > "$$tmp"; \
	printf 'CHAT_VAPID_PUBLIC_KEY=%s\nCHAT_VAPID_PRIVATE_KEY=%s\n' "$${pair% *}" "$${pair#* }" >> "$$tmp"; \
	mv "$$tmp" $(ENV_FILE); \
	echo "wrote CHAT_VAPID_PUBLIC_KEY and CHAT_VAPID_PRIVATE_KEY to $(ENV_FILE) (git-ignored)"

.PHONY: migrate-guard
migrate-guard:
	@if [ -z "$(MIGRATE_DSN)" ]; then \
		echo "no DSN for service '$(S)'."; \
		echo "expected $$(echo $(S) | tr '[:lower:]' '[:upper:]')_POSTGRES_DSN in core/deploy/.env — run 'make env' first"; \
		exit 1; \
	fi

.PHONY: db-extensions
db-extensions: ## Create the Postgres extensions on a database initialised before they were added
	$(COMPOSE) up -d --wait postgres
	$(COMPOSE) exec -T postgres sh -c 'psql -v ON_ERROR_STOP=1 -q -U "$$POSTGRES_USER" -d "$$POSTGRES_DB" -f /docker-entrypoint-initdb.d/02-extensions.sql'

.PHONY: migrate-up
migrate-up: migrate-guard db-extensions ## Apply pending migrations (make migrate-up S=auth)
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

.PHONY: proto
proto: ## Generate Go and TypeScript code from the contracts
	$(COMPOSE_TOOLS) run --rm buf generate

.PHONY: proto-lint
proto-lint: ## Lint the contracts
	$(COMPOSE_TOOLS) run --rm buf lint

.PHONY: proto-breaking
proto-breaking: ## Check the contracts for breaking changes against main
	$(COMPOSE_TOOLS) run --rm buf breaking --against '.git#branch=main'

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

.PHONY: test-integration
test-integration: ## Run integration tests (testcontainers spins up Postgres)
	cd core && go test -tags integration -count=1 ./services/auth/integration/... ./services/chat/integration/... ./services/game/integration/... ./services/analysis/integration/... ./services/tournament/integration/... ./shared/integration/...

E2E_ENV_FILE := core/deploy/.env.e2e

E2E_LOG_FILE := e2e-logs.txt

.PHONY: test-e2e
test-e2e: e2e-key ## Run e2e against the full stack on the shipping images
	@trap '$(COMPOSE_E2E) --env-file $(E2E_ENV_FILE) logs --no-color > $(E2E_LOG_FILE) 2>&1; \
	       $(COMPOSE_E2E) --env-file $(E2E_ENV_FILE) down -v --remove-orphans >/dev/null 2>&1' EXIT; \
	$(COMPOSE_E2E) --env-file $(E2E_ENV_FILE) run --rm --build e2e && \
	$(COMPOSE_E2E) --env-file $(E2E_ENV_FILE) run --rm e2e-chat

COMPOSE_SECURITY_E2E := BUILD_TARGET=runtime POSTGRES_HOST_PORT=55732 REDIS_HOST_PORT=56679 KAFKA_HOST_PORT=59392 MINIO_HOST_PORT=59300 \
	MINIO_CONSOLE_HOST_PORT=59301 GATEWAY_HOST_PORT=18380 WEB_HOST_PORT=3303 AUTH_HOST_PORT=18381 AUTH_METRICS_HOST_PORT=19391 \
	CHAT_HOST_PORT=18382 CHAT_METRICS_HOST_PORT=19392 GATEWAY_METRICS_HOST_PORT=19390 CONNECT_HOST_PORT=58183 REGISTRY_HOST_PORT=58181 \
	MTLS_WATCH_RENEWAL=$(if $(WATCH),1,) JWT_ROTATION_WATCH=$(if $(WATCH),3m,) \
	docker compose -p axon-security-e2e -f core/deploy/docker-compose.yml -f core/deploy/docker-compose.mtls.yml -f core/deploy/docker-compose.mtls-e2e.yml

.PHONY: test-e2e-security
test-e2e-security: jwt-key ## mTLS and key-rotation e2e on the base stack with the mTLS overlay (WATCH=1 also waits for renewals and rotations)
	@trap '$(COMPOSE_SECURITY_E2E) --profile e2e down -v --remove-orphans >/dev/null 2>&1' EXIT; \
	$(COMPOSE_SECURITY_E2E) --profile e2e run --rm --build security-e2e

COMPOSE_GAME_E2E := BUILD_TARGET=runtime POSTGRES_HOST_PORT=55832 REDIS_HOST_PORT=56779 KAFKA_HOST_PORT=59492 MINIO_HOST_PORT=59400 \
	MINIO_CONSOLE_HOST_PORT=59401 GATEWAY_HOST_PORT=18480 WEB_HOST_PORT=3403 AUTH_HOST_PORT=18481 AUTH_METRICS_HOST_PORT=19491 \
	CHAT_HOST_PORT=18482 CHAT_METRICS_HOST_PORT=19492 GATEWAY_METRICS_HOST_PORT=19490 CONNECT_HOST_PORT=58283 REGISTRY_HOST_PORT=58281 \
	docker compose -p axon-game-e2e -f core/deploy/docker-compose.yml -f core/deploy/docker-compose.game-e2e.yml

.PHONY: test-e2e-game
test-e2e-game: jwt-key ## Spectator and replay e2e through the gateway on the base stack
	@trap '$(COMPOSE_GAME_E2E) --profile e2e down -v --remove-orphans >/dev/null 2>&1' EXIT; \
	$(COMPOSE_GAME_E2E) --profile e2e run --rm --build game-e2e

COMPOSE_ANALYSIS_E2E := BUILD_TARGET=runtime POSTGRES_HOST_PORT=55932 REDIS_HOST_PORT=56879 KAFKA_HOST_PORT=59592 MINIO_HOST_PORT=59500 \
	MINIO_CONSOLE_HOST_PORT=59501 GATEWAY_HOST_PORT=18580 WEB_HOST_PORT=3503 AUTH_HOST_PORT=18581 AUTH_METRICS_HOST_PORT=19591 \
	CHAT_HOST_PORT=18582 CHAT_METRICS_HOST_PORT=19592 GATEWAY_METRICS_HOST_PORT=19590 CONNECT_HOST_PORT=58383 REGISTRY_HOST_PORT=58381 \
	docker compose -p axon-analysis-e2e -f core/deploy/docker-compose.yml -f core/deploy/docker-compose.analysis-e2e.yml

.PHONY: test-e2e-analysis
test-e2e-analysis: jwt-key ## Scholar's mate through the gateway, CDC and Stockfish analysis (one-off stack)
	@trap '$(COMPOSE_ANALYSIS_E2E) down -v --remove-orphans >/dev/null 2>&1' EXIT; \
	$(COMPOSE_ANALYSIS_E2E) run --rm --build analysis-e2e

COMPOSE_TOURNAMENT_E2E := BUILD_TARGET=runtime POSTGRES_HOST_PORT=56132 REDIS_HOST_PORT=57079 KAFKA_HOST_PORT=59792 MINIO_HOST_PORT=59700 \
	MINIO_CONSOLE_HOST_PORT=59701 GATEWAY_HOST_PORT=18780 WEB_HOST_PORT=3703 AUTH_HOST_PORT=18781 AUTH_METRICS_HOST_PORT=19791 \
	CHAT_HOST_PORT=18782 CHAT_METRICS_HOST_PORT=19792 GATEWAY_METRICS_HOST_PORT=19790 CONNECT_HOST_PORT=58583 REGISTRY_HOST_PORT=58581 \
	docker compose -p axon-tournament-e2e -f core/deploy/docker-compose.yml -f core/deploy/docker-compose.tournament-e2e.yml

.PHONY: test-e2e-tournament
test-e2e-tournament: jwt-key ## Eight-player tic-tac-toe bracket with a no-show through the gateway, CDC and the tournament saga (one-off stack)
	@trap '$(COMPOSE_TOURNAMENT_E2E) down -v --remove-orphans >/dev/null 2>&1' EXIT; \
	$(COMPOSE_TOURNAMENT_E2E) run --rm --build tournament-e2e

.PHONY: e2e-key
e2e-key:
	@touch $(E2E_ENV_FILE); \
	if ! grep -qE '^JWT_PRIVATE_KEY_DEV_1=.+' $(E2E_ENV_FILE); then \
		echo "generating a throwaway e2e signing key..."; \
		key=$$(docker run --rm alpine/openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 2>/dev/null | base64 | tr -d '\n'); \
		if [ -z "$$key" ]; then echo "key generation failed"; exit 1; fi; \
		grep -v '^JWT_PRIVATE_KEY_DEV_1=' $(E2E_ENV_FILE) > $(E2E_ENV_FILE).tmp; \
		printf 'JWT_PRIVATE_KEY_DEV_1=%s\n' "$$key" >> $(E2E_ENV_FILE).tmp; \
		mv $(E2E_ENV_FILE).tmp $(E2E_ENV_FILE); \
		echo "wrote a signing key to $(E2E_ENV_FILE) (git-ignored)"; \
	fi; \
	if ! grep -qE '^CHAT_VAPID_PRIVATE_KEY=.+' $(E2E_ENV_FILE); then \
		echo "generating a throwaway e2e push key..."; \
		pair=$$($(VAPID_KEYGEN)); \
		if [ -z "$$pair" ]; then echo "key generation failed"; exit 1; fi; \
		grep -vE '^CHAT_VAPID_(PUBLIC|PRIVATE)_KEY=' $(E2E_ENV_FILE) > $(E2E_ENV_FILE).tmp; \
		printf 'CHAT_VAPID_PUBLIC_KEY=%s\nCHAT_VAPID_PRIVATE_KEY=%s\n' "$${pair% *}" "$${pair#* }" >> $(E2E_ENV_FILE).tmp; \
		mv $(E2E_ENV_FILE).tmp $(E2E_ENV_FILE); \
		echo "wrote a push key to $(E2E_ENV_FILE) (git-ignored)"; \
	fi

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

.PHONY: web-test
web-test: ## Run the web client's unit tests (Vitest)
	$(COMPOSE_TOOLS) run --rm node npm test

.PHONY: web-check
web-check: web-lint web-typecheck web-test web-build ## Everything CI runs for the web client

.PHONY: tidy
tidy: ## Tidy every module in the workspace
	@for module in shared services/auth services/chat services/gateway services/game services/rating services/analysis services/tournament; do \
		echo "tidy $$module"; \
		(cd core/$$module && GOWORK=off go mod tidy) || exit 1; \
	done

.PHONY: check
check: fmt-check vet test ## Everything CI runs that needs no Docker

COMPOSE_RATING_E2E := docker compose -p axon-rating-e2e -f core/deploy/docker-compose.yml -f core/deploy/docker-compose.rating-e2e.yml --env-file core/deploy/.env --env-file core/deploy/rating-e2e.env

.PHONY: test-rating-e2e
test-rating-e2e: jwt-key ## Rated quick matches through gateway, CDC and rating, a Connect outage and a rebuild (one-off stack)
	@trap '$(COMPOSE_RATING_E2E) down -v --remove-orphans >/dev/null 2>&1' EXIT; \
	core/deploy/rating-e2e.sh $(COMPOSE_RATING_E2E)

COMPOSE_WEB_E2E := docker compose -f core/deploy/docker-compose.web-e2e.yml

.PHONY: test-web-e2e
test-web-e2e: e2e-key ## Run the browser e2e suite (Playwright) against the full stack
	@trap '$(COMPOSE_WEB_E2E) --env-file $(E2E_ENV_FILE) down -v --remove-orphans >/dev/null 2>&1' EXIT; \
	if ! $(COMPOSE_WEB_E2E) --env-file $(E2E_ENV_FILE) run --rm --build playwright; then \
		echo "--- stack logs ------------------------------------------------"; \
		$(COMPOSE_WEB_E2E) --env-file $(E2E_ENV_FILE) logs --no-color; \
		exit 1; \
	fi

APP_DIR         := ui/app
APP_LOCAL       := $(CURDIR)/.local/app
APP_SIMULATOR   ?= iPhone 18 Pro
APP_XCODEBUILD  := xcodebuild -project $(APP_DIR)/Axon.xcodeproj -derivedDataPath $(APP_LOCAL)/DerivedData \
	-clonedSourcePackagesDirPath $(APP_LOCAL)/spm -skipPackagePluginValidation

.PHONY: app-project
app-project: ## Generate the Xcode project for the SwiftUI app (XcodeGen)
	cd $(APP_DIR) && xcodegen generate --quiet

.PHONY: app-lint
app-lint: ## Lint the SwiftUI app (SwiftLint)
	cd $(APP_DIR) && swiftlint lint --strict --quiet

.PHONY: app-build
app-build: app-project ## Build the SwiftUI app for the iOS Simulator
	$(APP_XCODEBUILD) build -scheme Axon -destination 'generic/platform=iOS Simulator' CODE_SIGNING_ALLOWED=NO

.PHONY: app-test
app-test: app-project ## Run the SwiftUI app's unit tests on macOS
	$(APP_XCODEBUILD) test -scheme Axon -destination 'platform=macOS' CODE_SIGNING_ALLOWED=NO

COMPOSE_APP_E2E := BUILD_TARGET=runtime POSTGRES_HOST_PORT=56032 REDIS_HOST_PORT=56979 KAFKA_HOST_PORT=59692 MINIO_HOST_PORT=59600 \
	MINIO_CONSOLE_HOST_PORT=59601 GATEWAY_HOST_PORT=18680 WEB_HOST_PORT=3603 AUTH_HOST_PORT=18681 AUTH_METRICS_HOST_PORT=19691 \
	CHAT_HOST_PORT=18682 CHAT_METRICS_HOST_PORT=19692 GATEWAY_METRICS_HOST_PORT=19690 CONNECT_HOST_PORT=58483 REGISTRY_HOST_PORT=58481 \
	docker compose -p axon-app-e2e -f core/deploy/docker-compose.yml -f core/deploy/docker-compose.app-e2e.yml

APP_E2E_GATEWAY := http://localhost:18680
APP_E2E_RESULT  := $(APP_LOCAL)/AxonUITests.xcresult

.PHONY: test-e2e-app
test-e2e-app: jwt-key app-project ## XCUITest on the iPhone simulator against a one-off stack (KEEP=1 keeps it, TESTS=<Class> narrows)
	@mkdir -p $(APP_LOCAL); \
	$(if $(KEEP),,trap '$(COMPOSE_APP_E2E) logs --no-color > $(APP_LOCAL)/e2e-stand.log 2>&1; \
	       $(COMPOSE_APP_E2E) down -v --remove-orphans >/dev/null 2>&1' EXIT;) \
	$(COMPOSE_APP_E2E) up --build --wait gateway game && \
	rm -rf $(APP_E2E_RESULT) && \
	TEST_RUNNER_AXON_E2E_GATEWAY=$(APP_E2E_GATEWAY) $(APP_XCODEBUILD) test -quiet -scheme AxonUITests \
		-destination 'platform=iOS Simulator,name=$(APP_SIMULATOR)' -resultBundlePath $(APP_E2E_RESULT) \
		$(if $(TESTS),-only-testing:AxonUITests/$(TESTS)) && \
	echo "AxonUITests passed; screenshots are in $(APP_E2E_RESULT)"

.PHONY: app-e2e-down
app-e2e-down: ## Tear down the app e2e stack that KEEP=1 left running
	$(COMPOSE_APP_E2E) down -v --remove-orphans

KIND_CLUSTER ?= axon
K8S_DIR      := core/deploy/k8s

.PHONY: kind-up
kind-up: ## Create the kind cluster, load the images, install Argo CD and wait until every application is Synced and Healthy
	@if [ ! -d core/shared/gen/go ]; then $(MAKE) proto; fi
	KIND_CLUSTER=$(KIND_CLUSTER) $(K8S_DIR)/kind/up.sh

.PHONY: kind-down
kind-down: ## Delete the kind cluster
	kind delete cluster --name $(KIND_CLUSTER)

.PHONY: kind-load
kind-load: ## Rebuild the images and load them into the kind cluster (IMAGES="game web" for a subset)
	@if [ ! -d core/shared/gen/go ]; then $(MAKE) proto; fi
	KIND_CLUSTER=$(KIND_CLUSTER) $(K8S_DIR)/kind/images.sh

.PHONY: kind-e2e
kind-e2e: ## Run the auth, chat, rating and mTLS e2e suites inside the cluster against its gateway
	KIND_CLUSTER=$(KIND_CLUSTER) $(K8S_DIR)/kind/run-job.sh e2e $(K8S_DIR)/jobs/e2e.yaml e2e

.PHONY: kind-chaos
kind-chaos: ## Load game in the cluster under the Chaos Mesh workflow and check no acknowledged move was lost (CHAOS=0 for load only)
	KIND_CLUSTER=$(KIND_CLUSTER) $(K8S_DIR)/kind/chaos.sh

.PHONY: k8s-validate
k8s-validate: ## helm lint every chart and kubeconform the rendered manifests for kind and preview
	$(K8S_DIR)/ci/validate.sh
