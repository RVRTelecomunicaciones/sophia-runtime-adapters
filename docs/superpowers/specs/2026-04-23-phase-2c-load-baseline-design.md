# Phase 2C.2 — Load Baseline — Design

> **Status:** Design (pre-plan). Produced via `superpowers:brainstorming` on 2026-04-23.
> **Parent:** Phase 2C (Hardening), track 2 of 4 (2C.1 observability ✅ → **2C.2 load baseline** → 2C.3 chaos + minimal hardening → 2C.4 operational readiness).
> **Prior spec:** `docs/superpowers/specs/2026-04-21-phase-2c-observability-slos-design.md` (Phase 2C.1, closed at v0.2.0).

---

## 1. Purpose

Phase 2C.1 shipped the observability layer with **provisional** SLO targets — numbers that made the framework compile end-to-end but had no empirical grounding. Every SLO spec file under `ops/slo/*.yaml` carries a `PROVISIONAL` header that names 2C.2 as the track responsible for replacing those numbers with measured ones.

Phase 2C.2 closes that gap:

1. **Primary deliverable** — recalibrate the SLO targets in `ops/slo/*.yaml` with evidence from real load against a declared resource envelope.
2. **Secondary deliverable** — establish the scaffolding (docker-compose measurement env, k6 scenarios, calibration report format, CI advisory smoke) that 2C.3 and 2C.4 will reuse.
3. **Tertiary deliverable** — validate the 2C.1 metric + observability pipeline under real load (non-programmatic verification — programmatic E2E assertions are deferred to 2C.3).

The track is scoped tightly: **measurement + calibration**. No chaos injection, no soak tests, no real pager/ticket integration. Those belong to 2C.3 and 2C.4.

## 2. Scope

### 2.1 In-scope

- **`Dockerfile`** (repo root) — multi-stage Go 1.26.2 build producing a distroless runtime image.
- **`ops/local/compose.yaml`** — measurement/calibration environment (7 services, cgroup-pinned limits).
- **`ops/local/compose.ci-smoke.yaml`** — CI smoke environment (4 services, reduced limits for GHA).
- **`ops/local/prometheus.yaml`** — scrape config for the measurement environment.
- **`ops/otel-collector/config.yaml`** — OTLP gRPC receiver + Prometheus exporter (pull-based pipeline; no `prometheusremotewrite`).
- **`ops/otel-collector/.collector-version`** — pin file matching the collector image tag used in compose.
- **`ops/otel-collector/scripts/validate-config.sh`** — config validation wrapper invoking the pinned collector binary.
- **`ops/load/` subtree** — k6 scenarios, shared helpers, report generator, CI smoke comment script.
- **`ops/slo/calibration-reports/` subtree** — README, schema test code, evidence directories. First real report (`YYYY-MM-DD-baseline-v1.md`) is produced by the calibration run committed under this track.
- **`test/fixtures/git-bench/`** — regenerable git working-tree fixtures (source blueprints + Makefile generator; no committed `.git/`).
- **`.github/workflows/ci.yaml`** — three new jobs (`load-ops-lint`, `load-smoke`, `load-report-schema`) plus small extension to the existing `observability` job for 2C.2 ops config validation.
- **`Makefile`** — `load-up`, `load-baseline`, `load-smoke-local`, `load-down`, `fixture-git-bench` targets.
- **ADRs 0007 (k6 + pull-based pipeline), 0008 (pinned compose envelope)**.
- **`docs/load-baseline.md`** — human-facing reference describing the calibration flow, envelope declaration, report structure, re-run cadence.
- **CHANGELOG entry for 0.3.0** and release tag `v0.3.0`.

### 2.2 Out of scope (deferred to 2C.3 / 2C.4)

| Item | Landing track |
|---|---|
| Alertmanager + Tempo containers in compose | 2C.3 (chaos needs Alertmanager for E2E assertions) |
| Programmatic E2E pipeline tests (runtime → collector → prom → sloth → alertmanager → receiver) | 2C.3 |
| Soak tests (1h+ sustained) | 2C.3 if a leak symptom surfaces |
| Chaos injection scenarios | 2C.3 |
| Dedicated VM / self-hosted runner | 2C.4 |
| Real pager/ticket receiver integrations (PagerDuty, Opsgenie, Slack, Linear) | 2C.4 |
| pgx pool Prometheus collector (unblocks `PoolIdleZero`) | 2C.4 |
| Loki integration | 2C.4 |
| Full operational local stack (runbooks, alert delivery, production-like networking) | 2C.4 |
| SDK formal benchmarks | ad-hoc via `go test -bench` if a concrete question arises |
| `git.clone@v1` rough observation if adapter rejects `file://` URLs | `SKIPPED_IN_2C2` explicit in report; re-attempt in 2C.3 if needed |

## 3. Approach

Seven decisions cement the design. Each was closed during brainstorming with explicit rationale; they are referenced throughout as `D2C2.n`.

| ID | Decision | Source |
|---|---|---|
| **D2C2.1** | Scope tier — core 4 (`shell.exec`, `fs.read_file`, `fs.write_file`, `http.request`) calibrated with full evidence; `git.status` with SMOKE-CALIBRATED confidence; `git.clone`/`git.diff`/`git.commit` as ROUGH_NO_CHANGE observation | Q1 answer |
| **D2C2.2** | **k6** as primary load tool; SDK benchmarks are opt-in ad-hoc via `go test -bench`, not a track deliverable | Q2 answer |
| **D2C2.3** | Scenario shape: baseline (constant-arrival-rate) + saturation (ramping-arrival-rate). Soak deferred to 2C.3 | Q3 answer |
| **D2C2.4** | Resource envelope: docker-compose with cgroup-pinned CPU + memory limits. Targets in YAMLs are envelope-specific. Verification script (`verify-limits.sh`) embedded in run, output included in report evidence | Q4 answer |
| **D2C2.5** | Calibration flow: report-driven manual. Structured report generated from k6 + PromQL + envelope manifest; operator reviews, edits YAMLs manually; report + YAML diff commit together. No silent automation over `ops/slo/*.yaml` | Q5 answer |
| **D2C2.6** | CI regression smoke: advisory-only, `continue-on-error: true`. Core 4 only, reduced RPS (~10% of baseline), posts PR comment with delta vs committed baseline snapshot. Never fails the overall CI run | Q6 answer |
| **D2C2.7** | E2E observability pipeline: stack bring-up in 2C.2 compose (needed for baseline runs anyway); **programmatic** pipeline assertion tests deferred to 2C.3 | Q7 answer |

### 3.1 Why pull-based metrics pipeline

A mandatory cross-cutting decision surfaced during Section 2 review: the OTel Collector's `prometheusremotewrite` exporter has historical problems with OTel histogram translation (delta↔cumulative mismatches, exemplar drops per version, timestamp alignment). For a track whose deliverable is **calibration grounded in `execution.duration` distributions**, putting that exporter in the path is unacceptable risk.

**Adopted architecture**:

```
runtime-adapters  →  OTLP gRPC  →  otel-collector  (exposes Prom format at :8889/metrics)
                                         ↑
                                    scrape every 5s (pull-based)
                                         |
                                    prometheus
```

Collector uses the `prometheus` exporter (pull) not `prometheusremotewrite` (push). Histograms preserve native shape, buckets, count/sum, and exemplars. k6 writes its own metrics to `summary.json` via `handleSummary()` — they are NOT pushed to Prometheus in 2C.2 (simplicity; calibration doesn't need k6 metrics in Grafana, only runtime metrics do).

This is captured formally in **ADR 0007**.

## 4. Architecture and layout

Target file tree after 2C.2 lands (`†` = active artifact; others are tests/generators/configs):

```
sophia-runtime-adapters/
├── Dockerfile                                         # NEW † — multi-stage Go 1.26.2 distroless build
├── Makefile                                           # REVISED — adds load-* + fixture-git-bench targets
├── CLAUDE.md                                          # REVISED — tiny note pointing to ops/load/
├── CHANGELOG.md                                       # REVISED — adds [0.3.0] entry
├── .github/workflows/ci.yaml                          # REVISED — adds 3 new jobs; extends observability
├── cmd/runtime-adapters/main.go                       # UNCHANGED
├── internal/                                          # UNCHANGED
│
├── docs/
│   ├── load-baseline.md                               # NEW † — human-facing reference
│   └── adr/
│       ├── 0007-k6-for-load-baseline.md               # NEW † — tool + pipeline rationale
│       └── 0008-pinned-compose-envelope.md            # NEW † — envelope declaration rationale
│
├── ops/
│   ├── slo/                                           # existing (2C.1)
│   │   └── calibration-reports/                       # NEW subtree
│   │       ├── README.md                              # NEW † — format reference + cadence
│   │       ├── latest-baseline.json                   # NEW † — snapshot for CI smoke delta comparison
│   │       ├── YYYY-MM-DD-baseline-v1.md              # NEW † — first real report (produced this track)
│   │       ├── schema_test.go                         # NEW — Go test (build tag loadreport)
│   │       └── evidence/
│   │           └── YYYY-MM-DD-baseline-v1/
│   │               ├── manifest.json                  # NEW † — machine-readable envelope metadata
│   │               ├── summary.json                   # NEW † — raw k6 handleSummary output
│   │               ├── <capability>-baseline-prom-instant.json   # NEW † — per-cap PromQL instant query
│   │               ├── <capability>-baseline-prom-range.json     # NEW † — per-cap PromQL query_range
│   │               ├── git-status-smoke-split.json    # NEW † — clean vs dirty split metrics
│   │               └── git-rough-observations.json    # NEW † — rough tier raw data
│   │
│   ├── local/                                         # NEW subtree
│   │   ├── README.md                                  # NEW † — "measurement env for 2C.2; operational closure is 2C.4"
│   │   ├── compose.yaml                               # NEW † — 7-service measurement stack with cgroup-pinned limits
│   │   ├── compose.ci-smoke.yaml                      # NEW † — 4-service CI smoke stack (reduced limits)
│   │   └── prometheus.yaml                            # NEW † — scrape config for measurement stack
│   │
│   ├── otel-collector/                                # NEW subtree (was reserved in 2C.1 §4.1)
│   │   ├── config.yaml                                # NEW † — OTLP receiver + Prom exporter
│   │   ├── .collector-version                         # NEW † — pinned version matching compose tag
│   │   └── scripts/
│   │       └── validate-config.sh                     # NEW — wraps pinned otelcol validate
│   │
│   └── load/                                          # NEW subtree
│       ├── README.md                                  # NEW † — how to run baseline + smoke
│       ├── .k6-version                                # NEW † — pinned 0.58.0
│       ├── lib/
│       │   ├── common.js                              # NEW † — shared k6 helpers
│       │   ├── verify-limits.sh                       # NEW † — cgroup pre-run check
│       │   ├── generate-report.sh                     # NEW † — produces calibration report markdown
│       │   ├── ci-smoke-comment.sh                    # NEW † — produces PR comment from smoke summary
│       │   └── report-template.md.tmpl                # NEW † — markdown template
│       └── scenarios/
│           ├── suite.js                               # NEW † — entrypoint orchestrating full baseline run
│           ├── smoke.js                               # NEW † — CI smoke (core 4 only, reduced RPS)
│           ├── shell_exec.js                          # NEW † — core baseline + saturation
│           ├── filesystem_read_file.js                # NEW † — core
│           ├── filesystem_write_file.js               # NEW † — core
│           ├── http_request.js                        # NEW † — core (talks to http-upstream-mock)
│           ├── git_status.js                          # NEW † — smoke tier (clean + dirty variants)
│           └── git_rough.js                           # NEW † — rough tier (clone/diff/commit observation only)
│
└── test/fixtures/git-bench/                           # NEW subtree
    ├── README.md                                      # NEW † — fixture purpose + regeneration
    ├── Makefile                                       # NEW † — `make fixture-git-bench` rebuilds from blueprints
    ├── small-repo.sources/                            # NEW — blueprint files for the small-repo fixture
    │   ├── README.md
    │   └── file1.txt .. file10.txt
    ├── dirty-tree.sources/                            # NEW — blueprint for the dirty-tree fixture
    │   ├── file1.patch
    │   └── new-file.txt
    └── .gitignore                                     # NEW — ignores constructed `small-repo/` and `dirty-tree/` (with .git/)
```

### 4.1 Layering

- **`internal/`** unchanged. No production code changes in 2C.2.
- **`ops/local/`**, **`ops/otel-collector/`**, **`ops/load/`** are disjoint trees. Nothing imports across them at build time.
- **`ops/slo/calibration-reports/`** lives inside the existing `ops/slo/` subtree as a sibling to the Sloth YAMLs. Reports are not scanned by the 2C.1 Sloth coverage test (that globs `ops/slo/*.yaml` only — reports are Markdown + JSON, safe).
- **`test/fixtures/git-bench/`** is read-only during baseline runs. Mutating git scenarios (`git.commit@v1` rough) clone to tmpfs inside the container.

## 5. k6 scenarios

### 5.1 File layout principle

One file per capability (not per scenario) to keep iteration cycles cheap: rerunning only `shell_exec.js` while calibrating shell doesn't spin up the git fixtures. `suite.js` composes them for the full baseline run; `smoke.js` composes a separate reduced set for CI.

### 5.2 Common helpers (`ops/load/lib/common.js`)

```javascript
// baseURL, headers, idempotency / correlation ID generators, payload builders.
export const baseURL = __ENV.RUNTIME_URL || 'http://runtime-adapters:8080';
export const defaultHeaders = { 'Content-Type': 'application/json' };
export function newIdempotencyKey() { /* ULID */ }
export function newCorrelationID()  { /* 'bench-<ULID>' */ }
export function payloadForShellExec() { /* minimal shell.exec@v1 payload */ }
// ...per-capability payload builders
```

### 5.3 Core scenario template (4 capabilities)

Each core script exposes two `options.scenarios`:

- **`baseline`** — `constant-arrival-rate`, duration 3m at per-capability RPS (see table below).
- **`saturation`** — `ramping-arrival-rate`, 4 stages × 1m each, `startTime: '3m30s'` so it runs after baseline completes.

Thresholds on baseline reflect **current PROVISIONAL targets** from 2C.1's YAMLs; they may tighten after this track commits recalibrated targets. Saturation has no thresholds — we want the breakpoint to show, not a fail.

Per-capability baseline RPS + saturation envelope:

| Capability | Baseline RPS | Saturation ramp stages (1m each) | Notes |
|---|---|---|---|
| `shell.exec@v1` | 10 | 50 → 100 → 200 → 400 | allowlist-pinned to `echo` / `true` / `date` |
| `filesystem.read_file@v1` | 50 | 100 → 200 → 400 → 800 | two payload sizes (1 KiB, 10 KiB) via `exec.scenario.iterationInTest % 2` (D2C2.19) |
| `filesystem.write_file@v1` | 20 | 50 → 100 → 200 → 300 | writes to `/tmp/bench-<uuid>` — compose tmpfs volume, cleaned on teardown |
| `http.request@v1` | 30 | 100 → 200 → 400 → 500 | target = `http-upstream-mock:8080` inside bridge network (runtime's SSRF guards reject public/loopback; mock lives in the same compose network with a DNS name) |
| `git.status@v1` | 5 (smoke, 90s) | 20 → 40 → 80 (lite, 3 × 1m) | `exec.scenario.iterationInTest % 2` switches between `/bench/git/small-repo` and `/bench/git/dirty-tree` (D2C2.19) |

### 5.4 Smoke tier (`git.status@v1`)

Shorter durations + lower RPS. Two executors:

- `smoke` — constant-arrival-rate 5 rps × 90s.
- `saturation_lite` — ramping-arrival-rate 20→80 rps × 3m, starts after smoke.

Tags: `tier: 'smoke'`, `capability: 'git.status@v1'`. Report documents clean/dirty tree split via `k6 tags tree=small-repo|dirty-tree` and emits a separate evidence file `git-status-smoke-split.json`.

### 5.5 Rough tier (`git.clone`, `git.diff`, `git.commit`)

Single composite file `git_rough.js`, minimal scenarios, **no thresholds**. Intent is **observation**, not calibration. Config:

- **`git.clone@v1`** — `per-vu-iterations`, 1 VU × 20 iterations, max 5m. **Uses `file://` exclusively** (`file:///bench/git/small-repo`). If the adapter rejects `file://`, the scenario is marked `SKIPPED_IN_2C2` in the report (no observation, no YAML touch). Local path (`/bench/git/small-repo`) is **forbidden** because git detects local file-systems and uses hardlinks, producing artificially cheap timings.
- **`git.diff@v1`** — `constant-arrival-rate` 5 rps × 1m. `from=HEAD~1`, `to=HEAD`, `path=/bench/git/small-repo`.
- **`git.commit@v1`** — `per-vu-iterations`, 1 VU × 10 iterations. VU setup clones `small-repo` into a **fresh tmpfs copy** (`/tmp/bench-commit-<uuid>`), applies a pre-known change from `dirty-tree.sources/`, commits. Teardown removes the tmpfs copy.

The fixture source (`/bench/git/small-repo`) is mounted **read-only** during the entire run. Mutating scenarios operate on tmpfs copies. No working tree is shared mutably between scenarios.

### 5.6 `suite.js` — baseline entrypoint

Imports all six scenario files and composes them into a single ordered run. Total duration **~42–45 min** (core ~30 min + git ~12–13 min), sequential to avoid cross-capability contention in measurements:

| Segment | Duration breakdown | Subtotal |
|---|---|---|
| `shell.exec@v1` | 3m baseline + 30s gap + 4m saturation + 30s graceful | ~8 min |
| `filesystem.read_file@v1` | same shape | ~8 min |
| `filesystem.write_file@v1` | same shape | ~8 min |
| `http.request@v1` | same shape | ~8 min |
| **Core subtotal** | | **~32 min** |
| `git.status@v1` | 90s smoke + 5s gap + 3m saturation_lite + 30s graceful | ~4.5 min |
| `git_rough` composite | clone (maxDuration 5m) + 10s gap + diff (1m) + 15s gap + commit (~3–4m for 10 iters) | ~8–9 min |
| **Git subtotal** | | **~12.5–13.5 min** |
| **Full baseline total** | | **~42–45 min** |

For the first calibration, run sequentially (simplest; deterministic; no contention between capabilities' measurements). Concurrent runs can be evaluated later if the 45-min total becomes a constraint, but cross-capability CPU contention would pollute distributions — not recommended for calibration-grade runs.

`handleSummary(data)` is the single source of the machine-readable summary. It exports to `/summaries/summary.json` (mounted volume consumed by `generate-report.sh`). Human-facing stdout summary is also emitted for operator feedback during the run.

### 5.7 `smoke.js` — CI advisory entrypoint

Separate file, not imported by `suite.js`. Composes only the **core 4** at ~10% of baseline RPS:

| Capability | CI smoke RPS | Duration |
|---|---|---|
| `shell.exec@v1` | 1 | 30 s |
| `filesystem.read_file@v1` | 5 | 30 s |
| `filesystem.write_file@v1` | 2 | 30 s |
| `http.request@v1` | 3 | 30 s |

Staggered via `startTime`. Total runtime ~2 min 30 s (fits GHA job comfortably).

Threshold: single coarse guard `http_req_failed < 10%` for gross-failure detection. The job is `continue-on-error: true`, so even a threshold breach does not fail CI — see §11.

## 6. Compose architecture

### 6.1 Services inventory (baseline compose, 7 services)

| Service | Image | Purpose | Resource limits | Host expose |
|---|---|---|---|---|
| `runtime-adapters` | built from `./Dockerfile` | system under test | **2.0 CPU, 2 GiB mem_limit, 1 GiB mem_reservation** | `:8080` |
| `postgres` | `postgres:15-alpine` | persist `ExecutionReceipt` (A4.3) | 1.0 CPU, 512 MiB | — |
| `http-upstream-mock` | `nginx:1.27-alpine` serving a **fixed static response** (see §6.8) | upstream for `http.request@v1` scenarios | 0.5 CPU, 256 MiB | — |
| `otel-collector` | `otel/opentelemetry-collector-contrib:0.106.1` | OTLP gRPC receiver → Prometheus exporter at `:8889/metrics` | 0.5 CPU, 256 MiB | — |
| `prometheus` | `prom/prometheus:v3.11.2` (2C.1 pin) | scrape runtime + collector + Sloth rules eval | 1.0 CPU, 1 GiB | `:9090` |
| `grafana` | `grafana/grafana:11.3.0` | sanity visual during baseline; **not source of truth** | 0.5 CPU, 512 MiB | `:3000` |
| `k6` | `grafana/k6:0.58.0` | load driver (container-isolated to not contaminate measurement) | 1.0 CPU, 512 MiB | — |

**Total nominal**: ~6.5 CPU / ~5 GiB. Fits comfortably on a typical dev machine.

### 6.2 Compose v2 syntax for limits

Compose v2 (non-swarm) respects top-level `cpus:` and `mem_limit:` service-level keys. The `deploy.resources` block is swarm-only and silently ignored by `docker compose up`. We use:

```yaml
runtime-adapters:
  cpus: "2.0"
  mem_limit: 2g
  mem_reservation: 1g
```

### 6.3 Envelope verification

`ops/load/lib/verify-limits.sh` runs before any k6 scenario and asserts cgroup limits actually applied:

```bash
docker inspect <runtime-container> | jq '.[0].HostConfig | {NanoCpus, Memory, MemoryReservation}'
```

Expected: `NanoCpus=2000000000` (2.0 CPU), `Memory=2147483648` (2 GiB). Any deviation aborts the run with clear error. The raw output is embedded in the calibration report's evidence directory for auditability.

### 6.4 Networking + config mounts

Single bridge network `runtime-load`. DNS-by-name between services. Only 3 ports exposed to host:

- `runtime-adapters:8080` — for dev ad-hoc curl + k6 from outside compose.
- `prometheus:9090` — for PromQL sanity.
- `grafana:3000` — for visual sanity.

Read-only mounts for config + fixture data:

| Service | Mount | Source |
|---|---|---|
| `prometheus` | `/etc/prometheus/prometheus.yml` | `ops/local/prometheus.yaml` |
| `prometheus` | `/etc/prometheus/rules/` | `ops/prometheus/rules/` |
| `prometheus` | `/etc/prometheus/generated/` | `ops/prometheus/generated/` |
| `grafana` | `/etc/grafana/provisioning/` | `ops/grafana/provisioning/` |
| `grafana` | `/var/lib/grafana/dashboards/` | `ops/grafana/dashboards/` |
| `otel-collector` | `/etc/otel-collector/config.yaml` | `ops/otel-collector/config.yaml` |
| `runtime-adapters` | `/bench/git/` | `test/fixtures/git-bench/` (built by `make fixture-git-bench`) |
| `k6` | `/scripts/` | `ops/load/scenarios/` |
| `k6` | `/summaries/` | named volume `summaries` for summary.json |

### 6.5 Health + startup ordering

The runtime image is **distroless** (`gcr.io/distroless/static:nonroot`) — no shell, no curl, no busybox. Consequently:

> **`runtime-adapters` has NO container-level healthcheck.** Distroless does not ship `curl`/`wget`/`sh`; embedding a healthcheck binary is scope creep for 2C.2. Startup ordering is governed by `depends_on` on `postgres` (has `pg_isready`) and `otel-collector` (has `curl` in its contrib image); k6 scripts wait-and-retry `GET /healthz` at run start with a 30-second budget before aborting.

This is documented as **A2C2.3** adjustment.

### 6.6 Dockerfile

```dockerfile
# syntax=docker/dockerfile:1.7
FROM golang:1.26.2-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" \
    -o /out/runtime-adapters ./cmd/runtime-adapters

FROM gcr.io/distroless/static:nonroot
COPY --from=build /out/runtime-adapters /runtime-adapters
ENTRYPOINT ["/runtime-adapters"]
```

Distroless base for minimal attack surface + production-realistic shape. Scaffolding is reusable in 2C.4 without changes.

### 6.7 CI smoke compose (4 services, reduced limits)

`ops/local/compose.ci-smoke.yaml` is a **separate file**. Includes only `runtime-adapters`, `postgres`, `http-upstream-mock`, `k6`. No Prometheus, no Grafana, no OTel Collector — CI smoke uses k6's `summary.json` only, no PromQL cross-check.

Limits are tuned for GHA `ubuntu-latest` (private repo: **2 CPU / 8 GiB**):

| Service | `cpus` | `mem_limit` |
|---|---|---|
| `runtime-adapters` | `0.75` | `768m` |
| `postgres` | `0.4` | `256m` |
| `http-upstream-mock` | `0.3` | `128m` |
| `k6` | `0.4` | `256m` |

Total nominal ~1.85 CPU / ~1.4 GiB — leaves air for Docker engine + runner overhead within 2 CPU / 8 GiB. Runtime runs with `OTEL_ENABLED=false` (no collector to export to).

If 2C.2 is ever run on a **public** repo clone (4 CPU / 16 GiB runner), limits are conservative enough to still fit. The smoke is **not** designed to produce calibration-grade numbers — only to flag gross regressions.

> **Operational note for implementers (gotcha #6, not a design change):** if the smoke on `ubuntu-latest` shows excessive jitter or erratic p99 variance between consecutive PRs, lower the `runtime-adapters` and/or `k6` `cpus:` slightly (e.g., 0.75 → 0.6, 0.4 → 0.3) to prioritize smoke stability over aggressive runner utilization. Treat as tuning, not a spec revision.

### 6.8 `http-upstream-mock` determinism (D2C2.20 / A2C2.28)

The mock exists to measure `http.request@v1` latency — **not to test the mock itself**. Requirements:

- Image: `nginx:1.27-alpine` with a fixed static response configuration.
- Response: **`200 OK`**, static body of approximately **1 KiB** (fixed content, no dynamic generation).
- **No payload reflection** (no `/anything`, `/post`, `/put` style echo endpoints).
- **No dump endpoints** that log or return request headers/body.
- **No delay / slowloris / wait endpoints** (e.g., `/delay/{N}`) — latency comes from the runtime, not the mock.
- **No randomized content or timestamps** — the response is byte-identical across requests to isolate variance to the runtime path.

Concretely, `ops/local/mock/default.conf` ships an nginx config with a single location block:

```nginx
server {
    listen 8080;
    location / {
        default_type application/json;
        return 200 '{"status":"ok","body":"<~1KiB of fixed content>"}';
    }
}
```

Rationale: `httpbin` and similar general-purpose mocks introduce Python interpreter startup, route resolution, request logging, and header reflection — all of which contribute measurable variance at the RPS rates used for saturation. nginx serving a literal `return 200` is O(1) in CPU and deterministic in latency.

## 7. Metrics pipeline

### 7.1 Baseline pipeline (pull-based, source of truth for calibration)

```
runtime-adapters  →  OTLP gRPC (:4317)  →  otel-collector
                                               │
                                         Prometheus exporter
                                               │
                                         :8889/metrics  ←  scrape (5s)  ←  prometheus
```

`ops/otel-collector/config.yaml` shape:

```yaml
receivers:
  otlp:
    protocols:
      grpc:
        endpoint: 0.0.0.0:4317

processors:
  batch:
    timeout: 2s

exporters:
  prometheus:
    endpoint: 0.0.0.0:8889
    # Preserve full histogram shape, exemplars, and metric attributes.
    send_timestamps: true
    resource_to_telemetry_conversion:
      enabled: true

service:
  pipelines:
    metrics:
      receivers: [otlp]
      processors: [batch]
      exporters: [prometheus]
```

`ops/local/prometheus.yaml` scrapes:

- `runtime-adapters:8080/metrics` — only if the runtime exposes a Prometheus endpoint natively (2C.2 does NOT add this; relies on OTLP path via collector).
- `otel-collector:8889/metrics` — **primary source** of runtime metrics (post-OTLP translation by the collector's Prometheus exporter).
- `prometheus:9090/metrics` — self-monitoring.

### 7.2 Why not `prometheusremotewrite`

Explicitly rejected. Historical issues:

- OTel-style histograms can be mistranslated when converting to Prometheus delta/cumulative model.
- Exemplar support varies by exporter version; attribute preservation is fragile.
- Timestamp alignment with the rest of the scrape interval is non-trivial.

For a track whose output is **numerical calibration**, pull-based avoids these classes of drift. **ADR 0007** records the decision.

### 7.3 CI smoke pipeline

CI smoke does **not** use the Prometheus-based pipeline. Runtime runs with `OTEL_ENABLED=false`. k6's own `handleSummary()` is the only source of timing data. This is deliberate — CI smoke is advisory, reduced-fidelity, and avoiding the collector path saves ~5 seconds of compose bring-up per PR.

## 8. Calibration report

### 8.1 Location + naming

```
ops/slo/calibration-reports/
├── README.md                          # format reference
├── latest-baseline.json               # machine-readable snapshot for CI smoke comparison
├── schema_test.go                     # Go test (-tags loadreport)
├── YYYY-MM-DD-baseline-v1.md          # first real report (this track)
└── evidence/
    └── YYYY-MM-DD-baseline-v1/        # per-run evidence directory
```

Naming rule: `YYYY-MM-DD-baseline-v<N>.md`. Never edit a committed report; a new run produces a new file. `v<N>` increments on environmental changes (new hardware, compose bumps, code changes affecting perf).

### 8.2 Report structure (Markdown)

Six mandatory sections (enforced by `schema_test.go`):

1. **Header** — date, author, git SHA, track, run duration.
2. **Envelope** — host machine/OS, Docker/Compose versions, services running, cgroup verification output, metrics pipeline declaration, tool versions.
3. **Core tier tables** — one subsection per core capability: baseline distribution, saturation breakpoint, proposed YAML diff, evidence sources.
4. **Git smoke tier** — `git.status@v1` with explicit "SMOKE-CALIBRATED — limited confidence" header; clean vs dirty split documented.
5. **Git rough tier** — observation table for clone/diff/commit; explicit "ROUGH_NO_CHANGE — targets remain PROVISIONAL".
6. **Summary** — change inventory table with columns: File, SLO, Field, Before, After, Tier, Confidence, Decision.

Template lives at `ops/load/lib/report-template.md.tmpl`; rendered by `generate-report.sh`.

### 8.3 Confidence + Decision taxonomy

The Summary table's **Confidence** column is one of:

| Confidence | Meaning | Tier that produces it |
|---|---|---|
| `high` | Full baseline (3m+ at sustained RPS) + saturation + cross-check via PromQL instant + range queries | core |
| `limited` | Smoke profile (<2m load, lower RPS); partial observation | git.status |
| `none` | No load measurement; targets untouched | git.clone/diff/commit ROUGH |

The **Decision** column is one of:

| Decision | Meaning |
|---|---|
| `TIGHTEN` | Proposed target is stricter than current |
| `LOOSEN` | Proposed target is more lenient than current (baseline showed PROVISIONAL was too aggressive) |
| `KEEP` | Measurement confirms current target; no change |
| `SMOKE_CALIBRATED` | New value proposed under smoke profile (limited confidence) |
| `ROUGH_NO_CHANGE` | No measurement attempted; PROVISIONAL retained |
| `SKIPPED_IN_2C2` | Measurement not possible this track (e.g., `git.clone` without `file://` support); defer to 2C.3 |

### 8.4 Evidence artifacts

Per-run evidence directory `evidence/YYYY-MM-DD-baseline-v1/`:

- **`manifest.json`** — machine-readable envelope metadata (host, Docker, compose services + limits, tool versions, pipeline description). Separate from the Markdown report for programmatic access.
- **`summary.json`** — raw k6 `handleSummary(data)` output (structured, v0.58+ format).
- **`<capability>-baseline-prom-instant.json`** — PromQL instant query at run-end, per core capability.
- **`<capability>-baseline-prom-range.json`** — PromQL `query_range` over the full run window, per core capability. Both are required for evidence cross-check.
- **`git-status-smoke-split.json`** — `git.status` metrics segmented by `tree=small-repo|dirty-tree` tag (explicit clean vs dirty).
- **`git-rough-observations.json`** — rough tier raw data (k6 summary subset + selected Prom queries).

All evidence files are linked from the Markdown report via relative paths.

### 8.5 `latest-baseline.json` schema

Used by CI smoke for delta comparison. Committed with each calibration report:

```json
{
  "generated_at": "2026-04-23T14:22:00Z",
  "from_report": "ops/slo/calibration-reports/2026-04-23-baseline-v1.md",
  "envelope_manifest": "ops/slo/calibration-reports/evidence/2026-04-23-baseline-v1/manifest.json",
  "comparison_context": "This snapshot captures distributions measured under the 2C.2 local pinned compose envelope (2 CPU / 2 GiB, full stack with OTel collector, pgx postgres, http-upstream-mock). CI smoke runs under a different, more restrictive envelope (GHA ubuntu-latest private-repo class, reduced limits, stripped compose without OTel collector). Absolute comparison is NOT apples-to-apples; delta thresholds in ci-smoke-comment.sh are calibrated to flag GROSS regressions only (>50% p99 drift).",
  "runner_class_baseline": "local-pinned-compose-2cpu-2gib",
  "runner_class_smoke_expected": "github-actions-ubuntu-latest-private-2cpu-8gib",
  "core": {
    "shell.exec@v1":            { "p50_ms": 42,  "p95_ms": 180, "p99_ms": 340 },
    "filesystem.read_file@v1":  { "p50_ms": 10,  "p95_ms": 40,  "p99_ms": 75  },
    "filesystem.write_file@v1": { "p50_ms": 30,  "p95_ms": 110, "p99_ms": 200 },
    "http.request@v1":          { "p50_ms": 75,  "p95_ms": 290, "p99_ms": 480 }
  }
}
```

Schema enforced by `TestLatestBaseline_Schema` (Go, `-tags loadreport`):

- All 5 root fields required.
- `comparison_context` must be non-empty prose.
- `runner_class_baseline` + `runner_class_smoke_expected` must be non-empty strings.
- `core` must contain at least the 4 core capability keys, each with `p50_ms`/`p95_ms`/`p99_ms` numeric.

### 8.6 Report generation flow

`ops/load/lib/generate-report.sh`:

1. Read `ops/load/lib/report-template.md.tmpl`.
2. Collect inputs:
   - `summary.json` from k6 (via volume mount).
   - Output of `verify-limits.sh` (cgroup check).
   - `docker version`, `docker compose version`, `uname -a`, `sysctl machdep.cpu.brand_string` (macOS) or `lscpu` (Linux).
   - PromQL queries against `prometheus:9090/api/v1/query` and `/api/v1/query_range` per core capability.
3. Write `manifest.json` (machine-readable envelope).
4. Substitute template placeholders with measured values.
5. Write `YYYY-MM-DD-baseline-v1.md` (UTC date, auto-increment `v<N>` if file exists).
6. Copy raw summary + Prom query outputs into `evidence/YYYY-MM-DD-baseline-v1/`.
7. Update `latest-baseline.json` with the per-capability snapshot.
8. Print a human-readable summary to stdout for operator review.

Dependencies: `bash`, `jq`, `curl` — no extra installs (all ubiquitous).

## 9. Tiered git strategy

### 9.1 Fixture construction

`test/fixtures/git-bench/` holds **blueprint sources** (text files + patches) and a `Makefile`. The Makefile target `fixture-git-bench` constructs, from scratch:

- `small-repo/` — a real git repo with 2 commits (initial + file3.txt revision), 10 files total.
- `dirty-tree/` — a copy of `small-repo/` with a pre-applied modified file + pre-applied untracked file.

**Constructed `.git/` directories are NOT committed.** `.gitignore` entries:

```
test/fixtures/git-bench/small-repo/
test/fixtures/git-bench/dirty-tree/
```

Generation is deterministic (fixed commit authors, fixed timestamps via env, fixed content). The Makefile is the single source of truth.

`make fixture-git-bench` is invoked by the compose `load-up` target before the runtime container starts. A Go test (`TestFixtureGitBench_Regenerable`, `-tags fixture`) verifies the generator produces a consistent tree (same file contents + commit SHAs across runs).

### 9.2 Mutation rules

- Fixture sources (`small-repo/`, `dirty-tree/`) are mounted **read-only** in `runtime-adapters:/bench/git/`.
- `git.commit@v1` rough scenario MUST operate on a **fresh tmpfs copy** per iteration (`/tmp/bench-commit-<uuid>`).
- `git.clone@v1` rough scenario MUST use **`file://` URLs only** (`file:///bench/git/small-repo`). Local paths (`/bench/git/small-repo` without `file://` prefix) are forbidden because git uses hardlinks/reflinks for local paths, producing artificially low latency.
- If the adapter rejects `file://`, the scenario is marked `SKIPPED_IN_2C2` in the report (no observation, no YAML touch).

### 9.3 `git.status@v1` clean vs dirty

The smoke scenario alternates 50/50 between `/bench/git/small-repo` (clean working tree) and `/bench/git/dirty-tree` (modified + untracked files) via **`exec.scenario.iterationInTest % 2`** (from `import exec from 'k6/execution'` — the modern API; `__ITER`/`__VU` globals are deprecated). k6 tags emit `tree=small-repo|dirty-tree` so the evidence file `git-status-smoke-split.json` carries segmented metrics.

The report displays:

1. Aggregate distribution (combined).
2. Clean subset distribution.
3. Dirty subset distribution.

Operators see both variants before proposing a target — because dirty trees cost more and the PROVISIONAL target needs to accommodate both.

### 9.4 YAML markers post-calibration

`ops/slo/git.yaml` is edited to reflect tier confidence:

```yaml
# PROVISIONAL — initial operational hypotheses.
# Partially recalibrated in 2C.2 (see ops/slo/calibration-reports/):
#   - git.status@v1: SMOKE-CALIBRATED with limited confidence
#   - git.clone@v1, git.diff@v1, git.commit@v1: still ROUGH / PROVISIONAL
# Full calibration pending real-load evidence (tracked in 2C.3 plan).

slos:
  # ---- git.status@v1 — SMOKE-CALIBRATED (2C.2) ----
  - name: "git-status-availability"
    objective: <calibrated-value>    # smoke-profile, limited confidence
    ...
  - name: "git-status-latency"
    objective: <calibrated-value>    # smoke-profile, limited confidence
    ...

  # ---- git.clone@v1 — PROVISIONAL / ROUGH (pending 2C.3) ----
  - name: "git-clone-availability"
    objective: 99.0                  # unchanged; ROUGH until real-load evidence
    ...
  # ... same pattern for diff + commit
```

The 2C.1 `ops/slo/slo_coverage_test.go` (`-tags sloth`) continues to pass — it checks for SLO presence per Phase 1 capability, not target values.

## 10. CI advisory smoke

### 10.1 Runner constraints

GitHub Actions `ubuntu-latest` specs depend on repo visibility:

| Repo class | CPU | Memory |
|---|---|---|
| **Private** | 2 | 8 GiB |
| Public | 4 | 16 GiB |

The `RVRTelecomunicaciones/sophia-runtime-adapters` repo is treated as **private** — CI smoke is designed against the 2 CPU / 8 GiB profile. Public-repo clones (if any) will have headroom; private-repo is the binding constraint.

### 10.2 Job shape

New `load-smoke` job in `.github/workflows/ci.yaml`:

```yaml
load-smoke:
  name: load-smoke (advisory, PR only)
  if: github.event_name == 'pull_request'
  runs-on: ubuntu-latest
  continue-on-error: true            # NEVER fails the overall CI run
  permissions:
    pull-requests: write              # to post PR comment
  steps:
    - uses: actions/checkout@v4

    - name: Verify Docker + Compose
      run: |
        docker version
        docker compose version

    - name: Start CI smoke stack
      run: |
        docker compose -f ops/local/compose.ci-smoke.yaml up -d --wait

    - name: Wait for runtime readiness (inside compose network)
      run: |
        # Readiness probe runs INSIDE the compose network (no host port
        # dependency). D2C2.18 / A2C2.27: CI smoke compose does not publish
        # runtime:8080 to the host to avoid coupling the readiness check
        # to host port exposure. The k6 image (alpine-based) ships `sh` +
        # `wget`, so we use it as the probe driver.
        docker compose -f ops/local/compose.ci-smoke.yaml run --rm \
          --entrypoint sh k6 -c '
            for i in $(seq 1 30); do
              wget -q -O- http://runtime-adapters:8080/healthz && exit 0
              sleep 1
            done
            echo "runtime-adapters never became ready"; exit 1
          '

    - name: Run k6 smoke
      id: k6
      continue-on-error: true
      run: |
        mkdir -p /tmp/smoke
        docker compose -f ops/local/compose.ci-smoke.yaml run --rm \
          -v /tmp/smoke:/summaries k6 run /scripts/smoke.js
        # Note: smoke-summary.json is written by the script's handleSummary(data)
        # into /summaries/smoke-summary.json — that is the SOLE source of the
        # machine-readable summary. We deliberately avoid --summary-export to
        # prevent two parallel mechanisms producing the same artifact with drift
        # risk (D2C2.17 / A2C2.26).

    - name: Process summary + post PR comment
      if: always()
      env:
        GH_TOKEN: ${{ github.token }}
        K6_EXIT_CODE: ${{ steps.k6.outcome == 'success' && '0' || '1' }}
      run: |
        ./ops/load/lib/ci-smoke-comment.sh \
          /tmp/smoke/smoke-summary.json \
          ops/slo/calibration-reports/latest-baseline.json \
          "$K6_EXIT_CODE" \
          > /tmp/smoke/comment.md
        gh pr comment ${{ github.event.pull_request.number }} \
          --body-file /tmp/smoke/comment.md

    - name: Teardown
      if: always()
      run: docker compose -f ops/local/compose.ci-smoke.yaml down -v
```

### 10.3 PR comment mechanism

`ops/load/lib/ci-smoke-comment.sh` takes three args: smoke summary path, baseline snapshot path, k6 exit code. Behavior:

- **k6 exit 0 + baseline exists** — posts full delta table comparing smoke p99 vs baseline p99 per capability, with color-coded flags (🟢 ≤20% / 🟡 20–50% / 🔴 >50%).
- **k6 exit 0 + no baseline yet** — posts raw numbers with a clear `NO BASELINE YET` header ("this is the first calibration PR — numbers informational only").
- **k6 exit != 0 (failed or timed out)** — posts a **"⚠️ DID NOT COMPLETE CLEANLY"** comment explaining the smoke didn't finish, with a link to the workflow run, and an explicit note that this does NOT block the PR (advisory-only by contract).

All branches produce a PR comment — **the job never fails silently**, even if the underlying work did.

### 10.4 Baseline snapshot handling

Before the first calibration commits `latest-baseline.json`, the file doesn't exist. `ci-smoke-comment.sh` handles this explicitly with the "no baseline yet" branch. This avoids a circular dependency where the 2C.2 bundle that establishes the first baseline would otherwise fail its own CI smoke.

## 11. Testing strategy and CI gates

### 11.1 Testing matrix

| Artifact | Tool | Fail condition |
|---|---|---|
| k6 scenarios (syntax) | `k6 inspect ops/load/scenarios/*.js` | Syntax error, import broken, malformed scenario config |
| `ops/local/compose.yaml` | `docker compose -f <path> config -q` | Invalid YAML, undefined volume/image |
| `ops/local/compose.ci-smoke.yaml` | same | same |
| `ops/otel-collector/config.yaml` | `ops/otel-collector/scripts/validate-config.sh` (wraps pinned `otel/opentelemetry-collector-contrib:0.106.1 validate`) | Config shape invalid |
| `ops/local/prometheus.yaml` | `promtool check config` | Scrape config invalid |
| `Dockerfile` | `docker build -t runtime-adapters:ci-test .` | Build breaks |
| `ops/load/lib/*.sh` | `shellcheck` | Shellcheck warnings/errors |
| `ops/load/lib/report-template.md.tmpl` | `TestCalibrationReport_TemplateRenderable` (Go, `-tags loadreport`) | Placeholders don't substitute, markdown invalid |
| `latest-baseline.json` (if present) | `TestLatestBaseline_Schema` (Go, `-tags loadreport`) | Required fields missing, context/runner_class strings empty, core capabilities missing |
| Committed calibration reports (if present) | `TestCalibrationReport_Structure` (Go, `-tags loadreport`) | Envelope section missing, tier sections missing, Summary table missing |
| Fixture regenerability | `TestFixtureGitBench_Regenerable` (Go, `-tags fixture`) | `make fixture-git-bench` produces inconsistent tree across runs |

### 11.2 Pre-first-calibration handling

`load-report-schema` runs on every PR. When no `latest-baseline.json` and no `YYYY-MM-DD-baseline-v1.md` exist (the state during the 2C.2 bundle that introduces them):

- Tests validate **only artifacts independent of actual calibration runs**: template existence, README existence, Go schema test code compilability.
- Tests for the schemas themselves are skipped via a `ConditionalPresence` branch in each test, with a clear log message: "no calibration artifacts yet — skipping structural validation".
- Once the first calibration PR commits a report + snapshot, the tests transition to enforcing presence + structure on every future PR.

This pattern is encoded in `schema_test.go` as:

```go
func TestLatestBaseline_Schema(t *testing.T) {
    path := filepath.Join(findRepoRoot(t), "ops/slo/calibration-reports/latest-baseline.json")
    if _, err := os.Stat(path); os.IsNotExist(err) {
        t.Skip("latest-baseline.json not yet committed — 2C.2 first calibration pending")
    }
    // ... real schema validation
}
```

### 11.3 CI job structure (delta vs 2C.1)

Four changes to `.github/workflows/ci.yaml`:

1. **`observability` job extension** — a new step validates the 2C.2 ops configs:
   ```yaml
   - name: Validate 2C.2 ops configs
     run: |
       docker compose -f ops/local/compose.yaml config -q
       docker compose -f ops/local/compose.ci-smoke.yaml config -q
       ./ops/otel-collector/scripts/validate-config.sh
       promtool check config ops/local/prometheus.yaml
   ```

2. **`load-ops-lint` new job** — validates shellcheck + k6 scenario syntax on every push + PR.

3. **`load-smoke` new job** — advisory only, PR-only, `continue-on-error: true` (detailed in §10).

4. **`load-report-schema` new job** — runs Go tests with `-tags loadreport` against committed calibration reports; tolerates absence of artifacts before first calibration (§11.2).

### 11.4 Tool version pins (pattern consistent with 2C.1)

| Tool | Pin file | Version |
|---|---|---|
| `sloth` (from 2C.1) | `ops/slo/.sloth-version` | `0.16.0` |
| `promtool` (from 2C.1) | `ops/prometheus/.prometheus-version` | `3.11.2` |
| `amtool` (from 2C.1) | `ops/alertmanager/.alertmanager-version` | `0.32.0` |
| `dashboard-linter` (from 2C.1) | `ops/grafana/.dashboard-linter-version` | `v0.0.0-20250428211052-5e22d6dc65a1` |
| **`k6`** (new) | `ops/load/.k6-version` | `0.58.0` |
| **`otel-collector-contrib`** (new) | `ops/otel-collector/.collector-version` | `0.106.1` |

Both new pins are compared to the installed binary in CI before invocation:

```bash
grep -qF "$(cat ops/load/.k6-version)" <(k6 version)
```

The collector validate script uses the same tag via `docker run --rm otel/opentelemetry-collector-contrib:<version> validate`, so there is **no drift between the config validated in CI and the collector running in the baseline compose**.

## 12. ADRs to be authored

### 12.1 ADR 0007 — k6 for load baseline + pull-based pipeline

Records:

- k6 as primary load tool (vs vegeta, custom Go harness, hey/bombardier).
- Hybrid with opt-in Go benchmarks for SDK — but SDK benchmarks are NOT a 2C.2 deliverable.
- Pull-based metrics pipeline (OTLP → collector → Prometheus scrape) vs `prometheusremotewrite` — rejected for histogram fidelity reasons.
- Tiered git strategy (core serious, git.status smoke, git rough observation) with `file://`-only rule for `git.clone@v1`.

### 12.2 ADR 0008 — Pinned compose envelope for calibration

Records:

- Calibration env is declared via `ops/local/compose.yaml` with cgroup-pinned CPU + memory limits.
- Targets in `ops/slo/*.yaml` are envelope-specific; re-runs needed when env changes (new hardware, compose bumps, code perf deltas).
- CI smoke is advisory-only (`continue-on-error: true`) because GHA `ubuntu-latest` is noisy; never fails the overall run.
- `comparison_context` field in `latest-baseline.json` documents the envelope mismatch between local pinned compose and GHA smoke.

## 13. Out of scope — recap

Items in scope for later tracks, enumerated once more to make the deferral explicit (no surprises):

- **2C.3**: soak tests (if leak symptom appears), programmatic E2E pipeline assertions, chaos injection, alert-fidelity tests, Alertmanager + Tempo in compose.
- **2C.4**: runbooks, real pager/ticket receiver integrations, pgx pool Prometheus collector (unblocks `PoolIdleZero`), Loki integration, full operational local stack (close out of `ops/local/README.md` scope).
- **Indefinite**: SDK formal benchmarks (ad-hoc `go test -bench` is the pattern; no track commits to this yet).

## 14. Preview: bundle projection for the implementation plan

Approximate bundles (final decomposition is the writing-plans skill's job):

1. **Dockerfile + Makefile foundations** — runtime buildable via `docker build`; `make load-*` targets declared (no bodies yet).
2. **Compose foundation** — `ops/local/compose.yaml`, `ops/otel-collector/config.yaml` + pin, `ops/local/prometheus.yaml`, ADR 0007 authored.
3. **k6 scenarios core 4** — `shell_exec.js`, `filesystem_read_file.js`, `filesystem_write_file.js`, `http_request.js`, shared helpers, `suite.js`.
4. **k6 scenarios git tiered** — fixture generator + Makefile + `git_status.js` + `git_rough.js`.
5. **Report infrastructure** — `generate-report.sh`, template, Go schema tests, `latest-baseline.json` shape + first-run tolerance.
6. **CI jobs** — `load-ops-lint`, `load-smoke` with `ci-smoke-comment.sh`, `load-report-schema`, extension to `observability`.
7. **First calibration run + YAML recalibration + ADR 0008 + CHANGELOG 0.3.0 + v0.3.0 tag**.

---

## Appendix A — Closed decisions

| ID | Decision | Source |
|---|---|---|
| **D2C2.1** | Scope tier — core 4 serious calibration; `git.status` smoke-profile; `git.clone`/`diff`/`commit` ROUGH_NO_CHANGE | Q1 |
| **D2C2.2** | k6 as primary HTTP load tool; SDK benchmarks opt-in ad-hoc only | Q2 |
| **D2C2.3** | Scenario shape = baseline + saturation; soak deferred to 2C.3 | Q3 |
| **D2C2.4** | Resource envelope = docker-compose with cgroup-pinned limits; targets are envelope-specific | Q4 |
| **D2C2.5** | Report-driven manual calibration; no automation over `ops/slo/*.yaml` | Q5 |
| **D2C2.6** | CI smoke = advisory-only, `continue-on-error: true`, never fails CI | Q6 |
| **D2C2.7** | Stack bring-up in 2C.2 compose; programmatic E2E pipeline tests deferred to 2C.3 | Q7 |
| **D2C2.8** | Metrics pipeline = OTLP → collector → Prometheus scrape (pull); `prometheusremotewrite` rejected for histogram fidelity | Section 2 review |
| **D2C2.9** | `git.clone@v1` rough scenario uses `file://` only; local path forbidden (hardlink optimization); `SKIPPED_IN_2C2` if adapter rejects `file://` | Section 5 adjustment 1 |
| **D2C2.10** | Fixture source immutable during runs; mutating scenarios operate on tmpfs copies | Section 5 adjustment 2 |
| **D2C2.11** | `.git/` dirs NOT committed; fixture generator Makefile is single source of truth | Section 5 adjustment 3 |
| **D2C2.12** | `git.status@v1` smoke evidence documents clean vs dirty segmented metrics (50/50 split via `exec.scenario.iterationInTest % 2`, `tree=` tag) | Section 5 adjustment 4 |
| **D2C2.13** | CI smoke runner constraint = GHA `ubuntu-latest` private-repo class (2 CPU / 8 GiB); public-repo variant has headroom | Section 6 adjustment 1 |
| **D2C2.14** | Pre-first-calibration tolerance: `load-report-schema` skips structural checks before first calibration artifacts exist; mandatory thereafter | Section 7 adjustment 1 |
| **D2C2.15** | Collector validate uses the same pinned container tag as compose (no drift between validation + runtime) | Section 7 adjustment 2 |
| **D2C2.16** | Single source of truth for calibration artifacts at `ops/slo/calibration-reports/`; `docs/load-baseline.md` references the path (no symlink) | Section 7 adjustment 3 |
| **D2C2.17** | `handleSummary(data)` is the SOLE source of the machine-readable summary. `--summary-export` is NOT used — avoids dual mechanisms producing drift-risk artifacts | Post-spec review 1 |
| **D2C2.18** | CI smoke readiness probe runs INSIDE the compose network via k6 container's `sh` + `wget`; `runtime-adapters:8080` is NOT published to the host in `compose.ci-smoke.yaml` | Post-spec review 2 |
| **D2C2.19** | k6 scripts use the modern `k6/execution` API (`exec.scenario.iterationInTest`, `exec.vu.idInTest`, `exec.vu.iterationInScenario`); `__ITER` / `__VU` globals are NOT used | Post-spec review 4 |
| **D2C2.20** | `http-upstream-mock` is `nginx:1.27-alpine` with a fixed static `return 200` response (~1 KiB, no reflection, no dump endpoints, no delay, no randomization) — measures the runtime, not the mock | Post-spec review 5 |

## Appendix B — Post-approval adjustments

Recorded in order during brainstorming:

| ID | Adjustment | Section |
|---|---|---|
| **A2C2.1** | `git.status` wording: "recalibrado con confianza limitada bajo smoke profile", not just "recalibrated" | §1 |
| **A2C2.2** | `git.clone`/`diff`/`commit` explicit rule: no formal recalibration in 2C.2; stay PROVISIONAL/ROUGH; report documents range, not target decisions | §1 |
| **A2C2.3** | Envelope in report includes host machine/OS, Docker version, list of compose services running during measurement | §1 |
| **A2C2.4** | Metrics pipeline = pull-based (OTLP → collector → Prometheus scrape); reject `prometheusremotewrite` | §2 |
| **A2C2.5** | Resource-limits syntax pinned to Compose v2 non-swarm (`cpus:`, `mem_limit:` service-level); `deploy.resources` explicitly avoided | §2 |
| **A2C2.6** | Distroless runtime has no container healthcheck; `depends_on` + k6 retry handle startup ordering | §2 |
| **A2C2.7** | Grafana is sanity visual only; NOT source of truth for calibration | §2 |
| **A2C2.8** | Compose service count is 7, not 6 — `http-upstream-mock` explicit in inventory | §3 |
| **A2C2.9** | `suite.js` is single baseline entrypoint (not multiple `k6 run` invocations in Makefile) | §3 |
| **A2C2.10** | Machine-readable summary via k6 `handleSummary(data)` (v0.58+ structured format); `generate-report.sh` consumes it | §3 |
| **A2C2.11** | Evidence distinguishes Prometheus `instant` vs `query_range` — two separate files per core capability | §4 |
| **A2C2.12** | Envelope `manifest.json` is a separate machine-readable file (not embedded in the Markdown report) | §4 |
| **A2C2.13** | `confidence` column in Summary table (`high`/`limited`/`none`) cross-references tier | §4 |
| **A2C2.14** | `file://` only for `git.clone@v1`; local path forbidden; `SKIPPED_IN_2C2` if `file://` unsupported | §5 |
| **A2C2.15** | Fixture mount read-only during run; mutations use tmpfs copies | §5 |
| **A2C2.16** | `.git/` dirs not committed; Makefile is single source of truth | §5 |
| **A2C2.17** | `git.status` clean/dirty split documented in separate evidence file | §5 |
| **A2C2.18** | GHA runner class documented (private = 2 CPU / 8 GiB) | §6 |
| **A2C2.19** | CI smoke limits reduced to leave Docker/postgres/mock real headroom (runtime 0.75 CPU / 768m; k6 0.4 CPU / 256m) | §6 |
| **A2C2.20** | k6 compose service YAML fixed (single `volumes:` block) | §6 |
| **A2C2.21** | PR comment posted even when k6 fails (advisory contract: never fails silently) | §6 |
| **A2C2.22** | `latest-baseline.json` carries `comparison_context` + `runner_class_*` fields | §6 |
| **A2C2.23** | `load-report-schema` tolerates pre-first-calibration state | §7 |
| **A2C2.24** | Collector validate pinned to same version as compose container | §7 |
| **A2C2.25** | No symlinks in docs; `docs/load-baseline.md` references `ops/slo/calibration-reports/` by path | §7 |
| **A2C2.26** | `handleSummary()` is sole source; `--summary-export` removed from CI workflow | §10 |
| **A2C2.27** | CI smoke readiness inside compose network (no host port dep) via k6 container's `sh`+`wget` | §10 |
| **A2C2.28** | `suite.js` total duration corrected to ~42–45 min (core ~32m + git ~12.5–13.5m) with breakdown table; no cross-capability concurrent runs for calibration | §5 |
| **A2C2.29** | k6 scripts adopt `k6/execution` modern API; `__ITER`/`__VU` forbidden. Mock is nginx-static with explicit determinism requirements (§6.8) | §5 + §6 |
| **A2C2.30** | Implementer gotcha note in §6.7: if CI smoke jitter is excessive, reduce `runtime-adapters` + `k6` cpus slightly. Operational tuning, not spec revision | §6 |
