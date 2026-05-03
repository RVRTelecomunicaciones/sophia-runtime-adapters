.PHONY: all build test test-unit test-contract test-integration test-e2e chaos-integration lint vet fmt cover clean run help sloth-generate test-obs test-rules test-alertmanager test-dashboards test-observability load-up load-down load-baseline load-smoke-local fixture-git-bench chaos-up chaos-up-toxiproxy chaos-down chaos-local chaos-dump chaos-render-rules chaos-render-rules-check chaos-canary chaos-e2e-comprehensive secrets-write secrets-clean receivers-up receivers-down

GO              ?= go
GOLANGCI_LINT   ?= golangci-lint
PKG             := ./...
COVER_OUT       := coverage.out
COVER_THRESHOLD_DOMAIN := 85
COVER_THRESHOLD_APP    := 85
# Packages subject to the ≥85% per-package coverage gate.
# chaos package included here; gate is enforced once tests land in B1.
COV_GATE_PKGS := \
  ./internal/domain/... \
  ./internal/application/... \
  ./internal/infrastructure/obs/log/... \
  ./internal/infrastructure/chaos/...

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

chaos-integration:           ## Run per-PR chaos integration tests (no compose; DB testcontainer)
	$(GO) test -race -count=1 -tags=integration -timeout=5m ./test/chaos/integration/...

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
	sloth generate \
	  --fs-exclude '.*/test/.*|.*/windows/.*' \
	  --input ops/slo/ \
	  --out ops/prometheus/generated/

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

# ----- Phase 2C.3 chaos compose -----

COMPOSE_CHAOS_BASE    := ops/local/compose.yaml
COMPOSE_CHAOS_OVERLAY := ops/local/compose.chaos.yaml

chaos-up:                 ## Start chaos compose stack
	docker compose -f $(COMPOSE_CHAOS_BASE) -f $(COMPOSE_CHAOS_OVERLAY) up -d

chaos-up-toxiproxy:       ## Start chaos compose with toxiproxy profile
	docker compose -f $(COMPOSE_CHAOS_BASE) -f $(COMPOSE_CHAOS_OVERLAY) \
	  --profile toxiproxy up -d

chaos-down:               ## Tear down chaos compose
	docker compose -f $(COMPOSE_CHAOS_BASE) -f $(COMPOSE_CHAOS_OVERLAY) down -v

chaos-local:              ## Run a local-only chaos profile (PROFILE=name)
	@test -n "$(PROFILE)" || (echo "PROFILE=<name> required"; exit 1)
	@echo "Local-only chaos run not yet wired (B7 ships local catalogue)"

chaos-dump:               ## Dump receiver, prom, AM state for diagnostics
	./ops/chaos/scripts/dump.sh

# ----- Phase 2C.3 B6 — chaos test SLO render + canary -----

# macOS BSD sed requires `sed -i ''`; GNU sed accepts `sed -i` alone.
# Using `sed -i.bak` is safe on both (leaves a .bak we tolerate) and avoids
# conditional detection at the cost of an extra file per temp run.

chaos-render-rules:       ## Render Sloth from test specs into per-spec committed test rule files
	# Mirror the prod sloth-generate pattern: render each spec to its own
	# rule file under ops/prometheus/generated/test/. A naive concatenation
	# under one filename produces multiple top-level `groups:` keys (one
	# per Sloth output), which is NOT valid for Prometheus rule_files —
	# Prom would silently load only one. The chaos overlay's prometheus
	# config globs the test/ subdir.
	rm -rf ops/prometheus/generated/test
	mkdir -p ops/prometheus/generated/test
	sloth generate \
	  --slo-period-windows-path ops/slo/windows \
	  --default-slo-period 5m \
	  --input ops/slo/test/ \
	  --out ops/prometheus/generated/test/

chaos-render-rules-check: ## CI gate: idempotent render of test rules
	rm -rf /tmp/chaos-render-check
	mkdir -p /tmp/chaos-render-check
	sloth generate \
	  --slo-period-windows-path ops/slo/windows \
	  --default-slo-period 5m \
	  --input ops/slo/test/ \
	  --out /tmp/chaos-render-check/
	# Normalize the embedded sloth version line for cross-platform parity
	# (BSD vs GNU release strings); same fix as the prod observability gate.
	sed -i.bak 's/sloth_version: v\([0-9]\)/sloth_version: \1/g' /tmp/chaos-render-check/*.yaml
	sed -i.bak 's/Code generated by Sloth (v\([0-9]\)/Code generated by Sloth (\1/g' /tmp/chaos-render-check/*.yaml
	rm -f /tmp/chaos-render-check/*.bak
	diff -ru ops/prometheus/generated/test/ /tmp/chaos-render-check/
	rm -rf /tmp/chaos-render-check

chaos-canary:             ## Run per-PR canary E2E test
	$(GO) test -race -count=1 -tags=e2e -timeout=10m \
	  -run TestChaos_Canary_HttpConnectionReset \
	  ./test/chaos/e2e/...

chaos-e2e-comprehensive:  ## Run nightly comprehensive E2E test (all 6 profiles + inhibition contract)
	$(GO) test -race -count=1 -tags=e2e -timeout=30m \
	  -run TestChaos_Comprehensive \
	  ./test/chaos/e2e/...

# ---- Phase 2C.4 / A+B — alertmanager secret-file infrastructure -------

.secrets:
	@mkdir -p .secrets

# secrets-write: populate .secrets/* from env vars (typically loaded
# from .env via direnv, or set in the operator's shell). Uses
# `printf "%s"` to avoid trailing newlines that some Alertmanager
# versions did not trim (auto-trim landed for slack_api_url_file in
# v0.25.0 and webhook url_file in v0.26.0; routing_key_file has no
# explicit trim — defense in depth). Empty values are OK at this
# layer — Alertmanager will fail at runtime with a clear error
# pointing at the missing credential, surfacing the gap loud.
secrets-write: .secrets
	@printf "%s" "$$PAGERDUTY_TEST_ROUTING_KEY"        > .secrets/pagerduty_routing_key
	@printf "%s" "$$SLACK_TEST_INCIDENTS_WEBHOOK_URL"  > .secrets/slack_incidents_webhook_url
	@printf "%s" "$$SLACK_TEST_OPS_WEBHOOK_URL"        > .secrets/slack_ops_webhook_url
	@chmod 600 .secrets/*
	@echo "secrets-write: 3 files written under .secrets/ (mode 0600)"
	@for v in PAGERDUTY_TEST_ROUTING_KEY SLACK_TEST_INCIDENTS_WEBHOOK_URL SLACK_TEST_OPS_WEBHOOK_URL; do \
	  if [ -z "$$(eval echo \$$$$v)" ]; then \
	    echo "WARN: $$v is empty — Alertmanager will fail at runtime" >&2; \
	  fi; \
	done

# secrets-clean: paranoid removal. .secrets/ is gitignored so this
# is rarely needed in practice — but useful for fresh-clone bring-up.
secrets-clean:
	@rm -rf .secrets
	@echo "secrets-clean: .secrets/ removed"

# receivers-up / receivers-down: start/stop the alertmanager service
# via the receivers overlay (ops/local/compose.receivers.yaml).
# Composed from the base compose.yaml + the receivers-specific
# overlay (alertmanager service + secrets block). Depends on
# secrets-write so the source files exist at compose-up time. See
# ops/local/compose.receivers.yaml header for why we use an overlay
# instead of profile-gating in the base compose (chaos overlay
# collision avoidance).
receivers-up: secrets-write
	docker compose -f ops/local/compose.yaml -f ops/local/compose.receivers.yaml up -d --wait alertmanager

receivers-down:
	docker compose -f ops/local/compose.yaml -f ops/local/compose.receivers.yaml down
