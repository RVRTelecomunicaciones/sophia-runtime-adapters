---
name: execution-result-normalization
description: ResultNormalizer determinism; raw outcome never crosses the domain boundary.
triggers:
  - "internal/application/normalization/**/*.go"
  - "internal/application/services/**/*.go"
---

# execution-result-normalization

**When this skill applies**: Writing or reviewing `ResultNormalizer`, adapter normalizer closures, or any code that converts `AdapterRawOutcome` to `ExecutionResult`.

## Rules

- **`ResultNormalizer` is a pure dispatcher** (D3.5): It receives an `AdapterRawOutcome` and delegates to the registered `AdapterNormalizer` closure for that `AdapterID`. No normalization logic lives in `ResultNormalizer` itself beyond dispatch and fallback.
- **Adapters register `AdapterNormalizer` closures at bootstrap** (D7.9): Each adapter provides a closure (registered in `wire.go`) that converts its own raw outcome to `ExecutionResult`. Closures are registered once; never called at `init()`.
- **Raw outcome never crosses into domain or application** (D3.5 / R5): `AdapterRawOutcome` is consumed entirely inside the normalization layer. No domain entity, application service, or port interface references `AdapterRawOutcome` directly.
- **5-status closed enum** (R15): `ExecutionStatus` has exactly 5 values: `Success`, `Failed`, `PartialSuccess`, `Cancelled`, `TimedOut`. Every normalization path must produce one of these — no "unknown" status.
- **10-ErrorClass enum** (D4.5): `ErrorClass` has exactly 10 values. When no specific class applies, use `Unknown` (not a new value). Adding a class requires ADR + version bump.
- **Default retry hint mapping must be deterministic** (§5.9): Given the same raw outcome, the normalizer must always produce the same `RetryHint`. No randomness, no time-dependent branching.
- **Fallback normalizer for unregistered adapters** (D3.5): If no `AdapterNormalizer` is registered for an `AdapterID`, use a strict fallback: `ExecutionStatus=Failed`, `ErrorClass=Unknown`, `RetryHint=DoNotRetry`. Never panic on missing registration.
- **Normalizer closures are pure functions** (D3.5): No I/O, no state mutation, no side effects inside an `AdapterNormalizer` closure. Input → output only.

## Anti-patterns

- **Normalization logic in `ExecutionService`**: The service calls the normalizer — it does not contain status-mapping switch statements itself.
- **`AdapterRawOutcome` as a parameter on a domain method**: Raw outcomes are an infrastructure concept; domain methods work with `ExecutionResult` only.
- **Non-deterministic retry hint**: Using `rand` or wall-clock time inside a normalizer closure breaks reproducibility and makes tests flaky.
- **Panicking on unregistered adapter**: A missing registration is an operator misconfiguration, not a programmer error — return a failed result, do not crash.
- **Adding a new status inline**: `ExecutionStatus=Queued` (for example) is not in the closed enum; adding it without ADR silently breaks consumers that switch exhaustively.
