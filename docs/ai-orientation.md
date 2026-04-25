# AI orientation — runtime-adapters

Quick-entry reference for AI assistants (Claude Code, Cursor, Copilot, etc.) and humans new to the repo. **State and navigation, not history.** Read this first; follow the pointers for depth.

For *behavior* directives addressed to AI specifically (commit style, file edit policy, no AI attribution, etc.), read [`CLAUDE.md`](../CLAUDE.md). This document is content/structure orientation; `CLAUDE.md` is rules-of-engagement.

## What this repo is

`runtime-adapters` is the **governable execution layer** of the Sophia ecosystem. It materializes side effects on the system — processes, files, network, git — starting from typed requests, and returns normalized, auditable results.

It is a **boundary** separating decisions (made upstream) from execution (controlled here). Every execution gets three guarantees: a **contract** (declared input/output shape), a **limit** (timeout + cancellation honored), and a **receipt** (`ExecutionReceipt`, always produced — even for failures).

What this repo is **not**:

- Not a router (`agent-governance-core` chooses the adapter).
- Not a policy engine (governance/approval lives upstream).
- Not an orchestrator (workflow state is upstream).
- Not a retry mechanism (the runtime emits a `RetryHint`; the consumer decides).

For the full "is / is not" + design principles, see [`CLAUDE.md`](../CLAUDE.md) sections "What this repo is", "What this repo is NOT", and "Core design principles".

## Current phase state

| Phase | Description | Status |
|---|---|---|
| 1 | 4 concrete adapters (shell, git, filesystem, http) + receipts + idempotency | Done — `v0.1.0` |
| 2C.1 | Observability depth + SLOs + dashboards + Alertmanager routing | Done — `v0.2.0` |
| 2C.2 | Load baseline + SLO calibration + pinned compose envelope | Done — `v0.3.0` |
| 2C.3 | Chaos + minimal hardening (E2E pipeline, Alertmanager + Tempo, soak) | Pending |
| 2C.4 | Operational readiness (real receivers, runbooks, Loki, sustained git scenarios) | Pending |

Latest tag: `v0.3.0`. Latest GitHub Release: <https://github.com/RVRTelecomunicaciones/sophia-runtime-adapters/releases/tag/v0.3.0>.

Phase 2 still has deferred tracks (2A async, 2B locks, 2D fuzz, 2E auth, 2F git extended) — see the closing notes in [`docs/superpowers/specs/2026-04-19-runtime-adapters-phase1-design.md`](superpowers/specs/2026-04-19-runtime-adapters-phase1-design.md) §11.

## Navigation map — "to understand X, read Y"

| Topic | Authoritative source |
|---|---|
| Repo intent + role + non-goals | [`CLAUDE.md`](../CLAUDE.md) |
| Hexagonal layout + dependency direction | [`docs/architecture.md`](architecture.md) |
| Hard rules (R1..R16) — never-do list | [`docs/rules.md`](rules.md) |
| Domain invariants (I1..I23) | [`docs/domain-invariants.md`](domain-invariants.md) |
| Phase 1 design spec (D1.1, A1.1, etc.) | [`docs/superpowers/specs/2026-04-19-runtime-adapters-phase1-design.md`](superpowers/specs/2026-04-19-runtime-adapters-phase1-design.md) |
| Phase 2C observability/SLO design | [`docs/superpowers/specs/2026-04-21-phase-2c-observability-slos-design.md`](superpowers/specs/2026-04-21-phase-2c-observability-slos-design.md) |
| Phase 2C.2 load baseline design | [`docs/superpowers/specs/2026-04-23-phase-2c-load-baseline-design.md`](superpowers/specs/2026-04-23-phase-2c-load-baseline-design.md) |
| Architecture decisions (ADRs) | [`docs/adr/`](adr/) — `0001` through `0008` |
| How to add a capability or adapter | [`AGENTS.md`](../AGENTS.md) |
| Logging contract (slog wrapper, fields) | [`docs/logging.md`](logging.md) |
| Metric contract (instruments, cardinality) | [`docs/metrics.md`](metrics.md) |
| SLO conventions (Sloth specs, burn rates) | [`docs/slo.md`](slo.md) |
| SLO YAML sources | [`ops/slo/{shell,filesystem,http,git}.yaml`](../ops/slo/) |
| Generated Prometheus rules (Sloth output) | [`ops/prometheus/generated/`](../ops/prometheus/) |
| Hand-written infra alerts | [`ops/prometheus/rules/`](../ops/prometheus/rules/) |
| Calibrated SLO snapshot (machine-readable) | [`ops/slo/calibration-reports/latest-baseline.json`](../ops/slo/calibration-reports/latest-baseline.json) |
| Calibration evidence (per-run) | [`ops/slo/calibration-reports/evidence/<date>-baseline-v<N>/`](../ops/slo/calibration-reports/evidence/) |
| Load baseline operator workflow | [`docs/load-baseline.md`](load-baseline.md) + ADR 0007 + ADR 0008 |
| Calibration envelope (compose, cgroup-pinned) | [`ops/local/compose.yaml`](../ops/local/compose.yaml) |
| CI smoke compose (advisory) | [`ops/local/compose.ci-smoke.yaml`](../ops/local/compose.ci-smoke.yaml) |
| OTel collector config (pull-based pipeline) | [`ops/otel-collector/config.yaml`](../ops/otel-collector/config.yaml) |
| k6 scenarios (load) | [`ops/load/scenarios/`](../ops/load/scenarios/) |
| k6 shared helpers + envelope encoding | [`ops/load/lib/common.js`](../ops/load/lib/common.js) |
| Alertmanager routing | [`ops/alertmanager/alertmanager.yaml`](../ops/alertmanager/alertmanager.yaml) |
| Grafana dashboards (5 dashboards, pinned UIDs) | [`ops/grafana/dashboards/`](../ops/grafana/dashboards/) |
| CI workflow (jobs + gates) | [`.github/workflows/ci.yaml`](../.github/workflows/ci.yaml) |
| Release notes per version | [`CHANGELOG.md`](../CHANGELOG.md) |

## Key commands (Make targets)

```bash
make help                    # full target list with grouping
make build                   # go build ./...
make test                    # unit + contract (race detector on)
make test-integration        # integration tests (requires Docker)
make test-e2e                # E2E smoke (requires Docker)
make lint                    # golangci-lint run (built from source under runner Go)
make sloth-generate          # regenerate ops/prometheus/generated/*.yaml from ops/slo/*.yaml
make load-up                 # bring up the pinned compose stack
make load-down               # tear it down (-v wipes volumes)
make load-baseline           # full ~45 min baseline run + report generation
make load-smoke-local        # CI-equivalent advisory smoke locally
make fixture-git-bench       # regenerate the deterministic git fixtures
```

CI runs the same gates plus version-pin checks for sloth/promtool/amtool/dashboard-linter and a load-ops-lint job (shellcheck + `k6 inspect`).

## Calibrated SLO targets — `v0.3.0` (envelope: 2 CPU / 2 GiB pinned, ADR 0008)

| Capability | p99 latency target | Bucket `le` | Confidence |
|---|--:|---|---|
| `shell.exec@v1` | 250 ms | `0.25` | high |
| `filesystem.read_file@v1` | 100 ms | `0.1` | high |
| `filesystem.write_file@v1` | 500 ms | `0.5` | high |
| `http.request@v1` | 500 ms | `0.5` | high |
| `git.status@v1` | 250 ms | `0.25` | limited (smoke-only) |
| `git.clone@v1` | 30 000 ms | `30` | provisional (rough, deferred to 2C.4) |
| `git.diff@v1` | 3 000 ms | `3` | provisional (rough, deferred to 2C.4) |
| `git.commit@v1` | 2 000 ms | `2` | provisional (rough, deferred to 2C.4) |

Availability SLOs (8 — one per capability) target 99.0–99.9% non-error rate; not in 2C.2 calibration scope.

Histogram bucket set (`obs.DurationBuckets` in `internal/infrastructure/obs/metrics.go`):

```
[0.1, 0.25, 0.5, 1, 2, 3, 5, 10, 30, 60]
```

Adding any new `le=` value to a SLO YAML requires the bucket to exist here — `TestDurationBuckets_CoverSlothThresholds` enforces the cross-check.

## Pre-change checks (do these before editing)

- **Read the spec section relevant to your change** — every Dn.m / An.m has a citation in the design specs under `docs/superpowers/specs/`.
- **Identify the invariant(s) touched** — cite I1..I23 in the commit body if any move.
- **Conventional commits only** — `feat(scope) | fix(scope) | chore(scope) | docs(scope) | test(scope) | ci(scope)`. Scope is layer/adapter (`shell`, `git`, `filesystem`, `http`, `domain`, `ports`, `application`, `bootstrap`, `ci`, `docs`, `slo`, `load`).
- **Never `Co-Authored-By` or AI attribution in commits.**
- **Test pyramid:** unit + contract for everything; integration for adapters touching real subsystems; E2E only for the smoke flows under `test/e2e/`.
- **Coverage gate:** `internal/(domain|application|infrastructure/obs/log)/...` must stay ≥85% per package.
- **`make sloth-generate` is part of any SLO YAML edit** — the recording rules in `ops/prometheus/generated/` are checked-in and CI gates idempotency.

## Where decisions are recorded

- **ADRs** (`docs/adr/0001..0008`) — accepted decisions with options + tradeoffs.
- **Specs** (`docs/superpowers/specs/`) — full Phase design with closed Dn.m and post-approval An.m adjustments.
- **Plans** (`docs/superpowers/plans/`) — task-level breakdowns; each ends with a "post-merge mirror" section reflecting actual execution.
- **Engram memory** — recent decisions/discoveries persisted across sessions.
- **CHANGELOG** — per-tag delivery summary.
- **PR descriptions** — per-PR rationale; `gh pr view <N>` retrieves them.

## Where to start when picking up new work

1. Read this file.
2. Read [`CLAUDE.md`](../CLAUDE.md) for rules-of-engagement.
3. Pick the relevant phase track from "Current phase state" → its spec under `docs/superpowers/specs/`.
4. Check `docs/adr/` for any ADR that constrains the area.
5. Look for the latest "post-merge mirror" section in the relevant plan to see what actually shipped vs what was originally proposed.
6. If memory tooling is available (engram), search for the topic before re-investigating.
