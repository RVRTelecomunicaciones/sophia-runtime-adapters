---
name: execution-modeling
description: ExecutionRequest/Result/Receipt modeling integrity and lifecycle invariants.
triggers:
  - "internal/domain/execution/entities/**/*.go"
  - "internal/domain/execution/valueobjects/**/*.go"
---

# execution-modeling

**When this skill applies**: Writing or modifying domain execution entities (`ExecutionRequest`, `ExecutionResult`, `ExecutionReceipt`, `Provenance`, `Artifact`) or their value objects.

## Rules

- **Immutability after construction** (I14): Once built via `NewExecutionRequest(...)`, an `ExecutionRequest` must not be mutated. All fields are unexported; expose only getter methods. Same rule applies to `ExecutionReceipt`.
- **Closed enums — no extension without ADR** (R15): `ExecutionStatus` (5 values), `ErrorClass` (10 values), and `RetryHint` (3 values) are closed. Adding a value requires an ADR and a `schema_version` bump.
- **`schema_version = "v1"` on all persisted entities** (I20): `ExecutionReceipt` must carry `schema_version: "v1"` in its JSON representation. Readers must validate this field before deserializing.
- **Persistence before return** (R7 / A4.3): `ExecutionService` persists the receipt to the repository before returning the result to the caller. No result is handed back without a durable receipt.
- **Receipts are append-only** (R2 / I2): A receipt row is written once. No UPDATE on receipt rows — terminal state is final. The idempotency store may update a lock row, but the receipt itself is immutable after INSERT.
- **Artifacts are small scalars** (I17 / D5.13): Total artifact payload ≤ 2 KiB; values are scalar types only (string, number, bool). No binary blobs or nested objects as artifact values.
- **`CorrelationID` must be a valid ULID** (I1): Validate at `NewExecutionRequest`; reject with `ErrorClass=InvalidInput` if malformed.
- **`TimeoutBudget` bounded by cap** (A4.1): Effective timeout = `min(request.TimeoutBudget, capability.DefaultTimeout, MaxTimeoutBudget)`. Enforce in application layer, not domain.
- **Provenance tracks origin** (§5.10 / D5.10 / I19): `Provenance` carries `AdapterID`, `CapabilityName`, `CapabilityVersion`, and `ExecutedAt`. Never omit from a receipt.

## Anti-patterns

- **Mutable entity fields**: Exporting fields or providing setters on `ExecutionRequest` — immutability is a domain invariant, not a style preference.
- **Skipping schema_version check**: Deserializing a receipt without checking `schema_version` first silently corrupts domain state when schemas evolve.
- **Extending enums inline without ADR**: Adding a new `ExecutionStatus` value without an ADR and version bump breaks existing consumers silently.
- **Returning result before persist**: Any code path that returns `ExecutionResult` to the caller before the receipt is durable violates R7 and creates audit gaps.
- **Large artifact payloads**: Storing binary content, base64 blobs, or deeply nested objects in artifacts — use a dedicated storage adapter instead.
