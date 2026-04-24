.PHONY: all build test test-unit test-contract test-integration test-e2e lint vet fmt cover clean run help sloth-generate test-obs test-rules test-alertmanager test-dashboards test-observability load-up load-down load-baseline load-smoke-local fixture-git-bench

GO              ?= go
GOLANGCI_LINT   ?= golangci-lint
PKG             := ./...
COVER_OUT       := coverage.out
COVER_THRESHOLD_DOMAIN := 85
COVER_THRESHOLD_APP    := 85

all: fmt vet lint test

build:
	$(GO) build -o bin/runtime-adapters ./cmd/runtime-adapters

test: test-unit test-contract

test-unit:
	$(GO) test -race -count=1 -coverprofile=$(COVER_OUT) $(PKG)

test-contract:
	$(GO) test -race -count=1 -tags=contract ./test/contract/...

test-integration:
	$(GO) test -race -count=1 -tags=integration -timeout=5m $(PKG)

test-e2e:
	$(GO) test -race -count=1 -tags=e2e -timeout=10m ./test/e2e/...

cover: test-unit
	$(GO) tool cover -func=$(COVER_OUT) | tail -n 1
	$(GO) tool cover -html=$(COVER_OUT) -o coverage.html

lint:
	$(GOLANGCI_LINT) run

vet:
	$(GO) vet $(PKG)

fmt:
	$(GO) fmt $(PKG)

run:
	$(GO) run ./cmd/runtime-adapters

clean:
	rm -rf bin/ coverage.out coverage.html

help:
	@echo "Targets: all build test test-unit test-contract test-integration test-e2e cover lint vet fmt run clean"
	@echo "Obs:     sloth-generate test-obs test-rules test-alertmanager test-dashboards test-observability"

# ----- Phase 2C.1 observability -----

# Rule files live alongside promtool test fixtures under the same dirs.
# Sloth emits one recording+alert file per adapter under ops/prometheus/generated/,
# while hand-written `.test.yaml` fixtures belong to `promtool test rules`
# (not `check rules`). Filter them out explicitly for each target.
RULE_FILES       := $(filter-out %.test.yaml,$(wildcard ops/prometheus/rules/*.yaml) $(wildcard ops/prometheus/generated/*.yaml))
RULE_TEST_FILES  := $(wildcard ops/prometheus/rules/*.test.yaml) $(wildcard ops/prometheus/generated/*.test.yaml)

sloth-generate:
	sloth generate --input ops/slo/ --out ops/prometheus/generated/

test-obs:
	$(GO) test -race -count=1 ./internal/infrastructure/obs/...
	$(GO) test -tags sloth ./ops/slo/...

test-rules:
	promtool check rules $(RULE_FILES)
	promtool test rules $(RULE_TEST_FILES)

test-alertmanager:
	amtool check-config ops/alertmanager/alertmanager.yaml
	$(GO) test -tags alertmanager ./ops/alertmanager/...

test-dashboards:
	@for f in ops/grafana/dashboards/*.json; do \
		echo "dashboard-linter lint -c ops/grafana/dashboards/.lint $$f"; \
		dashboard-linter lint -c ops/grafana/dashboards/.lint "$$f" || exit 1; \
	done
	$(GO) test -tags dashboards ./ops/grafana/...

test-observability: test-obs test-rules test-alertmanager test-dashboards

# ----- Phase 2C.2 load baseline -----

COMPOSE_BASELINE := ops/local/compose.yaml
COMPOSE_CI_SMOKE := ops/local/compose.ci-smoke.yaml

# Pinned container for runtime build used by docker compose (image tag).
RUNTIME_IMAGE_TAG := runtime-adapters:ci-test

load-up:
	docker build -t $(RUNTIME_IMAGE_TAG) .
	docker compose -f $(COMPOSE_BASELINE) up -d --wait

load-down:
	docker compose -f $(COMPOSE_BASELINE) down -v

load-baseline: load-up fixture-git-bench
	./ops/load/lib/verify-limits.sh runtime-load-runtime-adapters-1
	@# Trap teardown — without this, a mid-k6 failure leaves compose
	@# containers running across subsequent runs (port conflicts, leaked
	@# resources). Single-line shell so the trap survives make's per-line
	@# fresh-shell semantics.
	set -e; \
	  trap '$(MAKE) load-down' EXIT INT TERM; \
	  docker compose -f $(COMPOSE_BASELINE) run --rm k6 run /scripts/suite.js && \
	  ./ops/load/lib/generate-report.sh

load-smoke-local:
	docker build -t $(RUNTIME_IMAGE_TAG) .
	set -e; \
	  trap 'docker compose -f $(COMPOSE_CI_SMOKE) down -v' EXIT INT TERM; \
	  docker compose -f $(COMPOSE_CI_SMOKE) up -d --wait && \
	  docker compose -f $(COMPOSE_CI_SMOKE) run --rm k6 run /scripts/smoke.js

fixture-git-bench:
	$(MAKE) -C test/fixtures/git-bench all
