GO ?= go
PYTHON ?= python3
COMPOSE ?= docker compose

STATICCHECK_VERSION := v0.8.2
DEMO_DIR := examples/ecommerce
VENV := $(DEMO_DIR)/.venv
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
LDFLAGS := -X main.version=$(VERSION) -X main.commit=$(COMMIT)

.DEFAULT_GOAL := help

.PHONY: help build test test-go test-demo lint lint-go lint-demo demo demo-fixed down clean

help: ## Show this help
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F ':.*## ' '{printf "  %-12s %s\n", $$1, $$2}'

build: ## Build bin/ghosttraffic
	$(GO) build -ldflags "$(LDFLAGS)" -o bin/ghosttraffic ./cmd/ghosttraffic

test: test-go test-demo ## Run all tests that do not need Docker

test-go:
	$(GO) test -race ./...

test-demo: $(VENV)/.installed
	cd $(DEMO_DIR) && .venv/bin/pytest -q

lint: lint-go lint-demo ## Run all linters

lint-go:
	@test -z "$$(gofmt -l .)" || { echo "gofmt needed:"; gofmt -l .; exit 1; }
	$(GO) vet ./...
	$(GO) run honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION) ./...

lint-demo: $(VENV)/.installed
	cd $(DEMO_DIR) && .venv/bin/ruff check . && .venv/bin/ruff format --check .

$(VENV)/.installed: $(DEMO_DIR)/requirements.txt $(DEMO_DIR)/requirements-dev.txt
	$(PYTHON) -m venv $(VENV)
	$(VENV)/bin/pip install --quiet -r $(DEMO_DIR)/requirements-dev.txt
	touch $@

demo: ## Start the demo stack and verify baseline and candidate seed state
	$(COMPOSE) up --build --detach --wait
	$(PYTHON) $(DEMO_DIR)/check_stack.py http://127.0.0.1:8000 http://127.0.0.1:8001
	@echo "Stack is up. Capture, replay and reporting are not built yet; see the README status table."

demo-fixed: ## Same as demo, plus the fixed backend
	$(COMPOSE) --profile fixed up --build --detach --wait
	$(PYTHON) $(DEMO_DIR)/check_stack.py http://127.0.0.1:8000 http://127.0.0.1:8001 http://127.0.0.1:8002
	@echo "Stack is up. Capture, replay and reporting are not built yet; see the README status table."

down: ## Stop the demo stack and discard its data
	$(COMPOSE) --profile fixed down --volumes --remove-orphans

clean: ## Remove build output and the demo virtualenv
	rm -rf bin $(VENV)
