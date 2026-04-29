# Chaos — operator guide

Operator-facing companion to the design spec
(`docs/superpowers/specs/2026-04-26-phase-2c.3-chaos-hardening-design.md`).
Covers what chaos is, how to enable it locally, the closed fault catalogue,
the profile YAML schema, the per-PR canary, the nightly comprehensive run,
how to add new faults or profiles, the reactive fix workflow, and a
troubleshooting cheat sheet.

## 1. What chaos means in this repo

Chaos in `runtime-adapters` is a **deliberate fault-injection layer** —
not random fault generation, not OS-level chaos engineering. Each fault
is a typed, named, classified failure mode the runtime can be asked to
synthesise so that downstream contracts (classification, persistence,
metrics, alerts) are exercised end-to-end without depending on a real
upstream outage. Spec §3 frames it.

Three principles govern every line of chaos code:

1. **Chaos-capable, not chaos-aware.** Adapters opt into chaos via the
   `ChaosCapable` interface (`internal/infrastructure/chaos/chaos_capable.go`).
   The runtime's domain and application layers are unchanged from the
   non-chaos case. Production binaries can compile chaos in and still
   run zero chaos because `MaybeWrapAdaptersWithChaos` is identity when
   `Config.Enabled == false` (`internal/infrastructure/chaos/wire.go`).

2. **Fail-closed in production (R17).** `LoadConfig` refuses to return a
   chaos-enabled config when `RUNTIME_ENV=production`, regardless of
   `RUNTIME_CHAOS_ENABLED`. The check lives at
   `internal/infrastructure/chaos/config.go:LoadConfig`. Operators must
   not rely on this as the only line of defence — see §2 for the full
   matrix and an explicit do-not-do warning.

3. **Evidence-driven.** Every behaviour the chaos framework promises is
   covered by an automated assertion: closed fault catalogue
   (`fault_kinds.go` + `TestProfile_AllCIProfilesParse`), schema validity
   (`profile_test.go`), classification contract (B3 chaos integration
   tests), receipt persistence + metric increment (B3 + B4 invariants),
   alert delivery within budget (B6 canary), inhibition contract (B7
   nightly comprehensive). Receipts always materialise (R13, I24).

The boundary is hard: the runtime never **decides** to run chaos. CI or
operators **request** it via `RUNTIME_CHAOS_ENABLED=true` plus a profile
path. Production never sees it (R17 + I24).

## 2. Enabling chaos locally

### Env var matrix (spec §8.1)

| Var | Default | Allowed | Effect |
|---|---|---|---|
| `RUNTIME_CHAOS_ENABLED` | `false` | `true` \| `false` | If `false`, chaos config is ignored; `MaybeWrapAdaptersWithChaos` is identity. |
| `RUNTIME_CHAOS_PROFILE` | `""` | path to `.yaml` inside the allowlist | Required if `ENABLED=true`. |
| `RUNTIME_ENV` | `development` | `development` \| `staging` \| `production` | If `production`, chaos is rejected even with `ENABLED=true`. |

Allowlist roots (loader-enforced, no parent traversal, no symlink escape):

- `/etc/runtime-adapters/chaos/profiles/ci/`
- `/etc/runtime-adapters/chaos/profiles/local/`
- `ops/chaos/profiles/ci/` (relative to repo root, dev only)
- `ops/chaos/profiles/local/` (relative to repo root, dev only)

### Compose

```bash
make chaos-up                       # alertmanager + receiver-stub + chaos env
make chaos-up-toxiproxy              # plus toxiproxy (real network chaos)
make chaos-down                      # tear down volumes + networks
```

### Run a single local-only profile

```bash
RUNTIME_CHAOS_ENABLED=true \
RUNTIME_CHAOS_PROFILE=ops/chaos/profiles/local/fs-eio.yaml \
RUNTIME_ENV=development \
docker compose -f ops/local/compose.yaml -f ops/local/compose.chaos.yaml up runtime-adapters

# or via the make wrapper
make chaos-local PROFILE=fs-eio
```

### Production warning

> **DO NOT enable chaos in production.** The runtime refuses to start if
> it detects `RUNTIME_ENV=production` with `RUNTIME_CHAOS_ENABLED=true`,
> but operators must not rely on that as the only line of defence. Treat
> `RUNTIME_CHAOS_*` env vars as development/staging-only. Auditing your
> deploy pipelines to ensure those vars are absent from production
> manifests is mandatory.

## 3. Fault catalogue

Closed enum (`internal/infrastructure/chaos/fault_kinds.go`). Adding a
new kind is governed by §7 of this guide.

| Kind | Adapter applicability | Status | error_class | retry_hint default | Notes |
|---|---|---|---|---|---|
| `latency` | any | success | `""` | n/a | Sleeps before/after dispatch; can drive caller timeout. |
| `connection_reset` | http | failure | `external_failure` | `unknown` | Synth ECONNRESET. |
| `hang_until_cancel` | shell, fs | cancelled | `cancelled` | `non_retryable` | Blocks until ctx cancellation. |
| `eio` | fs | failure | `external_failure` | `unknown` | Synthesised disk EIO. |
| `enospc` | fs | failure | `external_failure` | `non_retryable` | Out of space (write paths). |
| `permission_denied` | fs | failure | `external_failure` | `non_retryable` | EACCES (override `unknown` default — operator concern, not transient). |
| `remote_unreachable` | git, http | failure | `external_failure` | `retryable` | DNS / TCP unreachable. |
| `auth_failure` | git, http | failure | `external_failure` | `non_retryable` | 401/403 — won't fix on retry. |
| `inject_panic` | any (R4 audit) | failure | `adapter_internal_error` | `non_retryable` | Recovered by `defer recover` (R4); receipt still emitted. |
| `process_signal` | shell | failure | `adapter_internal_error` | `non_retryable` | SIGSEGV/SIGABRT; child process killed. |
| `process_exit` | shell | failure | `external_failure` | `unknown` | Non-zero exit code (e.g. 137 SIGKILL). |
| `slow_body` | http | failure | `timeout` | `retryable` | Response body trickled past caller deadline. |
| `redirect_loop` | http | failure | `external_failure` | `non_retryable` | Self-redirect chain. |
| `tls_handshake_fail` | http | failure | `external_failure` | `unknown` | TLS error pre-request. |
| `persist_failure` | persistence layer (not adapter) | n/a — caller sees error | n/a | n/a | Routes through `ChaosReceiptStore`; never reaches adapter dispatch. |

Authoritative dispatch: `internal/infrastructure/chaos/chaos_adapter.go`.
Per-adapter `ChaosCapable.SupportedChaosFaults()` lives in each
`internal/adapters/outbound/*/chaos.go`.

## 4. Profile YAML schema (v1)

Loader: `internal/infrastructure/chaos/profile.go`. Tests:
`internal/infrastructure/chaos/profile_test.go`.

### Annotated example

```yaml
version: 1                       # MUST be 1 — anything else rejected
name: ci-fs-readfile-eio         # unique across ci/ and local/ catalogues
description: |
  Free-text. Used by operators to understand intent. Not asserted.
adapters:                        # one or more capabilities under chaos
  filesystem.read_file@v1:       # canonical capability id (Phase 1 catalogue)
    fault: eio                   # MUST be in fault_kinds.go closed enum
    timing: post_dispatch        # pre_dispatch | post_dispatch
    # extra fields per fault kind:
    # latency:    match: { duration: 750ms }
    # process_signal:  signal: "SIGSEGV"
    # process_exit:    code: 137
expected_outcome:                # asserted by B3 chaos integration tests
  status: failure                # one of: success | failure | timeout | cancelled | partial (R15)
  error_class: external_failure  # see internal/domain/execution/valueobjects/error_class.go
  retry_hint: unknown            # retryable | non_retryable | unknown
persistence: null                # or { fault: persist_failure } for cross-cutting persist scenarios
```

### Stable contract fields

- `expected_outcome.status` — one of the 5 R15 statuses. Asserted in
  receipt classification tests.
- `expected_outcome.error_class` — must match a constant in
  `internal/domain/execution/valueobjects/error_class.go`.
- `expected_outcome.retry_hint` — `retryable` | `non_retryable` |
  `unknown`. Defaults derive from error class via `DefaultRetryHint()`;
  profiles MAY override (e.g. `permission_denied` overrides default
  `unknown` to `non_retryable` because EACCES is operator-fix-only).

Local profiles (under `ops/chaos/profiles/local/`) are exploratory —
`expected_outcome` documents intent but is not asserted by an automated
test (spec §7.1). CI profiles (under `ops/chaos/profiles/ci/`) ARE
asserted by the B3 chaos integration tests.

## 5. Per-PR canary

What runs: `TestChaos_Canary_HttpConnectionReset` in
`test/chaos/e2e/canary_test.go`. Single profile:
`ci-http-connection-reset.yaml`. GHA job: `chaos-canary-e2e` in
`.github/workflows/ci.yaml` (only on `pull_request`).

Why this profile: HTTP connection-reset exercises the full pipeline
end-to-end (runtime → otel-collector → prometheus → sloth burn-rate
rules → alertmanager → receiver-stub) and validates `(sloth_slo,
capability)` label precision plus tiered timing budget without coupling
to git or shell semantics.

### Tiered budget (spec §12.2, D2C3.21)

| Latency | Outcome |
|---|---|
| ≤ 60s   | pass |
| 60-90s  | pass-with-warning (logged, not failed) |
| > 90s   | fail (real pipeline gap — STOP rule applies; see §9) |

Test exercise: sustained 0.5 RPS of failed `http.request@v1` calls for
the duration of the assertion phase. The 30s OTel periodic-export
interval makes a 15s burst invisible to `rate(counter[1m])` — see
commit `65bc3d9` for the diagnostic and the fix.

## 6. Nightly comprehensive

What runs: `TestChaos_Comprehensive` in
`test/chaos/e2e/comprehensive_test.go`. 4 active scenarios + 2 skipped:

- `ci-persist-fail` — no test SLO covers
  `runtime_adapters.receipt.persist.failures`, so no burn-rate alert
  can fire (omitted at B7 design time).
- `ci-shell-hang-cancel` — drives 100% `status=cancelled` traffic;
  the shell availability SLO intentionally excludes `cancelled` from
  numerator and denominator, so the SLI ratio is undefined and no
  burn-rate alert can fire. Surfaced by first-nightly run
  25096750669 (2026-04-29). The chaos profile remains in
  `ops/chaos/profiles/ci/` because the B3 chaos integration test
  (`test/chaos/integration/`) covers the classification + receipt +
  metric contract for cancellation — that's a different layer than
  alert delivery. A cancellation-rate SLO that would let this
  scenario rejoin the suite is deferred to Track 2C.4 (operational
  readiness).

GHA workflow: `.github/workflows/chaos-nightly.yaml` (cron `0 7 * * *`
UTC + manual dispatch). Job timeout 30 min; suite wall ~15-20 min.

Each scenario:

1. ComposeUp + ComposeDown per scenario (env vars are fixed at
   container start; swapping `RUNTIME_CHAOS_PROFILE` requires a fresh
   container).
2. `rc.Clear` between scenarios so AlertsMatching only sees alerts from
   the current scenario.
3. Sustained traffic for the assertion window.
4. Label-precise critical assertion + tiered budget (60s/90s).
5. **Inhibition contract assertion** — `rc.AlertsMatchingSince(...,
   criticalAlert.Received)` must return zero warning alerts for the
   same `(sloth_slo, capability)` after the critical lands. This
   validates D2C1.17 (alertmanager inhibition uses sloth_slo +
   capability).

### Triage on failure

The workflow opens an auto-issue labeled `chaos-nightly-fail` and
attaches a 90-day diagnostics artifact named
`chaos-nightly-diagnostics-${{ github.run_id }}` containing:

- `prom-rules.json` — full rule set Prometheus knows about
- `prom-active-alerts.json` — what's firing right now
- `prom-execution-total.json` — counter snapshot
- `prom-sli-rate1m.json` — recording rule output (watch for `NaN` —
  see §10)
- `prom-alertmanagers.json` — Prom's view of AM discovery
- `am-alerts.json` — what AM sees / has fanned out
- `am-status.json` — AM cluster state
- `receiver-inspect.json` — what reached the receiver-stub
- `prometheus-logs.txt`, `alertmanager-logs.txt`, `runtime-logs.txt`

PRs are NOT blocked by nightly failures. Triage is human, same-day or
next-day. Per spec §13.4 ¶5: a scenario failing three nights
consecutively requires a human decision — reactive bundle (§9), raise
budget with justification, or `known-flaky` label with a tracked issue.

## 7. Adding a new fault type

Checklist:

1. **Justify** — ADR (§14.3 of the spec) if the fault changes the
   framework's contract (R11 default-rejected). If it's a closed-set
   addition that mirrors an existing pattern, an ADR is optional but
   the commit body must still cite which spec section authorises it.
2. **Enum entry** — add a `FaultXxx` constant to
   `internal/infrastructure/chaos/fault_kinds.go` and append to
   `allFaultKinds`.
3. **Decorator switch** — add a `case FaultXxx:` to the dispatch in
   `internal/infrastructure/chaos/chaos_adapter.go` with the synthesised
   raw outcome + classification mapping.
4. **Per-adapter capability** — extend `SupportedChaosFaults()` in each
   `internal/adapters/outbound/*/chaos.go` that supports the new fault.
   Adapters that don't support it return false and the loader rejects
   profiles that target unsupported (capability, fault) pairs.
5. **Profile validator** — if the fault has new schema fields (e.g. a
   `signal:` for `process_signal`), extend the parser in
   `internal/infrastructure/chaos/profile.go` and add unit coverage.
6. **Document** — extend the §3 fault catalogue table in this guide
   with the new row.
7. **Tests** — unit test for the dispatch + classification, and at
   least one CI or local profile that exercises the path. The
   `TestProfile_AllCIProfilesParse` and `TestProfile_AllLocalProfilesParse`
   walkers will catch unknown kinds at parse time.

## 8. Adding a new local-only profile

1. Pick a kebab-case filename in `ops/chaos/profiles/local/`. Use
   `<adapter>-<scenario>.yaml` (no `ci-` prefix). Cross-cutting
   profiles use `cross-<scenario>.yaml`.
2. Use the v1 schema (§4). Pick a fault from the closed catalogue
   (§3). Pick a Phase 1 capability id from
   `internal/domain/execution/valueobjects/catalog_test.go`.
3. Write `expected_outcome` consistent with the fault's classification.
   For local profiles `expected_outcome` is documentation, not
   asserted — but it must still pass the parse walker (R15 status
   enum, valid `error_class`, valid `retry_hint`).
4. Verify:
   ```bash
   go test ./internal/infrastructure/chaos/... \
     -run TestProfile_AllLocalProfilesParse -v
   ```
5. Optional: exercise locally:
   ```bash
   make chaos-up
   make chaos-local PROFILE=<filename-without-yaml>
   ```

### Operator caveats for existing latency-style profiles

- `http-5xx-storm` and `git-large-clone-timeout` use long-duration
  `latency` faults to drive caller-side timeouts. If your local
  `TimeoutBudget` differs from the runtime's default, tune the
  duration: open the YAML, raise/lower `match.duration`, restart the
  runtime container.
- `cross-pool-exhaustion` requires concurrent load to fire — the
  profile injects 5s latency on a single capability, which only
  saturates `MaxConcurrentExecutions` if the operator drives multiple
  parallel requests. Use `k6` or `hey` at concurrency > the
  configured cap (default 32; see runtime config).

## 9. Reactive fix workflow

When chaos surfaces a real runtime gap (the test exercises a path the
runtime mishandles):

1. **STOP rule.** Do NOT modify the test to make it pass. Do NOT raise
   the budget without evidence. Do NOT change labels blindly.
2. **Document the failure** — capture the diagnostic artifact, identify
   the failing assertion, and write a short root-cause note.
3. **Open a reactive bundle** — branch named
   `feat/<phase>-bundle-N-<short-name>` from main. The B-numbering
   continues (B1-B7 are the planned bundles; reactive bundles are B8,
   B9, …).
4. **Start with the failing test** — copy the failing canary or
   integration case into the reactive bundle so the gap is concrete.
5. **Implement the fix** — modify the relevant adapter/domain/
   application code. Tests pass; CI green.
6. **One ADR per architectural change** — e.g. the B9 panic-recovery
   completion landed `internal/application/services/execute_service.go`
   recover guards at four sites; if a future reactive bundle changes
   the shell adapter to use process groups, that's its own ADR.
7. **Merge order** — reactive bundles merge in order. The original
   chaos test PR (B3 / B4 / B6 / B7) rebases on main between merges.
   The release tag does NOT ship until the last reactive bundle merges
   and CI is fully green.

Concrete examples from Phase 2C.3:

- **B8** (PR #30) — chaos integration test surfaced a dormant
  `runtime_adapters.receipt.persist.failures` counter declared in
  2C.1 but never incremented. Reactive bundle wired the counter at
  both persist sites in `execute_service.go`.
- **B9** (PR #32) — B4 panic audit found that only adapter-execute
  had `defer recover`; normalizer/persist/idempotency-replay didn't.
  Reactive bundle added surgical recovery at four sites + a
  `panic_location` log field + an `adapter.panics` metric wire.

## 10. Troubleshooting

### Common failures and fixes

| Symptom | Likely cause | Fix |
|---|---|---|
| `profile failed to parse` | unknown fault kind, schema drift | `go test ./internal/infrastructure/chaos/... -run 'TestProfile_All.*ProfilesParse' -v` to see the offending file. |
| `chaos: refuses to start with RUNTIME_ENV=production` | bootstrap env says production | Set `RUNTIME_ENV=development` (or staging). Production never runs chaos by design. |
| `chaos: RUNTIME_CHAOS_PROFILE required when enabled` | `RUNTIME_CHAOS_ENABLED=true` without a profile path | Set `RUNTIME_CHAOS_PROFILE` to an absolute path inside the allowlist. |
| `chaos: profile path %q not in allowlist` | path traversal or wrong root | Move the profile under `ops/chaos/profiles/{ci,local}/` or `/etc/runtime-adapters/chaos/profiles/{ci,local}/`. No `..` segments after `filepath.Clean`. |
| compose stack health-check timeout | docker daemon overloaded, stale state | `make chaos-down`, optionally `docker system prune -f` (caution: prunes ALL stopped containers and dangling images), then `make chaos-up`. |
| canary alert not delivered within budget | sustained-traffic miss, NaN rate, or genuine pipeline gap | See "NaN-rate diagnosis" below. |
| inhibition contract violation | warning legitimately raced critical, or AM `inhibit_rules` config is wrong | Test now uses `AlertsMatchingSince(criticalAlert.Received)` (commit `7c0ec99`) to filter to post-critical warnings only. If it still flakes, inspect `ops/alertmanager/alertmanager.chaos.yaml` `inhibit_rules` — the `equal:` block must include `sloth_slo` and `capability`. |
| `make chaos-render-rules-check` fails on idempotency | operator forgot to commit rendered rules | `make chaos-render-rules` then commit the diff in `ops/prometheus/generated/test/`. |

### NaN-rate diagnosis (the B6 lesson)

Symptom: chaos-canary-e2e fails. Diagnostics show:

- `prom-execution-total.json` → counter has the right value
- `prom-sli-rate1m.json` → `"value": [..., "NaN"]`
- `prom-active-alerts.json` → empty
- `am-alerts.json` → empty
- `receiver-inspect.json` → `{"received": []}`

Root cause: the test exercise was shorter than one OTel periodic export
interval (currently 30s — see
`internal/infrastructure/obs/otel.go`). Prometheus scraped a flat
counter plateau, `rate(counter[1m]) = 0/0 = NaN`, no burn-rate alert
can fire.

Fix: drive sustained traffic across the full assertion window
(commit `65bc3d9` in canary, `ec893a9` in comprehensive). DO NOT lower
the OTel interval — that changes production behaviour. The test must
match the pipeline's emission cadence.

### Diagnostic command cheat sheet

```bash
# Live state inspection (compose stack must be UP)
curl -s http://localhost:9090/api/v1/alerts | jq
curl -s 'http://localhost:9090/api/v1/query?query=slo:sli_error:ratio_rate1m' | jq
curl -s 'http://localhost:9090/api/v1/query?query=runtime_adapters_execution_total' | jq
curl -s http://localhost:9093/api/v2/alerts | jq
curl -s http://localhost:9093/api/v2/status | jq
curl -s http://localhost:8088/inspect | jq

# Full diagnostic dump (writes to ./chaos-dump/ by default)
make chaos-dump

# Render and validate test SLO rules
make chaos-render-rules
make chaos-render-rules-check    # CI-equivalent idempotency gate

# Run a specific canary or scenario locally
make chaos-canary
make chaos-e2e-comprehensive

# Tear everything down
make chaos-down
```

### When in doubt

- The chaos spec is authoritative:
  `docs/superpowers/specs/2026-04-26-phase-2c.3-chaos-hardening-design.md`
- Runtime rules: `docs/rules.md` (R4, R10, R11, R15, R17 are chaos-relevant)
- Domain invariants: `docs/domain-invariants.md` (I24)
- ADRs: `docs/adr/0009-*` (chaos opt-in compiled-in), `0010-*`
  (receiver-stub for AM contract), `0011-*` (two-tier chaos CI) ship
  alongside this guide in B7.
