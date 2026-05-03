# Changelog

All notable changes to `runtime-adapters` will be documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.8.0] — 2026-05-02

Phase 2C.4 sub-project A+B — real alert receivers (PagerDuty + Slack + Linear). Fourth sub-project of the operational-readiness track. Replaces the `null-receiver` placeholders added in Phase 2C.1 with real receivers — critical alerts page on-call via PagerDuty Events API v2 + post to Slack `#incidents` (parallel fire), warnings post to Slack `#ops` + open/update/close Linear issues via a dedicated webhook adapter. `severity=info` remains silenced at the routing root (I-AB.1). Ships `make smoke-receivers` end-to-end smoke target + 4-layer CI guardrail (gitleaks + env-var-guard + bootstrap mode lock + tenant fingerprinting). Code is test-tenant-ready (D2C4AB.18) — production adoption is a purely operational milestone (which env vars are loaded in prod deploy), no further code changes needed. Spec-complete against `docs/superpowers/specs/2026-05-02-phase-2c.4-a-b-design.md`.

### Added

- `cmd/linear-webhook-adapter/` — new Go binary in the same module as runtime-adapters; separate process, separate container (port 9095), independent lifecycle (D2C4AB.4 + I-AB.6). Stateless, no local persistence (I-AB.4 / I-AB.7). Decoupled from runtime-adapters by process boundary so a Linear API retry storm cannot kill the runtime (I-AB.9).
- `internal/integrations/linear/` — hexagonal layout (D2C4AB.5):
  - `domain/severity.go` — `Severity` type with `LinearPriority()` mapping (critical→P1, warning→P3 per D2C4AB.10) + `ParseSeverity` rejecting info / unknown values (I-AB.1 enforcement at the type boundary).
  - `domain/issue.go` — `Issue` projection + `IssueState` enum (`Open` vs `Cancelled` per D2C4AB.8).
  - `domain/dedup.go` — `DedupLabel(groupKey)` returns `alert:<first-12-hex-chars-of-sha256>` per D2C4AB.7. Collision probability ~3.5e-15 per pair, negligible at our cardinality bound.
  - `ports/linear_client.go` — `LinearAPIClient` interface (FindIssuesByLabel, CreateIssue, UpdateIssue, AddComment, ArchiveIssue) + `CreateIssueInput` POJO.
  - `application/renderer.go` — `BuildTitle`, `BuildBody`, `BuildLabels` per spec §7.4. Title `[CRIT|WARN] <alertname> [— <capability>]`. Body Markdown with debug HTML-comment metadata block (debug only — matching uses the LABEL not the comment per A2C4AB.3.3).
  - `application/lifecycle.go` — firing/resolved branching engine + anti-spam (D2C4AB.9) per spec §7.5. Firing-no-existing → CreateIssue; firing-existing → UpdateIssue body + maybe AddComment if RecommentMinInterval elapsed; resolved-existing → AddComment + ArchiveIssue (Cancelled state); resolved-no-existing → no-op + 200 OK (A2C4AB.3.5 race-condition safe).
  - `application/webhook_handler.go` — HTTP handler with §7.6 status mapping: 200 normal, 400 bad input, 500 Linear 4xx or generic, 502 Linear 5xx, 405 non-POST.
  - `infrastructure/linear_graphql_client.go` — concrete `LinearAPIClient` against `https://api.linear.app/graphql` via net/http. Status mapping: HTTP 5xx/transport → `ErrLinearClient5xx`; HTTP 4xx or non-empty `errors[]` or `success=false` → `ErrLinearClient4xx`.
- `ops/alertmanager/alertmanager.yaml` — new `ops-critical` receiver (`pagerduty_configs` + `slack_configs` for `#incidents`) and `ops-warnings` receiver (`slack_configs` for `#ops` + `webhook_configs` for `http://linear-webhook:9095/webhook`). Both fire backends in parallel — Alertmanager dispatcher independence keeps a Slack outage from blocking PagerDuty (and vice versa). The 2 sub-route receiver references switch from `null-receiver` to the new names; `null-receiver` definition stays as the silenced sink for `severity=info`. Routing tree (group_by, timing, inhibit_rules) UNCHANGED from 2C.1 per D2C4AB.1.
- `ops/alertmanager/alertmanager_routing_test.go` — 2 new tests (`TestAlertmanager_OpsCriticalHasPagerDutyAndSlack`, `TestAlertmanager_OpsWarningsHasSlackAndWebhook`). `TestAlertmanager_AllRouteReceiversDeclared` tightened to require explicit ops-critical / ops-warnings mapping. `amConfig` schema extended with `pagerduty_configs` / `slack_configs` / `webhook_configs` for receiver-shape assertions.
- `Dockerfile` — multi-stage build with `runtime-adapters` and `linear-webhook-adapter` targets (D2C4AB.6). One shared `build` stage compiles both binaries; one runtime stage per binary selects via `--target`.
- `ops/local/compose.yaml` — new `alertmanager` and `linear-webhook` services under the `receivers` profile (NOT in default `up` — preserves the load-baseline measurement envelope unchanged per ADR 0008). Operators bring them up explicitly via `docker compose --profile receivers up`.
- `ops/smoke/smoke-receivers.sh` (~250 LOC) + `make smoke-receivers` — end-to-end smoke target per spec §8. Injects 3 alerts (critical, warning, info) via `amtool alert add` (D2C4AB.11), waits 90s for grouping windows, verifies positive routing per receiver via PD/Slack/Linear API queries (D2C4AB.12), verifies negative for info (D2C4AB.13 + I-AB.10), runs fail-soft cleanup (D2C4AB.14). Operator-driven, NOT a CI gate (D2C4AB.15).
- `.github/workflows/ci.yaml` — 2 new jobs: `secret-scan` using `gitleaks/gitleaks-action@v2` (Layer 1) + `env-var-guard` rejecting `^(PROD_|.*_PROD_|.*_PRODUCTION_)` env var names (Layer 2). Both run on every PR + push to main (D2C4AB.16 + I-AB.2).
- `internal/bootstrap/wire.go` step 0 — Layer 3 runtime-side bootstrap mode lock. Aborts `BuildRuntime` if `CI=true && RUNTIME_TENANT != test`; pre-empts logger / OTel / pool / adapter construction so a misconfigured CI run never opens prod resources (D2C4AB.16 + spec §9.3).
- `cmd/linear-webhook-adapter/main.go` — Layer 3 (adapter side) `enforceModeLock` + Layer 4 (adapter side) `enforceTenantFingerprint` (logs `team_id`, aborts if `LINEAR_TENANT_TYPE != test` under CI). Spec §7.8 + §9.3-9.4.
- `docs/test-tenant.md` — reproducible setup guide for PagerDuty test service, Slack bot user + channels, Linear test workspace + labels. Includes env summary template, run instructions, manual cleanup procedure, rotation policy. Spec §11 + D2C4AB.18.
- `.env.example` — new file documenting all 12 test-tenant env vars across the 3 bundles. Convention: test-tenant vars omit `PROD_` prefix/infix; production vars use `PROD_` (rejected by Layer 2). I-AB.2 + I-AB.5.

### Changed

- `ops/alertmanager/alertmanager.yaml` — `severity=critical` sub-route receiver `null-receiver` → `ops-critical`; `severity=warning` sub-route receiver `null-receiver` → `ops-warnings`. Root receiver remains `null-receiver` (info silencing — I-AB.1). Routing tree shape, group_by, inhibit_rules, timing UNCHANGED.

### Notes

- **No runtime adapter contract change** — runtime-adapters does NOT import the Linear adapter; the boundary between decision (governance) and execution (runtime) per CLAUDE.md D1.1/D1.2 is preserved. Linear adapter is **ops infrastructure**, not adapter execution. R1 "runtime does not decide" unchanged.
- **No metric-contract change** — runtime metrics namespace `runtime_adapters_*` is unchanged. R16 cardinality whitelist unchanged. Webhook adapter metrics (if added later) live under a separate `linear_webhook_adapter_*` namespace; deferred — not in v0.8.0 closure criteria.
- **No new dependencies in runtime-adapters** — the Linear adapter uses only stdlib (`net/http`, `encoding/json`, `crypto/sha256`, `log/slog`). No GraphQL client library, no Linear SDK.
- **Production adoption is operational** — v0.8.0 ships test-tenant-ready (D2C4AB.18). Rolling out to production = setting prod env vars in the prod deploy environment (vault / AWS Secrets Manager / 1Password Connect per the operator's choice). NO code changes needed for prod adoption.
- **Smoke target is operator-driven** (D2C4AB.15) — not gated in CI for v0.8.0. Matches the operator-driven pattern of `make load-baseline` from 2C.2. CI integration of smoke is deferred follow-up if it becomes valuable.
- **Anti-spam ships time-elapsed branch only** — the alert-set-change and severity-flip branches of D2C4AB.9 require adapter-side state which I-AB.7 forbids. Time-elapsed alone caps re-comment frequency to 1 per `LINEAR_RECOMMENT_MIN_INTERVAL` per issue (default 15m), which dominates spam scenarios in practice.
- **Healthcheck on linear-webhook compose service is best-effort** — distroless/static lacks wget/nc/curl, so no in-container probe. The smoke target probes `/healthz` from the test host directly. If a stricter healthcheck is needed, a Go-binary internal health client is a follow-up.

## [0.7.0] — 2026-05-02

Phase 2C.4 sub-project F — sustained git calibration + per-tree git.status thresholds. Third sub-project of the operational-readiness track. Closes the calibration deuda from 2C.2: all 4 git capabilities (`git.status@v1`, `git.clone@v1`, `git.diff@v1`, `git.commit@v1`) move out of PROVISIONAL/ROUGH, with `git.status@v1` promoted to CALIBRATED on per-tree evidence and the rough tier moved to SMOKE_CALIBRATED with tighter `le` values derived from the observed adapter floor + workload-tail allowance. No runtime Go code changes; no metric contract changes (R3 buckets frozen, R16 cardinality unchanged — the per-tree split is k6-side only). Spec-complete against `docs/superpowers/specs/2026-05-01-phase-2c.4-f-sustained-git-calibration-design.md`.

### Added

- `ops/load/scenarios/git_rough.js` and `ops/load/scenarios/git_status.js` ship 6 + 2 observation-only k6 thresholds (`p(99)<60000` for clone, `p(99)<10000` for diff/commit, `p(99)<5000` per tree for git.status). NOT SLO targets — instrumentation that forces k6 to emit per-tag p50/p95/p99 in `handleSummary` so `generate-report.sh` can extract per-cap and per-tree breakdowns. Values are deliberately ~10×–25× expected p99 (per A2C4F.1 / D2C4F.6) — they CAN fail if performance catastrophically regresses, surfacing pre-existing degradation rather than silently calibrating against degraded behavior.
- `ops/slo/calibration-reports/2026-05-02-baseline-v1.md` — sustained-run calibration report with hand-edited tier sections containing per-cap p99 + per-tree split + decisions. Replaces the prior 2026-04-25 baseline-v2 (which left the git smoke tier and rough tier sections as placeholders).
- `ops/slo/calibration-reports/evidence/2026-05-02-baseline-v1/` — full evidence dir with `summary.json` (k6 raw), per-cap PromQL queries (instant + range), `git-status-smoke-split.json` (per-tree breakdown small-repo + dirty-tree), `git-rough-observations.json` (clone + diff + commit observations), `manifest.json` (envelope metadata).

### Changed

- `ops/slo/git.yaml` — 4 latency SLO `le` values + tier labels updated:
  - `git-status-latency`: `le=0.25` → **`0.1`** (CALIBRATED ↑ from SMOKE_CALIBRATED). Worst-tree dirty-tree p99=16.4ms, n=4724, headroom 6.1×. All D2C4F.8 promotion criteria satisfied.
  - `git-clone-latency`: `le=30` → **`1`** (SMOKE_CALIBRATED ↑ from PROVISIONAL/ROUGH). Adapter floor p99=6.1ms, headroom 162×. Cannot promote to CALIBRATED — bench measures `file://` small-repo only, real-world latency depends on workload variables not captured (network, repo size, history depth).
  - `git-diff-latency`: `le=3` → **`0.5`** (SMOKE_CALIBRATED ↑). Adapter floor p99=20.4ms, headroom 24×.
  - `git-commit-latency`: `le=2` → **`0.5`** (SMOKE_CALIBRATED ↑). Adapter floor p99 ≤ 9.0ms (upper bound — k6 scenario tag inheritance contaminates pure git.commit p99 with the chain's clone+write calls). Headroom 55×.
- `ops/slo/git.yaml` header rewrites the calibration-state preamble to reflect the new mixed state (1 CALIBRATED + 3 SMOKE_CALIBRATED) and embeds the bench-observed numbers as inline evidence so future readers don't need to cross-reference the calibration report.
- `ops/prometheus/generated/git.yaml` regenerated by `make sloth-generate` — recording + alerting rules now query the new `le` buckets (`0.1`, `1`, `0.5`, `0.5`).
- `ops/load/scenarios/suite.js` (B1.5 fix) — bumps `git_clone_rough.iterations` 20 → 60 and `maxDuration` 5m → 15m; `git_commit_rough.iterations` 10 → 30 and `maxDuration` 4m → 6m; reschedules `T_GIT_ROUGH_DIFF` 43m45s → 53m45s and `T_GIT_ROUGH_COMMIT` 45m → 55m. Adds 6 observation-only thresholds for clone/diff/commit + 2 per-tree for git.status. Replaces the prior `gitCommit()` stub (left over from 2C.2 Bundle 3) with a proper clone → filesystem.write_file → git.commit chain so suite.js produces real git.commit measurements rather than instant-failure iterations against a non-existent workdir.
- `ops/load/lib/generate-report.sh:152-153` — fixes a jq selector bug that caused per-tree split to ship as `null` even when the data was present. The selectors looked for `http_req_duration{tree:small-repo}` (tree label only) but k6 emits the threshold-filtered metrics with the full tag set `http_req_duration{capability:git.status@v1,tree:small-repo}`. Updated to use the full key.

### Notes

- **Suite wall time grows from ~45m to ~63m** (clone bump 5m→15m + commit bump 4m→6m + 1 minute of generate-report.sh). `make load-baseline` operator workflow unchanged otherwise.
- **R16 unchanged** — runtime metrics for `git.status@v1` remain a single capability-keyed SLO (no `tree=` label on the runtime metric). The per-tree split is k6-side analysis only — it informs the threshold decision (worst-tree drives the SLO target) but does not surface as a runtime label.
- **R3 unchanged** — no new histogram buckets. New `le` values (`0.1`, `0.5`, `1`) all use existing buckets in `obs.DurationBuckets`.
- **B1's wiring miss (B1.5 retrospective):** the original B1 PR (commit `20af7f8`, merged as `c5131a7`) modified `git_rough.js` and `git_status.js` only. The first B2 baseline run on 2026-05-02 surfaced that those files are NOT the entrypoint k6 honors — `make load-baseline` runs `suite.js`, which has its own self-contained `options.scenarios` + `thresholds:` blocks. k6 only honors `options` from the entrypoint file; per-scenario `options` from imported files are NOT inherited. The B1.5 fix applies the same B1 changes to suite.js. Spec §1.1 documents this as a binding lesson for future load-test work in this repo.
- **Cancellation-rate SLOs untouched** (4 git capabilities × 1 SLO each, added in G 2C.4). They remain PROVISIONAL operator hypotheses pending production cancellation telemetry; bench evidence not relevant to them.
- **Core tier confirmed** — observed p99 13.69 / 15.91 / 12.69 / 11.85 ms for shell.exec / fs.read / fs.write / http.request, well within existing CALIBRATED targets. No core YAML changes proposed.

## [0.6.0] — 2026-05-01

Phase 2C.4 sub-project E — pgx pool Prometheus collector. Second sub-project of the operational-readiness track. Wires `pgxpool.Pool.Stat()` to 6 OTel observable instruments under `runtime_adapters.pgx_pool.*`, hand-rolled with zero new dependencies. Unblocks the dormant `PoolIdleZero` alert from 2C.1: its selector switches from the placeholder `pgx_pool_idle_conns` to the actual exposed `runtime_adapters_pgx_pool_idle_conns`. Spec-complete against `docs/superpowers/specs/2026-05-01-phase-2c.4-e-pgx-pool-collector-design.md`.

### Added

- `internal/infrastructure/obs/pgxpool_collector.go` — `PgxPoolCollector` + `PoolStatSnapshot` + `PoolStatProvider` + `SnapshotFromPgx`. Six instruments under `runtime_adapters.pgx_pool.*`:
  - Gauges: `idle_conns`, `max_conns`, `total_conns`, `acquired_conns`.
  - Counters: `acquire_count` (Prom: `..._total`), `empty_acquire_count` (Prom: `..._total`).
  All zero-label (R16). One `RegisterCallback` shares one `Stat()` snapshot across all 6 instruments per export tick (no torn read).
- `internal/infrastructure/obs/pgxpool_collector_test.go` — two unit tests:
  - `TestPgxPoolCollector_AllSixMetricsObservedFromSingleSnapshot` (P2 contract)
  - `TestPgxPoolCollector_CounterIsCumulativeMonotonicNotDelta` (P3 + D2C4E.5 contract: counter values pass through unchanged from `pgxpool.Stat()`; rate computation happens at Prom query time)
- Bootstrap wiring at step 3.5 in `internal/bootstrap/wire.go`. Collector held on `Runtime.PoolCollector` so the shutdown closure can unregister its callback before `pool.Close()`. Every error path in `BuildRuntime` after step 3.5 also calls `_ = poolCollector.Close()` before `pool.Close()` so the SDK never holds a callback over a torn pool — uniform with the shutdown ordering invariant.

### Changed

- `ops/prometheus/rules/infra_pool.yaml` — `PoolIdleZero` selector switches from `pgx_pool_idle_conns` to `runtime_adapters_pgx_pool_idle_conns`. The `DORMANT IN PRODUCTION (2C.1)` comment block is replaced with a one-line back-pointer to the new collector source file. Rule semantics (`min_over_time(... [1m]) == 0`, `for: 1m`, `severity: warning`) are unchanged.
- `Runtime` struct in `internal/bootstrap/wire.go` gains a `PoolCollector *obs.PgxPoolCollector` field. Shutdown closure adds a step (`poolCollector.Close()`) between HTTP shutdown and OTel shutdown so the SDK never holds a callback over a torn pool.

### Notes

- No new dependencies. The collector uses `go.opentelemetry.io/otel/metric` (already imported) and `github.com/jackc/pgx/v5/pgxpool` (already imported).
- No metric-contract change beyond the 6 new instrument registrations. R3 ports stable; R16 cardinality bounded (zero labels on all 6 instruments).
- `runtime_adapters.pool.connections.acquired.duration` histogram (added in 2C.1) is unchanged. `acquire_count` is a cumulative count of acquires, complementary to (not duplicated by) the histogram which records acquire latency distributions.
- The 6 deferred `Stat()` fields (`ConstructingConns`, `CanceledAcquireCount`, `NewConnsCount`, `MaxIdleDestroyCount`, `MaxLifetimeDestroyCount`, `AcquireDuration`) are explicit non-goals (NG1). Add as a follow-up bundle if dashboards or future SLOs demand.
- Smoke verification under the 2C.2 load envelope is recorded inline here as a one-time check, not a recurring CI gate (D2C4E.8). After this PR merges, an operator can drive saturation via `make load-baseline` against a deliberately under-provisioned pool and confirm `runtime_adapters_pgx_pool_idle_conns` is queryable in Prom and that `PoolIdleZero` fires within ~1 minute of sustained zero-idle. The roadmap §G exit criterion is satisfied by collector wiring + alert rule connection; ongoing verification of saturation behavior is operations.
- Real receivers (PagerDuty, Slack, Linear) for `PoolIdleZero` ship with sub-project A+B (`v0.8.0`); for now the alert routes to `null-receiver`.

## [0.5.0] — 2026-04-30

Phase 2C.4 sub-project G — cancellation-rate + persist-availability SLOs. First sub-project of the operational-readiness track. Adds 8 per-Phase-1-capability cancellation-rate SLOs and 1 global persist-availability SLO. Re-enables the two chaos scenarios skipped at `v0.4.0` (`ci-shell-hang-cancel`, `ci-persist-fail`); nightly comprehensive returns to 6/6 active. No runtime Go code changes; no metric contract changes; no alertmanager changes. Spec-complete against `docs/superpowers/specs/2026-04-29-phase-2c.4-g-cancellation-persist-slos-design.md`.

### Added

- 8 cancellation-rate SLOs in `ops/slo/{shell,git,filesystem,http}.yaml` — one per Phase 1 capability. PROVISIONAL initial objective: 99.0% (cancellation rate <1%). Burn-rate alerts: `ShellExecCancellationRateBurn`, `GitStatusCancellationRateBurn`, `GitCloneCancellationRateBurn`, `GitDiffCancellationRateBurn`, `GitCommitCancellationRateBurn`, `FilesystemReadFileCancellationRateBurn`, `FilesystemWriteFileCancellationRateBurn`, `HttpRequestCancellationRateBurn`.
- 1 persist-availability SLO in `ops/slo/persist.yaml` (NEW file). Global, no `capability` label. PROVISIONAL initial objective: 99.9% (persist failure rate <0.1%). Burn-rate alert: `PersistAvailabilityBurn`. SLI denominator derived from `runtime_adapters_execution_total + runtime_adapters_receipt_persist_failures`, exact via the RecordExecution-after-persist invariant established post-B8/B9.
- Test SLO mirrors under `ops/slo/test/` for the chaos canary stack (`service: "runtime-adapters-chaos-test"`, 5m period).
- Sloth-rendered recording rules + alerts under `ops/prometheus/generated/` (prod) and `ops/prometheus/generated/test/` (test).
- `Test_RecordExecution_OnlyAfterPersistSuccess` contract test in `internal/application/services/execute_service_test.go` — protects the persist SLI denominator derivation against future refactors of `execute_service.go` that would silently invalidate the SLO math. Failure messages explicitly point at the SLI file. (A2C4G.1.)
- `globalSLOs` allowlist in `ops/slo/slo_coverage_test.go` — explicit, named exception path for runtime-wide SLOs that intentionally lack `labels.capability`. `persist-availability` is the first entry.

### Changed

- `test/chaos/e2e/comprehensive_test.go` — `ci-shell-hang-cancel` and `ci-persist-fail` rows un-skipped. The first now expects `ShellExecCancellationRateBurn`; the second expects `PersistAvailabilityBurn` (with `canonicalCap: ""` because the SLO is global). File-header doc updated to "All six CI scenarios are active as of v0.5.0".
- `ops/slo/slo_coverage_test.go` — naming-contract switch now accepts `-cancellation-rate` suffix; per-capability coverage check now requires `-cancellation-rate` SLO for every Phase 1 capability (in addition to the existing availability + latency).

### Notes

- Initial objectives ship PROVISIONAL — operator hypotheses, not yet calibrated against real load. Recalibration with evidence is a follow-up if production signal demands. (NG1.)
- No metric contract change. The existing inhibition rule `equal: ['sloth_slo', 'capability']` in `ops/alertmanager/alertmanager.yaml` is unchanged — it already handles both labeled and unlabeled SLOs correctly because Alertmanager treats absent capability on both source and target as equal.
- Real receivers (PagerDuty, Slack, Linear) for the new alerts ship with sub-project A+B (`v0.8.0`).

## [0.4.0] — 2026-04-28

Phase 2C.3 — chaos + minimal hardening. Adds a deliberate fault-injection layer that compiles into the production binary as opt-in code gated at runtime by `RUNTIME_CHAOS_ENABLED` plus `RUNTIME_ENV != "production"` (R17 fail-closed). Adapters opt into chaos via a new `ChaosCapable` interface; the wrapper is identity unless config + env authorise. Six CI fault profiles plus ~24 local-only exploratory profiles cover shell, git, filesystem, http, and cross-cutting (panic, persist, pool-exhaustion) failure modes. Two-tier CI: per-PR canary (single profile, label-precise + tiered 60s/90s budget) plus nightly comprehensive (five active scenarios + inhibition contract). Two reactive bundles (B8 + B9) closed gaps surfaced by the chaos suite during development. Spec-complete against `docs/superpowers/specs/2026-04-26-phase-2c.3-chaos-hardening-design.md`.

### Added

#### Chaos framework (Bundle 1)

- `internal/infrastructure/chaos/` — decorator package with closed fault enum (`fault_kinds.go`), decorator-native `ChaosAdapter` (`chaos_adapter.go`), `ChaosReceiptRepository` wrapper for persist-fault injection, profile loader with v1 schema (`profile.go`, `loader.go`), path-hardened allowlist (`validate_support.go`), config + fail-closed env wiring (`config.go`, `wire.go`), opt-in `ChaosCapable` interface (`chaos_capable.go`).
- Per-adapter `ChaosCapable` implementations: `internal/adapters/outbound/{shell,git,filesystem,httpreq}/chaos.go`. Each adapter owns its `SupportedChaosFaults()` + `SyntheticOutcome()` mappings; the chaos package never constructs adapter-internal raw outcome types.
- `internal/bootstrap/wire.go` — integrates `MaybeWrapAdaptersWithChaos` into `BuildRuntime` after env-config load and before application service construction. Identity wrapper when chaos disabled; R17 fail-closed assertion at bootstrap.
- New rule **R17 — chaos fail-closed in production** (`docs/rules.md`).
- New invariant **I24 — chaos preserves runtime semantics** (`docs/domain-invariants.md`).

#### CI fault catalogue (Bundle 2)

- 6 PR-gating profiles under `ops/chaos/profiles/ci/`:
  - `ci-fs-readfile-eio.yaml` — synth EIO on `filesystem.read_file@v1`
  - `ci-git-remote-unreachable.yaml` — DNS/TCP unreachable on `git.clone@v1`
  - `ci-http-connection-reset.yaml` — synth ECONNRESET on `http.request@v1`
  - `ci-persist-fail.yaml` — `ChaosReceiptStore` returns persist error
  - `ci-shell-hang-cancel.yaml` — `hang_until_cancel` on `shell.exec@v1`
  - `ci-shell-panic.yaml` — `inject_panic` on `shell.exec@v1` (R4 audit)
- `TestProfile_AllCIProfilesParse` walks the CI directory and asserts every YAML parses against the test catalogue (R15 status validity).

#### Per-PR chaos integration tests (Bundle 3)

- `test/chaos/integration/chaos_integration_test.go` — table-driven test, build tag `integration`. Six subtests (one per CI profile) running classification + receipt persistence + metric increment assertions in a fresh testcontainers Postgres + in-process app.
- `test/chaos/integration/persist_invariant_test.go` — concurrent-load persist-before-return invariant (A4.3) under chaos.
- `test/chaos/integration/fixtures_test.go` — shared fixture helpers + builder.

#### Safety-net invariant tests (Bundle 4)

- `test/chaos/safety/panic_audit_test.go` — R4 panic-recoverer mount audit at four code paths: adapter-execute, normalizer, persist, idempotency-replay.
- `test/chaos/safety/normalize_property_test.go` — `testing/quick` property tests asserting `ResultNormalizer` determinism and idempotency across the closed status × error-class space (I3 + I22).
- Surfaced two reactive gaps (see Reactive bundles below).

#### Receiver stub + compose chaos overlay (Bundle 5)

- `ops/chaos/receiver/main.go` + `Dockerfile` — small purpose-built HTTP receiver-stub with `GET /inspect`, `GET /inspect?since=<rfc3339>`, `POST /clear`. Records every Alertmanager webhook POST in a ring buffer with server-side `Received time.Time`. ~150 LOC of Go.
- `ops/local/compose.chaos.yaml` — overlay extending `compose.yaml` with `alertmanager` (`prom/alertmanager:v0.27.0`), `receiver-stub`, `toxiproxy` (profile-gated), and chaos env vars on the runtime service. Mounts `ops/chaos/profiles/{ci,local}` at `/etc/runtime-adapters/chaos/profiles/`. Swaps Prometheus config for the chaos variant via deterministic file mount (D2C3.26 — no env-var YAML interpolation).
- `ops/prometheus/prometheus.chaos.yml` — overlay Prom config (1s scrape + evaluation interval, alertmanager:9093 target, glob over `/etc/prometheus/test-rules/*.yaml`).
- `ops/alertmanager/alertmanager.chaos.yaml` — chaos AM config routing all alerts to `http://receiver-stub:8088/`.
- `ops/chaos/scripts/dump.sh` — diagnostic dump (Prom rules/alerts/SLI query, AM alerts/status, receiver `/inspect`, container logs).
- Make targets: `chaos-up`, `chaos-up-toxiproxy`, `chaos-down`, `chaos-local`, `chaos-dump`, `chaos-render-rules`, `chaos-render-rules-check`.

#### Per-PR E2E canary + test SLO + GHA workflow (Bundle 6)

- `test/chaos/e2e/canary_test.go` :: `TestChaos_Canary_HttpConnectionReset` — drives sustained ~80s of failed `http.request@v1` calls and asserts `HttpRequestAvailabilityBurn` reaches the receiver-stub within the D2C3.21 tiered budget (60s target / 90s deadline).
- `test/chaos/e2e/{compose_lifecycle,helpers,breadcrumbs,receiver_client}.go` — shared E2E infrastructure (ComposeUp/Down lifecycle, repo-root finder, in-test diagnostics dump, receiver client with `WaitForAlert`/`AlertsMatching`/`AlertsMatchingSince`/`Clear`).
- `ops/slo/test/{shell,git,http,filesystem}.yaml` — test SLO specs with `service: "runtime-adapters-chaos-test"` (avoids `sloth_slo` collision with prod). Rendered with `--default-slo-period 5m`.
- `ops/slo/windows/5m.yaml` — custom Sloth `AlertWindows` catalog (Sloth 0.16.0 ships only the 30d catalog out of the box).
- `ops/prometheus/generated/test/{shell,git,http,filesystem}.yaml` — per-spec rendered burn-rate rules (one file per SLO spec; concatenation invalid for Prom `rule_files`).
- `.github/workflows/ci.yaml` — new `chaos-canary-e2e` job (PR-only, `timeout-minutes: 10`).
- Make target: `chaos-canary`.
- Sloth idempotency gate (`chaos-render-rules-check`) extends to test rules with explicit exclusion of the test path from the prod sloth-generate diff.

#### Nightly comprehensive + local catalogue + docs + ADRs (Bundle 7)

- `test/chaos/e2e/comprehensive_test.go` :: `TestChaos_Comprehensive` — parameterised version of the canary running across five active CI scenarios (`ci-persist-fail` skipped, no test SLO covers persist counter). Each scenario asserts the label-precise critical alert plus the inhibition contract (`AlertsMatchingSince(criticalAlert.Received)` returns zero warnings for the same `(sloth_slo, capability)` after the critical lands — D2C1.17).
- `.github/workflows/chaos-nightly.yaml` — daily cron `0 7 * * *` UTC + `workflow_dispatch`. Workflow-level `permissions: contents:read + issues:write`. Uploads 90-day diagnostics artifact (`chaos-nightly-diagnostics-${{ github.run_id }}`) on every run; opens auto-issue labeled `chaos-nightly-fail` via `gh issue create` direct on failure.
- 24 local-only profiles under `ops/chaos/profiles/local/` covering shell (4), git (3), filesystem (5), http (6), cross-cutting (6). Three §7.1 scenarios honestly skipped (no closed-catalog representation): `stdout-flood`, `dirty-workdir`, `corrupt-repo`. Two scenarios approximated with documentation (`http-5xx-storm` via `latency`, `cross-pool-exhaustion` via concurrent-load latency on a hot adapter).
- `TestProfile_AllLocalProfilesParse` — sibling of CI walker, walks `ops/chaos/profiles/local/` with R15 status enforcement.
- `docs/chaos.md` — operator guide (10 sections per spec §14.2): what chaos means, enabling locally, fault catalogue, profile schema, per-PR canary, nightly comprehensive, adding new fault types/profiles, reactive fix workflow, troubleshooting + diagnostic command cheat sheet.
- ADR 0009 — Chaos as opt-in compiled-in code (Q2 = d).
- ADR 0010 — Receiver-stub for the Alertmanager webhook contract.
- ADR 0011 — Two-tier chaos CI (Q3 = d, Q5 split, D2C3.21 tiered budget).
- Make target: `chaos-e2e-comprehensive`.

### Fixed

#### Reactive bundle B8 — receipt.persist.failures counter wire (PR #30)

- Surfaced by Bundle 3 chaos integration test on `ci-persist-fail`. The `runtime_adapters.receipt.persist.failures` counter declared in 2C.1 §6.3 was never incremented at the persist sites. Wired the counter at both persist call sites in `internal/application/services/execute_service.go`. No spec change; the spec already required this metric.

#### Reactive bundle B9 — complete panic recovery contract (PR #32)

- Surfaced by Bundle 4 panic audit. R4's "no panic crosses the application boundary" rule held only at adapter-execute; the normalizer, persist, and idempotency-replay paths all lacked `defer recover`. Added surgical recovery at all four sites in `internal/application/services/execute_service.go`, plus a `panic_location` log field and an `adapter.panics` counter wire. ADR pending in any future revision; the change closes R4 across the application boundary.

### Changed

- `RUNTIME_CHAOS_ENABLED`, `RUNTIME_CHAOS_PROFILE`, and `RUNTIME_ENV` join the runtime config surface. `RUNTIME_ENV=production` rejects chaos at bootstrap regardless of `ENABLED` (R17).
- `MaybeWrapAdaptersWithChaos` is now part of the bootstrap path. Production binaries carry the chaos package as inert code when chaos is disabled — no measurable runtime cost.
- Sloth idempotency-gate exclusions extended to cover the test rules path (`ci.yaml`).

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
