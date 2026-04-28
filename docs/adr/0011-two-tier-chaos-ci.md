# ADR 0011 — Two-tier chaos CI

## Status

Accepted — 2026-04-26.

## Context

Phase 2C.3 wants every PR to surface chaos regressions while ALSO
running a comprehensive scenario suite plus the inhibition contract.
Both signals are valuable; only one of them is fast enough to gate PRs.

The available shapes:

- **a)** Run all six CI scenarios + the inhibition contract on every
  PR. Strong signal; ~15-20 minute wall time per PR; potentially flaky
  on shared runners.
- **b)** Run nothing on PR. Move all chaos to nightly. Fastest PR
  velocity; chaos regressions slip through to main and surface only
  the next morning, by which point they're tangled with other PRs.
- **c)** Run a SUBSET on PR (e.g., a randomly-rotated single scenario).
  Saves time but loses determinism — operators cannot reason about why
  a regression is missed.
- **d)** Run a single LABEL-PRECISE canary on PR (one scenario,
  asserted with the full label set + tiered timing budget) AND run the
  comprehensive scenario suite + inhibition contract nightly.

D2C3.21 establishes the timing budget shape — tiered, with a 60s
target and a 90s deadline:

| Latency | Outcome |
|---|---|
| ≤ 60s   | pass |
| 60-90s  | pass-with-warning (logged, not failed) |
| > 90s   | fail (real pipeline gap — STOP rule) |

The split must answer: which scenarios and which assertions run where,
and why is that the right split.

## Decision

**Adopt option (d): two-tier chaos CI.**

### Tier 1 — per-PR canary

- One scenario: `ci-http-connection-reset.yaml` (HTTP adapter under
  in-process connection-reset injection). Single profile because the
  goal is pipeline coverage end-to-end, not breadth.
- Implementation: `test/chaos/e2e/canary_test.go` ::
  `TestChaos_Canary_HttpConnectionReset`. GHA job:
  `chaos-canary-e2e` in `.github/workflows/ci.yaml` (`if:
  github.event_name == 'pull_request'`).
- Assertions: label-precise critical alert delivery + tiered timing
  budget (D2C3.21).
- Wall time target: < 4 min. Triggers per PR.

### Tier 2 — nightly comprehensive

- Five active scenarios (`ci-http-connection-reset`,
  `ci-fs-readfile-eio`, `ci-git-remote-unreachable`,
  `ci-shell-hang-cancel`, `ci-shell-panic`) plus one explicitly
  skipped (`ci-persist-fail` — no test SLO covers
  `runtime_adapters.receipt.persist.failures`; documented in
  `comprehensive_test.go::comprehensiveScenarios`).
- Implementation: `test/chaos/e2e/comprehensive_test.go` ::
  `TestChaos_Comprehensive`. GHA workflow:
  `.github/workflows/chaos-nightly.yaml` (cron `0 7 * * *` UTC plus
  manual `workflow_dispatch`).
- Assertions per scenario: same label-precise critical + tiered budget
  as the canary, PLUS the inhibition contract (`AlertsMatchingSince`,
  D2C1.17): no warning alert with the same `(sloth_slo, capability)`
  is delivered AFTER the critical lands.
- Wall time target: 15-20 min for the suite (six ComposeUp/ComposeDown
  cycles, each ~90s exercise + ~30s setup + ~30s teardown). Job
  timeout: 30 min.
- On failure: 90-day diagnostics artifact + auto-issue labeled
  `chaos-nightly-fail` via `gh issue create` direct (no third-party
  action). PRs are NOT blocked.
- Three-night consecutive failure on a scenario triggers a human
  decision (spec §13.4 ¶5): reactive bundle, raise the budget with
  evidence, or `known-flaky` with a tracked issue.

## Options considered

- **Option a (everything on PR)** — rejected. 15-20 min per PR is
  longer than every other CI job in the repo; it would dominate the
  feedback loop. Six ComposeUp/Down cycles in a single job amplify
  shared-runner contention failures into PR-blocking flakes. Even
  with optimisation the floor is several minutes.
- **Option b (nothing on PR)** — rejected. Phase 2C.3 ships precisely
  because chaos pipelines are subtle (the OTel 30s export interval
  bug found in B6 — commit 65bc3d9 — is a perfect example).
  Regressions in the alert pipeline surface in nightly only after
  multiple PRs have landed; bisecting becomes painful and the STOP
  rule's effectiveness drops.
- **Option c (subset rotation)** — rejected. Non-deterministic gate.
  Operators cannot answer "did this PR break X?" by reading the run
  log; they must consult a rotation table. Rotation also amplifies
  per-scenario flake risk because each scenario fires on roughly 1/6
  of PRs.
- **Option d (canary + nightly)** — adopted. The canary is fast,
  deterministic, and exercises the full pipeline (runtime → otel →
  prometheus → sloth → alertmanager → receiver-stub). The nightly is
  comprehensive, runs the inhibition contract too, and produces
  artifacts for triage. The split matches the cost/value curve.

## Consequences

- PRs gate on a single chaos scenario. A regression that affects only
  fs/git/shell paths will not surface until the nightly. This is a
  conscious trade — single-pipeline failure modes (OTel pipeline,
  Sloth rendering, AM routing, receiver delivery) are caught on PR;
  adapter-specific failure modes are caught nightly. The B5/B6 bundle
  reactive history validates this split — every failure mode found
  during B7 development was a single-pipeline failure detectable by
  the canary alone.
- The nightly auto-issue must be triaged by humans. The workflow
  document at `docs/chaos.md` §6 + §10 covers triage; ownership of
  the `chaos-nightly-fail` label is operator-team.
- The 60s/90s tiered budget is shared between both tiers. Changes to
  the budget require updating BOTH workflows AND the spec (§12.2,
  D2C3.21). Spec is the single source of truth.
- Adding a new fault scenario follows §7-§8 of `docs/chaos.md`. The
  default placement is in `ops/chaos/profiles/local/` (operator-only,
  not asserted). Promotion to `ops/chaos/profiles/ci/` (asserted by
  the comprehensive nightly) is a deliberate decision per scenario.
- The diagnostics dump pattern is shared (B5 `ops/chaos/scripts/dump.sh`).
  The canary uses an in-test cleanup; the nightly uses an in-test
  cleanup PER SCENARIO. The chaos-canary-e2e and chaos-nightly
  workflows therefore both upload the test-side dump and do NOT run
  `make chaos-dump` as a separate post-step (commit e59bcff in B6
  established the pattern; reaffirmed in B7 Task 7.2).

## Spec references

- §6 (CI catalogue), §12 (canary), §13 (nightly + 13.3 inhibition + 13.4 failure handling)
- D2C3.5 (Q5 split), D2C3.21 (tiered budget), D2C3.22 (auto-issue)
- D2C1.17 (AM inhibition uses sloth_slo + capability)
- ADR 0009 (chaos opt-in compiled-in code), ADR 0010 (receiver-stub for AM contract)
