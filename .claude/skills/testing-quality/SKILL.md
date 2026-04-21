---
name: testing-quality
description: Test pyramid compliance, injectable Clock/IDGenerator, `-race` always on, coverage gate ≥ 85% domain+application.
triggers:
  - "**/*_test.go"
  - "internal/domain/**/*.go"
  - "internal/application/**/*.go"
---

# testing-quality

**When this skill applies**: Writing any test file, or reviewing production code that must be testable (injectable dependencies, deterministic behavior).

## Rules

- **Test pyramid: unit > contract > integration > E2E** (§10): Unit and contract tests are fast and have no external deps. Integration tests use `testcontainers-go` for real Postgres. E2E tests use a real runtime binary + Postgres. More tests at lower levels, fewer at higher levels.
- **Injectable `Clock` and `IDGenerator` for determinism** (D7.7 / R12): Domain and application code must receive `Clock` and `IDGenerator` as constructor parameters — never call `time.Now()` or `ulid.New()` directly. Test doubles set fixed values; production uses real implementations wired at bootstrap.
- **`-race` always on** (§10.4): Every `make test`, `make test-integration`, and CI run includes `-race`. No test suite is considered passing without the race detector. Add `-race` to all `go test` invocations in the Makefile.
- **Coverage gate ≥ 85% per package on `domain/` and `application/`** (T31): `make cover` enforces this. Adding code to these packages that drops coverage below the gate is a CI failure — write tests first or alongside.
- **Integration tests use `testcontainers-go` — no mock for DB** (§10.3): The `ReceiptRepositoryPG` and `IdempotencyStorePG` tests spin up a real ephemeral Postgres container. No in-memory fake replaces the real Postgres behavior for persistence tests.
- **E2E tests are tagged `//go:build e2e`** (§10.5): E2E tests are excluded from `make test` and `make test-integration`. They run only via `make test-e2e` which passes `-tags e2e`. Never add the `e2e` build tag to unit or integration tests.
- **Contract test suite is mandatory for every adapter** (D7.4): See `adapter-contracts` skill. Every adapter must pass `AdapterContractTestSuite` — it is part of the unit test layer, not integration.
- **Test doubles live in `internal/ports/outbound/testdoubles/`** (D7.7): Shared fakes (ClockFake, IDGeneratorFake, ReceiptRepositoryFake) live there. Adapters are never used as test doubles for domain/application tests.
- **`-count=1` to disable test caching in CI** (§10.4): Always pass `-count=1` in CI runs to get fresh results. Local dev may use caching, but the Makefile CI targets must not.

## Anti-patterns

- **`time.Now()` in domain or application code**: Makes tests time-dependent and non-deterministic. Inject `Clock` instead.
- **Mocking the database in integration tests**: A fake that doesn't enforce Postgres constraints (unique, FK, JSONB) misses the bugs that matter most in persistence code.
- **E2E test without build tag**: An E2E test running in `make test` slows the unit suite and requires Docker unconditionally.
- **Test that imports the real adapter to "simplify" setup**: Bypasses the port interface, couples the test to infrastructure, and defeats the hexagonal architecture.
- **Skipping `-race` "because it's slow"**: Race conditions in concurrent execution handling are silent until production — the detector is non-negotiable.
