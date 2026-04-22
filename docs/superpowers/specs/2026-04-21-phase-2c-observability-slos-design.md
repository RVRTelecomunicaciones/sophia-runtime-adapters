# Phase 2C.1 — Observability Depth + SLOs — Design

> **Status:** Design (pre-plan). Produced via `superpowers:brainstorming` on 2026-04-21.
> **Parent:** Phase 2C (Hardening), track 1 of 4 (2C.1 observability → 2C.2 load baseline → 2C.3 chaos + minimal hardening → 2C.4 operational readiness).
> **Phase 1 reference:** `docs/superpowers/specs/2026-04-19-runtime-adapters-phase1-design.md`.

---

## 1. Purpose

`runtime-adapters` Phase 1 landed an executable governable runtime: 8 capabilities across 4 adapters, `ExecutionReceipt` audit, 11-instrument OTel Registry, append-only persistence, HTTP + in-proc SDK peers. The runtime is **functional** — it is not yet **operable**.

Phase 2C.1 closes the observability gap required to operate the runtime under real traffic:

1. A **contract-bound contextual logger** (`slog`) so every execution emits a fixed, auditable set of fields — no sprintf, no payload leakage.
2. A **metric contract** with bounded cardinality, stable naming, and exemplar linkage to traces.
3. A **per-capability SLO framework** (availability + latency) generated declaratively from Sloth, paired with multi-window multi-burn-rate alerts.
4. A **hand-written layer of infra alerts** covering dependencies and runtime invariants that the SLO model cannot express.
5. A **three-tier alerting topology** (critical / warning / info) routed through Alertmanager with explicit silencing of `info`.
6. A **hierarchical Grafana dashboard set** — one `runtime-overview` for oncall, four per-adapter dashboards for drill-down.
7. A **CI gate** that rejects any change in `ops/` not backed by a parser, validator, or coverage test.

After 2C.1, the runtime can be observed, its SLOs can be measured (even with provisional targets), and an operator has a deterministic first-response surface. The quantitative calibration of the SLO targets themselves is the job of **2C.2 (load baseline)**. Pager/ticket integration, chaos, docker-compose local stack, Kubernetes manifests, and runbook authorship are **2C.3 and 2C.4**.

## 2. Scope

### 2.1 In-scope for 2C.1

- `internal/infrastructure/obs/log/` — contextual `slog` wrapper, contract fields, level mapping, env-driven config.
- `ops/slo/*.yaml` — Sloth SLO specs (one file per adapter).
- `ops/prometheus/generated/slo_rules.yaml` — Sloth-generated, checked-in.
- `ops/prometheus/rules/infra_*.yaml` — hand-written infra alerts + `*.test.yaml` fixtures.
- `ops/alertmanager/alertmanager.yaml` — three-tier routing, inhibition, grouping.
- `ops/otel-collector/config.yaml` — operational config (NOT contract).
- `ops/grafana/provisioning/` — datasource and dashboard providers.
- `ops/grafana/dashboards/` — 5 JSON dashboards.
- `.github/workflows/ci.yaml` — new `observability` job.
- `Makefile` targets: `sloth-generate`, `test-obs`, `test-rules`, `test-alertmanager`, `test-dashboards`.
- Metric contract refactor in `internal/infrastructure/obs/metrics.go` (11-instrument Registry → 11 instruments revised per §6.2).
- Extension of `ExecutionRequest` / HTTP inbound to propagate `correlation_id` through to the logger.
- Docs: `docs/metrics.md` (catalog), `docs/logging.md` (contract), `docs/slo.md` (framework overview).

### 2.2 Out of scope (deferred)

| Item | Landing track |
|---|---|
| `ops/local/docker-compose.yaml` | 2C.4 |
| `deploy/k8s/*` (Prometheus / Alertmanager / Grafana / Collector manifests) | 2C.4 |
| `docs/runbook/*.md` (per-alert runbooks) | 2C.4 |
| Real pager / ticket integrations (PagerDuty, Opsgenie, Slack, Linear) | 2C.4 |
| Loki log integration + dashboard click-through | 2C.4 or later (final landing TBD) |
| Annotations layer (deploys / incidents overlay) | 2C.4 |
| Dynamic log-level reload (SIGHUP or config watch) | 2C.4 if demanded |
| Load-derived SLO target calibration | **2C.2** |
| Alert delivery timing / latency validation | 2C.3 |
| Chaos injection + degradation SLO coverage | 2C.3 |
| End-to-end observability pipeline test (runtime → collector → prom → sloth → alertmanager → receiver) | 2C.2 / 2C.3 |
| Dashboard snapshot / golden-image regression tests | 2C.4 if demanded |
| OTel log signal export (we only emit stdout; collector picks up via container) | 2C.4 |
| Sampling of traces or logs | 2C.2 if volume problem |

## 3. Approach — Hybrid (B for SLOs, A for the rest)

Two candidate approaches were weighed during brainstorming:

- **A — Explicit and minimal:** everything hand-written, zero external generators.
- **B — Sloth-assisted:** declarative SLOs, Sloth generates recording + burn-rate rules.
- **C — Production-grade foundation:** Helm, Kustomize, codegen, dynamic reload, integration tests against real Prometheus.

**Chosen: hybrid B + A.**

- **B for SLOs** — Sloth generates availability and latency recording rules plus fast/slow burn alerts, one spec per adapter. Sloth's burn-rate math is Google-SRE-vetted; hand-writing 32 burn-rate alerts across 8 capabilities is error-prone and drift-prone.
- **A for everything else** — logger, dashboards, Alertmanager routing, infra alerts, runbooks: hand-written, versioned, owned by the team. Each line carries intent.
- **Not C** — Helm, Kustomize, codegen, dynamic reload, real-Prom integration tests belong to 2C.4 (operational readiness) when the need is concrete.

Sloth runs **build-time only**, never runtime. Its output lives checked-in at `ops/prometheus/generated/slo_rules.yaml`. Contributors read the generated file directly when debugging; CI enforces idempotency (`sloth generate` must produce a zero-diff against the checked-in version).

## 4. Architecture and layout

### 4.1 Repository file tree (post-2C.1)

```
internal/infrastructure/obs/log/            # NEW — contextual slog wrapper (§5)
  ├── logger.go                             # Logger, New, NewNop, With, Debug/Info/Warn/Error
  ├── config.go                             # Config + Load reading RUNTIME_LOG_*
  ├── ctx.go                                # ContextWith, FromContext (never-nil)
  ├── levels.go                             # LevelFor(status, errClass) → slog.Level
  └── *_test.go

internal/infrastructure/obs/metrics.go      # REVISED — 11-instrument Registry (§6.2)

ops/                                        # NEW — operational artifacts tree
  ├── slo/                                  # Sloth SLO specs, one per adapter (§7)
  │   ├── shell.yaml
  │   ├── git.yaml
  │   ├── filesystem.yaml
  │   ├── http.yaml
  │   └── .sloth-version                    # pinned Sloth CLI version
  ├── prometheus/
  │   ├── .prometheus-version               # pinned promtool version
  │   ├── generated/
  │   │   ├── slo_rules.yaml                # Sloth output, checked-in
  │   │   └── slo_rules.test.yaml           # promtool fixtures for SLO rules
  │   └── rules/                            # hand-written infra alerts (§8)
  │       ├── infra_pool.yaml
  │       ├── infra_pool.test.yaml
  │       ├── infra_otel.yaml
  │       ├── infra_otel.test.yaml
  │       ├── infra_migrate.yaml
  │       ├── infra_migrate.test.yaml
  │       ├── infra_panics.yaml
  │       ├── infra_panics.test.yaml
  │       ├── infra_persistence.yaml
  │       └── infra_persistence.test.yaml
  ├── alertmanager/
  │   ├── .alertmanager-version             # pinned amtool version
  │   └── alertmanager.yaml                 # three-tier routing (§9)
  ├── otel-collector/
  │   └── config.yaml                       # operational config, NOT contract
  └── grafana/
      ├── .dashboard-linter-version         # pinned dashboard-linter version
      ├── provisioning/
      │   ├── datasources/
      │   │   ├── prometheus.yaml
      │   │   ├── tempo.yaml
      │   │   └── alertmanager.yaml         # required by Alert list panel (§10)
      │   └── dashboards/
      │       └── provider.yaml             # path → ../dashboards/ mount (not provisioning/)
      └── dashboards/
          ├── runtime-overview.json         # UID: runtime-overview
          ├── runtime-shell.json            # UID: runtime-shell
          ├── runtime-git.json              # UID: runtime-git
          ├── runtime-fs.json               # UID: runtime-fs
          └── runtime-http.json             # UID: runtime-http

docs/
  ├── metrics.md                            # NEW — catalog + label discipline
  ├── logging.md                            # NEW — logger contract
  └── slo.md                                # NEW — framework overview

docs/runbook/                               # RESERVED — populated in 2C.4 (singular path)

deploy/k8s/                                 # RESERVED — populated in 2C.4

.github/workflows/ci.yaml                   # REVISED — new observability job (§11)
Makefile                                    # REVISED — new obs targets
```

### 4.2 Layering

- `internal/` imports nothing from `ops/`. The two trees are disjoint.
- The metric contract (names + labels) is **source-of-truth in code** (`internal/infrastructure/obs/metrics.go`); `ops/` consumes it by name via PromQL. CI tests (§11) verify every PromQL reference matches a declared instrument.
- `ops/otel-collector/config.yaml` is operational configuration — **replaceable**, not contract. The contract lives in metric names, metric labels, and the Prometheus rules/queries that consume them. A team can swap the collector for any OTLP-compatible alternative as long as it forwards the same metric names untouched.

### 4.3 Decoupling invariant

> **I23 (new):** The metric contract is defined by instrument names and label sets in `internal/infrastructure/obs/metrics.go`. No operational component (collector, scraper, exporter) is allowed to rename, filter, or drop instruments declared there. Rename/drop in `metrics.go` = ADR + schema bump.

## 5. Logger — `internal/infrastructure/obs/log`

### 5.1 Principle

The logger is **contract-bound, not free-form**. Every log emitted within the UC1 flow carries a fixed set of fields derived from `ExecutionRequest` and `ExecutionReceipt`. No sprintf. No payloads. No `request_id` as a core concern (chi's HTTP-layer `X-Request-Id` stays inside the inbound HTTP adapter; execution logs use `correlation_id` and `handle_id` for correlation).

### 5.2 API

```go
package log

type Config struct {
    Format    string // "text" | "json"; default "text" in 2C.1 source, "json" forced via env in ops
    Level     string // "debug" | "info" | "warn" | "error"; default "info"
    AddSource bool   // default false
}

type Logger struct { inner *slog.Logger }

func New(cfg Config) (*Logger, error)     // strict enum validation; R10 compliant
func NewNop() *Logger                     // discard handler; never-nil safety net

func (l *Logger) With(attrs ...slog.Attr) *Logger
func (l *Logger) Debug(ctx context.Context, msg string, attrs ...slog.Attr)
func (l *Logger) Info(ctx  context.Context, msg string, attrs ...slog.Attr)
func (l *Logger) Warn(ctx  context.Context, msg string, attrs ...slog.Attr)
func (l *Logger) Error(ctx context.Context, msg string, attrs ...slog.Attr)

// Context plumbing (never-nil invariant)
func ContextWith(ctx context.Context, l *Logger) context.Context
func FromContext(ctx context.Context) *Logger  // returns NewNop() if missing
```

### 5.3 Contractual fields

| Field | Presence | Origin | Appears at |
|---|---|---|---|
| `capability` | always | `ExecutionRequest.Capability` | Step 3 (resolution) onward |
| `adapter` | always | `CapabilityDef.Adapter` | Step 3 onward |
| `correlation_id` | always | request envelope / HTTP header `X-Correlation-Id` | Step 1 onward |
| `handle_id` | always (Step 5+) | `IDGen` ULID | Step 5 onward |
| `status` | always (Step 7+) | `ExecutionReceipt.Status` | Step 7 (post-normalize) onward |
| `receipt_id` | always (Step 8+) | `ExecutionReceipt.ID` | Step 8 (post-assembly) onward |
| `duration_ms` | always (Step 11) | `receipt.FinishedAt − StartedAt` | Step 11 (final emit) |
| `trace_id` | when span active | OTel span context | whenever a span exists |
| `error_class` | when `status != success` | `ExecutionReceipt.ErrorClass` | Step 7 onward, all levels (INFO/WARN/ERROR) |

**Forbidden:**

- `request_id` as a runtime-level core field (HTTP adapter's internal concern only).
- payload bodies (ni truncados ni hasheados).
- any field not declared in this table as a "standard" attribute of execution-scoped logs.

### 5.4 Level mapping (`levels.go`)

```go
func LevelFor(status ExecutionStatus, errClass ErrorClass) slog.Level {
    switch status {
    case Success, Cancelled:
        return slog.LevelInfo
    case Timeout, Partial:
        return slog.LevelWarn
    case Failure:
        switch errClass {
        case ValidationFailure, CapabilityUnknown, PayloadSchemaFailure, PreconditionFailure:
            return slog.LevelInfo    // caller fault; not a runtime concern
        case ExternalFailure, Interrupted:
            return slog.LevelWarn    // per-request transient; burn-rate decides if sustained
        case AdapterInternalError, NormalizationFailure:
            return slog.LevelError   // panic-recovery or invariant violation
        }
    }
    return slog.LevelError           // defensive: unknown status escalates
}
```

Orthogonal emissions (not tied to `ExecutionReceipt` outcome) are **always ERROR**:

- `"persist receipt"` failure (A4.3 violation — Step 9).
- `"panic recovered"` (R4 — middleware or adapter).
- `"migrate failed on startup"` (bootstrap aborts).
- `"otel exporter unhealthy"` — WARN (no-op degradation, runtime keeps running).

### 5.5 Context propagation

1. **Bootstrap** constructs the root logger via `log.New(cfg)` **before** OTel setup. Logger failure aborts startup.
2. **HTTP inbound middleware** `inboundhttp.LoggerMiddleware` runs after `RequestID` and before `panicRecoverer`. It binds the root logger to the request context, enriched with `correlation_id` sourced from `X-Correlation-Id` (generated if absent).
3. **`ExecuteService`** derives a child logger at Step 5: `FromContext(ctx).With(capability, adapter, handle_id)`. Enriches again at Step 7 with `status` (post-normalize) and optionally `error_class`; at Step 8 with `receipt_id`.
4. **Adapters** may call `FromContext(ctx)` for adapter-internal logs. Optional — not required. Raw adapter outcomes still do not cross the domain boundary (R5 unchanged).
5. **Final emission** at Step 11: `log.FromContext(ctx).<LevelFor(...)>(ctx, "execution complete", status=..., receipt_id=..., duration_ms=...)`.

### 5.6 Configuration

```
RUNTIME_LOG_FORMAT       = text | json             (default: text)
RUNTIME_LOG_LEVEL        = debug|info|warn|error   (default: info)
RUNTIME_LOG_ADD_SOURCE   = false | true            (default: false)
```

- Default `FORMAT=text` for dev-ergonomic output.
- Ops manifests (compose/k8s, 2C.4) will **force** `RUNTIME_LOG_FORMAT=json` via env var — no code change, just config.
- Strict enum validation in `Config.Validate()`: out-of-range → error at startup (R10).
- Unknown `RUNTIME_LOG_*` keys are **not rejected** (stdlib `os.LookupEnv` is key-specific). R10 applies to declared config surface; env variables by convention are additive.

### 5.7 Testing

- `logger_test.go` — in-memory `slog.Handler` that records `slog.Record`s; assertions on level, message, and attribute presence.
- `levels_test.go` — exhaustive table of (Status × ErrorClass) combinations; asserts expected level and that the `Failure` branch is the only one that inspects ErrorClass.
- `ctx_test.go` — `FromContext` on nil-derived ctx returns Nop; `ContextWith` round-trip preserves logger identity.
- Integration test in `internal/application/services/execute_service_log_test.go`: verifies a `success` → INFO with 7 required fields populated; a `failure/adapter_internal_error` → ERROR with `error_class`; a `cancelled` → INFO (NOT WARN).
- `trace_id` presence test: wrap a real OTel tracer (NoopTracerProvider insufficient; use `go.opentelemetry.io/otel/sdk/trace/tracetest`), assert `trace_id` field appears when span is active.

## 6. Metric contract

### 6.1 Principle

Metrics are **public contract**. Bounded cardinality is an invariant (R16, §12.1). Rename, removal, or re-typing of any declared instrument is breaking change: **ADR + schema version bump**. Additive labels on an existing instrument are non-breaking only if they respect the label blacklist.

### 6.2 Naming convention

- OTel-native in Go code: `runtime_adapters.execution.total`, `runtime_adapters.execution.duration`.
- Prometheus names derived by OTel SDK: `runtime_adapters_execution_total`, `runtime_adapters_execution_duration_seconds`.
- Prefix `runtime_adapters_` is mandatory (prevents collision when the collector shares scrape with other Sophia services).

### 6.3 Instrument catalog (post-2C.1)

Phase 1's 11-instrument Registry is revised. The new catalog keeps the cardinality at 11 instruments but realigns for SLI consumption:

| # | Name | Type | Unit | Labels | Purpose |
|---|---|---|---|---|---|
| 1 | `runtime_adapters.execution.total` | counter | `{executions}` | `capability`, `status` | SLI availability numerator + denominator |
| 2 | `runtime_adapters.execution.duration` | histogram | `s` | `capability` | SLI latency; **success-only** observations |
| 3 | `runtime_adapters.execution.active` | up-down counter | `{executions}` | `capability` | Inflight saturation |
| 4 | `runtime_adapters.concurrency.rejects` | counter | `{rejects}` | — | A9.1 fast-reject rate |
| 5 | `runtime_adapters.adapter.panics` | counter | `{panics}` | `adapter` | R4 panic signal |
| 6 | `runtime_adapters.receipt.persist.failures` | counter | `{failures}` | — | A4.3 violation signal |
| 7 | `runtime_adapters.idempotency.replays` | counter | `{replays}` | `capability` | D6.4 cache hit observability |
| 8 | `runtime_adapters.pool.connections.acquired.duration` | histogram | `s` | — | pgx pool saturation proxy |
| 9 | `runtime_adapters.otel.exporter.queue.size` | gauge | `{items}` | `signal` | Collector health |
| 10 | `runtime_adapters.migrate.failures` | counter | `{failures}` | — | Bootstrap health signal |
| 11 | `runtime_adapters.partial.signal` | counter | `{partials}` | `capability` | **Secondary simplified degradation signal** (§6.5) |

**Changes vs. Phase 1 Registry:**

1. **+ `partial.signal`** — new. Not a data-gap fix; `execution.total{status="partial"}` carries the same count. `partial.signal` exists as a **secondary simplified signal** for dashboards and warning-tier alerts that watch degradation without coupling to the SLO availability query.
2. **`execution.total` label change** — status now a label (5-enum). Was 5 separate counters; consolidated into 1 instrument × 5 label values. Aligns with Q5 balanced labels.
3. **Removed**: `timeout_executions_total`, `normalization_failures_total`. Subsumed by `execution.total{status="timeout"}` and `execution.total{status="failure"}` with `error_class` propagated to logs.
4. **`execution.duration` is success-only.** The histogram observes only executions with `status=success`. Failed and timed-out executions do NOT emit a duration observation. This keeps the latency SLI clean: `histogram_quantile` over this instrument is "p99 latency of successful executions" by construction, no filtering required. Diagnostic histograms for failure latency are **not in 2C.1**; if needed later, add a separate instrument (`execution.duration.failures`).

**Histogram bucket configuration.** `execution.duration` uses explicit bucket boundaries that include every latency threshold referenced in `ops/slo/*.yaml`. The provisional target table (§7.3) requires buckets at **0.5s, 1s, 2s, 3s, 5s, 10s, 30s** as a minimum; additional buckets (e.g. 0.1s, 0.25s, 60s) round out the distribution for dashboards. When 2C.2 calibrates the targets, any new threshold value must be accompanied by a matching bucket boundary — `metric_contract_test.go` enforces this by cross-checking Sloth spec `le="<value>"` references against the declared bucket set.

**Cardinality budget (Phase 1):**

- `execution.total`: 8 capabilities × 5 statuses = **40 series**.
- `execution.duration`: 8 capabilities × ~12 histogram buckets = **~96 series**.
- `partial.signal`: 8 capabilities = **8 series**.
- `idempotency.replays`: 8 capabilities = **8 series**.
- `adapter.panics`: 4 adapters = **4 series**.
- `execution.active`: 8 capabilities = **8 series**.
- `otel.exporter.queue.size`: ~3 signals = **3 series**.
- Scalar counters/gauges: 1 series each.

Total ceiling: **~170 series**. Comfortable below any scraper cost threshold.

### 6.4 Label discipline (R16)

**Permitted as metric labels:**

- `capability` (bounded by catalog; 8 in Phase 1).
- `adapter` (bounded; 4 in Phase 1).
- `status` (5-enum, closed).
- `signal` (OTel signal type: `traces|metrics|logs`).

**Prohibited as metric labels** (these belong in logs and/or exemplars):

- `error_class` — 10 values × 8 capabilities × 5 statuses = 400 extra series just on `execution.total`. Not worth it. Emitted as log field when `status != success`.
- `receipt_id`, `handle_id`, `correlation_id`, `trace_id` — unbounded. Attached as exemplars or log fields.
- `retry_hint` — derivable from `error_class` via `DefaultRetryHint` mapping; redundant.

**CI enforcement (§11):** `metric_contract_test.go` parses the instrument catalog at test time, asserts every declared label is in the whitelist, and enforces per-instrument cardinality budget (capability × status × adapter product, bounded by the Phase 1 catalog).

### 6.5 Exemplars

- Every observation of `execution.duration` attempts to attach exemplars `{trace_id, receipt_id}`.
- **`trace_id` is mandatory** when a span is active — the exemplar is not emitted at all if no span is in context.
- **`receipt_id` is best-effort**. If attachment fails (unexpected race or span not yet populated), the exemplar emits with only `trace_id`. The observation itself is never dropped.

This links dashboard p99 spikes → trace view in Tempo. Without exemplars, the `trace_id` in logs and the p99 bucket in the histogram are disconnected.

### 6.6 SLI recording rules (canonical input to §7)

```promql
# Availability per capability — cancelled + partial neutral (Q2)
runtime_adapters:sli_availability:ratio_rate5m =
  sum by (capability) (rate(runtime_adapters_execution_total{status="success"}[5m]))
  /
  sum by (capability) (rate(runtime_adapters_execution_total{status=~"success|failure|timeout"}[5m]))

# Latency SLI per capability (success-only histogram, no status filter needed)
runtime_adapters:sli_latency_p99:seconds =
  histogram_quantile(0.99,
    sum by (le, capability) (rate(runtime_adapters_execution_duration_seconds_bucket[5m])))
```

These two expressions are the **canonical SLI contract**. Sloth consumes them via its latency/availability spec forms; dashboards consume them directly.

### 6.7 Stability contract

- **Rename an instrument** → breaking. ADR + `runtime_adapters_metrics_schema_version` bump.
- **Add a new instrument** → additive. No ADR required.
- **Remove an instrument** → ADR + deprecation period (one release with warning).
- **Add a label respecting the whitelist** → additive, no ADR.
- **Add a label in the blacklist** → rejected in CI.
- **Change unit or type** → breaking. ADR required.

## 7. SLO specs via Sloth

### 7.1 Structure

One Sloth spec file per **adapter**, not per capability. Adapter-grouped SLOs share contextual labels (`adapter: "git"`) and allow adapter-level rollups without duplicating target definitions.

```
ops/slo/
  ├── shell.yaml        # 1 SLO pair (availability + latency) for shell.exec@v1
  ├── git.yaml          # 4 SLO pairs: status, clone, diff, commit
  ├── filesystem.yaml   # 2 SLO pairs: read_file, write_file
  ├── http.yaml         # 1 SLO pair for http.request@v1
  └── .sloth-version    # e.g. "v0.12.0" — pinned CLI version (§7.7)
```

Total: 8 capabilities × 2 SLOs (availability, latency) = 16 SLO definitions.

### 7.2 Sloth spec format (Prometheus v1)

Canonical shape of one SLO (git availability):

```yaml
version: "prometheus/v1"
service: "runtime-adapters"
labels:
  team: "platform"
  adapter: "git"
slos:
  - name: "git-status-availability"
    objective: 99.5
    description: "git.status@v1 non-error execution rate"
    sli:
      events:
        error_query: |
          sum(rate(runtime_adapters_execution_total{capability="git.status@v1",status=~"failure|timeout"}[{{.window}}]))
        total_query: |
          sum(rate(runtime_adapters_execution_total{capability="git.status@v1",status=~"success|failure|timeout"}[{{.window}}]))
    alerting:
      name: GitStatusAvailabilityBurn
      labels:
        capability: "git.status@v1"
      page_alert:
        labels: { severity: critical }
      ticket_alert:
        labels: { severity: warning }
```

Canonical shape of one latency SLO (bucket-based, **correct direction**):

```yaml
  - name: "git-status-latency"
    objective: 99.0
    description: "99% of git.status@v1 requests complete under 2s"
    sli:
      events:
        # bad events: requests that took LONGER than the threshold
        error_query: |
          sum(rate(runtime_adapters_execution_duration_seconds_count{capability="git.status@v1"}[{{.window}}]))
          -
          sum(rate(runtime_adapters_execution_duration_seconds_bucket{capability="git.status@v1",le="2"}[{{.window}}]))
        # total events: all counted requests
        total_query: |
          sum(rate(runtime_adapters_execution_duration_seconds_count{capability="git.status@v1"}[{{.window}}]))
    alerting:
      name: GitStatusLatencyBurn
      labels:
        capability: "git.status@v1"
      page_alert:
        labels: { severity: critical }
      ticket_alert:
        labels: { severity: warning }
```

### 7.3 Provisional targets (2C.1)

> **PROVISIONAL — operational hypotheses only.**
> Quantitative calibration is the job of **track 2C.2 (load baseline)**. The targets below exist so the framework compiles end-to-end. Changing these before 2C.2 closes requires justification in the PR description.

| Capability | Availability | Latency p99 threshold |
|---|---|---|
| `shell.exec@v1` | 99.5% | 5s |
| `git.status@v1` | 99.5% | 2s |
| `git.clone@v1` | 99.0% | 30s (high variance by repo size) |
| `git.diff@v1` | 99.5% | 3s |
| `git.commit@v1` | 99.5% | 2s |
| `filesystem.read_file@v1` | 99.9% | 500ms |
| `filesystem.write_file@v1` | 99.9% | 1s |
| `http.request@v1` | 99.0% | 10s |

Every Sloth spec file includes a header comment making the provisional status explicit:

```yaml
# PROVISIONAL — initial operational hypotheses.
# Quantitative calibration deferred to track 2C.2 (load baseline).
# Changing these targets before 2C.2 closes requires explicit justification in PR.
```

### 7.4 Burn-rate windows

Q3 decision: **multi-window multi-burn-rate, exactly 2 windows per SLO**. Sloth's default is 4; we explicitly disable the others.

- **Fast burn (page)** — 2% of 30d budget consumed in 1h → `severity: critical` → routes to oncall-pager slot.
- **Slow burn (ticket)** — 5% of 30d budget consumed in 6h → `severity: warning` → routes to ops-tickets slot.

Budget basis: **30d rolling window**.

Sloth CLI invocation:

```
sloth generate \
  --input ops/slo/ \
  --output ops/prometheus/generated/slo_rules.yaml \
  --default-slo-period 30d \
  # two alert windows only — Sloth exposes this via --default-slo-plugins-disable-alerts
  # or via per-slo `alerting.disable: [slow-burn-3d, slow-burn-30d]`; exact flag depends on pinned version
```

Exact Sloth flags are version-sensitive; the pinned `.sloth-version` locks the interface. The CI idempotency gate (§7.5) catches any drift if flags shift across versions.

### 7.5 Generated output + idempotency

- Output path: `ops/prometheus/generated/slo_rules.yaml` (single monolithic file).
- Contents (shape):
  - **Per-SLO recording rules** — Sloth emits SLI rate recordings at multiple aggregation windows per SLO (exact count depends on pinned Sloth version; expect O(N × W) where N = 16 SLOs and W = windows per SLO).
  - **Alert rules — exactly 2 per SLO** (fast-burn page + slow-burn ticket). Total = 32. Other burn windows Sloth would default-emit are explicitly disabled via the spec (§7.4).
  - **Metadata metrics** per SLO (`slo_objective_ratio`, `slo_time_period_days`, `slo_error_budget_ratio`).

- **Idempotency gate in CI:**

```bash
sloth generate --input ops/slo/ --output /tmp/regenerated.yaml
diff -u ops/prometheus/generated/slo_rules.yaml /tmp/regenerated.yaml
```

Non-zero diff fails the build. Developers run `make sloth-generate` after editing specs; CI catches anyone who forgot.

### 7.6 Coverage test vs. catalog

Test file `ops/slo/slo_coverage_test.go` (build tag `sloth`):

```go
func TestSloth_CoversAllPhase1Capabilities(t *testing.T) {
    catalog := valueobjects.Phase1CapabilityIDs()   // 8 IDs
    slos := parseSlothYAMLs("ops/slo/*.yaml")       // spec-level parse
    for _, c := range catalog {
        t.Run(c.String(), func(t *testing.T) {
            require.True(t, slos.HasObjective(c, "availability"))
            require.True(t, slos.HasObjective(c, "latency"))
        })
    }
}
```

Gate: adding a capability to Phase 1's catalog without adding matching SLO entries breaks CI. Same spirit as `VerifyCoversPhase1Catalog` from Phase 1's adapter registry.

### 7.7 Sloth version pin

- `ops/slo/.sloth-version` contains the exact released version of the Sloth CLI (e.g. `v0.12.0`).
- CI step **before** any `sloth generate` invocation:

```bash
VERSION="$(cat ops/slo/.sloth-version)"
sloth --version | grep -qF "${VERSION}" \
  || { echo "Sloth version mismatch: expected ${VERSION}"; exit 1; }
```

Since the generated YAML is committed, any drift in the Sloth output format (between versions) becomes a conscious upgrade: bump `.sloth-version`, regenerate, commit the diff.

### 7.8 Schema versioning (SLO layer)

Sloth specs themselves have no built-in `schema_version`. We define the contract:

- **Change of `objective` value** (e.g. 99.5 → 99.9) → operational tuning. No ADR.
- **Change of SLI query shape** (e.g. shifting the latency bucket formula, adding a filter) → **ADR** + bump of a metadata metric `runtime_adapters_slo_schema_version` emitted by Sloth (`labels:` block). Dashboards can surface the schema version so operators see when SLO semantics shifted.
- **Removal of an SLO** → ADR.
- **Addition of a new SLO** for a new capability → additive, no ADR.

## 8. Infra alerts (hand-written)

### 8.1 Inventory

Five families. Each `.yaml` is paired with a `.test.yaml` fixture for `promtool test rules`.

| Family | Rule file | Alerts | Default severity |
|---|---|---|---|
| Pool | `infra_pool.yaml` | `PoolAcquireLatencyHigh` (p95 > 1s, 5m) | warning |
| | | `PoolConnectionsExhausted` (p95 > 5s, 2m) | **critical** |
| | | `PoolIdleZero` (zero idle sustained, 1m) | warning |
| OTel | `infra_otel.yaml` | `OTelExporterQueueFull` (>90% capacity, 5m) | **critical** |
| | | `OTelExporterDown` (no exports, 10m) | warning |
| Migrate | `infra_migrate.yaml` | `MigrateFailureOnStartup` (counter > 0) | **critical** |
| Panics | `infra_panics.yaml` | `AdapterPanicSpike` (rate > 0.01/s, 5m) | **critical** |
| Persistence | `infra_persistence.yaml` | `ReceiptPersistFailureSpike` (rate > 0.1/s, 2m) | **critical** |
| | | `ReceiptPersistAnomaly` (rate > 0, sustained 10m) | warning |

Plus two `info`-tier alerts (dashboard-only, silenced at Alertmanager root):

| Alert | Rule file | Condition |
|---|---|---|
| `IdempotencyReplaySurge` | `infra_persistence.yaml` (shared file) | `rate(idempotency_replays_total[5m]) > <threshold>`, 10m sustained |
| `ConcurrencyRejectsElevated` | `infra_otel.yaml` (shared file) | `rate(concurrency_rejects_total[5m]) > <threshold>`, 10m sustained |

Thresholds for `info` alerts stay provisional (matched to 2C.2 baseline).

### 8.2 Canonical example

```yaml
# ops/prometheus/rules/infra_pool.yaml
groups:
  - name: runtime-adapters-pool
    interval: 30s
    rules:
      - alert: PoolConnectionsExhausted
        expr: |
          histogram_quantile(0.95,
            sum by (le) (
              rate(runtime_adapters_pool_connections_acquired_duration_seconds_bucket[5m])
            )
          ) > 5
        for: 2m
        labels:
          severity: critical
        annotations:
          summary: "pgx pool saturada (p95 acquire > 5s durante 2m)"
          description: |
            El pool pgx está al límite. Puede ser carga legítima o db degradada.
            Queries candidatas: receipt inserts bloqueadas, idempotency lookups lentas.
```

**No `runbook_url` annotation in 2C.1** — runbooks land in 2C.4. Annotations are added when the corresponding runbook file exists.

### 8.3 Test fixture shape

```yaml
# ops/prometheus/rules/infra_pool.test.yaml
rule_files:
  - infra_pool.yaml

evaluation_interval: 30s

tests:
  - name: pool_exhausted_fires_critical
    interval: 30s
    input_series:
      - series: 'runtime_adapters_pool_connections_acquired_duration_seconds_bucket{le="1"}'
        values: '0+0x20'
      - series: 'runtime_adapters_pool_connections_acquired_duration_seconds_bucket{le="5"}'
        values: '0+0x20'
      - series: 'runtime_adapters_pool_connections_acquired_duration_seconds_bucket{le="+Inf"}'
        values: '0+1x20'
    alert_rule_test:
      - eval_time: 4m
        alertname: PoolConnectionsExhausted
        exp_alerts:
          - exp_labels: { severity: critical }
            exp_annotations:
              summary: "pgx pool saturada (p95 acquire > 5s durante 2m)"
```

### 8.4 Stability contract (infra alerts)

- **Change of `expr` or `for` duration** → operational tuning. No ADR.
- **Change of `severity`** → ADR (changes Alertmanager routing and oncall expectations).
- **Rename of `alert`** → breaking (referenced in dashboards and potential future inhibition if ever relevant). ADR.

## 9. Alertmanager routing

### 9.1 Three-tier structure

File: `ops/alertmanager/alertmanager.yaml`.

```yaml
global:
  resolve_timeout: 5m

route:
  receiver: null-receiver
  group_by: ['alertname', 'adapter', 'capability']
  group_wait: 30s
  group_interval: 5m
  repeat_interval: 4h       # applies to base route (null-receiver anyway; inert)
  routes:
    - matchers: [ 'severity="critical"' ]
      receiver: null-receiver           # 2C.4 swaps to oncall-pager
      group_wait: 30s
      group_interval: 5m
      repeat_interval: 30m
      continue: false
    - matchers: [ 'severity="warning"' ]
      receiver: null-receiver           # 2C.4 swaps to ops-tickets
      group_wait: 1m
      group_interval: 15m
      repeat_interval: 4h
      continue: false
    # severity=info falls through to the root receiver (null-receiver) by default
    # — silenced with no explicit route needed.

receivers:
  - name: null-receiver
    # Intentionally empty. Real receivers (oncall-pager, ops-tickets) land in 2C.4.
    # The routing tree is validated by `amtool check-config` even without them.

inhibit_rules:
  - source_matchers: [ 'severity="critical"' ]
    target_matchers: [ 'severity="warning"' ]
    equal: ['sloth_slo', 'capability']
```

### 9.2 Grouping

`group_by: ['alertname', 'adapter', 'capability']`.

Rationale: with per-capability SLOs, a single notification must not merge multiple capabilities of the same adapter. Each `(alertname, adapter, capability)` triple is its own incident.

### 9.3 Inhibition

Uses `sloth_slo` — the label Sloth v1 emits on every generated rule — plus `capability`. When a fast-burn (`critical`) alert fires for a given SLO, the corresponding slow-burn (`warning`) alert for the same SLO is suppressed. Avoids double-notification.

Infra alerts (hand-written) do not carry `sloth_slo`; cross-tier inhibition for them is **not configured**. Each infra alert is self-contained per severity.

### 9.4 Null-receiver strategy

The routing tree, grouping, and inhibition are **fully configured in 2C.1**. What is deferred is only the final delivery endpoint.

- `null-receiver` is the default (root + both severity routes).
- `amtool check-config ops/alertmanager/alertmanager.yaml` validates **Alertmanager configuration syntax only**: YAML shape, receiver references, route reachability, template syntax. It does NOT validate that `inhibit_rules.equal` labels actually exist on the upstream Prometheus rules — that coverage check is the responsibility of the **Go custom validation test** (`alertmanager_routing_test.go`, see §11.2), which parses both `alertmanager.yaml` and the Prometheus rule outputs and asserts that every label referenced in `equal`, `matchers`, or `inhibit_rules` has a corresponding declaration upstream.
- No webhook points to a non-existent URL. No broken endpoints. When 2C.4 lands real integrations, the edit swaps `receiver: null-receiver` for `receiver: oncall-pager` / `receiver: ops-tickets` and appends the new receivers to the `receivers:` list — tree shape unchanged.

## 10. Grafana dashboards

### 10.1 Structure

- **1 overview dashboard** (`runtime-overview`) — oncall entry point. Consolidated. No `$capability` dropdown.
- **4 per-adapter dashboards** (`runtime-shell`, `runtime-git`, `runtime-fs`, `runtime-http`) — drill layer. Each has a multi-value `$capability` variable scoped to that adapter's capabilities.
- **No per-capability dashboards** in Phase 1. Filtering within a per-adapter dashboard suffices.

### 10.2 UIDs and stability contract

Dashboard UIDs are fixed and versioned: `runtime-overview`, `runtime-shell`, `runtime-git`, `runtime-fs`, `runtime-http`.

- **UID rename** → breaking (breaks dashboard links + embed URLs). ADR required.
- **Panel removal when linked from overview** → breaking. ADR required.
- **Variable rename** (e.g. `$capability` → `$cap`) → breaking (breaks data links). ADR required.
- **Query change** preserving result shape → non-breaking.

### 10.3 Query binding — dashboards depend on generated output, not assumed names

> **Dashboard queries reference the real versioned output in `ops/prometheus/generated/slo_rules.yaml`** — never "assumed" Sloth-conventional names. Sloth's recording and metadata rule names depend on the pinned CLI version, service name, and SLO name template. When a dashboard JSON pins a query like `sum(slo:current_burn_rate:ratio_rate1h{sloth_slo="git-status-availability"})`, that string must literally exist as a recording rule in the committed `slo_rules.yaml`. The **rule-existence test** in `dashboards_coverage_test.go` (§11.2) parses every dashboard JSON, extracts every PromQL metric/recording-rule reference, and asserts each one is present in either `ops/prometheus/generated/*.yaml` or `ops/prometheus/rules/*.yaml`. A Sloth version bump that renames recordings will break the test — that's the intended contract: **no drift** between generator output and dashboard consumption.

### 10.4 `runtime-overview` layout

| Row | Panels | Source |
|---|---|---|
| Health strip | 8 stat panels (one per capability), "Error budget 30d remaining" — green >50%, amber 10–50%, red <10% | Sloth metadata metrics (exact recording name read from `slo_rules.yaml`) |
| Burn rate matrix | Table: rows = capabilities, columns = [1h burn, 6h burn]; color by severity state | Sloth recording rules (exact names read from `slo_rules.yaml`) |
| Traffic | `rate(runtime_adapters_execution_total)` per adapter, stacked area, 1h window | `execution_total` (instrument contract, §6.3) |
| Infra health | 4 stats: Pool p95 acquire latency, OTel queue %, Persistence fail rate, Panic rate | infra recording rules (`ops/prometheus/rules/`) |
| Firing alerts | Alert list panel filtered `severity=~"critical\|warning"` | Alertmanager datasource |
| Drill-down | Dashboard links header: → `runtime-shell`, `runtime-git`, `runtime-fs`, `runtime-http` | dashboard links |

### 10.5 `runtime-<adapter>` template

Shared layout; each dashboard hard-codes its `$adapter` value and derives `$capability` from label values.

**Variables:**

```
$adapter      = "<fixed per dashboard>"     # not editable
$capability   = query(
                  label_values(
                    runtime_adapters_execution_total{adapter="$adapter"},
                    capability
                  ))
                multi: true, includeAll: true, default: $__all
$__rate_interval (stdlib)
```

**Rows:**

| Row | Panels |
|---|---|
| Header | Stat: error budget remaining for selected `$capability`; Stat: req/s total; Stat: active executions |
| Traffic breakdown | Time series stacked by `status`, filtered by `$capability`; separated per capability when `$capability = All` |
| Latency | Heatmap + p50/p95/p99 lines; **exemplars enabled** (trace_id → Tempo click-through) |
| Error rate | Time series of `rate(failure+timeout)` / `rate(success+failure+timeout)` — matches availability SLI |
| Partial signal | **Only in `runtime-git.json`** — `rate(partial_signal_total)`. Other adapter dashboards omit this panel entirely, with a header annotation: "Phase 1: partial applicable only to `git.clone@v1` (D4.6)". |
| Saturation | Pool acquire p95, Concurrency rejects rate, OTel queue % |
| Burn rate detail | Table of fast/slow burn per capability filtered by `$capability`; rows link to corresponding Alertmanager alert |
| Logs drill | Placeholder text panel: "Loki integration deferred — scope TBD (2C.4 or later)" |

### 10.6 Deep-link rules

- **Overview → per-adapter**: dashboard links in the overview header transfer `$__time_range`; they do NOT transfer `$capability` (each adapter owns its own dropdown).
- **Overview panel → per-adapter with capability preselect**: panels in the overview that represent a concrete capability (error-budget stats, burn-rate matrix rows) carry a **data link** with `var-capability=${__data.fields.capability}`. Click on a specific row → lands in the correct per-adapter dashboard with that capability preselected. Panels that are consolidated (traffic rollup, infra strip) do not link — they carry no capability affordance to transfer.
- **Per-adapter latency panel → Tempo trace**: exemplar click-through on latency histogram opens the trace in the Tempo datasource.

### 10.7 Provisioning

```
ops/grafana/provisioning/
  ├── datasources/
  │   ├── prometheus.yaml
  │   ├── tempo.yaml
  │   └── alertmanager.yaml        # required by overview Alert list panel
  └── dashboards/
      └── provider.yaml
```

`provider.yaml`:

```yaml
apiVersion: 1
providers:
  - name: runtime-adapters
    folder: "Runtime Adapters"
    type: file
    disableDeletion: true
    allowUiUpdates: false
    options:
      path: /var/lib/grafana/dashboards     # mount point for versioned JSON
```

Two disjoint mount paths:

- `/etc/grafana/provisioning/` ← mounts `ops/grafana/provisioning/`.
- `/var/lib/grafana/dashboards/` ← mounts `ops/grafana/dashboards/`.

`allowUiUpdates: false` enforces git-as-source-of-truth: UI edits are warned against; real changes land via PR.

### 10.8 Partial signal placement

Only `runtime-git.json` carries the `partial.signal` panel. Including it in `runtime-shell`, `runtime-fs`, `runtime-http` would show a permanent zero — cognitive noise for oncall. The header annotation on those dashboards carries a one-liner: *"Phase 1: partial applicable only to `git.clone@v1` (D4.6)."*

### 10.9 Datasources

| Datasource | Purpose | Provider file |
|---|---|---|
| Prometheus | recording rules, alerts, metrics | `datasources/prometheus.yaml` |
| Tempo | exemplar click-through from latency histogram | `datasources/tempo.yaml` |
| Alertmanager | overview Alert list panel | `datasources/alertmanager.yaml` |

Loki datasource is **not** provisioned in 2C.1.

## 11. Testing strategy and CI gates

### 11.1 Principle

Every artifact in `ops/` is gated by a parser, validator, or coverage test. Drift between `ops/` and the metric/instrument contract breaks CI — same spirit as Phase 1's `VerifyCoversPhase1Catalog`.

### 11.2 Test matrix

| Artifact | Tool | Fail condition |
|---|---|---|
| Logger (`internal/.../obs/log/`) | `go test` + in-memory `slog.Handler` | Level mapping table incomplete; missing contractual field; `FromContext()` returned nil |
| Metric contract (`internal/.../obs/metrics.go`) | `go test` | Label in blacklist present on instrument; cardinality budget exceeded; unknown instrument name shape |
| Sloth specs (`ops/slo/*.yaml`) | `go test -tags sloth` + Sloth CLI | Capability in catalog without SLO (availability ∧ latency); spec ↔ generated drift |
| Sloth version pin | shell check | `sloth --version` ≠ `cat ops/slo/.sloth-version` |
| Prometheus rules (infra + generated) | `promtool check rules` + `promtool test rules` | Syntactic invalid; synthetic series fire/don't-fire mismatch |
| Alertmanager syntax | `amtool check-config` | Invalid YAML; unreachable route; undefined receiver reference; malformed matcher |
| Alertmanager label coverage | `alertmanager_routing_test.go` (Go custom) | `inhibit_rules.equal` label not emitted by any upstream rule; `matchers` reference label not in any Sloth or infra rule output |
| Grafana dashboards | `dashboard-linter` (Grafana Labs) + `go test` | Duplicate UID; query references nonexistent rule; per-adapter dashboard missing `$capability` variable; broken data link |

### 11.3 CI job structure

New job `observability` in `.github/workflows/ci.yaml`, parallel to `lint-unit-contract`. Runs on push and PR. Does not require Docker.

```yaml
observability:
  name: observability
  runs-on: ubuntu-latest
  steps:
    - uses: actions/checkout@v4
    - uses: actions/setup-go@v5
      with:
        go-version: "1.26"
        cache: true

    # Tool installation — all versions pinned
    - name: Install Sloth
      run: |
        VERSION="$(cat ops/slo/.sloth-version)"
        curl -sL "https://github.com/slok/sloth/releases/download/${VERSION}/sloth-linux-amd64" \
          -o /usr/local/bin/sloth && chmod +x /usr/local/bin/sloth
    - name: Install promtool + amtool
      run: |
        PROM_VERSION="$(cat ops/prometheus/.prometheus-version)"
        AM_VERSION="$(cat ops/alertmanager/.alertmanager-version)"
        # download and extract; place on PATH
        ...
    - name: Install dashboard-linter
      run: |
        VERSION="$(cat ops/grafana/.dashboard-linter-version)"
        go install github.com/grafana/dashboard-linter@${VERSION}

    # Version pin checks
    - name: Sloth version pin
      run: sloth --version | grep -qF "$(cat ops/slo/.sloth-version)"

    # Validation gates — order matters: cheap checks first
    - name: Sloth idempotency
      run: |
        sloth generate --input ops/slo/ --output /tmp/regen.yaml
        diff -u ops/prometheus/generated/slo_rules.yaml /tmp/regen.yaml
    - name: promtool check rules
      run: |
        promtool check rules ops/prometheus/rules/*.yaml ops/prometheus/generated/*.yaml
    - name: promtool test rules
      run: |
        promtool test rules ops/prometheus/rules/*.test.yaml ops/prometheus/generated/*.test.yaml
    - name: amtool config
      run: amtool check-config ops/alertmanager/alertmanager.yaml
    - name: Dashboard JSON validation
      run: dashboard-linter lint ops/grafana/dashboards/*.json

    # Go tests for obs packages + parsers under ops/
    - name: Go tests
      run: |
        go test -race -count=1 \
          -tags "sloth promtool" \
          ./internal/infrastructure/obs/... \
          ./ops/...
```

### 11.4 Coverage gate extension

Phase 1's per-package ≥85% gate on `internal/domain/` + `internal/application/` extends to include `internal/infrastructure/obs/log/`:

```yaml
- name: coverage gate — domain + application + obs/log (≥85% per package)
  run: |
    go test -race -count=1 -cover \
      ./internal/domain/... \
      ./internal/application/... \
      ./internal/infrastructure/obs/log/... 2>&1 | tee cov-pkg.txt
    awk '
      /^ok[ \t]/ && /coverage:/ {
        pkg = $2
        for (i = 1; i <= NF; i++) {
          if ($i == "coverage:") {
            pct = $(i+1); sub(/%/, "", pct); val = pct + 0
            if (val < 85.0) { print "gate failed:", pkg, val "%"; fail = 1 }
          }
        }
      }
      END { if (fail) exit 1; print "✅ coverage gate passed" }
    ' cov-pkg.txt
```

`obs/log` is contract-bearing (fixed fields, level mapping invariants, never-nil ctx). The rest of `internal/infrastructure/obs/` (OTel setup, metric registry) stays outside the strict gate — integration code, not contract.

### 11.5 Tool version pinning

All external binaries in the observability CI are version-pinned via checked-in files:

| Binary | Pin file | Verify step |
|---|---|---|
| `sloth` | `ops/slo/.sloth-version` | `sloth --version \| grep -qF "$(cat ops/slo/.sloth-version)"` |
| `promtool` | `ops/prometheus/.prometheus-version` | `promtool --version` shell-diff |
| `amtool` | `ops/alertmanager/.alertmanager-version` | `amtool --version` shell-diff |
| `dashboard-linter` | `ops/grafana/.dashboard-linter-version` | installed via `go install @${VERSION}` — version pinned at install time |

Bumping any of these is a deliberate commit that regenerates affected outputs.

### 11.6 Go tests under `ops/...` are parsers/validators only

Tests under `ops/` trees are explicitly limited to:

- YAML spec parsing (Sloth spec structure, Prometheus rule shape, Alertmanager config shape).
- Coverage / existence checks (every Phase 1 capability has an SLO; every dashboard has required variables; every referenced recording rule exists).
- Label/value validation (blacklist enforcement, severity taxonomy match).
- Cross-file reference validation (dashboard queries resolve to declared rules; Alertmanager `equal` labels exist in rule outputs).

**Not in 2C.1:**

- Integration tests that launch real Prometheus/Grafana/Alertmanager instances.
- End-to-end tests that emit metrics from the runtime and observe them through the full pipeline.
- Golden-image / snapshot tests of dashboards.

These belong to 2C.2 (load baseline validates real observability), 2C.3 (chaos validates alert fidelity), or 2C.4 (local stack enables dev-ergonomic real-pipeline testing).

### 11.7 Makefile helpers

```make
sloth-generate:
	sloth generate --input ops/slo/ --output ops/prometheus/generated/slo_rules.yaml

test-obs:
	go test -race -count=1 -tags "sloth promtool" ./internal/infrastructure/obs/... ./ops/...

test-rules:
	promtool check rules ops/prometheus/rules/*.yaml ops/prometheus/generated/*.yaml
	promtool test rules ops/prometheus/rules/*.test.yaml ops/prometheus/generated/*.test.yaml

test-alertmanager:
	amtool check-config ops/alertmanager/alertmanager.yaml

test-dashboards:
	dashboard-linter lint ops/grafana/dashboards/*.json

test-observability: sloth-generate test-obs test-rules test-alertmanager test-dashboards
```

Matches Phase 1's `make test-*` convention. `test-observability` is the composite target consumed by CI and by devs before push.

## 12. New invariants and rules

### 12.1 R16 — Metric cardinality discipline

> **R16 — Metric cardinality bounded.** Every declared instrument has an explicit label whitelist. Only bounded-value labels are permitted: `capability`, `adapter`, `status`, `signal`. Unbounded or high-cardinality identifiers (`error_class`, `receipt_id`, `handle_id`, `correlation_id`, `trace_id`, `retry_hint`) are **prohibited** as metric labels; they belong in logs or exemplars. CI enforces this via `metric_contract_test.go`.

Added to `docs/rules.md` as R16. Displaces the implicit assumption in Phase 1 that labels are free-form.

### 12.2 I23 — Metric contract owned by code, not ops

> **I23 — Metric contract is code-defined.** Instrument names, types, units, and label sets live in `internal/infrastructure/obs/metrics.go`. Operational components (collector, scraper, relabeling rules) MUST NOT rename, filter, or drop instruments declared there. Any rename or drop in the code contract requires ADR + `runtime_adapters_metrics_schema_version` bump.

Added to `docs/domain-invariants.md` as I23.

## 13. Out of scope (explicit recap)

To preserve focus on 2C.1 deliverables, the following items are **not** part of this design and will not be shipped under 2C.1's plan:

- `ops/local/docker-compose.yaml` (local stack spin-up) — **2C.4**.
- `deploy/k8s/*` (Kubernetes manifests) — **2C.4**.
- `docs/runbook/*.md` (per-alert runbooks) — **2C.4**.
- `annotations.runbook_url` on any alert — added alongside runbooks in **2C.4**.
- Real receiver integrations (PagerDuty, Opsgenie, Linear, Slack) — **2C.4**.
- Loki datasource and log drill-through in dashboards — scope TBD between **2C.4** and later.
- Dynamic log-level reload — **2C.4** if operational demand.
- Trace/log sampling strategy — **2C.2** if volume problem.
- Load-derived SLO target calibration — **2C.2** (that is literally its purpose).
- Chaos injection + degradation SLO validation — **2C.3**.
- Alert delivery timing validation — **2C.3**.
- End-to-end observability pipeline test — **2C.2** / **2C.3**.
- Dashboard snapshot / golden-image regression tests — **2C.4**.
- Helm chart / Kustomize overlays — **2C.4** if operational demand.
- OTel log signal export (we stay on stdout-JSON; collector picks up via container logs) — **2C.4** if demanded.

---

## Appendix A — Closed decisions

| ID | Decision | Source |
|---|---|---|
| **D2C1.1** | Hybrid approach — Sloth for SLOs, hand-written for everything else | User choice post-approach proposal |
| **D2C1.2** | Per-capability SLOs as primary contract; adapter rollups and overall health exist only as dashboard derivations | Q1 answer |
| **D2C1.3** | `failure` and `timeout` burn budget; `cancelled` and `partial` are neutral to the SLO budget | Q2 answer |
| **D2C1.4** | Multi-window multi-burn-rate with exactly **2 windows** per SLO (2%/1h fast → page, 5%/6h slow → ticket); 30d rolling budget | Q3 answer |
| **D2C1.5** | Three-tier severity taxonomy: `critical` / `warning` / `info`; `info` has **no active receiver** (silenced at Alertmanager root via `null-receiver`) | Q4 answer |
| **D2C1.6** | Balanced label strategy: counters `{capability, status}`; histograms `{capability}` only; `error_class` → logs, not labels | Q5 answer |
| **D2C1.7** | Contextual mandatory logger via `slog`; `RUNTIME_LOG_FORMAT=text` default (json forced in ops) | Q6 answer |
| **D2C1.8** | Hierarchical dashboards — 1 `runtime-overview` + 4 per-adapter; no per-capability dashboards in Phase 1 (variable `$capability` within per-adapter serves drill) | Q7 answer |
| **D2C1.9** | Logger never-nil: `FromContext` returns `NewNop()` when missing | Section 2 presentation |
| **D2C1.10** | `execution.duration` histogram is **success-only**; no `status` label; failure latency is not measured in 2C.1 | Section 3 Adjustment 1 |
| **D2C1.11** | `partial.signal` is a secondary simplified signal, not a patch for a data gap | Section 3 Adjustment 2 |
| **D2C1.12** | `trace_id` is mandatory exemplar when span active; `receipt_id` is best-effort | Section 3 Adjustment 3 |
| **D2C1.13** | Sloth latency form: `error_query = total_count − bucket_le_threshold`, `total_query = total_count` | Section 4 Adjustment 1 |
| **D2C1.14** | All external binaries (Sloth, promtool, amtool, dashboard-linter) are version-pinned via `.<tool>-version` files | Sections 4 + 7 |
| **D2C1.15** | SLO targets are provisional for 2C.1; quantitative calibration is 2C.2's job; spec files carry explicit PROVISIONAL header | Section 4 Adjustment 3 |
| **D2C1.16** | Alertmanager `group_by: ['alertname', 'adapter', 'capability']` | Section 5 Adjustment 1 |
| **D2C1.17** | Alertmanager inhibition uses `sloth_slo` + `capability`, not `alertname` | Section 5 Adjustment 2 |
| **D2C1.18** | No broken webhook placeholders in 2C.1; all unwired routes terminate at `null-receiver`; 2C.4 swaps receivers without touching routing tree | Section 5 Adjustment 3 |
| **D2C1.19** | `provider.yaml` mounts `/var/lib/grafana/dashboards` (dashboard JSON), not the provisioning directory | Section 6 Adjustment 2 |
| **D2C1.20** | Alertmanager datasource provisioned explicitly for the overview Alert list panel | Section 6 Adjustment 1 |
| **D2C1.21** | Deep-link data links from overview panels that represent a concrete capability carry `var-capability=${__data.fields.capability}`; consolidated panels do not link | Section 6 Adjustment 3 |
| **D2C1.22** | `partial.signal` dashboard panel lives only in `runtime-git.json`; other adapter dashboards omit it, replaced by header annotation | Section 6 Adjustment 4 |
| **D2C1.23** | Go tests under `ops/...` are limited to parsers, validators, coverage/existence checks, and cross-file reference validation; no integration tests within 2C.1 | Section 7 Adjustment 2 |

## Appendix B — Post-approval adjustments

Recorded in order during the brainstorming session:

| ID | Adjustment | Applied to section |
|---|---|---|
| **A2C1.1** | Scope `ops/local/docker-compose.yaml`, `deploy/k8s/*`, `docs/runbook/*` out of 2C.1; land in 2C.4 | §1, §4, §13 |
| **A2C1.2** | Path convention: `docs/runbook/` singular, `ops/` operational, `docs/` human docs | §4 |
| **A2C1.3** | Collector config is operational, not contract; contract lives in metric names, labels, rules | §4.2, §4.3, I23 |
| **A2C1.4** | `correlation_id` is core logger contract field | §5.3 |
| **A2C1.5** | `trace_id` mandatory-when-available in logger contract | §5.3 |
| **A2C1.6** | `error_class` emitted whenever `status != success`, independent of level | §5.3 |
| **A2C1.7** | `RUNTIME_LOG_FORMAT=text` default; json forced in ops manifests | §5.6 |
| **A2C1.8** | Latency histogram success-only; no `status` label on `execution.duration` | §6.3 |
| **A2C1.9** | `partial.signal` documented as simplified secondary signal | §6.3 |
| **A2C1.10** | Exemplar: `trace_id` mandatory, `receipt_id` best-effort | §6.5 |
| **A2C1.11** | Latency Sloth spec error_query inverted to canonical form | §7.2 |
| **A2C1.12** | Sloth version pin explicit via `ops/slo/.sloth-version` | §7.7 |
| **A2C1.13** | Provisional SLO targets documented in spec header comments and in provisional-targets table | §7.3 |
| **A2C1.14** | `group_by` includes `capability` | §9.2 |
| **A2C1.15** | Inhibition uses `sloth_slo` not `alertname` | §9.3 |
| **A2C1.16** | Null-receiver strategy replaces webhook placeholders | §9.4 |
| **A2C1.17** | Alertmanager datasource file added | §10.9 |
| **A2C1.18** | Provider path points to dashboard mount, disjoint from provisioning mount | §10.7 |
| **A2C1.19** | Deep-link capability-aware from concrete-capability panels only | §10.6 |
| **A2C1.20** | Partial panel only in `runtime-git.json`; annotation elsewhere | §10.8 |
| **A2C1.21** | `dashboard-linter` version pinned via `ops/grafana/.dashboard-linter-version` | §11.5 |
| **A2C1.22** | Go tests under `ops/...` explicitly scoped to parsers/validators/coverage; no integration | §11.6 |

---

## Appendix C — Open questions (for 2C.2 / 2C.3 / 2C.4)

Not blocking for 2C.1, but to surface when the downstream tracks open:

1. **2C.2** — Exact burn-rate thresholds (budget % / window) may need re-calibration once baseline load numbers land.
2. **2C.2** — Whether the success-only latency histogram is enough, or `execution.duration.failures` becomes needed for diagnostic dashboards.
3. **2C.3** — Alert-fidelity acceptance criteria: how much latency between metric emission and `critical` alert fire is tolerable end-to-end.
4. **2C.4** — Receiver integration choice (PagerDuty vs Opsgenie vs Slack-only vs Linear webhooks).
5. **2C.4** — Loki landing decision: bundle with 2C.4, push to 2D, or skip entirely in favor of direct-to-stdout flow + operator's log aggregator.
6. **2C.4** — Dynamic log-level reload: needed for operators or overkill?
