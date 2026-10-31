GO ?= go
PYTHON ?= python3
COMPOSE ?= docker compose

STATICCHECK_VERSION := v0.8.2
DEMO_DIR := examples/ecommerce
WEB_DEPS := web/node_modules/.installed
VENV := $(DEMO_DIR)/.venv
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
LDFLAGS := -X main.version=$(VERSION) -X main.commit=$(COMMIT)

.DEFAULT_GOAL := help

.PHONY: help build test test-go test-demo lint lint-go lint-demo demo demo-fixed capture-demo replay-demo regression-demo workflow-demo image dashboard test-web lint-web down clean

help: ## Show this help
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F ':.*## ' '{printf "  %-12s %s\n", $$1, $$2}'

build: ## Build bin/ghosttraffic
	$(GO) build -ldflags "$(LDFLAGS)" -o bin/ghosttraffic ./cmd/ghosttraffic

test: test-go test-demo test-web ## Run all tests that do not need Docker

test-go:
	$(GO) test -race ./...

test-demo: $(VENV)/.installed
	cd $(DEMO_DIR) && .venv/bin/pytest -q

test-web: $(WEB_DEPS)
	cd web && npm test

lint: lint-go lint-demo lint-web ## Run all linters

lint-go:
	@test -z "$$(gofmt -l .)" || { echo "gofmt needed:"; gofmt -l .; exit 1; }
	$(GO) vet ./...
	$(GO) run honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION) ./...

lint-demo: $(VENV)/.installed
	cd $(DEMO_DIR) && .venv/bin/ruff check . && .venv/bin/ruff format --check .

lint-web: $(WEB_DEPS)
	cd web && npm run lint

$(WEB_DEPS): web/package-lock.json
	cd web && npm ci --no-audit --no-fund
	touch $@

web/dist/index.html: $(WEB_DEPS) $(shell find web/src web/index.html -type f 2>/dev/null)
	cd web && npm run build

$(VENV)/.installed: $(DEMO_DIR)/requirements.txt $(DEMO_DIR)/requirements-dev.txt
	$(PYTHON) -m venv $(VENV)
	$(VENV)/bin/pip install --quiet -r $(DEMO_DIR)/requirements-dev.txt
	touch $@

demo: ## Start the demo stack and verify baseline and candidate seed state
	$(COMPOSE) up --build --detach --wait
	$(PYTHON) $(DEMO_DIR)/check_stack.py http://127.0.0.1:8000 http://127.0.0.1:8001
	@echo "Stack is up. Run make regression-demo for the full demo."

demo-fixed: ## Same as demo, plus the fixed backend
	$(COMPOSE) --profile fixed up --build --detach --wait
	$(PYTHON) $(DEMO_DIR)/check_stack.py http://127.0.0.1:8000 http://127.0.0.1:8001 http://127.0.0.1:8002
	@echo "Stack is up. Run make regression-demo for the full demo."

capture-demo: build demo ## Record synthetic traffic and verify the trace
	scripts/capture-demo.sh

replay-demo: capture-demo ## Record, then replay against both backends and verify
	COMPOSE="$(COMPOSE)" scripts/replay-demo.sh

regression-demo: build ## Prove quiet on identical code, loud on the defect, clear on the fix
	COMPOSE="$(COMPOSE)" scripts/regression-demo.sh

workflow-demo: build ## Prove checkouts work on a target with different generated IDs
	COMPOSE="$(COMPOSE)" scripts/workflow-demo.sh

dashboard: build web/dist/index.html ## Serve run reports at http://127.0.0.1:7070
	bin/ghosttraffic serve --runs-dir runs --web-dir web/dist

image: ## Build the CLI container image
	docker build --build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) -t ghosttraffic:local .

down: ## Stop the demo stack and discard its data
	$(COMPOSE) --profile fixed --profile twin --profile dashboard down --volumes --remove-orphans

clean: ## Remove build output and the demo virtualenv
	rm -rf bin $(VENV) web/dist web/node_modules
