# Changelog

All notable changes to `runtime-adapters` will be documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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

[0.1.0]: https://github.com/RVRTelecomunicaciones/sophia-runtime-adapters/releases/tag/v0.1.0
