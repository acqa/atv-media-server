.DEFAULT_GOAL := help

COMPOSE ?= docker compose
SERVICE ?= media-server

GOLANGCI_LINT_VERSION ?= v2.1.0
GOLANGCI_LINT ?= $(shell go env GOPATH)/bin/golangci-lint

.PHONY: help up down restart rebuild logs ps shell build test fmt vet lint lint-install \
        env certs clean-cache clean-data clean-apple scan

help: ## Show list of targets
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z_-]+:.*##/ {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

env: ## Create .env from .env.example if it does not exist
	@test -f .env || cp .env.example .env && echo "Copied .env.example -> .env, edit the variables."

clean-apple: ## Remove AppleDouble files (._*) and .DS_Store — they interfere with docker build on NFS
	@find . -name '._*' -type f -delete 2>/dev/null || true
	@find . -name '.DS_Store' -type f -delete 2>/dev/null || true

up: env clean-apple ## Bring up the stack in the background
	$(COMPOSE) up -d

down: ## Stop the stack
	$(COMPOSE) down

restart: ## Restart media-server (DB and certificates are preserved)
	$(COMPOSE) restart $(SERVICE)

rebuild: env clean-apple ## Rebuild the image and bring it up
	$(COMPOSE) up -d --build

logs: ## Tail media-server logs
	$(COMPOSE) logs -f $(SERVICE)

ps: ## Container status
	$(COMPOSE) ps

shell: ## Shell inside the media-server container
	$(COMPOSE) exec $(SERVICE) sh

certs: ## Generate a self-signed certificate for appletv.redbull.tv
	./scripts/gen-cert.sh

scan: ## Trigger a library rescan
	curl -fsSk -X POST https://localhost/api/library/scan

clean-cache: ## Clear the transcode cache (HLS segments); restart yourself via make restart/rebuild
	rm -rf data/transcoded

clean-data: ## Delete the entire local DB and cache (IRREVERSIBLE)
	$(COMPOSE) down
	rm -rf data

build: ## Local Go binary build (without Docker)
	cd server && go build -trimpath -ldflags="-s -w" -o ../bin/server ./

test: ## Run go test
	cd server && go test ./...

fmt: ## go fmt
	cd server && go fmt ./...

vet: ## go vet
	cd server && go vet ./...

lint-install: ## Install pinned golangci-lint (override with GOLANGCI_LINT_VERSION=...)
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

lint: ## Run golangci-lint (auto-installs if missing)
	@test -x $(GOLANGCI_LINT) || $(MAKE) lint-install
	cd server && $(GOLANGCI_LINT) run ./...
