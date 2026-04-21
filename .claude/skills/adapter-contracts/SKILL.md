---
name: adapter-contracts
description: Uniform `outbound.Adapter` port compliance and `AdapterContractTestSuite` discipline.
triggers:
  - "internal/adapters/outbound/**/*.go"
  - "internal/ports/outbound/**/*.go"
---

# adapter-contracts

**When this skill applies**: Writing or reviewing any outbound adapter implementation or its contract tests.

## Rules

- **`Execute` returns `AdapterRawOutcome`, Go error only for structural failures** (D7.4): A Go `error` return means the adapter could not attempt execution (context cancelled, misconfiguration, dial failure). Business outcomes — including command failures, permission denied, file not found — are encoded in `AdapterRawOutcome.Status` and `AdapterRawOutcome.ErrorClass`. Never return a Go error for a business failure.
- **Raw outcome stays inside the adapter boundary** (R5 / D3.5): `AdapterRawOutcome` is consumed by `ResultNormalizer` and converted to `ExecutionResult`. The raw struct must not appear in domain or application code.
- **No panics in adapter code** (R4): Every adapter must recover from panics at its `Execute` entry point and convert them to a Go error or a failed `AdapterRawOutcome`. Unrecovered panics propagate through the semaphore and corrupt concurrency accounting.
- **Implement `AdapterContractTestSuite`** (D7.4): Every adapter must embed and pass the shared contract test suite from `internal/ports/outbound/testdoubles/`. The suite validates `Execute` signature, context propagation, and structural error classification.
- **`CapabilityDescriptor` must be accurate** (D4.1): Each adapter exposes its capabilities via `Capabilities() []CapabilityDescriptor`. `AllowsPartial`, `DefaultTimeout`, and `SchemaVersion` must match the spec for that capability.
- **No shared mutable state across `Execute` calls** (I10): Adapters may hold configuration (allowlists, HTTP clients) but must be safe for concurrent `Execute` calls. No per-call mutable struct fields.
- **Register at bootstrap, not self-register** (D7.9): Adapters do not call any global registry or `init()` hook. They are passed to the registry in `wire.go` only.

## Anti-patterns

- **Returning Go error for command-not-found or non-zero exit**: These are business failures; encode them in `AdapterRawOutcome` with the appropriate `ErrorClass`.
- **Leaking `AdapterRawOutcome` into application layer**: If application code switches on raw outcome fields, the normalization boundary has been broken.
- **Panic in Execute without recovery**: A panicking adapter kills the goroutine and leaves the semaphore acquired — use `defer recover()` at the top of `Execute`.
- **Skipping the contract test suite**: An adapter that does not run the shared suite can pass unit tests while violating the port contract.
- **Self-registering adapters via `init()`**: Creates hidden coupling and order-of-init bugs; composition happens at bootstrap only.
