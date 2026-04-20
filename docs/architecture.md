# Architecture — runtime-adapters

## Overview

`runtime-adapters` follows hexagonal (clean) architecture. There is one bounded context — `execution` — and one aggregate root — `ExecutionReceipt`. Inbound adapters (HTTP, SDK) depend on inbound port interfaces; application services depend on outbound port interfaces; concrete outbound adapters (shell, git, filesystem, httpreq) and PostgreSQL implementations depend on nothing inside the domain. The dependency arrow always points inward, toward the domain.

## Dependency diagram

```
┌─────────────────────────────────────────────────────────────────┐
│ adapters/inbound  (http, sdk)                                   │
│                       │                                         │
│                       ▼                                         │
│ ports/inbound  (RuntimeService, QueryService)                   │
│                       │                                         │
│                       ▼                                         │
│ application/services (ExecuteService, QueryServiceImpl)         │
│                       │                                         │
│                       ▼                                         │
│ ports/outbound  (Adapter, ReceiptRepository, IdempotencyStore)  │
│                       │                                         │
│       ┌───────────────┼───────────────┐                         │
│       ▼               ▼               ▼                         │
│ adapters/outbound   pg               (future)                   │
│ (shell, git,       (receipts,                                   │
│  filesystem,        idempotency)                                │
│  httpreq)                                                       │
└─────────────────────────────────────────────────────────────────┘

        domain/  (execution entities, VOs, ResultNormalizer, shared)
        ▲     ▲     ▲                         ▲
        │     │     │                         │
        └─────┴─────┴─ consumed by everything above
```

## Composition rule

`internal/bootstrap/wire.go` is the **only** file that imports concrete adapter and infrastructure implementations. Domain and application packages never import adapters or infrastructure. Violation of this rule is a build-time architecture error — `golangci-lint` enforces import direction.

## Request flow (UC1 — Execute, 11 steps)

Defined in spec §6.2. Each step maps to a distinct responsibility.

1. **Envelope validation** — required fields, regex constraints, payload size + JSON validity. Failure → `ValidationFailure` + structural receipt.
2. **Idempotency lookup** — if `idempotency_key` present and within window, return cached receipt (including failures — replay-everything per D6.4).
3. **Capability resolution** — resolve `(adapter_id, capability_name, version)` from registry. Miss → `CapabilityUnknown` + receipt.
4. **Timeout resolution** — effective timeout = `min(req.timeout_budget_ms, capability.default_timeout, MaxTimeoutBudget)`. Overflow → `ValidationFailure`.
5. **Handle generation** — new ULID `handle_id` + `correlation_id` + `started_at`.
6. **Context setup** — `context.WithTimeout` + cancel + OTel span with adapter/capability/handle attrs.
7. **`adapter.Execute(ctx, capability, payload)`** — returns `AdapterRawOutcome`; Go `error` only for structural failures (panic-recovered, invalid ctx).
8. **Normalize** — `ResultNormalizer.Normalize(capability, rawOutcome)` → `ExecutionResult`. Deterministic, no I/O.
9. **Assemble `ExecutionReceipt`** — bundles request snapshot, handle, result, provenance, timings.
10. **Persist** — insert-only; on failure, runtime call is marked failed per A4.3 (physical side effect may have occurred; caller receives `AdapterInternalError`).
11. **Return `(receipt, nil)`** to caller.

## Invariants enforced at each layer

| Layer | Invariants |
|---|---|
| Domain | I1–I20 — construction validation, closed enums, `schema_version: "v1"` |
| Application | I13 (persistence-before-return), I21 (no retry), I22 (`MaxConcurrentExecutions` semaphore) |
| Adapters | R4 (no panics), R5 (raw outcome never leaves adapter package), never emit `unknown` status |

Full invariant definitions: `docs/domain-invariants.md` (I1..I22).
Full rules: `docs/rules.md` (R1..R15).
