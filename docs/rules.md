# Hard Rules — runtime-adapters Phase 1

These 15 rules are absolute constraints. Every code review, every ADR, and every sub-agent prompt MUST check compliance. A rule may only be relaxed by a formal ADR with a default-rejected stance.

---

## R1 — Runtime does not decide

The runtime selects no adapter, evaluates no policy, and makes no routing decision. It receives a typed `ExecutionRequest` with a resolved capability and dispatches to the registered adapter.

**Rationale:** D1.2.

---

## R2 — Receipts do not mutate

Once an `ExecutionReceipt` is persisted, it is append-only. No field is overwritten. Corrections happen via new receipts, never in-place edits.

**Rationale:** D7.5, I2.

---

## R3 — Port contracts stable

Public port signatures (`RuntimeService`, `QueryService`, `Adapter`, `ReceiptRepository`, `IdempotencyStore`) freeze at Phase 1. Additions are allowed in Phase 2; removals and semantic changes require an ADR.

**Rationale:** D2.9, D7.8.

---

## R4 — Adapters do not panic

Adapters must return an `AdapterRawOutcome` for every termination path. Recovered panics are converted to `AdapterInternalError` by the runtime; the runtime process stays up.

**Rationale:** D9.6.

---

## R5 — Raw outcome stays in adapter

`AdapterRawOutcome` types are defined inside adapter packages and never cross the domain boundary. The normalizer dispatches by canonical capability key.

**Rationale:** D3.5, D8.6.

---

## R6 — Runtime does not retry

The runtime emits `RetryHint` (`retryable` / `non_retryable` / `unknown`) but never automatically re-invokes an adapter. The caller is responsible for retry policy.

**Rationale:** D9.3.

---

## R7 — Persistence-before-return

`ExecuteService` persists the receipt to the repository before returning to the caller. Persistence failure surfaces as `AdapterInternalError`; the physical side effect may have occurred, but the service-level outcome is failed.

**Rationale:** A4.3, D6.3.

---

## R8 — Envelope before semantics

Envelope validation (required fields, size caps, canonical capability lookup) runs before the adapter sees the payload. Adapter-level schema validation happens inside the adapter.

**Rationale:** D3.10 (capability registry), §6.2 step 1.

---

## R9 — No dynamic adapters

Adapters register at compile time via `RegisterAllPhase1`. There is no runtime plugin loader, no dynamic capability registration.

**Rationale:** D6.6.

---

## R10 — Config rejected by default

Unknown configuration keys cause startup failure; no silent fallback. Decoders use `DisallowUnknownFields` at the inbound JSON boundary.

**Rationale:** D5.14.

---

## R11 — Permanent-out-of-scope contract

Items listed in §11.4 (task routing, policy, workflow coordination, etc.) never move into `runtime-adapters` without a formal ADR with a default-rejected stance.

**Rationale:** A11.1.

---

## R12 — Test determinism

`Clock` and `IDGenerator` are injected everywhere time or identifiers are produced. No direct `time.Now()` in domain or application code (only in `RealClock`). Tests run with `-race`.

**Rationale:** D7.7, D10.8.

---

## R13 — Receipts always

Every completed or aborted execution produces an `ExecutionReceipt` — no exceptions. Pre-adapter failures (envelope, capability unknown, payload schema) also produce structural receipts.

**Rationale:** D1.4, D4.1, §6.2.

---

## R14 — `MaxConcurrentExecutions` required

A semaphore cap must be configured at startup. Excess requests receive a fast reject (HTTP 503, `ErrTooManyExecutions`); no queue.

**Rationale:** A9.1, D9.5.

---

## R15 — Only 5 statuses

`success`, `failure`, `timeout`, `cancelled`, `partial` are the only valid `ExecutionStatus` values. Adding a new status requires an ADR and a `schema_version` bump.

**Rationale:** D4.3, I3.
