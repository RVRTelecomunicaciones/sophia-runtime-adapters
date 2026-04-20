# runtime-adapters — Phase 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the `runtime-adapters` Phase 1 MVP — a governable execution layer exposing 8 capabilities across 4 adapters (shell / git / filesystem / http), a normalized `ExecutionReceipt` as the sole auditable artifact, sync-only transport via HTTP + in-proc Go SDK, and PostgreSQL persistence with caller-owned idempotency.

**Architecture:** Hexagonal / clean architecture with one bounded context (`execution`). Single aggregate root `ExecutionReceipt`. Rigid separation between adapter-specific `AdapterRawOutcome` (never crosses the domain boundary) and normalized `ExecutionResult`. `context.Context` is the sole mechanism for timeout / cancellation. Runtime never retries — it emits a `RetryHint` as signal and persists the receipt before returning. All decisions (`Dn.m`) and adjustments (`An.m`) are frozen in `docs/superpowers/specs/2026-04-19-runtime-adapters-phase1-design.md`.

**Tech Stack:**
- Language: Go `1.22+`
- DB: PostgreSQL `15+` via `jackc/pgx/v5`
- HTTP router: `go-chi/chi/v5`
- Git: `go-git/go-git/v5` (D8.7 — no hooks in Phase 1)
- Observability: OpenTelemetry (`go.opentelemetry.io/otel`)
- Testing: `stretchr/testify`, `testcontainers/testcontainers-go`, `net/http/httptest`
- Lint: `golangci-lint` (`govet`, `staticcheck`, `gosec`, `errcheck`, `revive`, `gocritic`, `errorlint`)
- Supply-chain: `govulncheck`

---

## Assumptions locked at plan-write time

Plan-level choices that the spec leaves open. If any of these are wrong, fix at T1 before other tasks pick up the dependency.

| # | Assumption | Rationale |
|---|---|---|
| P1 | Go module path = `github.com/sophia-ecosystem/runtime-adapters`. | Consistent with the Sophia ecosystem naming; change in `go.mod` only if upstream namespace differs. |
| P2 | HTTP router = `chi/v5`. | Idiomatic, stdlib-friendly, lightweight; spec doesn't mandate. |
| P3 | Postgres driver = `pgx/v5` (native, not `database/sql`). | Better types, prepared statements, pool control. |
| P4 | Migrations via `golang-migrate/migrate/v4` (embedded `.sql` files under `internal/adapters/outbound/pg/migrations/`). | Matches governance convention per §13 lessons. |
| P5 | Directory layout uses **`entities/` for VOs that live inside the aggregate** (per D3.4); pure shared VOs live in `internal/domain/shared/`. | Pragmatic — matches A3.1. |
| P6 | Repo folder = the current working directory (`sophia-runtime-adapters`). The spec header's "written by accident in another repo" warning is now **stale** — T1 moves the spec into `docs/superpowers/specs/` and strips that warning. | User confirmation. |
| P7 | All new files use `LF` line endings and UTF-8. `.editorconfig` enforces. | |
| P8 | Git init creates `main` as default branch. Initial commit is the bootstrap + spec move. | |

> **If any assumption is wrong**, flag it at the first commit of Bundle 1 and stop. Don't improvise downstream.

---

## Scope check

Phase 1 is **one** bounded context (`execution`, per D3.1) with **one** aggregate root (`ExecutionReceipt`, per D3.2). No sub-subsystem decomposition is required. The plan is large but cohesive. Bundles are delivery checkpoints, **not** independent subsystems.

Phase 2+ tracks (2A..2F per §11) are **explicitly out of scope** and have their own future specs.

---

## File structure map

Target layout after all bundles land. Files with `†` are **Phase-1-active**; everything else is documentation / tooling.

```
sophia-runtime-adapters/
├── .claude/
│   └── skills/                                       # 11 active skills (A12.1 excludes mailbox-locking-model)
│       ├── architecture-guardrails/SKILL.md
│       ├── execution-modeling/SKILL.md
│       ├── adapter-contracts/SKILL.md
│       ├── shell-execution-safety/SKILL.md
│       ├── git-file-operations/SKILL.md
│       ├── filesystem-safety/SKILL.md
│       ├── http-adapter-design/SKILL.md
│       ├── resilience-timeouts-retries/SKILL.md
│       ├── execution-result-normalization/SKILL.md
│       ├── observability-runtime/SKILL.md
│       └── testing-quality/SKILL.md
├── .github/
│   └── workflows/
│       ├── ci.yaml                                   # push=unit+contract, PR=integration, main=e2e
│       └── security.yaml                             # gosec + govulncheck (non-blocking per §10.5)
├── .editorconfig
├── .gitignore
├── .golangci.yaml
├── AGENTS.md                                         # operational directives (§12.3)
├── CLAUDE.md                                         # canonical entry (§12.2)
├── LICENSE                                           # MIT (or user's choice at T7)
├── Makefile                                          # build / lint / test / test-integration / test-e2e / run
├── README.md                                         # human-facing
├── cmd/
│   └── runtime-adapters/
│       └── main.go †                                 # binary entrypoint
├── docs/
│   ├── adr/
│   │   ├── 0000-TEMPLATE.md                          # ADR template
│   │   ├── 0001-phase-1-spec-adoption.md             # adopt the approved spec
│   │   └── 0002-go-git-only-no-hooks.md              # A8.1
│   ├── architecture.md                               # derived diagram + flow (§12.1)
│   ├── domain-invariants.md                          # I1..I22 (§12.5)
│   ├── rules.md                                      # 15 hard rules (§12.4)
│   └── superpowers/
│       ├── plans/
│       │   └── 2026-04-19-runtime-adapters-phase1.md # THIS FILE
│       └── specs/
│           └── 2026-04-19-runtime-adapters-phase1-design.md
├── go.mod †
├── go.sum †
├── internal/
│   ├── adapters/
│   │   ├── inbound/
│   │   │   ├── http/ †                               # REST handlers (chi)
│   │   │   │   ├── router.go
│   │   │   │   ├── execute_handler.go
│   │   │   │   ├── capabilities_handler.go
│   │   │   │   ├── receipts_handler.go
│   │   │   │   ├── middleware.go                     # panic-recover, OTel, request-id
│   │   │   │   └── errors.go                         # HTTP error envelope
│   │   │   └── sdk/ †                                # in-proc Go SDK
│   │   │       └── sdk.go
│   │   └── outbound/
│   │       ├── shell/ †                              # shell.exec@v1
│   │       ├── git/ †                                # git.{status,clone,diff,commit}@v1
│   │       ├── filesystem/ †                         # filesystem.{read_file,write_file}@v1
│   │       ├── httpreq/ †                            # http.request@v1 (named to avoid stdlib collision)
│   │       └── pg/ †                                 # ReceiptRepositoryPG + IdempotencyStorePG
│   │           ├── migrations/
│   │           │   ├── 0001_receipts.up.sql
│   │           │   ├── 0001_receipts.down.sql
│   │           │   ├── 0002_idempotency.up.sql
│   │           │   └── 0002_idempotency.down.sql
│   │           ├── receipt_repository.go
│   │           └── idempotency_store.go
│   ├── application/
│   │   └── services/ †
│   │       ├── execute_service.go                    # UC1 — 11-step flow (D6.2)
│   │       ├── query_service.go                      # UC2 + UC3
│   │       └── concurrency.go                        # MaxConcurrentExecutions semaphore (A9.1)
│   ├── bootstrap/ †
│   │   └── wire.go                                   # sole composition point (D7.9)
│   ├── domain/
│   │   ├── execution/
│   │   │   ├── entities/ †                           # aggregate + inner VOs (per D3.4 / A3.1)
│   │   │   │   ├── request.go
│   │   │   │   ├── handle.go
│   │   │   │   ├── result.go
│   │   │   │   ├── receipt.go
│   │   │   │   ├── artifact.go
│   │   │   │   ├── stream_ref.go
│   │   │   │   ├── provenance.go
│   │   │   │   └── timings.go
│   │   │   ├── valueobjects/ †
│   │   │   │   ├── adapter_id.go
│   │   │   │   ├── capability.go
│   │   │   │   ├── capability_registry.go
│   │   │   │   ├── payload.go
│   │   │   │   ├── timeout_budget.go
│   │   │   │   ├── execution_status.go
│   │   │   │   ├── retry_hint.go
│   │   │   │   └── error_class.go
│   │   │   └── services/ †
│   │   │       └── result_normalizer.go              # sole domain service in Phase 1 (D3.12)
│   │   └── shared/ †
│   │       ├── ids.go                                # ReceiptID, HandleID, CorrelationID, IdempotencyKey
│   │       └── clock.go                              # Clock interface + RealClock
│   ├── infrastructure/
│   │   ├── config/ †                                 # env → typed config
│   │   │   ├── config.go
│   │   │   └── load.go
│   │   └── obs/ †                                    # OTel setup + metrics/instrument registry
│   │       ├── otel.go
│   │       └── metrics.go
│   └── ports/
│       ├── inbound/ †
│       │   ├── runtime_service.go                    # RuntimeService interface
│       │   └── query_service.go                      # QueryService interface
│       └── outbound/ †
│           ├── adapter.go                            # Adapter interface + AdapterOutcome marker
│           ├── receipt_repository.go                 # ReceiptRepository + sentinels
│           └── idempotency_store.go                  # IdempotencyStore + sentinels
├── scripts/
│   ├── ci.sh                                         # unit + contract
│   ├── ci-integration.sh                             # -tags=integration
│   └── ci-e2e.sh                                     # -tags=e2e
└── test/
    ├── contract/ †
    │   ├── adapter_contract_suite.go                 # reusable AdapterContractTestSuite (D10.4)
    │   └── http_sdk_equivalence_test.go              # HTTP ≡ SDK (D10.4)
    └── e2e/ †
        ├── happy_path_test.go
        ├── git_clone_partial_test.go
        └── idempotency_replay_test.go
```

---

## Bundle overview (subagent-driven checkpoints)

| Bundle | Name | Tasks | Depends on | Checkpoint |
|--------|------|-------|------------|------------|
| 1 | Infra + harness | T1..T8 | — | Commit series; `go vet ./...` and `golangci-lint run` clean on empty skeleton |
| 2 | Domain | T9..T24 | Bundle 1 | Unit test coverage ≥ 85% on `internal/domain/...` |
| 3 | Ports + application | T25..T31 | Bundle 2 | `ExecuteService` unit tests pass with test doubles |
| 4 | Adapters | T32..T48 | Bundle 3 | Each adapter passes `AdapterContractTestSuite` |
| 5 | Persistence | T49..T52 | Bundle 3 | Integration tests green under `-tags=integration` with testcontainers |
| 6 | Inbound HTTP + SDK | T53..T57 | Bundles 4 & 5 | HTTP ≡ SDK contract test green |
| 7 | Bootstrap + observability | T58..T63 | Bundle 6 | `cmd/runtime-adapters` starts, OTel emits, graceful shutdown honored |
| 8 | E2E + docs + v0.1.0 | T64..T70 | Bundle 7 | 3 smoke scenarios green, docs complete, `v0.1.0` tagged |

**Bundle 4 is the largest.** It can be split into sub-bundles 4A (shell), 4B (git), 4C (filesystem), 4D (http) if running parallel subagents — each sub-bundle carries its own contract-suite verification.

---

# Bundle 1 — Infra + harness

Goal: empty but correct skeleton. After this bundle, `go build ./...`, `go vet ./...`, `golangci-lint run`, and `make test` all succeed on zero code.

## Task 1: Verify assumptions and initialize repo

**Files:**
- Create: `.gitignore`, `.editorconfig`, `LICENSE`, `go.mod`, `go.sum` (generated)
- Move: `runtime-adapters-phase1-design.md` → `docs/superpowers/specs/2026-04-19-runtime-adapters-phase1-design.md`

- [ ] **Step 1: Confirm P1..P8 assumptions**

Re-read the "Assumptions" block at the top of this plan. If any is wrong, **stop and flag**. Do not proceed.

- [ ] **Step 2: Initialize git and Go module**

Run:
```bash
cd /Users/russell/Documents/2026/sophia-runtime-adapters
git init -b main
go mod init github.com/sophia-ecosystem/runtime-adapters
```
Expected: `.git/` created; `go.mod` contains `module github.com/sophia-ecosystem/runtime-adapters` and `go 1.22`.

- [ ] **Step 3: Write `.gitignore`**

Content:
```
# Binaries
/bin/
/dist/
runtime-adapters
*.exe

# Go
*.test
*.out
coverage.out
coverage.html

# Env / local
.env
.env.local
.direnv/

# IDE
.idea/
.vscode/
*.swp

# macOS / Linux cruft
.DS_Store
```

- [ ] **Step 4: Write `.editorconfig`**

Content:
```
root = true

[*]
charset = utf-8
end_of_line = lf
insert_final_newline = true
indent_style = tab
indent_size = 4
trim_trailing_whitespace = true

[*.{md,yaml,yml,json,sql}]
indent_style = space
indent_size = 2
```

- [ ] **Step 5: Move the spec and strip stale warning**

Run:
```bash
mkdir -p docs/superpowers/specs
git mv runtime-adapters-phase1-design.md docs/superpowers/specs/2026-04-19-runtime-adapters-phase1-design.md
```

Then edit the moved file to **remove the blockquote** that reads:
> ⚠️ **Authoring context**: this file was written inside the working directory of another repo (`agent-governance-core`) by operational accident. ...

(lines around the header — between "Baseline" and "Table of contents"). Replace with nothing — just drop the blockquote. The spec is now in the correct repo; the warning is stale.

- [ ] **Step 6: Choose license and write `LICENSE`**

Default to MIT. If upstream requires another, substitute. Fill copyright year `2026` and holder `Sophia Ecosystem` (or user's org).

- [ ] **Step 7: Verify**

Run:
```bash
go vet ./...
git status
```
Expected: `go vet` says nothing (no Go files yet, exits 0); `git status` shows staged `.gitignore`, `.editorconfig`, `LICENSE`, `go.mod`, and the moved spec.

- [ ] **Step 8: Commit**

```bash
git add .gitignore .editorconfig LICENSE go.mod docs/
git commit -m "chore: bootstrap repo and move Phase 1 spec into docs/superpowers/specs"
```

---

## Task 2: Create full directory skeleton

**Files:** empty `.gitkeep` in each leaf directory from the file-structure map.

- [ ] **Step 1: Create all directories at once**

```bash
mkdir -p \
  cmd/runtime-adapters \
  internal/adapters/inbound/http \
  internal/adapters/inbound/sdk \
  internal/adapters/outbound/shell \
  internal/adapters/outbound/git \
  internal/adapters/outbound/filesystem \
  internal/adapters/outbound/httpreq \
  internal/adapters/outbound/pg/migrations \
  internal/application/services \
  internal/bootstrap \
  internal/domain/execution/entities \
  internal/domain/execution/valueobjects \
  internal/domain/execution/services \
  internal/domain/shared \
  internal/infrastructure/config \
  internal/infrastructure/obs \
  internal/ports/inbound \
  internal/ports/outbound \
  test/contract \
  test/e2e \
  docs/adr \
  .claude/skills/architecture-guardrails \
  .claude/skills/execution-modeling \
  .claude/skills/adapter-contracts \
  .claude/skills/shell-execution-safety \
  .claude/skills/git-file-operations \
  .claude/skills/filesystem-safety \
  .claude/skills/http-adapter-design \
  .claude/skills/resilience-timeouts-retries \
  .claude/skills/execution-result-normalization \
  .claude/skills/observability-runtime \
  .claude/skills/testing-quality \
  scripts \
  .github/workflows
```

- [ ] **Step 2: Add `.gitkeep` to each empty leaf**

```bash
find cmd internal test .claude/skills scripts .github -type d -empty -exec touch {}/.gitkeep \;
```

- [ ] **Step 3: Verify layout matches "File structure map"**

Compare `tree -I '.git' -a -L 5` output against the map above. No missing or extra directories.

- [ ] **Step 4: Commit**

```bash
git add .
git commit -m "chore: scaffold Phase 1 directory layout"
```

---

## Task 3: Write `Makefile`

**Files:** Create `Makefile`.

- [ ] **Step 1: Author the Makefile**

```makefile
.PHONY: all build test test-unit test-contract test-integration test-e2e lint vet fmt cover clean run help

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

cover:
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
```

- [ ] **Step 2: Verify**

```bash
make help
```
Expected: lists targets. `make vet` passes on empty tree.

- [ ] **Step 3: Commit**

```bash
git add Makefile
git commit -m "chore: add Makefile with test/lint/build targets"
```

---

## Task 4: Write `.golangci.yaml`

**Files:** Create `.golangci.yaml`.

- [ ] **Step 1: Author the lint config**

```yaml
run:
  timeout: 5m
  go: "1.22"
  tests: true

linters:
  disable-all: true
  enable:
    - errcheck
    - errorlint
    - gocritic
    - gofmt
    - goimports
    - gosec
    - govet
    - ineffassign
    - misspell
    - revive
    - staticcheck
    - unused

linters-settings:
  gosec:
    excludes:
      - G104 # errcheck covers it
  revive:
    rules:
      - name: exported
        disabled: true # we'll add doc.go files later
```

- [ ] **Step 2: Install if missing**

```bash
which golangci-lint || brew install golangci-lint
```

- [ ] **Step 3: Verify**

```bash
golangci-lint run
```
Expected: exits 0 (no files to lint yet).

- [ ] **Step 4: Commit**

```bash
git add .golangci.yaml
git commit -m "chore: configure golangci-lint for Phase 1"
```

---

## Task 5: GitHub Actions CI (`ci.yaml`)

**Files:** Create `.github/workflows/ci.yaml`.

- [ ] **Step 1: Author workflow**

```yaml
name: ci

on:
  push:
    branches: [main]
  pull_request:

jobs:
  lint-unit-contract:
    name: lint + unit + contract
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.22"
          cache: true
      - name: vet
        run: go vet ./...
      - name: lint
        uses: golangci/golangci-lint-action@v6
        with:
          version: latest
      - name: unit
        run: make test-unit
      - name: contract
        run: make test-contract
      - name: coverage gate — domain + application
        run: |
          go tool cover -func=coverage.out > cov.txt
          cat cov.txt
          awk '
            /^(github\.com\/sophia-ecosystem\/runtime-adapters\/internal\/(domain|application))/ {
              match($3, /[0-9.]+/); val=substr($3, RSTART, RLENGTH)+0;
              if (val < 85) { print "coverage gate failed:", $1, val; exit 1 }
            }
          ' cov.txt
        if: ${{ !cancelled() }}

  integration:
    name: integration (PR only)
    if: github.event_name == 'pull_request'
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.22"
          cache: true
      - name: integration
        run: make test-integration

  e2e:
    name: e2e (main only)
    if: github.event_name == 'push' && github.ref == 'refs/heads/main'
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.22"
          cache: true
      - name: e2e
        run: make test-e2e
```

- [ ] **Step 2: Author `.github/workflows/security.yaml`**

```yaml
name: security

on:
  schedule: [{ cron: "0 8 * * 1" }]
  workflow_dispatch:

jobs:
  scan:
    runs-on: ubuntu-latest
    continue-on-error: true # non-blocking per §10.5
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: "1.22", cache: true }
      - name: gosec
        uses: securego/gosec@master
        with: { args: "./..." }
      - name: govulncheck
        run: |
          go install golang.org/x/vuln/cmd/govulncheck@latest
          govulncheck ./...
```

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/
git commit -m "ci: configure Phase 1 workflows (unit/contract on push, integration on PR, e2e on main, weekly security scan)"
```

---

## Task 6: Docs skeletons — CLAUDE.md + AGENTS.md + architecture.md

**Files:**
- Create: `CLAUDE.md`, `AGENTS.md`, `docs/architecture.md`

- [ ] **Step 1: Write `CLAUDE.md`** per §12.2. Sections in exact order: "What this repo is", "What it is NOT", "Required mindset", "Must-read files (specs, invariants, rules, adapters safety)", "Core design principles (D1.1..D1.6, D3.5, D6.4, A4.3, A9.1)", "Before-coding checklist", "Tech stack", "Output style", "Never-do list (mirrors `rules.md` R1..R15)".

- [ ] **Step 2: Write `AGENTS.md`** per §12.3. Sections: "SDD workflow", "Skills catalogue (11 skills)", "Conventions (conventional commits, never `Co-Authored-By` per D12.6)", "Invariants pointer", "How to add a capability", "How to add an adapter (requires ADR)".

- [ ] **Step 3: Write `docs/architecture.md`** with ASCII diagram (dependency direction) derived from §7.4. Include: domain → ports; adapters inbound/outbound → ports; composition only at `internal/bootstrap/wire.go`.

- [ ] **Step 4: Commit**

```bash
git add CLAUDE.md AGENTS.md docs/architecture.md
git commit -m "docs: add CLAUDE.md, AGENTS.md, and architecture overview"
```

---

## Task 7: Docs skeletons — rules.md + domain-invariants.md

**Files:**
- Create: `docs/rules.md`, `docs/domain-invariants.md`

- [ ] **Step 1: Write `docs/rules.md`** with exactly the 15 hard rules from §12.4 (R1..R15 verbatim). Each rule one paragraph + `Rationale:` line citing the decision it derives from (e.g. R1 → D1.2, R6 → D9.3, R7 → A4.3, R14 → A9.1, R15 → D4.3).

- [ ] **Step 2: Write `docs/domain-invariants.md`** with I1..I22 extracted from §4 and §5. Suggested numbering:
  - I1 `ReceiptID` is ULID and unique.
  - I2 Receipts are append-only (D7.5).
  - I3 `ExecutionStatus` ∈ {success, failure, timeout, cancelled, partial} (D4.3).
  - I4 `RetryHint` ∈ {retryable, non_retryable, unknown} (D4.4).
  - I5 `ErrorClass` is the 10-value closed enum (D4.5).
  - I6 `partial` ⇔ 4-AND rule (D4.6).
  - I7 `success` ⇒ `error_class` empty (§5.9).
  - I8 `shell.exec` never produces artifacts (D4.8).
  - I9 Payload JSON-only, ≤ `MaxPayloadBytes` (D5.5).
  - I10 `TimeoutBudget` > 0 and ≤ `MaxTimeoutBudget` (§5.6).
  - I11 `Capability` canonical = `adapter.name@vX` (D3.7).
  - I12 `AdapterID` regex `^[a-z][a-z0-9_]{1,31}$` (D3.6).
  - I13 Receipt is persisted before caller gets a response (D4.12 / A4.3).
  - I14 `ExecutionRequest` is immutable post-construction (D4.1).
  - I15 `Timings` fields are monotonic (submitted ≤ started ≤ completed) (§5.11).
  - I16 `StreamRef` inline ≤ `InlineStreamLimit` else `truncated` flag (D4.9).
  - I17 `Artifact.attributes` = small scalar strings, total ≤ 2 KiB (D5.13).
  - I18 `adapter_meta` = small scalar strings, total ≤ 8 KiB (A4.2).
  - I19 `Provenance.source` ∈ {governance, sdk, http, cli, test} (D5.10).
  - I20 `schema_version` = "v1" in Phase 1 (D4.11).
  - I21 Runtime does not retry internally (D9.3).
  - I22 `MaxConcurrentExecutions` enforced by semaphore without queue (A9.1).

- [ ] **Step 3: Commit**

```bash
git add docs/rules.md docs/domain-invariants.md
git commit -m "docs: add hard rules R1..R15 and domain invariants I1..I22"
```

---

## Task 8: ADR template + initial ADRs + skill placeholders + README

**Files:**
- Create: `docs/adr/0000-TEMPLATE.md`, `docs/adr/0001-phase-1-spec-adoption.md`, `docs/adr/0002-go-git-only-no-hooks.md`
- Create: `.claude/skills/<skill>/SKILL.md` for all 11 active skills (A12.1 — NO mailbox-locking-model placeholder)
- Create: `README.md`

- [ ] **Step 1: Write ADR template**

```markdown
# ADR NNNN — <Title>

- **Status:** proposed | accepted | deprecated | superseded-by-NNNN
- **Date:** YYYY-MM-DD
- **Deciders:** <names>
- **Context:**
- **Decision:**
- **Consequences:**
- **Spec references:** Dn.m, An.m, §X
```

- [ ] **Step 2: Write `0001-phase-1-spec-adoption.md`** — Status: `accepted`. Context: summarize why Phase 1 spec was closed. Decision: adopt all `Dn.m` and `An.m` as authoritative. Consequences: contract stability per D2.9.

- [ ] **Step 3: Write `0002-go-git-only-no-hooks.md`** per A8.1. Status: `accepted`. Consequences: Phase-1 `git.commit@v1` does not execute client-side hooks. Documented limitation surfaced in `.claude/skills/git-file-operations/SKILL.md`.

- [ ] **Step 4: Create 11 SKILL.md placeholders**

For each of the 11 skills in §12.6, write a minimal frontmatter-only stub. Example for `architecture-guardrails`:

```markdown
---
name: architecture-guardrails
description: Enforce hexagonal boundaries (domain/ports/adapters); dependency direction one-way; composition only at bootstrap/wire.go.
triggers: [file matches `internal/**/*.go`, task involves adding dependencies, task involves composition]
---

# architecture-guardrails — Phase 1

_(populated in Bundle 8 T68 with compact rules derived from the spec.)_
```

Repeat for the other 10 skills with matching `description` one-liners from §12.6.

- [ ] **Step 5: Write `README.md`**

Sections: Overview (one paragraph quoting §1.4), Status (`Phase 1 — greenfield`), Quick start (deferred to Bundle 8), Links (to spec, rules, invariants, ADRs), License.

- [ ] **Step 6: Commit and tag bundle checkpoint**

```bash
git add docs/adr README.md .claude/skills
git commit -m "docs: add ADR template, ADR-0001/0002, skill placeholders, and README"
git tag bundle-1-complete
```

**Bundle 1 exit criteria:** `make vet`, `make lint`, `git status` clean; tree matches layout; CI workflow valid (try `act pull_request` if installed, else inspect by eye).

---

# Bundle 2 — Domain

Goal: every value object and entity from §4 and §5 constructed with validation, immutable, equal-by-value, with 100% validation test coverage. `ResultNormalizer` is deterministic and testable in isolation. `CapabilityRegistry` populated at `NewCapabilityRegistry()` and rejects unknown keys.

TDD is **mandatory** for this bundle. Write the failing test first, then the minimal code.

## Task 9: Shared IDs (ReceiptID, HandleID, CorrelationID, IdempotencyKey)

**Files:**
- Create: `internal/domain/shared/ids.go`, `internal/domain/shared/ids_test.go`

- [ ] **Step 1: Write failing tests for all four ID types**

Table-driven test in `ids_test.go`:

```go
package shared_test

import (
	"testing"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
)

func TestNewReceiptID(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{"valid ULID", "01HZXK5JC6QK7XV0YQXA0QJ0YZ", false},
		{"empty", "", true},
		{"not ULID", "not-a-ulid", true},
		{"uuid", "7e2b3d2e-3f8c-4c9f-8d4f-7a0d0a4e4b7e", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id, err := shared.NewReceiptID(tc.in)
			if tc.wantErr {
				if err == nil { t.Fatalf("expected error for %q", tc.in) }
				return
			}
			if err != nil { t.Fatalf("unexpected error: %v", err) }
			if id.String() != tc.in { t.Fatalf("want %s got %s", tc.in, id.String()) }
		})
	}
}

func TestIDsDoNotCrossAssign(t *testing.T) {
	// compile-time check; this is essentially a code review comment surfaced as a test
	// by ensuring the types are named and have accessors
	_ = shared.NewHandleID
	_ = shared.NewCorrelationID
	_ = shared.NewIdempotencyKey
}
```

Add analogous tests for `NewHandleID`, `NewCorrelationID`, `NewIdempotencyKey`. Run `go test ./internal/domain/shared/...` — expect failure.

- [ ] **Step 2: Implement `ids.go`**

```go
package shared

import (
	"errors"
	"fmt"
	"regexp"
)

var (
	ErrInvalidID = errors.New("invalid id")
	ulidRegex    = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`)
	uuidRegex    = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
)

type ReceiptID struct{ v string }
type HandleID struct{ v string }
type CorrelationID struct{ v string }
type IdempotencyKey struct{ v string }

func NewReceiptID(s string) (ReceiptID, error)    { return ReceiptID{v: s}, validateULID(s) }
func NewHandleID(s string) (HandleID, error)      { return HandleID{v: s}, validateULID(s) }
func NewCorrelationID(s string) (CorrelationID, error) {
	return CorrelationID{v: s}, validateULID(s)
}
func NewIdempotencyKey(s string) (IdempotencyKey, error) {
	if ulidRegex.MatchString(s) || uuidRegex.MatchString(s) {
		return IdempotencyKey{v: s}, nil
	}
	return IdempotencyKey{}, fmt.Errorf("%w: idempotency_key must be ULID or UUID: %q", ErrInvalidID, s)
}

func (r ReceiptID) String() string      { return r.v }
func (h HandleID) String() string       { return h.v }
func (c CorrelationID) String() string  { return c.v }
func (k IdempotencyKey) String() string { return k.v }

func validateULID(s string) error {
	if !ulidRegex.MatchString(s) {
		return fmt.Errorf("%w: expected 26-char ULID, got %q", ErrInvalidID, s)
	}
	return nil
}
```

- [ ] **Step 3: Run tests green**

Run: `go test -race ./internal/domain/shared/...`. Expect PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/domain/shared/
git commit -m "feat(domain/shared): add nominal ID types (ReceiptID, HandleID, CorrelationID, IdempotencyKey)"
```

---

## Task 10: Clock interface + RealClock

**Files:**
- Create: `internal/domain/shared/clock.go`, `internal/domain/shared/clock_test.go`

- [ ] **Step 1: Write failing test**

```go
package shared_test

import (
	"testing"
	"time"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
)

func TestRealClockReturnsUTC(t *testing.T) {
	c := shared.RealClock{}
	now := c.Now()
	if now.Location() != time.UTC {
		t.Fatalf("expected UTC, got %v", now.Location())
	}
	if time.Since(now) > time.Second {
		t.Fatalf("RealClock.Now() should be current time")
	}
}

func TestFakeClockAdvances(t *testing.T) {
	start := time.Date(2026, 4, 19, 0, 0, 0, 0, time.UTC)
	fc := &shared.FakeClock{T: start}
	if !fc.Now().Equal(start) { t.Fatal("initial time mismatch") }
	fc.Advance(5 * time.Second)
	if !fc.Now().Equal(start.Add(5 * time.Second)) { t.Fatal("advance failed") }
}
```

- [ ] **Step 2: Implement**

```go
package shared

import "time"

type Clock interface { Now() time.Time }

type RealClock struct{}

func (RealClock) Now() time.Time { return time.Now().UTC() }

type FakeClock struct { T time.Time }

func (f *FakeClock) Now() time.Time { return f.T }
func (f *FakeClock) Advance(d time.Duration) { f.T = f.T.Add(d) }
```

- [ ] **Step 3: Verify**

`go test -race ./internal/domain/shared/...` → PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/domain/shared/clock.go internal/domain/shared/clock_test.go
git commit -m "feat(domain/shared): add Clock interface with Real and Fake implementations"
```

---

## Task 11: Closed enums — ExecutionStatus + RetryHint + ErrorClass

**Files:**
- Create: `internal/domain/execution/valueobjects/execution_status.go` + `_test.go`
- Create: `internal/domain/execution/valueobjects/retry_hint.go` + `_test.go`
- Create: `internal/domain/execution/valueobjects/error_class.go` + `_test.go`

- [ ] **Step 1: Write tests covering (a) valid values parse, (b) unknown values rejected, (c) JSON marshal/unmarshal round-trip, (d) `ErrorClass → RetryHint` default mapping per §5.9**

Example for `execution_status_test.go`:

```go
package valueobjects_test

import (
	"encoding/json"
	"testing"

	vo "github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
)

func TestExecutionStatusParse(t *testing.T) {
	want := []string{"success", "failure", "timeout", "cancelled", "partial"}
	for _, s := range want {
		got, err := vo.ParseExecutionStatus(s)
		if err != nil { t.Fatalf("unexpected error for %q: %v", s, err) }
		if got.String() != s { t.Fatalf("round-trip: want %s, got %s", s, got.String()) }
	}
	if _, err := vo.ParseExecutionStatus("unknown"); err == nil {
		t.Fatalf("expected error for unknown status")
	}
}

func TestExecutionStatusJSON(t *testing.T) {
	s, _ := vo.ParseExecutionStatus("partial")
	b, err := json.Marshal(s)
	if err != nil { t.Fatal(err) }
	if string(b) != `"partial"` { t.Fatalf("got %s", string(b)) }
	var back vo.ExecutionStatus
	if err := json.Unmarshal(b, &back); err != nil { t.Fatal(err) }
	if back != s { t.Fatalf("round-trip lost value") }
}
```

For `error_class_test.go`, include a table asserting the §5.9 mapping (e.g., `Timeout → retryable`, `Cancelled → non_retryable`, `ExternalFailure → unknown`, etc.) by calling `ec.DefaultRetryHint()`.

- [ ] **Step 2: Implement each enum as a string-backed nominal type with `Parse*`, `String()`, `MarshalJSON`, `UnmarshalJSON`, and (for `ErrorClass`) `DefaultRetryHint()`**

Pattern for `ExecutionStatus`:

```go
package valueobjects

import (
	"encoding/json"
	"fmt"
)

type ExecutionStatus string

const (
	StatusSuccess   ExecutionStatus = "success"
	StatusFailure   ExecutionStatus = "failure"
	StatusTimeout   ExecutionStatus = "timeout"
	StatusCancelled ExecutionStatus = "cancelled"
	StatusPartial   ExecutionStatus = "partial"
)

var allStatuses = map[ExecutionStatus]struct{}{
	StatusSuccess: {}, StatusFailure: {}, StatusTimeout: {}, StatusCancelled: {}, StatusPartial: {},
}

func ParseExecutionStatus(s string) (ExecutionStatus, error) {
	es := ExecutionStatus(s)
	if _, ok := allStatuses[es]; !ok {
		return "", fmt.Errorf("invalid execution_status %q", s)
	}
	return es, nil
}

func (s ExecutionStatus) String() string { return string(s) }

func (s ExecutionStatus) MarshalJSON() ([]byte, error) { return json.Marshal(string(s)) }

func (s *ExecutionStatus) UnmarshalJSON(b []byte) error {
	var raw string
	if err := json.Unmarshal(b, &raw); err != nil { return err }
	parsed, err := ParseExecutionStatus(raw)
	if err != nil { return err }
	*s = parsed
	return nil
}
```

Apply the **same pattern** to `RetryHint` (values `retryable`, `non_retryable`, `unknown`) and `ErrorClass` (10 values per §4.6). Add to `ErrorClass`:

```go
func (e ErrorClass) DefaultRetryHint() RetryHint {
	switch e {
	case ErrValidationFailure, ErrCapabilityUnknown, ErrPayloadSchemaFailure,
	     ErrPreconditionFailure, ErrCancelled, ErrNormalizationFailure, ErrAdapterInternalError:
		return HintNonRetryable
	case ErrTimeout, ErrInterrupted:
		return HintRetryable
	case ErrExternalFailure:
		return HintUnknown
	default:
		return HintUnknown
	}
}
```

- [ ] **Step 3: Run tests green**

- [ ] **Step 4: Commit**

```bash
git add internal/domain/execution/valueobjects/
git commit -m "feat(domain/vo): add ExecutionStatus, RetryHint, ErrorClass closed enums with default retry mapping"
```

---

## Task 12: AdapterID VO

**Files:**
- Create: `internal/domain/execution/valueobjects/adapter_id.go` + `_test.go`

- [ ] **Step 1: Test regex (D3.6) — valid `shell`, `git`, `filesystem`, `httpreq`; invalid uppercase, too-short, too-long, leading digit**

- [ ] **Step 2: Implement `AdapterID` as struct wrapping validated string**

```go
package valueobjects

import (
	"fmt"
	"regexp"
)

var adapterIDRegex = regexp.MustCompile(`^[a-z][a-z0-9_]{1,31}$`)

type AdapterID struct{ v string }

func NewAdapterID(s string) (AdapterID, error) {
	if !adapterIDRegex.MatchString(s) {
		return AdapterID{}, fmt.Errorf("invalid adapter_id %q: must match ^[a-z][a-z0-9_]{1,31}$", s)
	}
	return AdapterID{v: s}, nil
}

func (a AdapterID) String() string { return a.v }
```

Add JSON methods identical in spirit to the enums' pattern.

- [ ] **Step 3 — 4: Green + commit**

```bash
git commit -m "feat(domain/vo): add AdapterID with regex validation"
```

---

## Task 13: Capability + CapabilityRegistry

**Files:**
- Create: `internal/domain/execution/valueobjects/capability.go` + `_test.go`
- Create: `internal/domain/execution/valueobjects/capability_registry.go` + `_test.go`

- [ ] **Step 1: Capability tests** — construction validates `adapter_id`, `name` (regex `^[a-z][a-z0-9_.]{1,63}$`, D4.2), `version` (regex `^v[0-9]+$`). `Canonical()` returns `adapter.name@version`. Equality is structural.

- [ ] **Step 2: Implement Capability**

```go
type Capability struct {
	adapterID      AdapterID
	name           string
	version        string
	allowsPartial  bool
	defaultTimeout time.Duration
}

var (
	capNameRegex = regexp.MustCompile(`^[a-z][a-z0-9_.]{1,63}$`)
	capVerRegex  = regexp.MustCompile(`^v[0-9]+$`)
)

func NewCapability(aid AdapterID, name, version string, allowsPartial bool, def time.Duration) (Capability, error) {
	if !capNameRegex.MatchString(name) { return Capability{}, fmt.Errorf("invalid capability name %q", name) }
	if !capVerRegex.MatchString(version) { return Capability{}, fmt.Errorf("invalid capability version %q", version) }
	if def <= 0 { return Capability{}, fmt.Errorf("default timeout must be > 0") }
	return Capability{adapterID: aid, name: name, version: version, allowsPartial: allowsPartial, defaultTimeout: def}, nil
}

func (c Capability) AdapterID() AdapterID         { return c.adapterID }
func (c Capability) Name() string                  { return c.name }
func (c Capability) Version() string               { return c.version }
func (c Capability) AllowsPartial() bool           { return c.allowsPartial }
func (c Capability) DefaultTimeout() time.Duration { return c.defaultTimeout }
func (c Capability) Canonical() string             { return c.adapterID.String() + "." + c.name + "@" + c.version }
```

- [ ] **Step 3: CapabilityRegistry tests** — `Register` rejects duplicates (same canonical); `Lookup` returns capability or `ErrCapabilityUnknown`; `List` returns snapshot stable-ordered; `FilterByAdapter` returns subset.

- [ ] **Step 4: Implement `CapabilityRegistry` as an in-memory, construction-only map keyed by canonical string**

```go
type CapabilityRegistry struct { byCanon map[string]Capability }

var ErrCapabilityUnknown = errors.New("capability unknown")

func NewCapabilityRegistry(caps ...Capability) (*CapabilityRegistry, error) {
	r := &CapabilityRegistry{byCanon: map[string]Capability{}}
	for _, c := range caps {
		if _, dup := r.byCanon[c.Canonical()]; dup {
			return nil, fmt.Errorf("duplicate capability: %s", c.Canonical())
		}
		r.byCanon[c.Canonical()] = c
	}
	return r, nil
}

func (r *CapabilityRegistry) Lookup(canon string) (Capability, error) {
	c, ok := r.byCanon[canon]
	if !ok { return Capability{}, fmt.Errorf("%w: %s", ErrCapabilityUnknown, canon) }
	return c, nil
}
// ... List, FilterByAdapter
```

Registry is **write-once at construction**; dynamic registration is explicitly forbidden (D6.6).

- [ ] **Step 5: Commit**

```bash
git commit -m "feat(domain/vo): add Capability and immutable in-memory CapabilityRegistry"
```

---

## Task 14: Payload VO

**Files:**
- Create: `internal/domain/execution/valueobjects/payload.go` + `_test.go`

- [ ] **Step 1: Tests** — construction rejects empty data, non-JSON data, data > `MaxPayloadBytes` (injected); accepts `application/json` only (D5.5); JSON valid via `json.Valid`.

- [ ] **Step 2: Implement**

```go
type Payload struct {
	contentType string
	data        json.RawMessage
}

func NewPayload(contentType string, data json.RawMessage, maxBytes int) (Payload, error) {
	if contentType != "application/json" {
		return Payload{}, fmt.Errorf("unsupported content_type %q (Phase 1: application/json only)", contentType)
	}
	if len(data) == 0 { return Payload{}, fmt.Errorf("payload data is empty") }
	if len(data) > maxBytes { return Payload{}, fmt.Errorf("payload exceeds max %d bytes", maxBytes) }
	if !json.Valid(data) { return Payload{}, fmt.Errorf("payload is not valid JSON") }
	return Payload{contentType: contentType, data: data}, nil
}

func (p Payload) ContentType() string      { return p.contentType }
func (p Payload) Raw() json.RawMessage     { r := make(json.RawMessage, len(p.data)); copy(r, p.data); return r }
```

- [ ] **Step 3 — 4: Green + commit**

```bash
git commit -m "feat(domain/vo): add Payload (application/json only, size-bounded, json-valid)"
```

---

## Task 15: TimeoutBudget VO

**Files:**
- Create: `internal/domain/execution/valueobjects/timeout_budget.go` + `_test.go`

- [ ] **Step 1: Tests** — reject zero / negative; reject > max; accept values in range; `Milliseconds()` returns int64.

- [ ] **Step 2: Implement** with constructor `NewTimeoutBudget(ms int64, max time.Duration) (TimeoutBudget, error)`; wire representation field `timeout_budget_ms` per §5.6.

- [ ] **Step 3 — 4: Green + commit**

---

## Task 16: StreamRef + Artifact + ArtifactType

**Files:**
- Create: `internal/domain/execution/entities/stream_ref.go` + `_test.go`
- Create: `internal/domain/execution/entities/artifact.go` + `_test.go`

- [ ] **Step 1: StreamRef tests** — modes `inline`, `truncated`; truncation when size > `inlineLimit` with `truncated_at_byte` set; JSON encodes `data` as base64 (Go default).

- [ ] **Step 2: Implement StreamRef**

```go
type StreamMode string
const (
	StreamInline    StreamMode = "inline"
	StreamTruncated StreamMode = "truncated"
)

type StreamRef struct {
	Mode             StreamMode `json:"mode"`
	Data             []byte     `json:"data,omitempty"`
	SizeBytes        int64      `json:"size_bytes"`
	TruncatedAtByte  int64      `json:"truncated_at_byte,omitempty"`
}

func NewStreamRef(data []byte, totalSize int64, inlineLimit int) StreamRef {
	if int64(len(data)) <= int64(inlineLimit) && totalSize <= int64(inlineLimit) {
		return StreamRef{Mode: StreamInline, Data: append([]byte(nil), data...), SizeBytes: totalSize}
	}
	return StreamRef{Mode: StreamTruncated, Data: append([]byte(nil), data[:inlineLimit]...), SizeBytes: totalSize, TruncatedAtByte: int64(inlineLimit)}
}
```

- [ ] **Step 3: Artifact + ArtifactType tests** — enum values `{file, directory, git_ref, git_commit, http_response_snapshot}`; construction validates non-empty `uri`, `size_bytes ≥ 0`; `attributes` total ≤ 2 KiB (I17).

- [ ] **Step 4: Implement Artifact**

```go
type ArtifactType string
const (
	ArtifactFile                ArtifactType = "file"
	ArtifactDirectory           ArtifactType = "directory"
	ArtifactGitRef              ArtifactType = "git_ref"
	ArtifactGitCommit           ArtifactType = "git_commit"
	ArtifactHTTPResponseSnapshot ArtifactType = "http_response_snapshot"
)

type Artifact struct {
	Type       ArtifactType       `json:"type"`
	URI        string             `json:"uri"`
	SizeBytes  int64              `json:"size_bytes"`
	Checksum   string             `json:"checksum,omitempty"`
	Attributes map[string]string  `json:"attributes,omitempty"`
}

func NewArtifact(t ArtifactType, uri string, size int64, checksum string, attrs map[string]string) (Artifact, error) {
	// validate enum, non-empty uri, size>=0, attrs total ≤ 2 KiB
	...
}
```

- [ ] **Step 5 — 6: Green + commit**

```bash
git commit -m "feat(domain/entities): add StreamRef (inline+truncated) and Artifact with ArtifactType enum"
```

---

## Task 17: Provenance + Timings VOs

**Files:**
- Create: `internal/domain/execution/entities/provenance.go` + `_test.go`
- Create: `internal/domain/execution/entities/timings.go` + `_test.go`

- [ ] **Step 1: Provenance tests** — `source` ∈ closed enum `{governance, sdk, http, cli, test}` (D5.10); `sourceVersion`, `host`, `runtimeVersion` required; `invocationID` nullable.

- [ ] **Step 2: Implement Provenance** with closed enum `Source` and `NewProvenance(...)` validator.

- [ ] **Step 3: Timings tests** — builder enforces `submitted ≤ started ≤ completed`; `persisted` nullable but if set must be `≥ completed` (§5.11); all UTC ms.

- [ ] **Step 4: Implement Timings with builder**

```go
type Timings struct {
	SubmittedAt time.Time  `json:"submitted_at"`
	StartedAt   time.Time  `json:"started_at"`
	CompletedAt time.Time  `json:"completed_at"`
	PersistedAt *time.Time `json:"persisted_at,omitempty"`
}

type TimingsBuilder struct { clk shared.Clock; t Timings }

func NewTimingsBuilder(clk shared.Clock) *TimingsBuilder { return &TimingsBuilder{clk: clk} }
func (b *TimingsBuilder) MarkSubmitted() *TimingsBuilder { b.t.SubmittedAt = b.clk.Now(); return b }
// ... MarkStarted, MarkCompleted, MarkPersisted

func (b *TimingsBuilder) Build() (Timings, error) {
	if b.t.SubmittedAt.After(b.t.StartedAt) { return Timings{}, fmt.Errorf("invariant: submitted <= started") }
	if b.t.StartedAt.After(b.t.CompletedAt) { return Timings{}, fmt.Errorf("invariant: started <= completed") }
	if b.t.PersistedAt != nil && b.t.PersistedAt.Before(b.t.CompletedAt) {
		return Timings{}, fmt.Errorf("invariant: persisted >= completed")
	}
	return b.t, nil
}
```

- [ ] **Step 5 — 6: Green + commit**

```bash
git commit -m "feat(domain/entities): add Provenance (closed source enum) and Timings (builder with temporal invariants)"
```

---

## Task 18: ExecutionRequest

**Files:**
- Create: `internal/domain/execution/entities/request.go` + `_test.go`

- [ ] **Step 1: Tests** — construction validates per §4.1 (all required fields present, `actor_id`/`task_id`/`workflow_run_id` ≤ 128 chars, `retry_attempt ≥ 0` if set); zero-value invalid; immutable after construction.

- [ ] **Step 2: Implement**

```go
type ExecutionRequest struct {
	correlationID    shared.CorrelationID
	adapterID        vo.AdapterID
	capabilityName   string
	capabilityVersion string
	payload          vo.Payload
	timeoutBudget    vo.TimeoutBudget
	idempotencyKey   *shared.IdempotencyKey
	actorID          string
	taskID           string
	workflowRunID    string
	retryAttempt     int
	submittedAt      time.Time
}

type ExecutionRequestInput struct { /* public fields mirror the VO, used only in constructor */ }

func NewExecutionRequest(in ExecutionRequestInput, clk shared.Clock) (ExecutionRequest, error) {
	// validate, then build
}

func (r ExecutionRequest) CapabilityCanonical() string { return r.adapterID.String() + "." + r.capabilityName + "@" + r.capabilityVersion }
// getters for every field; NO setters
```

JSON marshaling uses snake_case per D5.14. `DisallowUnknownFields` is applied at decode boundary (inbound handlers), not here.

- [ ] **Step 3 — 4: Green + commit**

```bash
git commit -m "feat(domain/entities): add ExecutionRequest (immutable, validated at construction)"
```

---

## Task 19: ExecutionHandle

**Files:**
- Create: `internal/domain/execution/entities/handle.go` + `_test.go`

- [ ] **Step 1: Tests** — construction requires `handle_id`, `correlation_id`, `adapter_id`, `capability`, `started_at`; `GenerateHandle` is deterministic given injected ID generator.

- [ ] **Step 2: Implement + injectable ID generator**

```go
type IDGenerator interface { New() string }

type ULIDGen struct{} // real impl using oklog/ulid

type ExecutionHandle struct {
	HandleID      shared.HandleID
	CorrelationID shared.CorrelationID
	AdapterID     vo.AdapterID
	Capability    vo.Capability
	StartedAt     time.Time
}

func NewExecutionHandle(gen IDGenerator, cid shared.CorrelationID, cap vo.Capability, clk shared.Clock) (ExecutionHandle, error) {
	hid, err := shared.NewHandleID(gen.New())
	if err != nil { return ExecutionHandle{}, err }
	return ExecutionHandle{
		HandleID: hid, CorrelationID: cid, AdapterID: cap.AdapterID(),
		Capability: cap, StartedAt: clk.Now(),
	}, nil
}
```

Add `oklog/ulid/v2` to `go.mod`.

- [ ] **Step 3 — 4: Green + commit**

```bash
git commit -m "feat(domain/entities): add ExecutionHandle with injectable ID generator"
```

---

## Task 20: ExecutionResult

**Files:**
- Create: `internal/domain/execution/entities/result.go` + `_test.go`

- [ ] **Step 1: Tests** — consistency invariants per §5.9 (I7): `success` ⇒ no `error_class`; `timeout` ⇒ `ErrTimeout`; `cancelled` ⇒ `ErrCancelled`; non-success requires non-empty `error_message` and valid `ErrorClass`; `adapter_meta` total ≤ 8 KiB (I18).

- [ ] **Step 2: Implement `NewExecutionResult` with full invariant check**

Signature:
```go
func NewExecutionResult(
	status vo.ExecutionStatus,
	retry vo.RetryHint,
	errClass vo.ErrorClass, // empty when success
	errMsg string,
	stdoutRef, stderrRef *StreamRef,
	exitCode *int,
	artifacts []Artifact,
	adapterMeta map[string]string,
	bytesRead, bytesWritten int64,
	duration time.Duration,
	completedAt time.Time,
) (ExecutionResult, error)
```

Invariant block:
```go
switch status {
case vo.StatusSuccess:
	if errClass != "" || errMsg != "" {
		return ExecutionResult{}, errors.New("success must not carry error_class/error_message")
	}
case vo.StatusTimeout:
	if errClass != vo.ErrTimeout { return ExecutionResult{}, errors.New("timeout must carry ErrTimeout") }
case vo.StatusCancelled:
	if errClass != vo.ErrCancelled { return ExecutionResult{}, errors.New("cancelled must carry ErrCancelled") }
case vo.StatusFailure, vo.StatusPartial:
	if errClass == "" { return ExecutionResult{}, errors.New("failure/partial must carry error_class") }
	if errMsg == "" { return ExecutionResult{}, errors.New("failure/partial must carry error_message") }
default:
	return ExecutionResult{}, fmt.Errorf("unknown status %q", status)
}
if artifacts == nil { artifacts = []Artifact{} }
if adapterMeta == nil { adapterMeta = map[string]string{} }
if totalBytes(adapterMeta) > 8*1024 {
	return ExecutionResult{}, errors.New("adapter_meta total size exceeds 8 KiB")
}
if len(errMsg) > 4096 {
	errMsg = errMsg[:4096]
}
```

- [ ] **Step 3 — 4: Green + commit**

```bash
git commit -m "feat(domain/entities): add ExecutionResult with consistency invariants per §5.9"
```

---

## Task 21: ExecutionReceipt (aggregate root)

**Files:**
- Create: `internal/domain/execution/entities/receipt.go` + `_test.go`

- [ ] **Step 1: Tests** — construction requires all fields per §4.4; `schema_version == "v1"`; appending a second time to the same identity is forbidden (aggregate root immutability D7.5); round-trip JSON is stable.

- [ ] **Step 2: Implement**

```go
type ExecutionReceipt struct {
	ReceiptID     shared.ReceiptID
	SchemaVersion string // always "v1" in Phase 1 (D4.11)
	Request       ExecutionRequest
	Handle        ExecutionHandle
	Result        ExecutionResult
	Provenance    Provenance
	Timings       Timings
	CreatedAt     time.Time
	PersistedAt   *time.Time // nullable; set by persistence adapter
}

func NewExecutionReceipt(
	gen IDGenerator, req ExecutionRequest, h ExecutionHandle,
	res ExecutionResult, prov Provenance, timings Timings, clk shared.Clock,
) (ExecutionReceipt, error) {
	rid, err := shared.NewReceiptID(gen.New())
	if err != nil { return ExecutionReceipt{}, err }
	return ExecutionReceipt{
		ReceiptID: rid, SchemaVersion: "v1", Request: req, Handle: h,
		Result: res, Provenance: prov, Timings: timings, CreatedAt: clk.Now(),
	}, nil
}

func (r ExecutionReceipt) WithPersistedAt(t time.Time) ExecutionReceipt {
	// returns a COPY with PersistedAt set; the original remains immutable
	r.PersistedAt = &t
	return r
}
```

Define a stable JSON marshaling with snake_case via explicit `MarshalJSON`, or tag-based. Prefer tag-based for simplicity; add an `UnmarshalJSON` that uses `json.Decoder` with `DisallowUnknownFields` **only at the boundary** (handlers), not here.

- [ ] **Step 3 — 4: Green + commit**

```bash
git commit -m "feat(domain/entities): add ExecutionReceipt aggregate root (schema v1, PersistedAt via WithPersistedAt copy)"
```

---

## Task 22: Partial-vs-Failure rule helper

**Files:**
- Add helper `internal/domain/execution/entities/partial_rule.go` + `_test.go`

- [ ] **Step 1: Tests** — cover the four AND conditions of D4.6 / §4.5 in a truth table:

| AllowsPartial | artifacts | uncompleted step | reverted | expected |
|---|---|---|---|---|
| false | any | any | any | `failure` |
| true | 0 | yes | no | `failure` |
| true | ≥1 | 0 | n/a | `success` |
| true | ≥1 | yes | yes | `failure` |
| true | ≥1 | yes | no | `partial` |

- [ ] **Step 2: Implement**

```go
type PartialClassification struct {
	AllowsPartial      bool
	HasArtifacts       bool
	HasUncompletedStep bool
	Reverted           bool
}

func (p PartialClassification) Classify() vo.ExecutionStatus {
	if !p.AllowsPartial { return vo.StatusFailure }
	if !p.HasUncompletedStep { return vo.StatusSuccess }
	if !p.HasArtifacts { return vo.StatusFailure }
	if p.Reverted { return vo.StatusFailure }
	return vo.StatusPartial
}
```

- [ ] **Step 3 — 4: Green + commit**

---

## Task 23: ResultNormalizer (domain service)

**Files:**
- Create: `internal/domain/execution/services/result_normalizer.go` + `_test.go`

- [ ] **Step 1: Define `AdapterRawOutcome` marker in outbound port (forward-declare here via interface type)**

Note: the interface itself lives in `internal/ports/outbound/adapter.go` (Task 26), but the **contract** used by the normalizer is a tagged interface. For now we declare a minimal interface inside the normalizer package for testability; it will unify with the port in Bundle 3.

- [ ] **Step 2: Tests — table-driven per `(capability, rawOutcome) → ExecutionResult`**

Cases:
- `shell.exec@v1` + exit 0 → `success`
- `shell.exec@v1` + exit ∈ `exit_success` list → `success`
- `shell.exec@v1` + exit ∉ → `failure` + `ErrExternalFailure`
- `shell.exec@v1` + ctx deadline exceeded → `timeout` + `ErrTimeout`
- `shell.exec@v1` + ctx cancelled → `cancelled` + `ErrCancelled`
- `git.clone@v1` + fetch ok + checkout fail + ≥1 artifact → `partial` + `ErrExternalFailure`
- `git.commit@v1` + atomic fail → `failure`
- `filesystem.write_file@v1` + io error → `failure`
- `http.request@v1` + 2xx → `success`
- `http.request@v1` + 4xx → `failure` + `HintUnknown`
- `http.request@v1` + 5xx → `failure` + `HintRetryable` (adapter-refined override)
- Unknown raw outcome type → `failure` + `ErrNormalizationFailure`

- [ ] **Step 3: Implement normalizer with type-switch per adapter (D8.6)**

```go
type ResultNormalizer struct {
	inlineLimit int
}

func NewResultNormalizer(inlineLimit int) *ResultNormalizer { return &ResultNormalizer{inlineLimit: inlineLimit} }

func (n *ResultNormalizer) Normalize(cap vo.Capability, raw AdapterRawOutcome, clk shared.Clock) (entities.ExecutionResult, error) {
	switch r := raw.(type) {
	case shellRaw:
		return n.normalizeShell(cap, r, clk)
	case gitStatusRaw, gitCloneRaw, gitDiffRaw, gitCommitRaw:
		return n.normalizeGit(cap, r, clk)
	case filesystemReadRaw, filesystemWriteRaw:
		return n.normalizeFilesystem(cap, r, clk)
	case httpRaw:
		return n.normalizeHTTP(cap, r, clk)
	default:
		return entities.NewExecutionResult(
			vo.StatusFailure, vo.HintNonRetryable, vo.ErrNormalizationFailure,
			fmt.Sprintf("unknown raw outcome type: %T", raw),
			nil, nil, nil, nil, nil, 0, 0, 0, clk.Now(),
		)
	}
}
```

**Note:** the raw types (`shellRaw`, etc.) are defined in the adapter packages (Bundle 4). For Bundle 2, declare **placeholder interfaces** in `internal/domain/execution/services/raw_types.go` as `type AdapterRawOutcome interface { adapterRawMarker() }` and implement test doubles in the test file. Adapters in Bundle 4 will register themselves.

**Revised design:** to avoid a circular dependency (domain → adapter), we invert: each adapter registers a `Normalizer` closure with the registry at bootstrap. The `ResultNormalizer` becomes a dispatcher:

```go
type AdapterNormalizer func(cap vo.Capability, raw AdapterRawOutcome, clk shared.Clock) (entities.ExecutionResult, error)

type ResultNormalizer struct {
	inlineLimit  int
	byCanonical  map[string]AdapterNormalizer
}

func (n *ResultNormalizer) Register(capCanonical string, fn AdapterNormalizer) { ... }
func (n *ResultNormalizer) Normalize(cap vo.Capability, raw AdapterRawOutcome, clk shared.Clock) (entities.ExecutionResult, error) {
	fn, ok := n.byCanonical[cap.Canonical()]
	if !ok { return entities.ExecutionResult{}, errors.New("no normalizer registered for "+cap.Canonical()) }
	return fn(cap, raw, clk)
}
```

This keeps the domain free of adapter imports. The adapter-specific normalizer lives next to the adapter (Bundle 4) and is registered in `bootstrap/wire.go` (Bundle 7).

- [ ] **Step 4: Implement + green**

- [ ] **Step 5: Commit**

```bash
git commit -m "feat(domain/services): add ResultNormalizer dispatcher (adapters register per-capability normalizers)"
```

---

## Task 24: Phase-1 capability catalog constants

**Files:**
- Create: `internal/domain/execution/valueobjects/catalog.go` + `_test.go`

- [ ] **Step 1: Tests** — `NewPhase1Capabilities()` returns exactly the 8 capabilities listed in §5.4 with correct `AllowsPartial` and `DefaultTimeout` values. All 8 are valid `Capability` values.

- [ ] **Step 2: Implement**

```go
func NewPhase1Capabilities() ([]Capability, error) {
	shellID, _ := NewAdapterID("shell")
	gitID, _   := NewAdapterID("git")
	fsID, _    := NewAdapterID("filesystem")
	httpID, _  := NewAdapterID("httpreq") // wire name "http.request@v1" still uses subspace
	must := func(c Capability, err error) Capability { if err != nil { panic(err) }; return c }
	return []Capability{
		must(NewCapability(shellID, "exec",        "v1", false, 30*time.Second)),
		must(NewCapability(gitID,   "status",      "v1", false, 10*time.Second)),
		must(NewCapability(gitID,   "clone",       "v1", true,  120*time.Second)),
		must(NewCapability(gitID,   "diff",        "v1", false, 15*time.Second)),
		must(NewCapability(gitID,   "commit",      "v1", false, 15*time.Second)),
		must(NewCapability(fsID,    "read_file",   "v1", false, 5*time.Second)),
		must(NewCapability(fsID,    "write_file",  "v1", false, 10*time.Second)),
		must(NewCapability(httpID,  "request",     "v1", false, 15*time.Second)),
	}, nil
}
```

> **Note on wire names:** the adapter package lives at `internal/adapters/outbound/httpreq/` to avoid shadowing Go's `net/http`, but the `AdapterID` string is `"httpreq"`. The canonical wire form for the HTTP capability is therefore `"httpreq.request@v1"`. If the spec expects literal `"http.request@v1"` on the wire (§5.4 table), override `AdapterID.String()` via an exported mapping, OR rename the adapter folder to `httpadp/`. **Flag this at T1 for user decision; default behavior here is `httpreq.request@v1` on the wire.**

- [ ] **Step 3 — 4: Green + commit**

```bash
git commit -m "feat(domain/vo): add NewPhase1Capabilities() returning the 8 Phase-1 capabilities"
git tag bundle-2-complete
```

**Bundle 2 exit criteria:**
- `make test-unit` passes with `-race`.
- `go tool cover -func=coverage.out | grep 'internal/domain'` shows ≥ 85% per file.
- `go vet ./...` and `golangci-lint run` clean.
- `docs/domain-invariants.md` cross-references are valid (every `I*` has a matching test or constructor assertion).

---

# Bundle 3 — Ports + application services

Goal: inbound + outbound port interfaces locked (§7.1, §7.2). `ExecuteService` implements UC1's 11-step flow (§6.2) and `QueryService` implements UC2 + UC3 (§6.3, §6.4). Full unit coverage via in-memory test doubles for outbound ports.

## Task 25: Outbound ports — Adapter, ReceiptRepository, IdempotencyStore

**Files:**
- Create: `internal/ports/outbound/adapter.go`, `receipt_repository.go`, `idempotency_store.go` + tests for sentinel errors and interface satisfaction via a `_testdouble` subpackage.

- [ ] **Step 1: Define `Adapter` port per D7.4**

```go
package outbound

import (
	"context"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/entities"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
)

type AdapterRawOutcome interface{ adapterRawMarker() }

type Adapter interface {
	ID() valueobjects.AdapterID
	Capabilities() []valueobjects.Capability
	Execute(ctx context.Context, cap valueobjects.Capability, payload valueobjects.Payload) (AdapterRawOutcome, error)
}
```

`error` return is **only** for structural failures (ctx invalid, panic recovered). External failures travel inside `AdapterRawOutcome` (D7.4).

- [ ] **Step 2: Define `ReceiptRepository` port**

```go
type ReceiptRepository interface {
	Save(ctx context.Context, receipt entities.ExecutionReceipt) (entities.ExecutionReceipt, error) // insert-only; returns receipt with PersistedAt set
	FindByID(ctx context.Context, id shared.ReceiptID) (entities.ExecutionReceipt, error)
}

var (
	ErrReceiptNotFound      = errors.New("receipt not found")
	ErrReceiptAlreadyExists = errors.New("receipt already exists")
)
```

- [ ] **Step 3: Define `IdempotencyStore` port**

```go
type IdempotencyStore interface {
	Lookup(ctx context.Context, key shared.IdempotencyKey) (shared.ReceiptID, bool, error) // (id, found, err)
	Record(ctx context.Context, key shared.IdempotencyKey, id shared.ReceiptID, window time.Duration) error
}

var (
	ErrIdempotencyKeyNotFound = errors.New("idempotency key not found")
	ErrIdempotencyKeyConflict = errors.New("idempotency key conflict")
)
```

- [ ] **Step 4: Add in-memory test doubles in `internal/ports/outbound/testdoubles/`**

```go
// InMemoryReceiptRepo — safe for concurrent use via sync.RWMutex
// StubAdapter — returns programmable raw outcomes
// InMemoryIdempotencyStore — with clock-backed expiry
```

Doubles live **under outbound** so application tests use them without importing a separate package.

- [ ] **Step 5: Tests** — interface satisfaction assertions (`var _ Adapter = (*StubAdapter)(nil)`), sentinel errors round-trip through `errors.Is`.

- [ ] **Step 6: Commit**

```bash
git commit -m "feat(ports/outbound): define Adapter, ReceiptRepository, IdempotencyStore with sentinels and test doubles"
```

---

## Task 26: Inbound ports — RuntimeService, QueryService

**Files:**
- Create: `internal/ports/inbound/runtime_service.go`, `query_service.go`

- [ ] **Step 1: Define interfaces per §7.1 + A7.1**

```go
package inbound

type RuntimeService interface {
	Execute(ctx context.Context, req entities.ExecutionRequest) (entities.ExecutionReceipt, error)
}

type CapabilityFilter struct { AdapterID *valueobjects.AdapterID }

type ListCapabilitiesResponse struct {
	Capabilities    []valueobjects.Capability `json:"capabilities"`
	RuntimeVersion  string                     `json:"runtime_version"`
	SchemaVersion   string                     `json:"schema_version"` // "v1"
}

type GetReceiptOptions struct { IncludeStreams bool } // default true

type QueryService interface {
	ListCapabilities(ctx context.Context, f CapabilityFilter) (ListCapabilitiesResponse, error)
	GetReceipt(ctx context.Context, id shared.ReceiptID, opts GetReceiptOptions) (entities.ExecutionReceipt, error)
}
```

- [ ] **Step 2: Add structural / assertion tests** (interface is empty until Bundle 3 task 28 implements).

- [ ] **Step 3: Commit**

```bash
git commit -m "feat(ports/inbound): define RuntimeService (Execute) and QueryService (ListCapabilities, GetReceipt)"
```

---

## Task 27: Concurrency semaphore (A9.1)

**Files:**
- Create: `internal/application/services/concurrency.go` + `_test.go`

- [ ] **Step 1: Tests** — `Acquire` blocks when full; with `TryAcquire`, it **does not block** — returns `ErrTooManyExecutions` immediately (A9.1: no queue, fast-reject). Release restores slot. Concurrency bounded per `MaxConcurrentExecutions`.

- [ ] **Step 2: Implement**

```go
package services

import (
	"context"
	"errors"
	"sync"
)

var ErrTooManyExecutions = errors.New("max concurrent executions reached")

type ConcurrencyLimiter struct {
	slots chan struct{}
}

func NewConcurrencyLimiter(max int) *ConcurrencyLimiter {
	if max <= 0 { panic("ConcurrencyLimiter max must be > 0") }
	return &ConcurrencyLimiter{slots: make(chan struct{}, max)}
}

func (l *ConcurrencyLimiter) TryAcquire() error {
	select {
	case l.slots <- struct{}{}: return nil
	default: return ErrTooManyExecutions
	}
}

func (l *ConcurrencyLimiter) Release() { <-l.slots }
```

- [ ] **Step 3 — 4: Green + commit**

```bash
git commit -m "feat(application): add ConcurrencyLimiter (semaphore, no queue — fast-reject per A9.1)"
```

---

## Task 28: ExecuteService — UC1 11-step flow

**Files:**
- Create: `internal/application/services/execute_service.go` + `_test.go`

The 11 steps per §6.2:
1. Envelope validation (already done by `NewExecutionRequest` at the boundary, but re-validate if needed).
2. Idempotency lookup.
3. Capability resolution.
4. Timeout resolution `min(req.timeout, cap.default, MaxTimeoutBudget)`.
5. Handle generation.
6. Context setup (timeout + cancel + OTel span attrs).
7. `adapter.Execute(ctx, cap, payload)`.
8. Normalize.
9. Assemble receipt.
10. Persist.
11. Return.

- [ ] **Step 1: Tests — 11 independent scenarios, each verifying one branch**

1. Happy path (shell.exec) → success receipt persisted.
2. Idempotency hit → cached receipt returned, adapter NOT called.
3. Capability unknown → failure receipt with `ErrCapabilityUnknown`, persisted, adapter NOT called.
4. Payload overflow → `ErrPayloadSchemaFailure` pre-adapter (validator runs before adapter).
5. Timeout budget < capability minimum (0 / negative) → `ErrValidationFailure`.
6. Ctx cancelled mid-flight → `cancelled` status + `ErrCancelled`.
7. Ctx deadline exceeded → `timeout` + `ErrTimeout`.
8. Adapter panics → recovered, `ErrAdapterInternalError`, panic.stack in `adapter_meta` (A9.1 + D9.6).
9. Adapter raw outcome of unknown type → `ErrNormalizationFailure`.
10. Persistence fails → outer error returned + receipt NOT returned (A4.3).
11. Concurrency limiter full → `ErrTooManyExecutions` surfaced to caller (HTTP 503 in handler).

Use `StubAdapter`, `InMemoryReceiptRepo`, `InMemoryIdempotencyStore`, `FakeClock`, `FakeIDGen`. All scenarios pure unit.

- [ ] **Step 2: Implement**

```go
type ExecuteService struct {
	adapters     map[valueobjects.AdapterID]outbound.Adapter
	registry     *valueobjects.CapabilityRegistry
	normalizer   *services.ResultNormalizer
	receipts     outbound.ReceiptRepository
	idempotency  outbound.IdempotencyStore
	limiter      *ConcurrencyLimiter
	clock        shared.Clock
	idGen        entities.IDGenerator
	maxTimeout   time.Duration
	idempWindow  time.Duration
	provenance   entities.Provenance // baseline, filled per-request
}

func (s *ExecuteService) Execute(ctx context.Context, req entities.ExecutionRequest) (entities.ExecutionReceipt, error) {
	if err := s.limiter.TryAcquire(); err != nil {
		return entities.ExecutionReceipt{}, err
	}
	defer s.limiter.Release()

	// 1. envelope already validated; no-op
	// 2. idempotency
	if key, ok := req.IdempotencyKey(); ok {
		if rid, found, err := s.idempotency.Lookup(ctx, key); err == nil && found {
			return s.receipts.FindByID(ctx, rid)
		}
	}

	// 3. capability
	cap, err := s.registry.Lookup(req.CapabilityCanonical())
	if err != nil {
		return s.persistStructural(ctx, req, vo.ErrCapabilityUnknown, err.Error())
	}

	// 4. timeout
	effective := minDuration(req.TimeoutBudget().Duration(), cap.DefaultTimeout(), s.maxTimeout)
	if effective <= 0 { return s.persistStructural(ctx, req, vo.ErrValidationFailure, "timeout budget invalid") }

	// 5. handle
	handle, err := entities.NewExecutionHandle(s.idGen, req.CorrelationID(), cap, s.clock)
	if err != nil { return entities.ExecutionReceipt{}, err }

	// 6. ctx setup
	execCtx, cancel := context.WithTimeout(ctx, effective)
	defer cancel()

	// 7. adapter
	adapter, ok := s.adapters[cap.AdapterID()]
	if !ok { return s.persistStructural(ctx, req, vo.ErrCapabilityUnknown, "no adapter for "+cap.AdapterID().String()) }

	var raw outbound.AdapterRawOutcome
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				raw = NewPanicOutcome(rec, truncatedStack()) // implements AdapterRawOutcome, maps to ErrAdapterInternalError
			}
		}()
		raw, err = adapter.Execute(execCtx, cap, req.Payload())
	}()

	// 7b. detect ctx outcome
	raw = adjustForContext(execCtx, raw)

	// 8. normalize
	result, nerr := s.normalizer.Normalize(cap, raw, s.clock)
	if nerr != nil {
		result = mustNewNormalizationFailureResult(s.clock, nerr)
	}

	// 9. assemble
	tim, _ := entities.NewTimingsBuilder(s.clock).MarkSubmitted().MarkStarted().MarkCompleted().Build()
	receipt, err := entities.NewExecutionReceipt(s.idGen, req, handle, result, s.provenance, tim, s.clock)
	if err != nil { return entities.ExecutionReceipt{}, err }

	// 10. persist (A4.3)
	saved, err := s.receipts.Save(ctx, receipt)
	if err != nil {
		return entities.ExecutionReceipt{}, fmt.Errorf("persistence failed; side effect may have occurred: %w", err)
	}

	// 10b. idempotency record (best-effort)
	if key, ok := req.IdempotencyKey(); ok {
		_ = s.idempotency.Record(ctx, key, saved.ReceiptID, s.idempWindow)
	}

	// 11. return
	return saved, nil
}
```

The helper `persistStructural` builds a receipt with pre-adapter failure and persists it (receipts-always invariant R13 / I13).

- [ ] **Step 3: Run unit tests — expect all 11 scenarios PASS**

- [ ] **Step 4: Commit**

```bash
git commit -m "feat(application): ExecuteService implements UC1 11-step flow (D6.2) with invariant tests"
```

---

## Task 29: QueryService — UC2 ListCapabilities + UC3 GetReceipt

**Files:**
- Create: `internal/application/services/query_service.go` + `_test.go`

- [ ] **Step 1: Tests**
  - `ListCapabilities(nil filter)` returns all 8.
  - `ListCapabilities(adapter=git)` returns 4 git caps only.
  - `GetReceipt(id, default)` returns receipt with streams.
  - `GetReceipt(id, IncludeStreams=false)` strips `stdout_ref`/`stderr_ref` (A6 / §6.4).
  - `GetReceipt(unknown id)` → `ErrReceiptNotFound`.

- [ ] **Step 2: Implement**

```go
type QueryServiceImpl struct {
	registry       *valueobjects.CapabilityRegistry
	receipts       outbound.ReceiptRepository
	runtimeVersion string
}

func (q *QueryServiceImpl) ListCapabilities(ctx context.Context, f inbound.CapabilityFilter) (inbound.ListCapabilitiesResponse, error) {
	all := q.registry.List()
	if f.AdapterID != nil {
		filtered := make([]vo.Capability, 0, len(all))
		for _, c := range all { if c.AdapterID() == *f.AdapterID { filtered = append(filtered, c) } }
		all = filtered
	}
	return inbound.ListCapabilitiesResponse{
		Capabilities: all, RuntimeVersion: q.runtimeVersion, SchemaVersion: "v1",
	}, nil
}

func (q *QueryServiceImpl) GetReceipt(ctx context.Context, id shared.ReceiptID, opts inbound.GetReceiptOptions) (entities.ExecutionReceipt, error) {
	r, err := q.receipts.FindByID(ctx, id)
	if err != nil { return entities.ExecutionReceipt{}, err }
	if !opts.IncludeStreams {
		r = r.StripStreams() // returns a copy with stdout_ref/stderr_ref = nil
	}
	return r, nil
}
```

Add `StripStreams()` as a copy-returning method on `ExecutionReceipt`.

- [ ] **Step 3 — 4: Green + commit**

```bash
git commit -m "feat(application): QueryService implements ListCapabilities (UC2) and GetReceipt (UC3) with include_streams flag"
```

---

## Task 30: Structural error helpers + panic outcome

**Files:**
- Create: `internal/application/services/structural.go` + `_test.go`

- [ ] **Step 1: Tests** — `panicOutcome` implements `AdapterRawOutcome`; `adjustForContext` flips raw outcome to timeout/cancel when ctx error matches; `persistStructural` produces valid receipts with correct `ErrorClass` per §5.9.

- [ ] **Step 2: Implement `panicOutcome`, `adjustForContext`, `persistStructural`, `mustNewNormalizationFailureResult`**

All three return normalized `ExecutionResult` via the normalizer or direct construction (for cases where normalization itself fails).

- [ ] **Step 3 — 4: Green + commit**

```bash
git commit -m "feat(application): add structural error helpers (panic, ctx adjust, pre-adapter failure receipts)"
```

---

## Task 31: Domain + application coverage gate check

- [ ] **Step 1: Run**

```bash
make test
go tool cover -func=coverage.out | grep -E 'internal/(domain|application)' | awk '{print $1, $3}'
```
Expected: every line shows ≥ 85.0%.

- [ ] **Step 2: If a gap exists**, add targeted tests to lift coverage. Do not change code for coverage's sake.

- [ ] **Step 3: Commit (if tests added) + tag**

```bash
git commit -m "test(application): raise coverage on X" || true
git tag bundle-3-complete
```

**Bundle 3 exit criteria:** coverage gate green; all 11 ExecuteService scenarios pass with `-race`.

---

# Bundle 4 — Adapters (shell, git, filesystem, http)

Four adapter sub-bundles. Each follows the **same anatomy** (§8.1):
1. Per-capability payload + raw-outcome types in `types.go`.
2. Adapter struct implementing `outbound.Adapter`.
3. Per-capability normalizer functions registered with the normalizer at bootstrap.
4. Unit tests (payload validation, execution happy / error / ctx).
5. Integration tests (real resources) behind `-tags=integration`.

The `AdapterContractTestSuite` (T32) is authored once and applied to every adapter.

## Task 32: AdapterContractTestSuite

**Files:**
- Create: `test/contract/adapter_contract_suite.go`

- [ ] **Step 1: Define reusable suite** verifying every `outbound.Adapter` must:
  - `ID()` returns a registered `AdapterID`.
  - `Capabilities()` returns a stable list where each is `Canonical() == <id>.<name>@<version>`.
  - For each capability: an invalid payload returns a non-nil raw outcome of class `ValidationFailure` or `PayloadSchemaFailure` (NOT a Go `error` — D7.4).
  - Canceled context → raw outcome normalizes to `cancelled`.
  - Exceeded deadline → normalizes to `timeout`.
  - Panics do NOT escape (adapter's responsibility to avoid; wrapper adds defense).

- [ ] **Step 2: Expose** `RunContractSuite(t *testing.T, a outbound.Adapter, samples map[string]SamplePayload)` where `SamplePayload` is `{Valid json.RawMessage, Invalid []json.RawMessage}`.

- [ ] **Step 3: Commit**

```bash
git commit -m "test(contract): add reusable AdapterContractTestSuite"
```

---

## Task 33: shell adapter — types + adapter + normalizer

**Files:**
- Create: `internal/adapters/outbound/shell/types.go`, `adapter.go`, `normalize.go`, `safety.go`, `*_test.go`

- [ ] **Step 1: Define payload**

```go
type ExecPayload struct {
	Command     string            `json:"command"`
	Args        []string          `json:"args,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	WorkingDir  string            `json:"working_dir,omitempty"`
	Stdin       []byte            `json:"stdin,omitempty"` // base64 on wire
	ExitSuccess []int             `json:"exit_success,omitempty"` // default [0]
}
```

Plus `execRaw` struct holding `exit_code`, `stdout`, `stderr`, `durationMs`, `ctxErr`, `error`. Implement marker method `adapterRawMarker()`.

- [ ] **Step 2: Safety (per §8.2)**
  - `BaseEnv` — minimal list (`PATH`, `HOME`, `LANG`, `LC_ALL`, `TZ`).
  - `AllowedCommandsPath` config — only commands resolvable in allowlist or absolute path allowed.
  - `AllowedWorkingDirs` config — working_dir must be within allowlist.
  - No shell interpolation: exec via `exec.CommandContext` with args array, never `/bin/sh -c`.

- [ ] **Step 3: Adapter.Execute**

```go
func (a *Adapter) Execute(ctx context.Context, cap vo.Capability, payload vo.Payload) (outbound.AdapterRawOutcome, error) {
	var p ExecPayload
	if err := json.Unmarshal(payload.Raw(), &p); err != nil {
		return execRaw{ctxErr: nil, error: vo.ErrPayloadSchemaFailure, message: err.Error()}, nil
	}
	if err := a.validate(p); err != nil {
		return execRaw{error: vo.ErrValidationFailure, message: err.Error()}, nil
	}
	cmd := exec.CommandContext(ctx, a.resolveCommand(p.Command), p.Args...)
	cmd.Dir = p.WorkingDir
	cmd.Env = a.buildEnv(p.Env)
	if len(p.Stdin) > 0 { cmd.Stdin = bytes.NewReader(p.Stdin) }
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out; cmd.Stderr = &errBuf
	t0 := time.Now()
	err := cmd.Run()
	elapsed := time.Since(t0)
	raw := execRaw{
		exitCode:   cmd.ProcessState.ExitCode(),
		stdout:     out.Bytes(), stderr: errBuf.Bytes(),
		durationMs: elapsed.Milliseconds(),
		ctxErr:     ctx.Err(),
	}
	if err != nil { raw.runErr = err }
	raw.exitSuccess = p.ExitSuccess
	if len(raw.exitSuccess) == 0 { raw.exitSuccess = []int{0} }
	return raw, nil
}
```

- [ ] **Step 4: Normalizer in `normalize.go`**

```go
func (a *Adapter) normalize(cap vo.Capability, raw outbound.AdapterRawOutcome, clk shared.Clock) (entities.ExecutionResult, error) {
	r := raw.(execRaw) // safe — registered only for shell.exec@v1
	// ctx error has priority
	if r.ctxErr != nil {
		if errors.Is(r.ctxErr, context.DeadlineExceeded) {
			return entities.NewExecutionResult(vo.StatusTimeout, vo.HintRetryable, vo.ErrTimeout, "timeout", /* streams+exit+empty artifacts */ ...)
		}
		return entities.NewExecutionResult(vo.StatusCancelled, vo.HintNonRetryable, vo.ErrCancelled, "cancelled", ...)
	}
	// exit
	success := false
	for _, code := range r.exitSuccess { if code == r.exitCode { success = true; break } }
	stdout := entities.NewStreamRefSafe(r.stdout, a.inlineLimit)
	stderr := entities.NewStreamRefSafe(r.stderr, a.inlineLimit)
	if success {
		return entities.NewExecutionResult(vo.StatusSuccess, vo.HintNonRetryable, "", "", &stdout, &stderr, &r.exitCode, nil, nil, int64(len(r.stdout)), int64(len(r.stderr)), time.Duration(r.durationMs)*time.Millisecond, clk.Now())
	}
	return entities.NewExecutionResult(vo.StatusFailure, vo.HintUnknown, vo.ErrExternalFailure, fmt.Sprintf("exit %d not in success list", r.exitCode), &stdout, &stderr, &r.exitCode, nil, nil, int64(len(r.stdout)), int64(len(r.stderr)), time.Duration(r.durationMs)*time.Millisecond, clk.Now())
}
```

**No artifacts ever emitted** (D4.8 / R8).

- [ ] **Step 5: Unit tests**
  - Payload validation covers all fields (empty command, disallowed command, disallowed working_dir, env override of `PATH` rejected).
  - Execute happy path with `echo` (integration tag).
  - Execute with `sleep 10` + ctx timeout → `timeout`.
  - Execute with ctx cancelled → `cancelled`.

- [ ] **Step 6: Integration test (`-tags=integration`)**

File: `adapter_integration_test.go`. Uses real `echo`, `sleep`. Uses `t.TempDir()` for `working_dir`.

- [ ] **Step 7: Apply contract suite**

File: `adapter_contract_test.go`:
```go
func TestShell_ContractSuite(t *testing.T) {
	a := shell.NewAdapter(shell.Config{AllowedCommandsPath: []string{"/bin", "/usr/bin"}, AllowedWorkingDirs: []string{os.TempDir()}, InlineStreamLimit: 16*1024})
	contract.RunContractSuite(t, a, map[string]contract.SamplePayload{
		"shell.exec@v1": {Valid: ..., Invalid: []json.RawMessage{...}},
	})
}
```

- [ ] **Step 8: Commit**

```bash
git commit -m "feat(adapter/shell): implement shell.exec@v1 with allowlist safety, timeout, cancel, normalizer, contract suite"
```

---

## Task 34: git adapter — scaffolding (shared helpers + auth)

**Files:**
- Create: `internal/adapters/outbound/git/types.go`, `adapter.go`, `auth.go`, `safety.go`, `*_test.go`

- [ ] **Step 1: Add go-git dep**

```bash
go get github.com/go-git/go-git/v5
```

- [ ] **Step 2: Define adapter struct + `Execute` dispatcher by capability name**

```go
func (a *Adapter) Execute(ctx context.Context, cap vo.Capability, payload vo.Payload) (outbound.AdapterRawOutcome, error) {
	switch cap.Name() {
	case "status": return a.status(ctx, cap, payload)
	case "clone":  return a.clone(ctx, cap, payload)
	case "diff":   return a.diff(ctx, cap, payload)
	case "commit": return a.commit(ctx, cap, payload)
	default: return gitRaw{err: vo.ErrCapabilityUnknown, msg: cap.Name()}, nil
	}
}
```

- [ ] **Step 3: Auth helper** — SSH agent only (D8.3 / §8.3). No password, no embedded creds.

```go
func buildAuth(mode string) (transport.AuthMethod, error) {
	switch mode {
	case "none", "": return nil, nil
	case "ssh-agent":
		sock := os.Getenv("SSH_AUTH_SOCK")
		if sock == "" { return nil, errors.New("SSH_AUTH_SOCK not set for ssh-agent auth") }
		return ssh.NewSSHAgentAuth("git")
	default:
		return nil, fmt.Errorf("unsupported auth_mode %q (Phase 1: ssh-agent | none)", mode)
	}
}
```

- [ ] **Step 4: Safety helpers** — `destination` must be within `AllowedWorkingDirs` allowlist (§8.6 matrix).

- [ ] **Step 5: Commit**

```bash
git commit -m "feat(adapter/git): scaffold git adapter (dispatcher, SSH-agent-only auth, path allowlist)"
```

---

## Task 35: git.status@v1

**Files:**
- Add `status.go`, `status_test.go` in `internal/adapters/outbound/git/`

- [ ] **Step 1: Payload + raw outcome types**

```go
type StatusPayload struct { RepoPath string `json:"repo_path"` }

type statusRaw struct {
	clean      bool
	branch     string
	head       string
	entries    []gitStatusEntry // path + staged + worktree codes
	durationMs int64
	ctxErr     error
	runErr     error
}
```

- [ ] **Step 2: Implement `status(ctx, cap, payload)` using go-git `repo.Worktree().Status()`**

Read-only; no artifacts (§8.3).

- [ ] **Step 3: Register normalizer** — map `clean` to success with `adapter_meta["branch"]`, `adapter_meta["head"]`, `adapter_meta["entries_count"]`.

- [ ] **Step 4: Unit + integration tests** — integration uses `git.PlainInit(t.TempDir(), false)` + commits via go-git.

- [ ] **Step 5: Commit**

```bash
git commit -m "feat(adapter/git): implement git.status@v1 (read-only, no artifacts)"
```

---

## Task 36: git.clone@v1 (AllowsPartial=true)

**Files:**
- Add `clone.go`, `clone_test.go`

- [ ] **Step 1: Payload**

```go
type ClonePayload struct {
	RepoURL         string `json:"repo_url"`         // https:// or ssh://
	DestinationPath string `json:"destination_path"`
	Ref             string `json:"ref,omitempty"`    // branch / tag / sha
	Depth           int    `json:"depth,omitempty"`
	AuthMode        string `json:"auth_mode,omitempty"` // ssh-agent | none
}
```

- [ ] **Step 2: `cloneRaw` with step-level status**

```go
type cloneRaw struct {
	fetchOK     bool
	checkoutOK  bool
	repoPath    string
	headSHA     string
	refResolved string
	durationMs  int64
	ctxErr      error
	runErr      error
	artifacts   []entities.Artifact
}
```

- [ ] **Step 3: Execute**

1. Validate destination within allowlist.
2. Build auth.
3. `git.PlainCloneContext(ctx, destination, false, &CloneOptions{...})`.
4. If clone returns but HEAD/ref resolution fails, mark `fetchOK=true; checkoutOK=false`.
5. Produce artifacts: `directory` (destination), `git_commit` (HEAD), `git_ref` (resolved ref) when available.

- [ ] **Step 4: Normalizer applies D4.6 partial rule**

```go
class := entities.PartialClassification{
	AllowsPartial: cap.AllowsPartial(),
	HasArtifacts: len(r.artifacts) > 0,
	HasUncompletedStep: !r.checkoutOK,
	Reverted: false, // go-git doesn't revert; mark explicitly
}.Classify()
```

- [ ] **Step 5: Integration tests**
  - On-the-fly local bare repo (`git.PlainInit` + `Push` into a second temp dir bare repo) — no network (A10.1).
  - Successful clone → `success`.
  - Repo with bad ref → `partial` with `directory` + `git_commit` artifacts.
  - Clone URL invalid → `failure` with `ErrExternalFailure`.

- [ ] **Step 6: Commit**

```bash
git commit -m "feat(adapter/git): implement git.clone@v1 with partial support (D4.6, A10.1 on-the-fly fixtures)"
```

---

## Task 37: git.diff@v1

**Files:**
- Add `diff.go`, `diff_test.go`

- [ ] **Step 1: Payload**

```go
type DiffPayload struct {
	RepoPath string `json:"repo_path"`
	From     string `json:"from,omitempty"` // sha or ref; default working tree vs HEAD
	To       string `json:"to,omitempty"`
	MaxBytes int    `json:"max_bytes,omitempty"` // default 1 MiB
}
```

- [ ] **Step 2: Implement via go-git `Worktree.Diff()` or `Commit.Patch(other)` fallback**

Build unified diff string, truncate at `MaxBytes` and set `truncated=true` in `adapter_meta`.

- [ ] **Step 3: Normalizer** — read-only → no artifacts; diff lives in `stdout_ref`.

- [ ] **Step 4: Tests** — unit + integration on on-the-fly repos.

- [ ] **Step 5: Commit**

```bash
git commit -m "feat(adapter/git): implement git.diff@v1 (read-only, truncate at max_bytes)"
```

---

## Task 38: git.commit@v1 (no hooks, atomic)

**Files:**
- Add `commit.go`, `commit_test.go`

- [ ] **Step 1: Payload**

```go
type CommitPayload struct {
	RepoPath      string   `json:"repo_path"`
	Message       string   `json:"message"`
	AuthorName    string   `json:"author_name"`
	AuthorEmail   string   `json:"author_email"`
	Paths         []string `json:"paths,omitempty"`    // stage specific paths; if empty → stage all tracked
	AllowEmpty    bool     `json:"allow_empty,omitempty"`
}
```

- [ ] **Step 2: Implement**

Stage via `Worktree.AddWithOptions(&AddOptions{Path: p})` for each path (or `AddGlob`). Then `Commit(...)` with explicit author. **No hooks** — go-git does not run them.

Artifacts: `git_commit` (SHA) + `git_ref` (updated branch head).

- [ ] **Step 3: Normalizer** — no partial (atomic); success → 2 artifacts; failure → 0 artifacts + `ErrExternalFailure` (or `ErrValidationFailure` for empty commit without `allow_empty`).

- [ ] **Step 4: Tests** — integration verifies artifacts present and HEAD advanced.

- [ ] **Step 5: Document limitation** — update `.claude/skills/git-file-operations/SKILL.md` stub to note "hooks do NOT run in Phase 1 (A8.1)".

- [ ] **Step 6: Commit**

```bash
git commit -m "feat(adapter/git): implement git.commit@v1 atomic, no hooks (A8.1)"
```

---

## Task 39: git adapter contract suite application

**Files:**
- `internal/adapters/outbound/git/adapter_contract_test.go`

- [ ] **Step 1: Apply `RunContractSuite` across all 4 git capabilities**

- [ ] **Step 2: Commit**

```bash
git commit -m "test(adapter/git): apply AdapterContractTestSuite to all 4 git capabilities"
```

---

## Task 40: filesystem adapter — safety helpers

**Files:**
- Create: `internal/adapters/outbound/filesystem/safety.go`, `types.go`, `adapter.go`, `*_test.go`

- [ ] **Step 1: Safety helpers** (§8.4)
  - `AllowedFilesystemRoots` config.
  - Canonicalize path via `filepath.EvalSymlinks` + `filepath.Abs` → reject if outside allowlist.

- [ ] **Step 2: Commit**

```bash
git commit -m "feat(adapter/filesystem): scaffold adapter + path-allowlist + symlink-escape safety"
```

---

## Task 41: filesystem.read_file@v1

**Files:**
- Add `read.go`, `read_test.go`

- [ ] **Step 1: Payload**

```go
type ReadFilePayload struct {
	Path     string `json:"path"`
	MaxBytes int    `json:"max_bytes,omitempty"` // default 1 MiB
}
```

- [ ] **Step 2: Implement**

- Canonicalize + allowlist check.
- `os.Open` + `io.ReadAll` up to `MaxBytes+1`; if exceeds, mark truncate.
- Raw outcome carries bytes + truncate flag + size.

- [ ] **Step 3: Normalizer** — content in `stdout_ref`; no artifacts.

- [ ] **Step 4: Unit + integration tests** (use `t.TempDir()`).

- [ ] **Step 5: Commit**

```bash
git commit -m "feat(adapter/filesystem): implement filesystem.read_file@v1 with truncate at max_bytes"
```

---

## Task 42: filesystem.write_file@v1 (atomic tmp+rename)

**Files:**
- Add `write.go`, `write_test.go`

- [ ] **Step 1: Payload**

```go
type WriteFilePayload struct {
	Path       string `json:"path"`
	Data       []byte `json:"data"` // base64 on wire
	Overwrite  bool   `json:"overwrite,omitempty"` // default false
	CreateDirs bool   `json:"create_dirs,omitempty"`
	Mode       uint32 `json:"mode,omitempty"` // default 0644
}
```

- [ ] **Step 2: Implement (atomic — §8.4 / D8.4)**

1. Canonicalize target dir + allowlist check.
2. If `CreateDirs`, `os.MkdirAll(dir, 0755)`.
3. If target exists and `!Overwrite` → `ErrPreconditionFailure`.
4. Write to `path.tmp.<random>`, `fsync`, `os.Rename` → atomic.
5. Artifact: `file` with `size_bytes`, `checksum` (sha256 hex), `attributes: {mode}`.

- [ ] **Step 3: Normalizer** — `success` + 1 artifact on clean; `failure` otherwise. Never partial (D8.4).

- [ ] **Step 4: Tests** — verify atomicity via a simulated mid-write failure (inject error after rename); verify overwrite rejection.

- [ ] **Step 5: Commit**

```bash
git commit -m "feat(adapter/filesystem): implement filesystem.write_file@v1 (atomic tmp+fsync+rename, no partial)"
```

---

## Task 43: filesystem contract suite application

**Files:**
- `internal/adapters/outbound/filesystem/adapter_contract_test.go`

- [ ] **Step 1 — 2: Apply + commit**

```bash
git commit -m "test(adapter/filesystem): apply AdapterContractTestSuite"
```

---

## Task 44: http adapter — SSRF helpers

**Files:**
- Create: `internal/adapters/outbound/httpreq/ssrf.go`, `types.go`, `adapter.go`, `*_test.go`

- [ ] **Step 1: SSRF helpers** (§8.5)
  - Resolve host via `net.LookupIP`; reject if any IP is in private ranges (10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, 127.0.0.0/8, 169.254.0.0/16, ::1, fe80::/10, fc00::/7, link-local).
  - Allow override via config `HTTPAllowPrivateNetworks=true` (A8.3).
  - Enforce scheme `http` or `https` only.

- [ ] **Step 2: Tests** — table-driven of IP ranges (both allow and block scenarios).

- [ ] **Step 3: Commit**

```bash
git commit -m "feat(adapter/httpreq): add SSRF helper (private-IP block by default, configurable override)"
```

---

## Task 45: http.request@v1

**Files:**
- Add to `adapter.go`, `execute.go`, `*_test.go`

- [ ] **Step 1: Payload**

```go
type RequestPayload struct {
	Method          string            `json:"method"`  // GET/POST/...
	URL             string            `json:"url"`
	Headers         map[string]string `json:"headers,omitempty"`
	Body            []byte            `json:"body,omitempty"` // base64
	ExpectedStatus  []int             `json:"expected_status,omitempty"` // default 200..299
	MaxBodyBytes    int               `json:"max_body_bytes,omitempty"`  // default 10 MiB
	MaxRedirects    int               `json:"max_redirects,omitempty"`   // default 5
}
```

- [ ] **Step 2: Implement**

- Enforce SSRF pre-check.
- `http.Client{Timeout: ctxDeadline - now}`; transport with `CheckRedirect` capped at 5 (per §8.5).
- Mandatory TLS verify (§8.5 — no `InsecureSkipVerify` in Phase 1).
- Read body capped at `MaxBodyBytes`; if exceeds, truncate + flag.
- Raw outcome: status, headers, body (truncated), durationMs, ctxErr, runErr.

- [ ] **Step 3: Normalizer** (§8.5 status mapping)

- `expected_status` present: if status in → `success`; else → `failure`.
- No `expected_status`: 2xx → `success`; 3xx not followed past max → `failure`; 4xx → `failure` + `HintUnknown`; 5xx → `failure` + `HintRetryable` (adapter refinement).
- `stdout_ref` = response body; `stderr_ref` = response headers serialized JSON line.
- Artifact (optional, per §4.8): `http_response_snapshot` if opt-in via payload `snapshot: true`.

- [ ] **Step 4: Tests** — use `httptest.NewServer` for unit scenarios (2xx, 4xx, 5xx, redirect, timeout, private IP block).

- [ ] **Step 5: Contract suite**

- [ ] **Step 6: Commit**

```bash
git commit -m "feat(adapter/httpreq): implement http.request@v1 (strict SSRF, TLS verify, redirect cap, status mapping)"
```

---

## Task 46: Adapter registration helper

**Files:**
- Create: `internal/adapters/outbound/registration.go`

- [ ] **Step 1: Provide a convenience function** consumed by `bootstrap/wire.go`:

```go
func RegisterAllPhase1(
	normalizer *services.ResultNormalizer,
	cfg config.Config,
	clk shared.Clock,
) (map[vo.AdapterID]outbound.Adapter, error) {
	s := shell.NewAdapter(cfg.Shell)
	g := git.NewAdapter(cfg.Git)
	f := filesystem.NewAdapter(cfg.Filesystem)
	h := httpreq.NewAdapter(cfg.HTTP)

	// Register normalizers
	normalizer.Register("shell.exec@v1",            s.Normalize)
	normalizer.Register("git.status@v1",            g.NormalizeStatus)
	normalizer.Register("git.clone@v1",             g.NormalizeClone)
	normalizer.Register("git.diff@v1",              g.NormalizeDiff)
	normalizer.Register("git.commit@v1",            g.NormalizeCommit)
	normalizer.Register("filesystem.read_file@v1",  f.NormalizeRead)
	normalizer.Register("filesystem.write_file@v1", f.NormalizeWrite)
	normalizer.Register("httpreq.request@v1",       h.Normalize)

	return map[vo.AdapterID]outbound.Adapter{
		s.ID(): s, g.ID(): g, f.ID(): f, h.ID(): h,
	}, nil
}
```

- [ ] **Step 2: Commit + tag**

```bash
git commit -m "feat(adapters): add RegisterAllPhase1 helper for bootstrap wiring"
git tag bundle-4-complete
```

**Bundle 4 exit criteria:** every adapter passes its contract-suite tests; integration tests under `-tags=integration` pass locally.

---

# Bundle 5 — Persistence (PostgreSQL)

## Task 47: PG schema + migrations

**Files:**
- Create: `internal/adapters/outbound/pg/migrations/0001_receipts.up.sql` + `down.sql`
- Create: `internal/adapters/outbound/pg/migrations/0002_idempotency.up.sql` + `down.sql`
- Add: `internal/adapters/outbound/pg/migrations/embed.go` (use `//go:embed`)

- [ ] **Step 1: Author `0001_receipts.up.sql`**

```sql
CREATE TABLE execution_receipts (
    receipt_id        TEXT PRIMARY KEY,
    schema_version    TEXT NOT NULL,
    correlation_id    TEXT NOT NULL,
    adapter_id        TEXT NOT NULL,
    capability        TEXT NOT NULL,
    status            TEXT NOT NULL,
    error_class       TEXT,
    retry_hint        TEXT NOT NULL,
    submitted_at      TIMESTAMPTZ NOT NULL,
    started_at        TIMESTAMPTZ NOT NULL,
    completed_at      TIMESTAMPTZ NOT NULL,
    persisted_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    duration_ms       BIGINT NOT NULL,
    payload           JSONB NOT NULL,
    result            JSONB NOT NULL,
    provenance        JSONB NOT NULL,
    full_receipt      JSONB NOT NULL
);
CREATE INDEX idx_receipts_correlation   ON execution_receipts(correlation_id);
CREATE INDEX idx_receipts_capability    ON execution_receipts(capability);
CREATE INDEX idx_receipts_completed_at  ON execution_receipts(completed_at DESC);
```

- [ ] **Step 2: Author `0002_idempotency.up.sql`**

```sql
CREATE TABLE idempotency_keys (
    key        TEXT PRIMARY KEY,
    receipt_id TEXT NOT NULL REFERENCES execution_receipts(receipt_id),
    expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_idempotency_expires ON idempotency_keys(expires_at);
```

- [ ] **Step 3: Author down migrations** — drop tables in reverse order.

- [ ] **Step 4: Embed migrations**

```go
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
```

- [ ] **Step 5: Commit**

```bash
git commit -m "feat(pg): add 0001/0002 migrations for receipts + idempotency_keys"
```

---

## Task 48: ReceiptRepositoryPG

**Files:**
- Create: `internal/adapters/outbound/pg/receipt_repository.go` + `_test.go`

- [ ] **Step 1: Tests (integration, `-tags=integration`)** — use `testcontainers-go/postgres`.
  - `Save` inserts + returns receipt with `PersistedAt` populated.
  - Duplicate `receipt_id` → `ErrReceiptAlreadyExists`.
  - `FindByID` round-trip preserves the full receipt byte-for-byte (via `full_receipt JSONB`).

- [ ] **Step 2: Implement**

```go
type ReceiptRepositoryPG struct { pool *pgxpool.Pool; clk shared.Clock }

func (r *ReceiptRepositoryPG) Save(ctx context.Context, rec entities.ExecutionReceipt) (entities.ExecutionReceipt, error) {
	fullJSON, _ := json.Marshal(rec)
	payloadJSON := rec.Request.Payload().Raw()
	resultJSON, _ := json.Marshal(rec.Result)
	provJSON, _ := json.Marshal(rec.Provenance)
	now := r.clk.Now()
	_, err := r.pool.Exec(ctx, `
		INSERT INTO execution_receipts (
			receipt_id, schema_version, correlation_id, adapter_id, capability, status,
			error_class, retry_hint, submitted_at, started_at, completed_at, persisted_at,
			duration_ms, payload, result, provenance, full_receipt
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
	`, rec.ReceiptID.String(), rec.SchemaVersion, rec.Request.CorrelationID().String(),
	   rec.Request.CapabilityCanonical(), /* ... */ now, /* ... */ fullJSON)
	if err != nil {
		if isUniqueViolation(err) {
			return entities.ExecutionReceipt{}, outbound.ErrReceiptAlreadyExists
		}
		return entities.ExecutionReceipt{}, err
	}
	return rec.WithPersistedAt(now), nil
}
```

- [ ] **Step 3: Commit**

```bash
git commit -m "feat(pg): ReceiptRepositoryPG (insert-only, ErrReceiptAlreadyExists on duplicate)"
```

---

## Task 49: IdempotencyStorePG

**Files:**
- Create: `internal/adapters/outbound/pg/idempotency_store.go` + `_test.go`

- [ ] **Step 1: Tests (integration)** — Lookup within window returns hit; Lookup after `expires_at` < now returns miss; `Record` inserts with upsert semantics (second record for same key within window → `ErrIdempotencyKeyConflict`).

- [ ] **Step 2: Implement**

```go
func (s *IdempotencyStorePG) Lookup(ctx context.Context, key shared.IdempotencyKey) (shared.ReceiptID, bool, error) {
	row := s.pool.QueryRow(ctx, `SELECT receipt_id FROM idempotency_keys WHERE key=$1 AND expires_at > NOW()`, key.String())
	var rid string
	if err := row.Scan(&rid); err != nil {
		if errors.Is(err, pgx.ErrNoRows) { return shared.ReceiptID{}, false, nil }
		return shared.ReceiptID{}, false, err
	}
	r, _ := shared.NewReceiptID(rid)
	return r, true, nil
}

func (s *IdempotencyStorePG) Record(ctx context.Context, key shared.IdempotencyKey, id shared.ReceiptID, window time.Duration) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO idempotency_keys(key, receipt_id, expires_at)
		VALUES ($1, $2, NOW() + $3::interval)
		ON CONFLICT (key) DO NOTHING
	`, key.String(), id.String(), fmt.Sprintf("%d seconds", int(window.Seconds())))
	return err
}
```

- [ ] **Step 3: Commit**

```bash
git commit -m "feat(pg): IdempotencyStorePG (window-based expiry, record upsert, conflict sentinel)"
```

---

## Task 50: Migration runner helper + integration harness

**Files:**
- Create: `internal/adapters/outbound/pg/migrate.go`

- [ ] **Step 1: Implement migration runner** using `golang-migrate/migrate/v4` + `iofs` source from the embedded FS.

- [ ] **Step 2: Integration test harness** in `internal/adapters/outbound/pg/harness_test.go`:

```go
//go:build integration

func startPG(t *testing.T) (*pgxpool.Pool, func()) {
	ctx := context.Background()
	c, err := postgres.RunContainer(ctx,
		postgres.WithDatabase("runtime_adapters"),
		postgres.WithUsername("test"), postgres.WithPassword("test"),
		testcontainers.WithImage("postgres:15-alpine"),
	)
	// ... build dsn, pool, run migrations, return pool + teardown fn
}
```

- [ ] **Step 3: Commit + tag**

```bash
git commit -m "feat(pg): add migration runner + testcontainers integration harness"
git tag bundle-5-complete
```

**Bundle 5 exit criteria:** `make test-integration` green locally.

---

# Bundle 6 — Inbound HTTP + SDK

## Task 51: HTTP router + error envelope

**Files:**
- Create: `internal/adapters/inbound/http/router.go`, `middleware.go`, `errors.go`

- [ ] **Step 1: Add chi**

```bash
go get github.com/go-chi/chi/v5
```

- [ ] **Step 2: Implement router**

```go
func NewRouter(svc inbound.RuntimeService, q inbound.QueryService) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.Recoverer, OTelMiddleware)
	r.Post("/api/v1/execute", executeHandler(svc))
	r.Get("/api/v1/capabilities", capabilitiesHandler(q))
	r.Get("/api/v1/receipts/{id}", receiptHandler(q))
	return r
}
```

- [ ] **Step 3: Error envelope** (consistent JSON shape)

```go
type HTTPError struct {
	Code       int    `json:"-"`
	Class      string `json:"error_class"`
	Message    string `json:"error_message"`
	Retryable  string `json:"retryable"`   // "retryable" | "non_retryable" | "unknown"
	ReceiptID  string `json:"receipt_id,omitempty"`
}
```

Structural failures (invalid JSON, unknown capability, overloaded) return an `HTTPError`. When a receipt exists (application/execute produced one even for failure), include `receipt_id` and return 200 with the receipt body — the receipt itself communicates failure.

- [ ] **Step 4: Commit**

```bash
git commit -m "feat(http): router skeleton with chi + error envelope + panic-recover middleware"
```

---

## Task 52: POST /api/v1/execute

**Files:**
- Create: `internal/adapters/inbound/http/execute_handler.go` + `_test.go`

- [ ] **Step 1: Tests** — table-driven:
  - Valid request → 200 + receipt JSON (HTTP==SDK).
  - Unknown capability → 200 + receipt with `status=failure`, `error_class=capability_unknown`.
  - Invalid JSON (missing required field) → 400 + `HTTPError{ValidationFailure}`.
  - Body > `MaxPayloadBytes` → 413 + `PayloadSchemaFailure`.
  - Concurrent burst beyond `MaxConcurrentExecutions` → 503 + `ErrTooManyExecutions`.

- [ ] **Step 2: Implement decoder with `DisallowUnknownFields` (D5.14)**

```go
func executeHandler(svc inbound.RuntimeService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, int64(MaxPayloadBytes+4096)))
		dec.DisallowUnknownFields()
		var wire wireRequest
		if err := dec.Decode(&wire); err != nil {
			writeHTTPError(w, 400, "validation_failure", err.Error())
			return
		}
		req, err := wire.toDomain(realClock)
		if err != nil {
			writeHTTPError(w, 400, "validation_failure", err.Error())
			return
		}
		receipt, err := svc.Execute(r.Context(), req)
		if errors.Is(err, services.ErrTooManyExecutions) {
			writeHTTPError(w, 503, "adapter_internal_error", err.Error())
			return
		}
		if err != nil {
			writeHTTPError(w, 500, "adapter_internal_error", err.Error())
			return
		}
		writeJSON(w, 200, receipt)
	}
}
```

- [ ] **Step 3: Commit**

```bash
git commit -m "feat(http): POST /api/v1/execute (DisallowUnknownFields, size cap, 503 on overload)"
```

---

## Task 53: GET /api/v1/capabilities + GET /api/v1/receipts/{id}

**Files:**
- Create: `capabilities_handler.go`, `receipts_handler.go` + tests

- [ ] **Step 1: Tests** — `?adapter_id=git` filters; `?include_streams=false` strips; 404 on missing.

- [ ] **Step 2: Implement** standard handler patterns.

- [ ] **Step 3: Commit**

```bash
git commit -m "feat(http): GET /capabilities (filter by adapter_id) and GET /receipts/{id} (include_streams flag)"
```

---

## Task 54: In-proc Go SDK

**Files:**
- Create: `internal/adapters/inbound/sdk/sdk.go` + `_test.go`

- [ ] **Step 1: SDK is a thin wrapper around the inbound interfaces**

```go
package sdk

type Client struct {
	runtime inbound.RuntimeService
	query   inbound.QueryService
}

func NewClient(runtime inbound.RuntimeService, query inbound.QueryService) *Client { return &Client{runtime, query} }

func (c *Client) Execute(ctx context.Context, in ExecuteInput) (entities.ExecutionReceipt, error) {
	req, err := in.toDomain(realClock)
	if err != nil { return entities.ExecutionReceipt{}, err }
	return c.runtime.Execute(ctx, req)
}

func (c *Client) ListCapabilities(ctx context.Context, filter ListCapabilitiesFilter) (inbound.ListCapabilitiesResponse, error) {
	return c.query.ListCapabilities(ctx, filter.toDomain())
}

func (c *Client) GetReceipt(ctx context.Context, id string, opts GetReceiptOptions) (entities.ExecutionReceipt, error) {
	rid, err := shared.NewReceiptID(id)
	if err != nil { return entities.ExecutionReceipt{}, err }
	return c.query.GetReceipt(ctx, rid, opts.toDomain())
}
```

`ExecuteInput`/`ListCapabilitiesFilter`/`GetReceiptOptions` are plain Go structs (not domain VOs) so SDK callers aren't forced into VO constructors.

- [ ] **Step 2: Commit**

```bash
git commit -m "feat(sdk): in-proc Go SDK wrapping RuntimeService + QueryService"
```

---

## Task 55: HTTP ≡ SDK contract test

**Files:**
- Create: `test/contract/http_sdk_equivalence_test.go`

- [ ] **Step 1: Test** — for the same `ExecuteInput`, invoking via HTTP (against an `httptest.NewServer`) and via SDK returns **byte-identical JSON** (modulo `receipt_id`, `handle_id`, timestamps) — diff via a normalizer that zeroes out variable fields.

- [ ] **Step 2: Commit + tag**

```bash
git commit -m "test(contract): HTTP ≡ SDK equivalence test with variable-field normalization"
git tag bundle-6-complete
```

**Bundle 6 exit criteria:** HTTP+SDK contract test green; all three endpoints respond correctly; `make test-contract` green.

---

# Bundle 7 — Bootstrap + observability + graceful shutdown

## Task 56: Configuration loader

**Files:**
- Create: `internal/infrastructure/config/config.go`, `load.go` + `_test.go`

- [ ] **Step 1: Config struct mirrors §9.8 table**

```go
type Config struct {
	HTTPAddr                string        // default ":8080"
	MaxTimeoutBudget        time.Duration // default 30m (A4.1)
	MaxPayloadBytes         int           // default 1 MiB
	InlineStreamLimit       int           // default 16 KiB
	IdempotencyWindow       time.Duration // default 24h (A2.2)
	ShutdownGracePeriod     time.Duration // default 30s (D9.7)
	MaxConcurrentExecutions int           // default 64 (A9.1)

	Shell      ShellConfig
	Git        GitConfig
	Filesystem FilesystemConfig
	HTTP       HTTPConfig

	Postgres PGConfig

	OTelEnabled          bool
	OTelServiceName      string
	OTelExporterEndpoint string
	OTelTracesSampler    string // default "parentbased_traceidratio"
	OTelTracesSamplerArg float64 // default 0.1
}
```

- [ ] **Step 2: Load from env** with explicit type parsing and defaults; unknown env vars don't cause failure but are logged.

- [ ] **Step 3: Validation at load time** — allowlists non-empty if corresponding adapter enabled; PG DSN parseable.

- [ ] **Step 4: Tests** table-driven defaults + overrides.

- [ ] **Step 5: Commit**

```bash
git commit -m "feat(config): env-driven configuration with defaults mirroring §9.8"
```

---

## Task 57: OTel setup

**Files:**
- Create: `internal/infrastructure/obs/otel.go`, `metrics.go` + `_test.go`

- [ ] **Step 1: OTel providers**
  - Trace provider with OTLP exporter (optional — skip when `OTelEnabled=false`).
  - Metric provider with OTLP exporter.
  - Sampler respects `OTEL_TRACES_SAMPLER` + `OTEL_TRACES_SAMPLER_ARG` env (§9.7).

- [ ] **Step 2: Instrument registry** (`metrics.go`)
  - Counters: `execute_attempted_total`, `timeout_total`, `cancelled_total`, `panics_recovered_total`, `idempotency_hit_total`, `idempotency_miss_total`.
  - Histograms: `execute_duration_ms`, `persist_duration_ms`, `adapter_<id>_execute_duration_ms`.
  - Gauges: `bytes_read_total`, `bytes_written_total`.

- [ ] **Step 3: Tests** — construction smoke; verify no-op providers when disabled.

- [ ] **Step 4: Commit**

```bash
git commit -m "feat(obs): OTel providers + metric registry (sampler via env)"
```

---

## Task 58: Bootstrap wire.go

**Files:**
- Create: `internal/bootstrap/wire.go` + `_test.go`

- [ ] **Step 1: Composition** — sole place that imports concrete adapters.

```go
func BuildRuntime(ctx context.Context, cfg config.Config) (*Runtime, error) {
	clk := shared.RealClock{}
	idGen := entities.ULIDGen{}

	pool, err := pgxpool.New(ctx, cfg.Postgres.DSN)
	if err != nil { return nil, err }
	if err := pg.Migrate(ctx, pool); err != nil { return nil, err }

	receiptRepo := &pg.ReceiptRepositoryPG{Pool: pool, Clk: clk}
	idempStore  := &pg.IdempotencyStorePG{Pool: pool}

	caps, _ := vo.NewPhase1Capabilities()
	registry, _ := vo.NewCapabilityRegistry(caps...)
	normalizer := services.NewResultNormalizer(cfg.InlineStreamLimit)

	adapters, err := outboundreg.RegisterAllPhase1(normalizer, cfg, clk)
	if err != nil { return nil, err }

	limiter := appsvc.NewConcurrencyLimiter(cfg.MaxConcurrentExecutions)
	prov, _ := entities.NewProvenance("http", runtimeVersion, hostname(), runtimeVersion, nil)

	execSvc := &appsvc.ExecuteService{
		Adapters: adapters, Registry: registry, Normalizer: normalizer,
		Receipts: receiptRepo, Idempotency: idempStore, Limiter: limiter,
		Clock: clk, IDGen: idGen, MaxTimeout: cfg.MaxTimeoutBudget,
		IdempWindow: cfg.IdempotencyWindow, Provenance: prov,
	}
	querySvc := &appsvc.QueryServiceImpl{Registry: registry, Receipts: receiptRepo, RuntimeVersion: runtimeVersion}

	router := httpin.NewRouter(execSvc, querySvc)

	return &Runtime{Server: &http.Server{Addr: cfg.HTTPAddr, Handler: router}, Pool: pool}, nil
}
```

- [ ] **Step 2: Test** — wire fresh with in-memory doubles; verify `.Server.Handler` not nil.

- [ ] **Step 3: Commit**

```bash
git commit -m "feat(bootstrap): wire.go — sole composition point (D7.9)"
```

---

## Task 59: cmd/runtime-adapters/main.go

**Files:**
- Create: `cmd/runtime-adapters/main.go`

- [ ] **Step 1: main**

```go
func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	cfg, err := config.Load()
	if err != nil { log.Fatal(err) }

	shutdown, err := obs.SetupOTel(ctx, cfg)
	if err != nil { log.Fatal(err) }
	defer shutdown(context.Background())

	rt, err := bootstrap.BuildRuntime(ctx, cfg)
	if err != nil { log.Fatal(err) }

	errCh := make(chan error, 1)
	go func() { errCh <- rt.Server.ListenAndServe() }()

	select {
	case <-ctx.Done():
		log.Println("shutdown signal received; draining for", cfg.ShutdownGracePeriod)
		drainCtx, drainCancel := context.WithTimeout(context.Background(), cfg.ShutdownGracePeriod)
		defer drainCancel()
		_ = rt.Server.Shutdown(drainCtx)
	case err := <-errCh:
		log.Fatal(err)
	}
}
```

- [ ] **Step 2: Commit**

```bash
git commit -m "feat(cmd): main entrypoint with signal-driven graceful shutdown (30s grace)"
```

---

## Task 60: Graceful shutdown correctness test

**Files:**
- Add: `internal/bootstrap/shutdown_test.go`

- [ ] **Step 1: Test** — start server in a goroutine against `httptest`; send SIGTERM; verify `ListenAndServe` returns within `ShutdownGracePeriod`; verify in-flight executions are persisted before exit.

- [ ] **Step 2: Commit**

```bash
git commit -m "test(bootstrap): graceful shutdown persists in-flight receipts within grace period"
```

---

## Task 61: Panic recovery correctness test

**Files:**
- Add to `execute_service_test.go` or new `panic_recovery_test.go`.

- [ ] **Step 1: Test** — adapter that deliberately panics → receipt still produced with `ErrAdapterInternalError` and `adapter_meta["panic.stack"]` truncated at 4 KiB. Runtime did not crash.

- [ ] **Step 2: Commit + tag**

```bash
git commit -m "test: adapter panic is recovered, producing AdapterInternalError receipt (D9.6)"
git tag bundle-7-complete
```

**Bundle 7 exit criteria:** `make run` boots the binary, `curl` against `/api/v1/capabilities` returns 8 capabilities, SIGTERM triggers clean shutdown.

---

# Bundle 8 — E2E + docs + v0.1.0

## Task 62: E2E smoke — happy path

**Files:**
- Create: `test/e2e/happy_path_test.go`

- [ ] **Step 1: Test** under `-tags=e2e`:
  1. Start testcontainer PG.
  2. Build the full runtime via `bootstrap.BuildRuntime`.
  3. POST `/api/v1/execute` with `shell.exec@v1` running `echo hello`.
  4. Assert receipt: status=success, `stdout_ref` contains `hello\n`, `exit_code==0`, `persisted_at != null`.
  5. GET `/api/v1/receipts/{id}` returns the same receipt.

- [ ] **Step 2: Commit**

```bash
git commit -m "test(e2e): happy path smoke — shell.exec end-to-end"
```

---

## Task 63: E2E smoke — git.clone partial

**Files:**
- Create: `test/e2e/git_clone_partial_test.go`

- [ ] **Step 1: Test** — build a local bare repo, clone with a bogus `ref`, verify receipt status=partial with ≥1 artifact (directory) and `error_class=ExternalFailure`.

- [ ] **Step 2: Commit**

```bash
git commit -m "test(e2e): git.clone partial scenario end-to-end"
```

---

## Task 64: E2E smoke — idempotency replay

**Files:**
- Create: `test/e2e/idempotency_replay_test.go`

- [ ] **Step 1: Test** — POST `/execute` with `idempotency_key` twice in quick succession; second response has same `receipt_id`; adapter invoked only once (check via counter metric).

- [ ] **Step 2: Commit**

```bash
git commit -m "test(e2e): idempotency replay-everything scenario"
```

---

## Task 65: Finalize README quickstart

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Add sections**

- Prerequisites: Go 1.22+, Docker (for PG), golangci-lint.
- Quickstart (dev):
  ```bash
  make test
  docker run --rm -d -p 5432:5432 -e POSTGRES_PASSWORD=test -e POSTGRES_USER=test -e POSTGRES_DB=runtime_adapters postgres:15-alpine
  RUNTIME_POSTGRES_DSN="postgres://test:test@localhost:5432/runtime_adapters?sslmode=disable" make run
  curl -s localhost:8080/api/v1/capabilities | jq
  ```
- Config reference pointing at §9.8 in the spec.
- Links: spec, rules, invariants, ADRs, architecture, contributing (AGENTS.md).

- [ ] **Step 2: Commit**

```bash
git commit -m "docs(README): add quickstart, config reference, and doc links"
```

---

## Task 66: Populate 11 SKILL.md files with compact rules

**Files:**
- Modify each of the 11 `.claude/skills/*/SKILL.md`.

- [ ] **Step 1: For each skill, write a compact rules block** derived from the spec. Example for `execution-modeling`:

```markdown
---
name: execution-modeling
description: ExecutionRequest/Result/Receipt modeling rules; invariants across request → result → receipt.
triggers:
  - file matches `internal/domain/execution/**/*.go`
  - task involves receipt, result, or request shape changes
---

# Compact rules

- `ExecutionRequest` is immutable after construction (I14).
- `ExecutionStatus` ∈ {success, failure, timeout, cancelled, partial} — adding a value requires ADR + schema_version bump (D4.3, R15).
- `RetryHint` ∈ {retryable, non_retryable, unknown} (I4).
- `ErrorClass` uses the 10-value closed enum; `DefaultRetryHint` mapping is deterministic (§5.9).
- `partial` requires all four AND conditions (D4.6) — never use it loosely.
- `success` carries no `error_class` / `error_message` (I7).
- Receipt is persisted **before** returning to the caller (I13 / A4.3).
- `schema_version` is "v1" in Phase 1 (I20).
```

Apply the same pattern to the other 10 skills, each scoped to its domain (shell safety, git ops, fs safety, http SSRF, resilience, normalization, observability, testing-quality, architecture-guardrails, adapter-contracts).

- [ ] **Step 2: Commit**

```bash
git commit -m "docs(skills): populate 11 Phase-1 skills with compact rules derived from the spec"
```

---

## Task 67: Final invariants check + ADR-closeouts

**Files:**
- Verify: `docs/domain-invariants.md` references match actual tests.
- Add if missing: `docs/adr/0003-chi-router-choice.md`, `docs/adr/0004-pgx-v5.md` (minor — document the plan-level P2/P3 choices).

- [ ] **Step 1: Grep `I1..I22` in tests**

```bash
grep -R "I[0-9]\+" internal/ --include='*_test.go' | cut -d: -f3 | sort -u
```
Expected: each invariant referenced by at least one test (use code comments like `// invariant I14: request immutable`).

- [ ] **Step 2: Open ADR-0003 and ADR-0004**, set status `accepted`, summarize rationale.

- [ ] **Step 3: Commit**

```bash
git commit -m "docs: final invariant cross-check + ADR-0003 (chi) and ADR-0004 (pgx/v5)"
```

---

## Task 68: Tag v0.1.0

- [ ] **Step 1: Run the full gate**

```bash
make all
make test-integration
make test-e2e
```
Expected: all green.

- [ ] **Step 2: Update `CHANGELOG.md`** (create if missing) with entry for `v0.1.0` referencing all Dn.m and An.m implemented.

- [ ] **Step 3: Tag and commit**

```bash
git add CHANGELOG.md
git commit -m "chore(release): v0.1.0 — Phase 1 MVP (8 capabilities across shell/git/filesystem/http)"
git tag -a v0.1.0 -m "runtime-adapters Phase 1 MVP"
git tag bundle-8-complete
```

**Bundle 8 exit criteria:**
- All E2E scenarios pass.
- All 15 `R*` rules and 22 `I*` invariants cross-checkable in code/tests.
- All 11 Phase-1 skills populated.
- `v0.1.0` tag created.

---

# Self-review

## 1. Spec coverage

Walk each spec section and confirm a task (or explicit deferral) covers it.

| Spec section | Covered by |
|---|---|
| §1.2 R1..R8 | Bundle 2 (domain) + Bundle 3 (ExecuteService); R13 at T28 (receipt-always); R7 at T20 (exit_codes only where relevant) |
| §1.3 non-responsibilities | Enforced by architecture (no governance imports); documented in `docs/rules.md` (T7) |
| §1.5 Phase 1 success criteria | Each bullet mapped: contract at T25/T26; 4 adapters at T33/34–45; timeout test at T28 scenario 7; unambiguous status at T20; retry hint at T11/T20; receipt uniform at T21; forward-compat via `schema_version` at T21 |
| §2 Consumer model | Transport HTTP+SDK at T51–55 (D2.2); standalone binary at T59; sync-only at T28 (ExecutionHandle internal, no async endpoint); idempotency at T28 step 2 + T49; no auth (T51 skeleton has none); correlation_id required at T18; contract stability addressed via port-freeze in Bundle 3 |
| §3 Domain strategy | Bounded context enforced by directory; `AdapterRawOutcome` never leaves adapters (T23/T25); `AdapterID`, `Capability`, `Payload` modeling at T12/T13/T14; governance types kept as opaque strings at T18 |
| §4 Entities | T16..T21 cover all entity/VO types with invariants; partial rule explicit at T22; A4.3 persistence-before-return at T28 step 10 |
| §5 Value objects | T9..T17 + T24 cover all VOs, closed enums, serialization conventions; `schema_version v1` at T21 |
| §6 Use cases | UC1 at T28; UC2 at T29; UC3 at T29 with `include_streams`; idempotency replay-everything at T28 step 2 |
| §7 Ports | Active 3 outbound at T25; inbound 2 at T26; A7.1 `Execute` returns `(ExecutionReceipt, error)` at T26; deferred ports (EventPublisher/LockManager/Mailbox) explicitly NOT implemented |
| §8 Adapters | T33 (shell) / T34–39 (git 4 caps) / T40–43 (fs 2 caps) / T44–45 (http) — all with safety matrices, contract suite at T39/43/45 and reusable suite at T32 |
| §9 Execution strategy | ctx-based at T28; timeout/cancel semantics at T28 scenarios 6 & 7; retry hint policy at T11; MaxConcurrentExecutions at T27; panic recovery at T30/T61; graceful shutdown at T59/T60; OTel sampling at T57 |
| §10 Testing | Unit/contract/integration/e2e present per bundle; coverage gate at T31 (domain+app ≥85%); CI config at T5; determinism via injectable clock+ID at T10/T19 |
| §11 Out of Phase 1 | Deferrals respected: async endpoint not implemented, no LockManager/EventPublisher/Mailbox code, no dynamic registration (T13 registry is construction-only) |
| §12 Docs & skills | CLAUDE/AGENTS/rules/invariants/architecture at T6–T7; ADR template + 2 initial ADRs at T8; 11 skill placeholders at T8 → populated at T66 |
| §13 SDD prep | Spec moved to conventional path at T1; plan placed under `docs/superpowers/plans/` (this file); bundles mapped to subagent-driven pattern; engram saves happen at each commit milestone (session summary at end) |
| Appendix A / B | Each Dn.m / An.m referenced inline by task — no decision left unimplemented |

**Gaps found during self-review:**

- **Gap-1: HTTP wire name `http.request@v1` vs folder `httpreq`.** T24 flags this with a default (wire = `httpreq.request@v1`). If the spec §5.4 requires literal `http.request@v1` on the wire, add a single-task addendum: either rename the package to `httpadp` (cheapest) or add an `AdapterID.Alias()` / canonical override. **Flag this at T1 for user confirmation.**
- **Gap-2: Spec does not fix the HTTP address / port default.** Plan assumes `:8080` at T56. If the spec reserves a different default, override there.
- **Gap-3: `docs/skills-roadmap.md`** mentioned in §7.3 — add as a deferred doc at T7 (list Phase 2A–F tracks) or explicitly note as out-of-scope in CLAUDE.md. Recommended: add one-paragraph section inside `AGENTS.md` listing the 6 reserved tracks.

These gaps are **annotations**, not blocking issues — no spec contradiction; no task missing for a spec-mandated feature.

## 2. Placeholder scan

Swept for "TBD", "TODO", "implement later", bare `...`, "similar to Task N", etc. Remaining bare `...` in this plan are:
- `runtime_service.go` interface body — legitimate (it's the full interface, not a placeholder).
- ID/Stream/Artifact code snippets — placeholder-free; where I use `...` it's marking fields the engineer copies from the VO spec above. Since each VO's fields are already enumerated verbatim in the linked task, this is not a gap.

No "TODO" / "implement later" markers remain.

## 3. Type consistency

Key types used across tasks, verified consistent:

| Type | Defined | Used in |
|---|---|---|
| `ExecutionReceipt` | T21 | T28, T29, T48, T51, T54 |
| `ExecutionResult` | T20 | T23, T28, T30, all adapter tasks |
| `AdapterRawOutcome` | T25 (port) + T23 (dispatcher) | all adapter tasks |
| `Capability` | T13 | T13, T23, T24, T28, all adapters |
| `CapabilityRegistry` | T13 | T28, T29, T58 |
| `ResultNormalizer` | T23 | T28, T46, T58 |
| `ConcurrencyLimiter` | T27 | T28, T58 |
| `ShellConfig`/`GitConfig`/etc. | T56 | T46, T58 |
| `NewPhase1Capabilities()` | T24 | T58 |

No mismatches detected. Method names consistent (`Execute`, `Normalize`, `Save`, `FindByID`, `Lookup`, `Record`, `Acquire`→ `TryAcquire`, `Release`, `Register`).

## 4. Critical contradictions in the spec detected during planning

Per user instruction, only flag truly critical items. **None found.** The spec is internally consistent. The two wire-name / default-port items above are plan-level resolution points, not spec contradictions.

---

# Execution handoff

Plan complete and saved to `docs/superpowers/plans/2026-04-19-runtime-adapters-phase1.md`. Two execution options per the writing-plans skill:

1. **Subagent-Driven (recommended)** — dispatch a fresh subagent per task, review between tasks, fast iteration. Natural checkpoints at bundle boundaries (`bundle-N-complete` tags).
2. **Inline Execution** — execute tasks in this session using `superpowers:executing-plans`, batch execution with checkpoints after each bundle.

**Suggested cadence for subagent-driven:**
- Bundles 1 + 2 first — they're foundational and mostly independent.
- Bundle 3 gates the adapters.
- Bundles 4A–4D (shell / git / filesystem / http sub-bundles) can run in **parallel** once the contract suite (T32) is in place.
- Bundle 5 can run in parallel with any 4A–4D sub-bundle (different surfaces).
- Bundle 6 depends on Bundles 4 & 5.
- Bundles 7 & 8 are final.

Which approach?
