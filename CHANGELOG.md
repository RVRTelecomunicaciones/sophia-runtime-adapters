# Changelog

All notable changes to `runtime-adapters` will be documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.3.0] — 2026-04-25

Phase 2C.2 — load baseline + calibration. Replaces the PROVISIONAL SLO targets from 2C.1 with measured ones under a declared compose envelope (2 CPU / 2 GiB runtime). Adds k6 load scenarios, a pinned docker-compose measurement stack, a report generator that produces auditable Markdown + machine-readable evidence, and a GHA advisory smoke job that posts regression deltas as PR comments. Spec-complete against `docs/superpowers/specs/2026-04-23-phase-2c-load-baseline-design.md`.

### Added

#### Runtime image (Bundle 1)

- `Dockerfile` — multi-stage Go 1.26.2 → `gcr.io/distroless/static:nonroot`.
- `.dockerignore` excluding docs/, ops/, test/ + build detritus.

#### Measurement environment (Bundle 2)

- `ops/local/compose.yaml` — 7-service baseline stack with cgroup-pinned limits. Services: runtime-adapters, postgres, otel-collector, prometheus, grafana, http-upstream-mock, k6.
- `ops/local/compose.ci-smoke.yaml` — 4-service stack for GHA advisory smoke. Reduced limits (0.75 CPU / 768 MiB runtime; 0.4 CPU / 256 MiB k6).
- `ops/local/prometheus.yaml` — scrape config (scrapes collector Prom exporter on `:8889`, not runtime directly).
- `ops/local/mock/default.conf` — nginx fixed static ~1 KiB response with explicit determinism requirements (§6.8).
- `ops/otel-collector/config.yaml` — OTLP receiver → Prometheus exporter (pull-based, ADR 0007). Pinned at `0.106.1`.
- `ops/otel-collector/scripts/validate-config.sh` — validates config against the same pinned container tag as compose.

#### k6 scenarios (Bundles 3 + 4)

- `ops/load/lib/common.js` — shared helpers (Crockford base-32 ULID generation for correlation_id + idempotency_key, payload builders mirroring adapter `types.go`, `executeRequest` wrapper). Modern k6/execution API throughout (D2C2.19); `__ITER`/`__VU` globals never used.
- 6 scenarios under `ops/load/scenarios/`: `shell_exec.js`, `filesystem_read_file.js`, `filesystem_write_file.js`, `http_request.js`, `git_status.js`, `git_rough.js`.
- `suite.js` — sequential entrypoint for the full baseline (~42–45 min).
- `smoke.js` — CI advisory reduced scenarios (core 4, ~10% RPS, ~2m30s).
- `handleSummary(data)` is the SOLE source of machine-readable summary (D2C2.17); `--summary-export` explicitly NOT used.
- `executeRequest` injects `phase` (derived from scenario name suffix) + `scenario_name` (raw) + `capability` (from arg) as request-level k6 tags. The k6 system `scenario` tag is **never** overridden — earlier attempts to override it produced empty `count=0` filtered sub-metrics (commit `238ed38`).

#### Git fixtures (Bundle 4)

- `test/fixtures/git-bench/` — deterministic blueprint sources + Makefile generator. Constructed `.git/` dirs NOT committed (D2C2.11).
- `test/fixtures/git-bench/regen_test.go` — asserts byte-identical regeneration (`-tags fixture`).

#### Report infrastructure (Bundle 5)

- `ops/load/lib/verify-limits.sh` — pre-run cgroup assertion (D2C2.4).
- `ops/load/lib/report-template.md.tmpl` — Markdown skeleton with 6 mandatory sections (§8.2).
- `ops/load/lib/generate-report.sh` — consumes k6 summary + docker inspect + PromQL instant + query_range; emits Markdown report + evidence dir + `latest-baseline.json` + `manifest.json`.
- `ops/load/lib/ci-smoke-comment.sh` — posts PR comment with 3-branch handling (delta / no baseline / k6 failed); shellcheck clean.
- `ops/slo/calibration-reports/README.md` + `schema_test.go` (go tests with pre-first-calibration Skip branches per D2C2.14).

#### First calibration (Bundle 7)

- `ops/slo/calibration-reports/2026-04-25-baseline-v2.md` — first real calibration report. Core tier: 4 capabilities fully calibrated with `high` confidence (TIGHTEN). `git.status@v1`: SMOKE_CALIBRATED with `limited` confidence (90s targeted standalone run, no saturation evidence). `git.clone@v1` / `git.diff@v1` / `git.commit@v1`: ROUGH_NO_CHANGE (no YAML edits; targets remain PROVISIONAL pending real-load evidence in 2C.4 per D2C2.9).
- `ops/slo/calibration-reports/latest-baseline.json` — machine-readable per-capability p50/p95/p99 snapshot. Carries `comparison_context` + `runner_class_*` fields (A2C2.22). CI smoke consumes this for delta comparison.
- `ops/slo/calibration-reports/evidence/2026-04-25-baseline-v2/` — `manifest.json`, `summary.json`, per-capability PromQL files, git-status-smoke-split.json, git-rough-observations.json (latter two `null` this run — sub-metrics only emit when filtered by a threshold; intentional limitation, resolvable in 2C.4).

#### CI jobs (Bundle 6)

- `observability` job: new step `Validate 2C.2 ops configs` runs `compose config -q` + `otelcol validate` + `promtool check config` on the new ops files.
- `load-ops-lint` job: shellcheck on `ops/load/` + collector scripts; pinned k6 install via `grafana/setup-k6-action@v1` + `k6 inspect` across all scenarios.
- `load-smoke` job (PR-only, `continue-on-error: true`): advisory smoke on `ubuntu-latest`. Never fails CI (D2C2.6). Posts PR comment via `ci-smoke-comment.sh` with 3 branches.
- `load-report-schema` job: Go tests with pre-first-calibration Skip branches; TemplateRenderable always runs.

#### ADRs + docs

- ADR 0007 — k6 for load baseline + pull-based metrics pipeline.
- ADR 0008 — pinned compose envelope for calibration.
- `docs/load-baseline.md` — operator-facing workflow reference.
- `CLAUDE.md` pointer to ops/load + docs/load-baseline.md.

### Recalibrated SLO targets (Bundle 7 T44)

| Capability | Before | After | `le` | Decision | Confidence |
|---|--:|--:|---|---|---|
| `shell.exec@v1` | 5000 ms | **250 ms** | `0.25` | TIGHTEN | high |
| `filesystem.read_file@v1` | 500 ms | **100 ms** | `0.1` | TIGHTEN | high |
| `filesystem.write_file@v1` | 1000 ms | **500 ms** | `0.5` | TIGHTEN | high |
| `http.request@v1` | 10000 ms | **500 ms** | `0.5` | TIGHTEN | high |
| `git.status@v1` | 2000 ms | **250 ms** | `0.25` | SMOKE_CALIBRATED | limited |
| `git.clone@v1` | 30000 ms | 30000 ms | `30` | ROUGH_NO_CHANGE | n/a |
| `git.diff@v1` | 3000 ms | 3000 ms | `3` | ROUGH_NO_CHANGE | n/a |
| `git.commit@v1` | 2000 ms | 2000 ms | `2` | ROUGH_NO_CHANGE | n/a |

All new `le` values reuse existing buckets in `obs.DurationBuckets {0.1, 0.25, 0.5, 1, 2, 3, 5, 10, 30, 60}` — no histogram boundary changes; no re-baseline required.

### Changed

- `ops/slo/{shell,filesystem,http}.yaml` — core-tier latency targets recalibrated per the baseline v2 report. Headers updated from "PROVISIONAL — initial operational hypotheses" to "Calibrated — Phase 2C.2 baseline 2026-04-25 under envelope 2 CPU / 2 GiB (ADR 0008)".
- `ops/slo/git.yaml` — `git.status@v1` SMOKE_CALIBRATED to 250 ms; rough tier (clone/diff/commit) retained PROVISIONAL. Header reflects mixed calibration state.
- `ops/prometheus/generated/*.yaml` — regenerated from updated Sloth specs via `make sloth-generate`. Bucket `le=` values shifted in 35 burn-rate recording rules across the 7 SLO multi-window periods (5m / 30m / 1h / 2h / 6h / 1d / 3d).
- `Makefile` — filled the Bundle 1 stubs with real bodies (`load-up`, `load-down`, `load-baseline`, `load-smoke-local`, `fixture-git-bench`).

### Tooling

- k6: `0.58.0` (pinned via `ops/load/.k6-version`).
- otel-collector-contrib: `0.106.1` (pinned via `ops/otel-collector/.collector-version`). Used both in compose and by `validate-config.sh` — no drift (D2C2.15).
- Go, sloth, promtool, amtool, dashboard-linter: unchanged from 2C.1.

### Out of scope for 2C.2 (deferred)

Per spec §2.2 + §13:

- **2C.3** — chaos + minimal hardening: programmatic E2E pipeline tests, Alertmanager + Tempo in compose, soak tests (if leak surfaces), alert fidelity validation.
- **2C.4** — operational readiness: real pager/ticket receivers, runbooks, pgx pool Prometheus collector (unblocks `PoolIdleZero`), Loki integration, full operational local stack. Also picks up: per-tree git.status sub-metric thresholds, sustained git scenarios for full calibration, rough-tier observability thresholds.

### Metrics gate

- All packages green on `go test -race -count=1 ./...`.
- 4 CI jobs running on every PR: `lint-unit-contract`, `observability` (extended), `load-ops-lint`, `load-smoke` (advisory), `load-report-schema`.
- `shellcheck ops/load/lib/*.sh ops/otel-collector/scripts/*.sh` clean.
- `k6 inspect` clean on all scenarios.
- `docker compose config -q` clean on both compose files.
- `TestDurationBuckets_CoverSlothThresholds` (from 2C.1) passes with recalibrated thresholds.

---

## [0.2.0] — 2026-04-22

Phase 2C.1 — observability depth + SLOs. Adds contract-bound `slog` logger, revised metric Registry with bounded cardinality (R16), code-defined metric contract (I23), Sloth-generated per-capability SLOs with multi-window burn-rate alerts, hand-written infra alerts, three-tier Alertmanager routing with `null-receiver` (real integrations in 2C.4), and 5 Grafana dashboards (1 overview + 4 per-adapter). New `observability` CI job enforces tool pins, Sloth idempotency, promtool/amtool/linter validation, and Go coverage tests. Spec-complete against `docs/superpowers/specs/2026-04-21-phase-2c-observability-slos-design.md`.

### Added

#### Contextual logger (Bundle 1)

- **`internal/infrastructure/obs/log`** package — contract-bound `slog` wrapper with fixed field set (§5.3).
- `New(Config)` + `NewNop()` + `NewWithHandler(slog.Handler)` constructors; `Debug/Info/Warn/Error(ctx, msg, attrs...)` emission API.
- `ContextWith(ctx, *Logger)` + `FromContext(ctx) *Logger` — **never-nil invariant** (returns Nop when missing, for nil ctx, or on type-assertion miss).
- `LevelFor(status, errClass) slog.Level` per spec §5.4 with defensive fallthrough → ERROR for unspecced combinations.
- Env-driven config: `RUNTIME_LOG_FORMAT` (text|json, default `text`), `RUNTIME_LOG_LEVEL` (debug|info|warn|error, default `info`), `RUNTIME_LOG_ADD_SOURCE` (bool, default `false`). R10 strict validation at startup.

#### Metric contract refactor (Bundle 2)

- **`InstrumentCatalog()`** declarative catalog — 11 instruments per §6.3.
- Consolidated `execute_attempted/timeout/cancelled_total` → `runtime_adapters.execution.total{capability, status}`.
- `execution.duration` histogram is **success-only**, with explicit bucket boundaries `[0.1, 0.25, 0.5, 1, 2, 3, 5, 10, 30, 60]` seconds. No `status` label.
- New: `partial.signal` (secondary degradation signal), `execution.active` (saturation), `receipt.persist.failures` (A4.3 signal), `otel.exporter.queue.size`, `migrate.failures`.
- Removed: `bytes_read/written_total`, `idempotency_hit/miss_total`, `persist_duration_ms`, `adapter_execute_duration_ms`.
- `NewRegistry(meter metric.Meter)` — cleaner signature decouples from `otel` global.
- `(*Registry).RecordExecution(ctx, capability, status, receiptID, durationSec)` emission helper.
- **Exemplar emission on `execution.duration`** (§6.5): `trace_id` captured automatically via `sdkmetric.WithExemplarFilter(exemplar.AlwaysOnFilter)` when a span is active in ctx; `receipt_id` attached best-effort as an observation attribute, dropped from aggregation by a `WithView` filter so cardinality stays bounded (R16) while the attribute survives as an exemplar tag. Links Grafana latency-panel clicks → Tempo trace.

#### Logger + metric integration in ExecuteService (Bundle 3)

- **HTTP `LoggerMiddleware`** binds request-scoped logger + `correlation_id` (from `X-Correlation-Id` header, or generated ULID).
- Middleware chain: `chimw.RequestID → LoggerMiddleware → requestIDHeader → panicRecoverer`.
- **Bootstrap constructs root logger BEFORE OTel** (step 1 of `BuildRuntime`); fail-fast on bad config.
- **ExecuteService enrichment** at spec §5.3 touchpoints: correlation_id → capability+adapter (post-Step 3) → handle_id (post-Step 5) → status+error_class+receipt_id+duration_ms (final emit at Step 11). Level chosen by `LevelFor`.
- **A4.3 ERROR log** on receipt persistence failure at both happy-path and `persistStructural` sites.
- Metric emission via Registry: `ConcurrencyRejects` on limiter reject, `ExecutionActive ±1` around adapter dispatch, `RecordExecution` on every terminal path.
- `ExecuteServiceConfig.Metrics *obs.Registry` (nil-safe for tests; bootstrap always passes non-nil).

#### SLOs via Sloth (Bundle 4)

- **4 Sloth v1 specs** under `ops/slo/` — shell, git, filesystem, http — covering 8 Phase 1 capabilities × 2 SLO types = 16 SLOs.
- Availability SLI excludes `cancelled`+`partial` (Q2 neutrality). Latency SLI uses inverted-bucket form (`count − bucket_le`) for clean semantics.
- Each spec carries a **PROVISIONAL** header; 2C.2 recalibrates targets under load.
- **Sloth-generated rules** at `ops/prometheus/generated/<adapter>.yaml` (checked in): 272 recording + metadata rules, **exactly 32 alerts** (16 SLOs × 2 burn windows: critical-page + warning-ticket).
- `sloth_slo` label on every rule — upstream contract for Bundle 6 inhibition.
- **Go coverage test** (`ops/slo/slo_coverage_test.go`, `-tags sloth`) asserts every Phase 1 capability has both availability + latency SLO entries.
- **Bucket ↔ Sloth threshold cross-check** (`TestDurationBuckets_CoverSlothThresholds`) — every `le="..."` in SLO specs must map to a bucket in `DurationBuckets`.

#### Infra alerts hand-written (Bundle 5)

- **5 rule families** under `ops/prometheus/rules/`: `infra_pool` (3 alerts), `infra_otel` (3), `infra_migrate` (1), `infra_panics` (1), `infra_persistence` (3) = **11 alerts total**.
- Severity taxonomy: `critical` / `warning` / `info`. Three info-tier alerts silenced at Alertmanager root.
- Each family has paired `.test.yaml` promtool fixtures — 11/11 alerts have fires-scenario coverage.
- `PoolIdleZero` marked **dormant** (metric `pgx_pool_idle_conns` requires pgx collector wiring in 2C.4).

#### Alertmanager routing (Bundle 6)

- **Three-tier routing** at `ops/alertmanager/alertmanager.yaml`: critical → null-receiver (2C.4 swaps to oncall-pager), warning → null-receiver (2C.4 swaps to ops-tickets), info → silenced.
- **Grouping** `[alertname, adapter, capability]` — D2C1.16 (prevents per-capability SLO notification merges).
- **Inhibition** via `sloth_slo` + `capability` (D2C1.17) — fast-burn suppresses slow-burn for the same SLO.
- **Go routing test** (`ops/alertmanager/alertmanager_routing_test.go`, `-tags alertmanager`) with 6 subtests enforcing null-receiver declared, route receivers declared, group_by contains capability, sub-routes have `continue: false`, inhibit uses `sloth_slo` not `alertname`, every matcher label exists upstream.

#### Grafana dashboards (Bundle 7)

- **5 dashboards** at `ops/grafana/dashboards/`: `runtime-overview` (oncall entry, consolidated) + `runtime-shell`, `runtime-git`, `runtime-fs`, `runtime-http` (per-adapter drill with `$capability` variable).
- UIDs pinned as stability contract. All `editable: false`, schemaVersion 39, tagged `["runtime-adapters", "2c.1"]`.
- **Partial signal panel only in `runtime-git`** (D4.6 — only `git.clone@v1` is partial-capable in Phase 1).
- `runtime-overview` layout: 8-stat health strip (error budget remaining), burn-rate matrix (1h + 6h columns), traffic by adapter, infra health, Alertmanager alert list, drill-down links with capability-aware data links (D2C1.21).
- **Provisioning**: 3 datasources (Prometheus with exemplar link → Tempo, Tempo, Alertmanager) + file provider mounted at `/var/lib/grafana/dashboards` (D2C1.19).
- **`.lint` config** disables 4 dashboard-linter rules inapplicable to single-service runtime (job / instance matcher rules).
- **Go coverage test** (`ops/grafana/dashboards_coverage_test.go`, `-tags dashboards`) — 3 subtests enforce UIDs canonical+unique, per-adapter dashboards have `$capability` variable, every PromQL reference resolves against `ops/prometheus/` or the runtime instrument catalog.

#### CI + docs (Bundle 8)

- **New `observability` CI job** — installs + version-pins sloth/promtool/amtool/dashboard-linter; gates on sloth idempotency, promtool check+test, amtool check-config, dashboard-linter, and Go tests with `sloth`/`alertmanager`/`dashboards` build tags.
- **Coverage gate extended** to `internal/infrastructure/obs/log/...` (join `internal/domain` + `internal/application`).
- **Makefile targets**: `sloth-generate`, `test-obs`, `test-rules`, `test-alertmanager`, `test-dashboards`, `test-observability` (composite).
- **Reference docs**: `docs/metrics.md`, `docs/logging.md`, `docs/slo.md`.
- **Rules + invariants**: R16 in `docs/rules.md`, I23 in `docs/domain-invariants.md`, R16 also in `CLAUDE.md` never-do list.
- **ADRs**: 0005 Sloth for SLO generation (hybrid B+A rationale), 0006 metric contract invariants (R16 + I23).
- **Architecture doc**: new observability layer section in `docs/architecture.md`.

### Changed

- `internal/infrastructure/obs/metrics.go` — full replacement; no backward-compat shims. Old field names (`ExecuteAttempted`, `IdempotencyHit`, `BytesRead`, etc.) removed cleanly.
- `NewRouter(svc, query)` → `NewRouter(svc, query, logger *log.Logger)` — signature change propagated to 6 call sites.
- `ExecuteServiceConfig` adds `Metrics *obs.Registry` field (nil-safe; production always non-nil).
- `BuildRuntime` constructs root logger at step 1 (before OTel at step 2); metric registry constructed after pool+migrations.

### Removed

- Phase 1 Registry fields: `ExecuteAttempted`, `ExecuteTimeout`, `ExecuteCancelled`, `ExecutePanicsRecovered`, `IdempotencyHit`, `IdempotencyMiss`, `ExecuteDurationMs`, `PersistDurationMs`, `AdapterExecuteDurationMs`, `BytesRead`, `BytesWritten`. No callers existed outside `obs/` (verified) — clean break.

### Tooling

- Sloth CLI: `0.16.0` (pinned, brew-current stable; bumped from plan-assumed 0.12.0).
- promtool: `3.11.2` (pinned).
- amtool: `0.32.0` (pinned).
- dashboard-linter: `v0.0.0-20250428211052-5e22d6dc65a1` (pinned via `go install`).
- Go toolchain: unchanged (`1.26`, pinned to `go1.26.2`).

### Out of scope for 2C.1 (deferred)

Per spec §2.2 + §13, deferred to later tracks:

- **2C.2** (load baseline): quantitative SLO target calibration; stronger promtool fixtures with real load patterns; scraping validation.
- **2C.3** (chaos + minimal hardening): alert delivery timing validation; chaos-derived degradation scenarios; SLO coverage under fault injection.
- **2C.4** (operational readiness): `ops/local/docker-compose.yaml`, `deploy/k8s/*` manifests, runbook authoring (`docs/runbook/*.md`), real pager/ticket receiver integrations (PagerDuty, Opsgenie, Linear webhooks), pgx pool collector wiring (unblocks `PoolIdleZero`), Loki integration for logs drill-through panel.

### Metrics gate

- All 22 packages green on `go test -race -count=1 ./...`.
- Coverage: `internal/domain/...` 97-100%, `internal/application/services` 92%, `internal/infrastructure/obs/log` 91.9% (gate ≥85%), adapters + obs + bootstrap all above their per-package thresholds.
- `observability` CI job green: sloth idempotency, promtool check+test (272+ rules + 6 fixtures), amtool check-config, dashboard-linter on 5 dashboards, Go tests across all build tags.
- `golangci-lint run` clean.
- Integration tests green with Docker (`make test-integration`).
- E2E smoke green with Docker (`make test-e2e`).

---

## [0.1.0] — 2026-04-21

Phase 1 MVP — governable execution layer of the Sophia ecosystem. Closes the 68-task implementation plan across 8 bundles. Spec-complete against `docs/superpowers/specs/2026-04-19-runtime-adapters-phase1-design.md`.

### Added

#### Core domain (Bundle 2)

- **8 capabilities** across 4 adapters (§5.4): `shell.exec@v1`, `git.status@v1`, `git.clone@v1` (only partial-capable), `git.diff@v1`, `git.commit@v1`, `filesystem.read_file@v1`, `filesystem.write_file@v1`, `http.request@v1`.
- **ExecutionReceipt aggregate root** (§3.2, D3.2, I20) — `schema_version: "v1"`, append-only (R2/I2), persisted before return (R7/A4.3).
- **5-value `ExecutionStatus` closed enum** (D4.3/R15): success, failure, timeout, cancelled, partial.
- **10-value `ErrorClass` closed enum** (§4.6/D4.5) with deterministic `DefaultRetryHint` mapping per §5.9.
- **`ResultNormalizer` dispatcher** (D3.12/D3.5) — adapters register per-capability closures; raw outcomes never cross the domain boundary (R5).
- **D4.6 partial classification** (4-AND rule) applied to `git.clone@v1`.

#### Ports & application (Bundle 3)

- **`Adapter` / `ReceiptRepository` / `IdempotencyStore` outbound ports** (§7.2, D7.4-D7.6) with sentinel errors + in-memory test doubles.
- **`RuntimeService` / `QueryService` inbound ports** (§7.1, A7.1).
- **`ExecuteService` UC1 11-step flow** (§6.2) — envelope validation, idempotency lookup, capability resolution, timeout resolution, handle generation, adapter execution with panic recovery, normalization, receipt assembly, persistence, idempotency record, return.
- **`QueryService` UC2/UC3** (§6.3/§6.4) — ListCapabilities + GetReceipt with `include_streams` flag (default `true` per §6.4).
- **`ConcurrencyLimiter`** (A9.1/R14/I22) — buffered-channel semaphore, fast-reject, no queue.

#### Adapters (Bundle 4)

- **`shell.exec@v1`** — allowlist-resolved commands, minimal env (PATH/HOME/LANG/LC_ALL/TZ + additive overrides), no shell interpolation, no artifacts (D4.8/I8).
- **git adapter (4 capabilities, go-git only per D8.7/ADR-0002)** — SSH-agent auth only, no embedded credentials (D8.3), no client-side hooks (A8.1/ADR-0002), D4.6 partial verified end-to-end.
- **`filesystem.{read_file,write_file}@v1`** — atomic writes via tmp+fsync+rename (D8.4), symlink-resolved allowlist, sha256 checksum on write artifacts.
- **`http.request@v1`** — strict SSRF default blocking RFC 1918/6598/loopback/link-local/multicast (IPv4+IPv6), TLS verify mandatory, redirect cap 5, status mapping per §8.5.
- **`AdapterContractTestSuite`** — reusable across all 4 adapters.
- **`RegisterAllPhase1` + `VerifyCoversPhase1Catalog`** — single composition helper with startup catalog-drift guard.

#### Persistence (Bundle 5)

- **PostgreSQL schema** — `execution_receipts` (insert-only, full receipt as JSONB + denormalized indexable columns) + `idempotency_keys` (first-writer-wins, window-based expiry).
- **Embedded migrations** via `go:embed` + `golang-migrate/migrate/v4` with pgx/v5 driver.
- **`ReceiptRepositoryPG`** — insert-only Save (unique_violation 23505 → `ErrReceiptAlreadyExists`), FindByID (no-rows → `ErrReceiptNotFound`).
- **`IdempotencyStorePG`** — Lookup with `expires_at > NOW()`, Record with ON CONFLICT DO NOTHING + conflict detection.
- **testcontainers-go harness** — postgres:15-alpine integration tests.

#### Inbound HTTP + SDK (Bundle 6)

- **HTTP router (chi/v5 per plan P2/ADR-0003)** — `POST /api/v1/execute`, `GET /api/v1/capabilities`, `GET /api/v1/receipts/{id}`, `GET /healthz`. Middleware: RequestID → requestIDHeader → panicRecoverer.
- **`HTTPError` envelope** — stable snake_case JSON for structural failures; status mapping: `ErrTooManyExecutions` → 503 retryable, ctx cancel → 499, generic → 500 unknown retry.
- **In-process Go SDK** — thin wrapper around `RuntimeService` + `QueryService` with plain-struct inputs; domain constructors handle validation.
- **HTTP ≡ SDK contract test** — byte-identical JSON after normalizing variable fields.
- **`DisallowUnknownFields` + `MaxBytesReader`** on inbound decode (documented stdlib limitation when types implement custom `UnmarshalJSON`).

#### Bootstrap + observability (Bundle 7)

- **Env-driven `Config`** — §9.8 defaults with validation (`RUNTIME_*`, `OTEL_*`).
- **OpenTelemetry (opt-in via `OTEL_ENABLED`)** — OTLP gRPC exporters, 6 samplers supported, 11-instrument Registry per §9.7. No-op providers when disabled — zero side effects locally.
- **`bootstrap.BuildRuntime`** — sole composition point (D7.9); wires OTel → pool → migrate → repos → registry → normalizer → adapters → services → router.
- **`cmd/runtime-adapters/main.go`** — signal-driven entrypoint (SIGTERM/SIGINT), graceful drain via `ShutdownGracePeriod` (default 30s), exit 0 clean / 1 on error.
- **Shutdown order documented**: HTTP → OTel → pool.
- **Panic recovery correctness test** — 4 KiB stack truncation, goroutine-stack shape.
- **Graceful shutdown integration test** via testcontainers.

#### E2E (Bundle 8)

- **3 smoke scenarios under `//go:build e2e`**: happy path (`shell.exec`), `git.clone` partial (D4.6 verified through the API), idempotency replay (D6.4 replay-everything confirmed via same `receipt_id` + same `started_at` across two POSTs).

#### Documentation

- **Canonical specs**: `docs/superpowers/specs/2026-04-19-runtime-adapters-phase1-design.md` (13 sections, Dn.m/An.m log), `docs/superpowers/plans/2026-04-19-runtime-adapters-phase1.md` (68-task plan).
- **ADRs**: 0001 Phase 1 spec adoption, 0002 go-git only no hooks (A8.1), 0003 chi router for HTTP (plan P2), 0004 pgx/v5 for persistence (plan P3).
- **Operational docs**: `CLAUDE.md`, `AGENTS.md`, `docs/rules.md` (R1..R15), `docs/domain-invariants.md` (I1..I22), `docs/architecture.md`.
- **11 `.claude/skills/*/SKILL.md` files populated** with compact rules + triggers (A12.1 — `mailbox-locking-model` deliberately absent).
- **README quickstart** — Docker + `make run` + curl examples + env-var reference.

### Toolchain

- Go 1.26 (toolchain pinned to `go1.26.2`).
- External deps: `go-chi/chi/v5`, `jackc/pgx/v5`, `golang-migrate/migrate/v4`, `go-git/go-git/v5`, `oklog/ulid/v2`, `testcontainers/testcontainers-go` + `modules/postgres`, `go.opentelemetry.io/otel` + SDK + OTLP gRPC exporters.

### Key decisions (full list in Appendix A of the spec)

- **External wire contract frozen** — `AdapterID = "http"` (not `"httpreq"`) despite internal Go package name; no alias machinery (Gap-1 resolved pre-Bundle 2).
- **Go version bumped** from original plan's 1.22 to 1.26.2 mid-implementation (build: 0c10252).
- **Per-package coverage gate ≥ 85%** on `internal/domain/` + `internal/application/` (switched from per-function in T31 to handle Go's 0.0%-on-empty-marker-method artifact honestly).
- **No internal retries (R6/I21)** — `RetryHint` is a signal; caller decides.
- **Persistence-before-return (R7/A4.3)** — receipt audit completes even when caller ctx is cancelled; wire.go uses `context.Background()` for the final persist.

### Out of scope for Phase 1 (deferred)

Per §11.2 trigger-driven Phase 2 tracks (A..F), NOT implemented:

- **2A** Async execution + `EventPublisher` (trigger: long-running capability degrades sync UX)
- **2B** `LockManager` (trigger: real concurrent collision on shared resource)
- **2C** Hardening — load baseline, SLOs, chaos
- **2D** Fuzz + central JSON Schema (trigger: incident from malformed payload — partially mitigated by `DisallowUnknownFields` limitation documented in `execute_handler.go`)
- **2E** Auth + multi-tenant (trigger: shared/multi-tenant deployment)
- **2F** Git extended (`push`, `fetch`, etc.) — trigger: CI/CD pipeline requirement

### Metrics

- 21 packages, all green on `go test -race -count=1 ./...`.
- Coverage: `internal/domain/...` 97-100%, `internal/application/services` 92.2%, adapters 85-95%, inbound 92-100%, obs 76% (rest requires a real OTLP collector), config 98.7%.
- `golangci-lint run` clean.
- Integration tests green with Docker running (`make test-integration`).
- E2E smoke green with Docker running (`make test-e2e`).

---

[0.3.0]: https://github.com/RVRTelecomunicaciones/sophia-runtime-adapters/releases/tag/v0.3.0
[0.2.0]: https://github.com/RVRTelecomunicaciones/sophia-runtime-adapters/releases/tag/v0.2.0
[0.1.0]: https://github.com/RVRTelecomunicaciones/sophia-runtime-adapters/releases/tag/v0.1.0
