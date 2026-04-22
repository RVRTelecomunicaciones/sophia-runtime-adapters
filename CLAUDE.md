# CLAUDE.md — runtime-adapters

## What this repo is

`runtime-adapters` is the **governable execution layer** of the Sophia ecosystem. Its sole responsibility is to materialize real side effects on the system — processes, files, network, git — starting from typed requests, returning normalized and auditable results. It is not a utility library. Not a toolkit. Not an orchestrator. It is a boundary separating decisions (made upstream by `agent-governance-core` and `orchestrator`) from execution (controlled here).

## What this repo is NOT

- Does not decide which adapter to use for a task — that is `agent-governance-core` (routing).
- Does not evaluate whether an execution is permitted — that is policy/approval upstream.
- Does not coordinate multiple steps — that is the `orchestrator`.
- Does not maintain workflow state — that lives in governance.
- Does not retry automatically — runtime marks retryability; the consumer decides.
- Does not store operational knowledge — that is `memory-engine`.
- Does not mix technical results ("exit=1") with business semantics ("task failed").
- Does not expose adapters that "do several things" without explicit per-capability contracts.
- Does not hide partial failures behind a success — classifies them explicitly.
- Does not return generic `error` without structured classification.

## Required mindset

> **The runtime doesn't think, it executes. But it executes with a contract, with a limit, and with a receipt.**

Every execution gets three guarantees: a **contract** (caller knows exactly what input is accepted and what output is returned), a **limit** (timeout and cancellation are respected), and a **receipt** (`ExecutionReceipt` is always produced — successful or not).

## Must-read files

- `docs/superpowers/specs/2026-04-19-runtime-adapters-phase1-design.md` — the Phase 1 spec; self-contained with all closed decisions (Dn.m) and post-approval adjustments (An.m).
- `docs/rules.md` — 16 hard rules R1..R16.
- `docs/domain-invariants.md` — I1..I23.
- `docs/architecture.md` — dependency diagram and request flow.
- `docs/adr/` — all accepted ADRs.

## Core design principles

- **D1.1** — `runtime-adapters` is a governable execution layer, not a wrapper collection; it owns the boundary between decision and execution.
- **D1.2** — The repo does NOT decide, route, orchestrate, or evaluate policy. Those responsibilities belong upstream (governance/orchestrator).
- **D1.3** — Every outcome is normalized; no adapter invents its own error shape; `unknown` status is never emitted.
- **D1.4** — `ExecutionReceipt` is the central auditable artifact of the runtime. Every completed or aborted execution produces one.
- **D1.5** — Retryability is a signal, not a decision. The runtime emits a `RetryHint` (`retryable` / `non_retryable` / `unknown`); the caller decides whether and how to retry.
- **D1.6** — Phase 1 contract = 4 concrete adapters (shell, git, filesystem, http); Phase 2 adds locks, mailbox, reservations.
- **D3.5** — Rigid separation: raw adapter outcome → `ResultNormalizer` → `ExecutionResult`; raw outcome never crosses the domain boundary.
- **D6.4** — Idempotency is replay-everything: even cached failures are replayed; retrying a failure requires a new key.
- **A4.3** — Persistence-before-return: the receipt is persisted before the caller gets a response; a persistence failure marks the runtime call failed even if the physical side effect occurred.
- **A9.1** — Fast-reject on overload: `MaxConcurrentExecutions` semaphore with no queue; excess requests receive HTTP 503 immediately.

## Before-coding checklist

- [ ] I read the spec section relevant to this task.
- [ ] I know which invariant(s) this change touches (cite in commit body).
- [ ] I have a test plan (unit + contract + integration as applicable).
- [ ] I know the commit message prefix (`feat` / `fix` / `chore` / `ci` / `docs` / `test`).
- [ ] I know whether an ADR is required (see "How to add a capability" and "How to add an adapter" in `AGENTS.md`).

## Tech stack

- Language: Go 1.26+ (toolchain pinned to `go1.26.2`)
- DB: PostgreSQL 15+ via `pgx/v5`
- HTTP router: `chi/v5`
- Git library: `go-git/v5` — no hooks executed (A8.1)
- Observability: OpenTelemetry (traces + metrics)
- Testing: `testify` + `testcontainers-go`
- Lint: `golangci-lint`

## Output style

Conventional commits (`feat(scope)`, `fix(scope)`, `chore(scope)`, `docs(scope)`, `test(scope)`). Never `Co-Authored-By` or any AI attribution. Scope = layer or adapter name (`shell`, `git`, `filesystem`, `http`, `domain`, `ports`, `application`, `bootstrap`, `ci`, `docs`). Terse technical prose in docs; one sentence per principle; no filler.

## Never-do list

1. **R1** — Runtime does not decide. Route, policy, and capability selection belong upstream.
2. **R2** — Receipts do not mutate. Once persisted, an `ExecutionReceipt` is append-only.
3. **R3** — Port contracts stable. Required-field signatures freeze at Phase 1; removals require ADR.
4. **R4** — Adapters do not panic. Recovered panics → `AdapterInternalError`; the runtime must stay up.
5. **R5** — Raw outcome stays in adapter. `AdapterRawOutcome` never crosses into domain or application.
6. **R6** — Runtime does not retry. `retry_hint` is a signal for the consumer, not an internal mechanism.
7. **R7** — Persistence-before-return. Return to caller only after the receipt is persisted.
8. **R8** — Envelope before semantics. Validate the request envelope before dispatching to the adapter.
9. **R9** — No dynamic adapters. Adapter registration is compile-time only; no runtime plugin loading.
10. **R10** — Config rejected by default. Unknown configuration keys are rejected at startup; no silent fallback.
11. **R11** — Permanent-out-of-scope contract. Items in §11.4 of the spec never move into runtime-adapters without a formal ADR with default-rejected stance.
12. **R12** — Test determinism. `Clock` and `IDGenerator` must be injectable; no direct `time.Now()` in domain/application.
13. **R13** — Receipts always. Every completed or aborted execution produces an `ExecutionReceipt`, no exceptions.
14. **R14** — `MaxConcurrentExecutions` required. A semaphore cap must be configured and enforced at runtime startup.
15. **R15** — Only 5 statuses. `success`, `failure`, `timeout`, `cancelled`, `partial` are the only valid values; adding a new status requires an ADR and a `schema_version` bump.
16. **R16** — Metric cardinality bounded. Label whitelist: `capability`, `adapter`, `status`, `signal`. High-cardinality identifiers (error_class, receipt_id, handle_id, correlation_id, trace_id, retry_hint) go to logs / exemplars, not metrics.
