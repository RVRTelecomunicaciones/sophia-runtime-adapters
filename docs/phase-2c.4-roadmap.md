# Phase 2C.4 — operational readiness roadmap

## 1. What this doc is and is not

This is a **navigation roadmap** that decomposes Phase 2C.4 into independent
sub-projects, names their dependencies, and fixes the release order.

It is **not** a design. It does not describe how to build anything. Each
sub-project gets its own design spec under `docs/superpowers/specs/` and its
own implementation plan, brainstormed and written when its turn comes.

It also fixes one piece of project hygiene: per-sub-project minor-version
releases instead of one mega-release at the end of 2C.4.

## 2. Sub-project catalogue

Eight items inherited from prior specs (2C.1 §47-59, 2C.2 calibration deferrals,
2C.3 reactive bundle B10).

### G — Cancellation-rate SLO + persist-availability SLO

- **Scope:** Two new SLOs — `cancellation_rate_per_capability` and
  `persist_availability_total`. Generate Sloth rules. Re-enable the two
  scenarios currently skipped in `TestChaos_Comprehensive`
  (`ci-shell-hang-cancel`, `ci-persist-fail`).
- **Size:** Small (~3-5 days).
- **Depends on:** Nothing — fully independent.
- **Entry:** Branch from `main` post-`v0.4.0`.
- **Exit:**
  - New SLO YAML specs under `ops/slo/` (test + prod variants under `ops/slo/test/`).
  - Rendered rules committed under `ops/prometheus/generated/` + `ops/prometheus/generated/test/`.
  - `test/chaos/e2e/comprehensive_test.go` removes `skipReason` from `ci-shell-hang-cancel` and `ci-persist-fail` rows.
  - Nightly chaos run passes 6/6 active scenarios + inhibition contract for both new alert types.
  - `CHANGELOG 0.5.0` entry + `v0.5.0` tag.
- **Out of scope:**
  - Real-load calibration of the new SLO targets. Initial values may ship PROVISIONAL with a one-line rationale per target; if real-load calibration is needed before A+B routes them to a real pager, open a calibration follow-up at that time. F is git-specific and does NOT cover these new SLOs.
  - Routing the new alerts to a real pager — that's A+B.
- **Tag:** `v0.5.0`.

### E — pgx pool Prometheus collector

- **Scope:** Wire `pgxpool.Pool.Stat()` snapshots to Prometheus via the OTel
  collector. Unblock the dormant `PoolIdleZero` alert declared in 2C.1.
- **Size:** Small (~3-5 days).
- **Depends on:** Nothing — fully independent.
- **Entry:** Branch from `main` post-`v0.5.0`.
- **Exit:**
  - Pool stats (`AcquireCount`, `IdleConns`, `TotalConns`, `MaxConns`, etc.) exposed as `runtime_adapters.pgx_pool.*` metrics under R16 label whitelist.
  - `PoolIdleZero` alert exits "dormant" state — fires correctly under saturated load.
  - Smoke verification under the 2C.2 load envelope.
  - `CHANGELOG 0.6.0` entry + `v0.6.0` tag.
- **Out of scope:**
  - Pool sizing tuning (config decision, not code).
  - Other DB-level metrics (table sizes, transaction counters, etc.).
- **Tag:** `v0.6.0`.

### F — Sustained git calibration + per-tree git.status sub-metric thresholds

- **Scope:** Run sustained-load scenarios against `git.clone@v1`,
  `git.diff@v1`, `git.commit@v1`. Calibrate ROUGH_NO_CHANGE / PROVISIONAL
  targets to measured ones. Add per-tree-shape sub-metric thresholds for
  `git.status@v1`.
- **Size:** Small-medium (~1 week).
- **Depends on:** 2C.2 calibration infrastructure (already merged); E
  (so saturated runs surface pool pressure correctly via `PoolIdleZero`).
- **Entry:** Branch from `main` post-`v0.6.0`.
- **Exit:**
  - New calibration report under `ops/slo/calibration-reports/<date>-git-sustained.md`.
  - `ops/slo/git.yaml` clone/diff/commit transition from `PROVISIONAL` / `ROUGH_NO_CHANGE` to `CALIBRATED`.
  - `git.status@v1` per-tree sub-metric thresholds added to SLI selector or as a separate SLO.
  - Updated CHANGELOG entry calling out the calibration deltas.
  - `v0.7.0` tag.
- **Out of scope:**
  - Adding new git capabilities (git.fetch, git.push, etc.) — Phase 1 catalogue is frozen.
  - Re-fixturing `test/fixtures/git-bench/` — the existing deterministic blueprints suffice.
- **Tag:** `v0.7.0`.

### A+B — Pager/ticket receivers + per-alert runbooks

- **Scope:** Swap the `null-receiver` placeholders in
  `ops/alertmanager/alertmanager.yaml` for real PagerDuty/Opsgenie
  (critical → on-call) and Slack/Linear (warning → ops-tickets) webhooks.
  Author per-alert runbooks under `docs/runbook/<alertname>.md`. Wire
  `runbook_url` annotation to each alert rule. A and B are tightly coupled:
  alerts reference runbooks, runbooks reference alerts. They ship as one
  bundle.
- **Size:** Medium (~1-2 weeks).
- **Depends on:** External account provisioning (PagerDuty / Opsgenie /
  Slack / Linear) — accounts and webhook tokens must exist BEFORE entry.
- **Entry:**
  - External accounts created and webhook URLs / API keys captured in a secret store (1Password, Vault, or env vars + `.env` for local).
  - Branch from `main` post-`v0.7.0`.
- **Exit:**
  - `alertmanager.yaml` has real `webhook_configs:` for critical + warning routes; `null-receiver` retained ONLY for `info` (silenced).
  - `docs/runbook/<alertname>.md` exists for every alert in `ops/prometheus/generated/<adapter>.yaml` plus the hand-written infra alerts (HighErrorRate, HighLatency, PoolIdleZero, etc.).
  - Each alert rule has a `runbook_url` annotation pointing to the matching runbook (relative or absolute URL — repo decision documented in spec).
  - End-to-end smoke: trigger an alert via chaos compose; verify it reaches the real receiver. Smoke test runs locally only — NOT in CI (no real-account secrets in CI).
  - `CHANGELOG 0.8.0` + `v0.8.0` tag.
- **Out of scope:**
  - PagerDuty escalation policy design (operator decision; documented separately).
  - On-call rotation setup.
  - SLA contract documents.
  - Replacing the receiver-stub in chaos compose (it stays — chaos still runs on the stub).
- **Tag:** `v0.8.0`.

### D — Loki + OTel log signal export + log dashboards + annotations layer

- **Scope:** Add Loki to the compose + chaos compose stacks. Switch the
  runtime from stdout-only logs to OTel log signal export (via the OTel SDK
  log signal or via collector tail-on-stdout). Add Loki Grafana datasource.
  Add a "logs for the past N min" drill-through panel to the per-adapter
  dashboards. Add an annotations layer (Grafana annotations API) for
  deployment + incident overlays on time-series panels.
- **Size:** Medium-large (~2 weeks).
- **Depends on:** A+B (so dashboards reference real alert names that have
  runbooks). NOT dependent on C.
- **Entry:** Branch from `main` post-`v0.8.0`.
- **Exit:**
  - Loki container in `ops/local/compose.yaml` + `ops/local/compose.chaos.yaml`.
  - Runtime emits OTel log signal carrying trace_id + span_id + capability + correlation_id at minimum (R16 label whitelist still applies on derived metrics).
  - Grafana datasources include Loki; per-adapter dashboards have a "Recent logs" panel filtered by alert context.
  - Annotations layer in place for deploys (commit SHA + tag) and incidents (manual or alertmanager-driven).
  - `CHANGELOG 0.9.0` + `v0.9.0` tag.
- **Out of scope:**
  - Log retention sizing for production (operational decision; documented in C).
  - Log-based alerting (Loki ruler) — possible Phase 3 work; explicitly NOT in 2C.4.
  - PII redaction policy (operational, not code).
- **Tag:** `v0.9.0`.

### C — Kubernetes manifests / Phase 2C closure

- **Scope:** `deploy/k8s/*` manifests for the full operational stack —
  runtime-adapters, postgres, otel-collector, prometheus, alertmanager,
  grafana, loki, receiver-stub-or-equivalent. Secret-management strategy
  (k8s `Secret` + sealed-secrets or external-secrets). Namespace strategy.
  Helm vs Kustomize vs raw manifests decision (ADR-gated).
- **Size:** Large (~2-3 weeks).
- **Depends on:** All previous sub-projects (G/E/F/A+B/D). Specifically, it
  must deploy alerts wired to real receivers (A+B), Loki (D), and the
  recalibrated SLOs (G/E/F).
- **Entry:** Branch from `main` post-`v0.9.0`.
- **Exit:**
  - `deploy/k8s/` populated with manifests for the full stack.
  - One ADR documenting the Helm vs Kustomize vs raw decision.
  - Local cluster smoke (kind / k3d / minikube): `kubectl apply -k deploy/k8s/local` brings up the stack; chaos canary E2E runs against it; passes.
  - `CHANGELOG 0.10.0` + `v0.10.0` tag — closes Phase 2C.
- **Out of scope:**
  - Per-environment values for real production clusters (`deploy/k8s/local` only; production overlays come from operators).
  - CI/CD pipeline that pushes to a real cluster.
  - Multi-cluster federation, GitOps adoption (Argo/Flux).
- **Tag:** `v0.10.0`.

### H — Parking lot ("if demanded")

Items mentioned in 2C.1 spec line 53 + line 58 with the explicit guard
"if demanded" — they do NOT have a sub-project today. Open one if
operations actually request:

- Dynamic log-level reload (SIGHUP or config watch).
- Dashboard snapshot / golden-image regression tests.

If new "if demanded" items surface during 2C.4, append them here without
re-versioning the meta-spec.

## 3. Order and dependency graph

```
G  (independent)            ──→ v0.5.0
                                │
E  (independent)            ──→ v0.6.0
                                │
F  (depends on E)           ──→ v0.7.0
                                │
A+B (depends on alerts)     ──→ v0.8.0
                                │
D  (depends on A+B)         ──→ v0.9.0
                                │
C  (depends on G/E/F/A+B/D) ──→ v0.10.0  (closes Phase 2C)
```

Strict serial execution. Could G and E run in parallel since they touch
nothing in common? Technically yes — but the project has a single
operator (today) and serial cadence preserves momentum + clean
review-per-PR rhythm. Parallelism is on the table if the team grows.

## 4. Release strategy

**One minor version per sub-project.** No tag per commit, no tag per
bundle inside a sub-project. A tag is created ONLY when:

1. The sub-project's exit criteria from §2 are all met.
2. CI is green on the merge commit.
3. The CHANGELOG entry for the new minor is committed.
4. An end-of-sub-project smoke verification has run (the specifics are
   in each sub-project's spec; e.g., G runs the nightly comprehensive,
   E runs the load smoke, A+B runs an end-to-end alert delivery test).

Tag annotations follow the v0.4.0 shape: full release notes inside the
annotated tag message, with explicit references to the spec, plan, and
the closed sub-project letter.

| Tag | Sub-project | Closes |
|---|---|---|
| `v0.5.0` | G | Cancellation-rate + persist-availability SLOs; nightly chaos at 6/6 |
| `v0.6.0` | E | pgx pool collector; PoolIdleZero un-dormant |
| `v0.7.0` | F | git clone/diff/commit calibrated; per-tree git.status sub-metric thresholds |
| `v0.8.0` | A+B | Real pager + ticket receivers + per-alert runbooks |
| `v0.9.0` | D | Loki + OTel log export + log drill-through + annotations |
| `v0.10.0` | C | Kubernetes manifests; Phase 2C completed |

## 5. `v0.10.0` definition

`v0.10.0` means **Phase 2C completed and operating-baseline ready to run
in production from Kubernetes**.

It does NOT mean "1.0 product complete" or "stable public API". The
SemVer 1.0 milestone is reserved for when:

- The HTTP API + receipt schema + metric contract (R3, R16, I23) are
  declared frozen as a public contract.
- Any future breaking change is willing to ship as a major bump.

Until that decision lands, `v0.x.y` (with `x` advancing per sub-project)
is the discipline. After 2C.4 closes at `v0.10.0`, future tracks
(Phase 3+, or 2D, or whatever comes next) continue the `v0.x.y` line
unless a `v1.0.0` declaration ships explicitly with a CHANGELOG entry
that calls out the API freeze.

## 6. Cross-cutting constraints (apply to ALL sub-projects)

Every sub-project must respect the existing repo contracts. None of the
sub-projects below has authority to break these without an explicit
ADR:

- **R-rules R1–R17** (`docs/rules.md`) — runtime doesn't decide, receipts
  don't mutate, ports stable, no panics, raw outcome stays in adapter,
  no auto-retry, persist-before-return, envelope before semantics, no
  dynamic adapters, config rejected by default, OOS contract permanent,
  test determinism, receipts always, MaxConcurrentExecutions required,
  only 5 statuses, metric cardinality bounded, chaos fail-closed in production.
- **I-invariants I1–I24** (`docs/domain-invariants.md`) — including the
  newly-added I24 (chaos preserves runtime semantics).
- **Conventional commits** with `feat(scope)` / `fix(scope)` / `docs(scope)`
  / `ci(scope)` / `chore(scope)` / `test(scope)`. NO `Co-Authored-By` or
  AI attribution.
- **Per-bundle review cadence** (one of the cadences from 2C.3 — review
  per task, single-dispatch + combined review, etc.) chosen at the start
  of each sub-project's plan.
- **Each sub-project ships its own spec, plan, bundles, and tag.** No
  sharing of spec/plan files across sub-projects.

## 7. Spec convention

Each sub-project's design spec lives at:

```
docs/superpowers/specs/YYYY-MM-DD-phase-2c.4-<letter>-<slug>-design.md
```

Examples:

- `docs/superpowers/specs/2026-04-30-phase-2c.4-g-cancellation-persist-slos-design.md`
- `docs/superpowers/specs/2026-05-15-phase-2c.4-c-k8s-manifests-design.md`

Each sub-project's implementation plan lives at:

```
docs/superpowers/plans/YYYY-MM-DD-phase-2c.4-<letter>-<slug>.md
```

Each sub-project's ADRs continue the existing 4-digit numbered series in
`docs/adr/` (the next free number when you start a sub-project — no
pre-allocation).

## 8. Process

For each sub-project, in order:

1. **Brainstorm** — invoke `superpowers:brainstorming` skill to design the
   sub-project. Save spec to the path in §7.
2. **Plan** — invoke `superpowers:writing-plans` skill from the spec.
   Save plan to the path in §7.
3. **Execute** — pick a cadence (review-per-task, single-dispatch +
   combined review, or hybrid) and ship bundles.
4. **Verify exit criteria** from §2 for that sub-project.
5. **Tag** per §4. Update this roadmap's "Status" line at §9 (below) by
   appending a single line.

## 9. Status

| Sub-project | Tag | Status | Date |
|---|---|---|---|
| G | `v0.5.0` | closed | 2026-05-01 |
| E | `v0.6.0` | not started | — |
| F | `v0.7.0` | not started | — |
| A+B | `v0.8.0` | not started | — |
| D | `v0.9.0` | not started | — |
| C | `v0.10.0` | not started | — |

Update this table when each sub-project closes. Status values:
`not started` → `in progress` → `closed (vX.Y.Z, YYYY-MM-DD)`.

## References

- 2C.1 spec: `docs/superpowers/specs/2026-04-21-phase-2c-observability-slos-design.md` — original 2C.4 deferrals at lines 47-59
- 2C.2 spec: `docs/superpowers/specs/2026-04-23-phase-2c-load-baseline-design.md` — git ROUGH_NO_CHANGE deferrals
- 2C.3 spec: `docs/superpowers/specs/2026-04-26-phase-2c.3-chaos-hardening-design.md` — cancellation/persist SLO deferrals
- 2C.3 reactive bundle B10: PR #37 — surfaced the cancellation SLO gap
- CHANGELOG 0.4.0 — closing summary of 2C.3 + the "what's deferred to 2C.4" line at line 187
