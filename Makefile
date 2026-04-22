.PHONY: all build test test-unit test-contract test-integration test-e2e lint vet fmt cover clean run help \
	sloth-generate test-obs test-rules test-alertmanager test-dashboards test-observability

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
