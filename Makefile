# Lewbox: build, test and run from one entry point.
#
#   make            build the web console and bin/lewbox
#   make help       list every target

SHELL := /bin/bash

VERSION ?= dev
BIN ?= bin/lewbox
PORT ?= 8080
UI_DIR ?= ../ui

.DEFAULT_GOAL := build

.PHONY: help deps web build build-go run dev test fmt docker docker-test up down e2e update-ui clean distclean

help: ## List the targets and what they do.
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z0-9_.\/-]+:.*## / {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

# Installs the web dependencies once, and again whenever the lockfile changes.
web/node_modules/.package-lock.json: web/package.json web/package-lock.json
	npm --prefix web ci

deps: web/node_modules/.package-lock.json ## Install the web console's dependencies.

web: deps ## Build the web console into web/dist.
	npm --prefix web run build

build: web ## Build bin/lewbox with the web console embedded (default).
	@mkdir -p $(dir $(BIN))
	go build -trimpath -ldflags "-X main.version=$(VERSION)" -o $(BIN) ./cmd/lewbox

build-go: ## Build bin/lewbox from Go only; run `make web` first.
	@mkdir -p $(dir $(BIN))
	go build -trimpath -ldflags "-X main.version=$(VERSION)" -o $(BIN) ./cmd/lewbox

run: build ## Build, then serve on the port in PORT. Set LEWBOX_BOOTSTRAP_ADMIN_EMAIL and LEWBOX_BOOTSTRAP_ADMIN_PASSWORD to create the first admin.
	$(BIN) serve --port $(PORT)

dev: deps ## Run the Vite dev server on :5173, proxying /v0 to :8080. Run `make run` in another terminal.
	npm --prefix web run dev

test: deps ## Run every check: gofmt, go vet, Go tests, web typecheck and tests.
	@test -z "$$(gofmt -l .)" || { echo "gofmt needed:"; gofmt -l .; exit 1; }
	go vet ./...
	go test ./...
	npm --prefix web run typecheck
	npm --prefix web test

fmt: ## Format every Go file.
	gofmt -w .

docker: ## Build the runtime image lewbox:local through Docker Compose.
	LEWBOX_VERSION=$(VERSION) LEWBOX_ADMIN_PASSWORD=unused docker compose build lewbox

docker-test: ## Build the image's test stages: Go checks and web console tests.
	LEWBOX_ADMIN_PASSWORD=unused docker compose --profile test build test web-test

up: ## Start the orchestrator with Docker Compose (keeps the data volume).
	docker compose up -d --build

down: ## Stop the orchestrator, keeping its data volume.
	docker compose down

e2e: ## Build lewbox:local and smoke-test the container.
	deploy/docker/e2e.sh

update-ui: ## Re-vendor @harlew-dev/ui from the UI_DIR checkout and update the lockfile.
	@test -d "$(UI_DIR)" || { echo "update-ui: no UI checkout at $(UI_DIR) (set UI_DIR=...)"; exit 1; }
	npm --prefix $(UI_DIR) run build
	rm -f web/vendor/harlew-dev-ui-*.tgz
	@name=$$(cd "$(UI_DIR)" && npm pack --pack-destination "$(CURDIR)/web/vendor" | tail -n 1); \
		test -n "$$name" || { echo "update-ui: npm pack produced no tarball"; exit 1; }; \
		echo "vendored web/vendor/$$name"; \
		cd web && npm install "@harlew-dev/ui@file:vendor/$$name"

clean: ## Remove bin/ and built web console files, keeping web/dist/.gitkeep.
	rm -rf bin
	@mkdir -p web/dist
	find web/dist -mindepth 1 ! -name .gitkeep -delete

distclean: clean ## Remove bin/, the built console and web/node_modules.
	rm -rf web/node_modules
