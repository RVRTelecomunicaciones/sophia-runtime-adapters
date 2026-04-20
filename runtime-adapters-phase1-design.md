# runtime-adapters — Phase 1 Design Spec

**Date**: 2026-04-19
**Status**: Approved (brainstorming closed)
**Scope**: Phase 1 MVP — Execution layer for the Sophia ecosystem
**Baseline**: greenfield; peers are `agent-governance-core` (v0.6.0), `memory-engine-sophia`, future `orchestrator`
**Stack**: Go, PostgreSQL, `go-git`, OpenTelemetry, hexagonal/clean architecture
**Purpose**: Close the design of `runtime-adapters` with the same rigor used for `agent-governance-core`. This document is self-contained — it consolidates 13 sections of brainstorming, each with closed decisions (`Dn.m`) and post-approval adjustments (`An.m`). No code, no implementation plan — **only the spec**.

> ⚠️ **Authoring context**: this file was written inside the working directory of another repo (`agent-governance-core`) by operational accident. It is intended to be moved to a freshly-created `runtime-adapters` repository. Any folder paths referenced below describe the **target** layout, not the current filesystem.

---

## Table of contents

1. [Objective of the repository](#section-1--objective-of-the-repository)
2. [Consumer model](#section-2--consumer-model)
3. [Domain strategy](#section-3--domain-strategy)
4. [Entities / operational models](#section-4--entities--operational-models)
5. [Value objects](#section-5--value-objects)
6. [MVP use cases](#section-6--mvp-use-cases)
7. [Inbound and outbound ports](#section-7--inbound-and-outbound-ports)
8. [Phase 1 adapters](#section-8--phase-1-adapters)
9. [Execution / timeout / cancellation / retry hint strategy](#section-9--execution--timeout--cancellation--retry-hint-strategy)
10. [Testing strategy](#section-10--testing-strategy)
11. [Out of Phase 1](#section-11--out-of-phase-1)
12. [Repository-level docs and skills](#section-12--repository-level-docs-and-skills)
13. [SSD + Subagent-driven development preparation](#section-13--ssd--subagent-driven-development-preparation)
14. [Appendix A — Consolidated decision log](#appendix-a--consolidated-decision-log)
15. [Appendix B — Post-approval adjustments](#appendix-b--post-approval-adjustments)

---

## Section 1 — Objective of the repository

### 1.1 Core statement

`runtime-adapters` is the **governable execution layer** of the Sophia ecosystem. Its sole responsibility is to **materialize real side effects on the system** — processes, files, network, git — starting from typed requests, returning normalized and auditable results.

It is not a utility library. Not a toolkit. Not an orchestrator. It is a **boundary** separating decisions (made upstream by `agent-governance-core` and `orchestrator`) from execution (controlled here).

### 1.2 Responsibilities (in-scope, Phase 1)

| # | Responsibility |
|---|---------------|
| R1 | Accept a typed `ExecutionRequest` and validate it against the target adapter |
| R2 | Propagate `timeout_budget` and `cancellation` to the adapter throughout execution |
| R3 | Invoke the concrete adapter registered for the requested `Capability` |
| R4 | Normalize the adapter outcome to an `ExecutionResult` with explicit status (success / failure / timeout / cancelled / partial) |
| R5 | Classify failures with `retryable` / `non-retryable` as a signal — not a decision |
| R6 | Produce an auditable `ExecutionReceipt` for every completed or aborted execution |
| R7 | Expose metadata sufficient (duration, exit codes, stdout/stderr refs, bytes written, etc.) without mixing business semantics |
| R8 | Guarantee technical isolation between adapters |

### 1.3 Non-responsibilities (explicit)

The repo **NEVER**:
1. Decides which adapter to use for a task — that is `agent-governance-core` (routing).
2. Evaluates whether an execution is permitted — that is policy/approval upstream.
3. Coordinates multiple steps — that is the `orchestrator`.
4. Maintains workflow state — that lives in governance.
5. Retries automatically — runtime **marks** retryability; the consumer decides if it retries.
6. Stores operational knowledge — that is `memory-engine`.
7. Mixes technical results ("exit=1") with business semantics ("task failed because user rejected").
8. Exposes adapters that "do several things" — but an adapter **may** expose multiple well-contracted capabilities (per A1.1).
9. Hides partial failures behind a success — it classifies them explicitly.
10. Returns generic `error` without structured classification.

### 1.4 Guiding principle

> **The runtime doesn't think, it executes. But it executes with a contract, with a limit, and with a receipt.**

Three operational guarantees per execution: **contract** (the caller knows exactly what input is accepted and what output is returned), **limit** (timeout and cancellation are respected), **receipt** (an `ExecutionReceipt` is always produced — successful or not).

### 1.5 Phase 1 success criteria

Phase 1 closes when:

- A consumer (governance or test harness) can submit a typed `ExecutionRequest` and receive a normalized `ExecutionReceipt` containing the `ExecutionResult`.
- The 4 Phase 1 adapters (shell / git / filesystem / http) work under the **same contract**, without each inventing its own error shape.
- Timeout and cancellation propagate and are respected — there is a test that demonstrates this.
- Each `ExecutionResult` has an unambiguous status — never `unknown`.
- Each failure carries `retryable` / `non_retryable` as a structured signal.
- All adapters produce `ExecutionReceipt` with the same structure.
- Contracts are prepared for Phase 2+ (locks, mailbox, reservations) without breaking changes.

### 1.6 Decisions closed in §1

| # | Decision |
|---|----------|
| D1.1 | `runtime-adapters` is a governable execution layer, not a wrapper collection |
| D1.2 | The repo does **NOT** decide, route, orchestrate, or evaluate policy |
| D1.3 | Every outcome is normalized; no ambiguous errors |
| D1.4 | `ExecutionReceipt` is the central auditable artifact of the runtime |
| D1.5 | Retryability is a signal, not a decision |
| D1.6 | Phase 1 = contract + 4 concrete adapters; Phase 2 = locks + mailbox + reservations |

### 1.7 Adjustments (A1)

- **A1.1** An adapter **may** expose multiple capabilities; but each capability must have an explicit contract, validation, and normalized result.
- **A1.2** `partial` is a first-class status from Phase 1; the rule for `partial` vs `failure + metadata` is closed in §4.5.
- **A1.3** Phase 1 includes 4 adapters: `shell`, `git`, `filesystem`, `http`, with `git` scoped tight (no `push`).

---

## Section 2 — Consumer model

### 2.1 Consumer taxonomy

| Level | Consumer | Phase | Relationship |
|-------|----------|-------|--------------|
| Primary | `agent-governance-core` | 1 | Routing decides adapter + capability → emits `ExecutionRequest` → awaits `ExecutionReceipt` |
| Secondary (future) | `orchestrator` | 2+ | Coordinates sequences; emits multiple requests with `correlation_id` |
| Testing / internal | test harnesses, CLI tools, operator debug tools | 1 | Use the same contract; privileged callers but not technically distinct |
| Out-of-scope | end-user UIs, ad-hoc developer scripts, external agents | — | Do not talk to runtime directly; go through governance |

### 2.2 Transport — HTTP + SDK (D2.2)

The runtime is a standalone binary with:
- HTTP REST API (`POST /api/v1/execute`, `GET /api/v1/capabilities`, `GET /api/v1/receipts/{id}`)
- Go SDK (in-process, for co-located callers such as governance during the pilot)

Two inbound transports, one set of inbound ports. Contract tests guarantee equivalence (§10.3).

**Rejected**: gRPC/protobuf (over-ceremony for co-located pilot); embedded library only (kills distributable deployment).

### 2.3 Deployment model

- Phase 1: **standalone binary** (`cmd/runtime-adapters/`), can run alongside governance in a piloting docker-compose.
- Protocol: HTTP over localhost when co-located; HTTP over network when separated.
- State: **stateless per request**, except for persistence of `ExecutionReceipt`. A restart loses in-flight executions but preserves historical receipts. No resumption in Phase 1.

### 2.4 Execution mode — sync only exposed (D2.4)

| Mode | Phase 1 | Phase 2 |
|------|---------|---------|
| **Sync** (request blocks until complete) | ✅ default | ✅ |
| **Async with `ExecutionHandle`** | 🟡 contract internally modeled, not exposed | ✅ endpoints activated |
| **Streaming** (live stdout/stderr) | ❌ | 🟡 optional, not committed |

`ExecutionHandle` exists as a domain type in Phase 1 (generated, embedded in the receipt) but there is no `GET /executions/:handle_id` endpoint.

### 2.5 Idempotency (D2.5 + A2.2)

**Caller-owned idempotency**:
- Request carries optional `idempotency_key`.
- Runtime maintains a `(idempotency_key → ReceiptID)` map with **configurable window** (default 24h recommended).
- A second request within the window with the same key → runtime does NOT re-execute; returns the original receipt + cached result.
- **Replay-everything** (D6 closure): even cached failures are replayed. If the caller wants to retry, they must use a **new key**.

Store lives in the **same PostgreSQL** as receipts (simpler, consistent).

### 2.6 Auth / authz — none in Phase 1 (D2.6)

Phase 1, single-tenant pilot:
- No auth on HTTP (localhost-only in pilot).
- SDK in-proc has no auth surface.
- `actor_id` + `task_id` + `workflow_run_id` travel in the request as metadata for audit only — they are NOT an authz mechanism.
- Runtime does NOT evaluate permissions — governance did that before calling.

When deployment becomes shared/multi-tenant: add mTLS + API keys. Until then, **explicitly NOT apt for shared networks**.

### 2.7 Multi-tenancy — not in Phase 1 (A2.1)

- Single-tenant; no `tenant_id` in public contract of Phase 1.
- Reserved as a design space; contracts are flexible enough to accept it as an optional field in a future version without breaking change.

### 2.8 Correlation with governance

Every `ExecutionRequest` carries:

| Field | Phase 1 required | Use |
|-------|------------------|-----|
| `correlation_id` (ULID) | yes (generated by governance) | cross-repo log / trace correlation |
| `task_id` | optional | audit |
| `workflow_run_id` | optional | audit |
| `actor_id` | optional | audit |
| `idempotency_key` | optional | dedup |

The receipt mirrors the same fields — the hinge that allows audit joins between governance audit entries and runtime receipts.

### 2.9 Contract stability

Phase 1 promise: **public inbound-port contracts do NOT break in Phase 2.** Additions are allowed (new fields optional, new endpoints, new status values with default reject-on-unknown). Removals and semantic changes are not.

### 2.10 Decisions closed in §2

| # | Decision |
|---|----------|
| D2.1 | Primary consumer = governance; secondary future = orchestrator |
| D2.2 | Transport = HTTP + in-proc SDK (same pattern as governance) |
| D2.3 | Deployment = standalone binary, stateless per request except receipts |
| D2.4 | Phase 1 = sync-only exposed; `ExecutionHandle` internal, async deferred |
| D2.5 | Idempotency = caller-owned with `idempotency_key` + configurable window |
| D2.6 | Auth = none in pilot; explicitly "not apt for shared networks" |
| D2.7 | Multi-tenant = no in Phase 1; reserved in design |
| D2.8 | Correlation = `correlation_id` required, others optional |
| D2.9 | Contract stability = required-field freeze from Phase 1 |

### 2.11 Adjustments (A2)

- **A2.1** `tenant_id` NOT in public Phase 1 contract; reserved in design only.
- **A2.2** Idempotency window **configurable**, default 24h recommended.

---

## Section 3 — Domain strategy

### 3.1 Bounded context

**One bounded context**: `execution`. Ubiquitous language: `ExecutionRequest`, `ExecutionResult`, `ExecutionReceipt`, `ExecutionHandle`, `Adapter`, `Capability`, `Payload`.

NOT part of the runtime's ubiquitous language: `Task`, `Workflow`, `Policy`, `Approval`, `Routing` — these belong to governance. The runtime receives them as **opaque metadata strings** in the receipt.

Rejected: sub-contexts per adapter family. The adapter family is an implementation detail, not a context boundary.

### 3.2 Aggregate design

**Single aggregate**: `ExecutionReceipt`, with all evidence cohesively embedded.

```
ExecutionReceipt (AGGREGATE ROOT — identity: ReceiptID, immutable)
├── request: ExecutionRequest (VO — frozen input)
├── handle:  ExecutionHandle (VO — lifecycle reference)
├── result:  ExecutionResult (VO — normalized output)
├── provenance: Provenance (VO)
└── timings: Timings (VO)
```

### 3.3 Raw outcome vs normalized result — the critical separation

Every adapter produces heterogeneous native output (`shell` → stdout/stderr/exit; `git` → SHA/files; `http` → status/headers/body). If each returned its own shape, four contracts would collapse the downstream.

**Two layers rigidly separated**:
- **`AdapterRawOutcome`** — adapter-specific type, lives inside the outbound adapter package. Runtime domain does NOT know it.
- **`ExecutionResult`** — uniform domain type. Everything downstream sees this and only this.

The translator is `ResultNormalizer` (a domain service), which takes `(capability, rawOutcome)` and returns `ExecutionResult`. Deterministic, testable, no I/O.

### 3.4 Entities vs Value Objects (clarification)

In the Phase 1 folder convention, entities/ holds both the aggregate root and inmutable input/output types with identity. Semantically:

| Type | DDD role | Identity |
|------|----------|----------|
| `ExecutionReceipt` | Aggregate root | `ReceiptID` |
| `ExecutionRequest` | Value object | `correlation_id` (not identity — correlation only) |
| `ExecutionResult` | Value object | — |
| `ExecutionHandle` | Value object with id | `HandleID` (lightweight reference) |

### 3.5 Capability, AdapterID, Payload modeling

- **`AdapterID`** — string, regex `^[a-z][a-z0-9_]{1,31}$`; registered values only.
- **`Capability`** — composite `(adapter_id, name, version, allows_partial, default_timeout)`; canonical string `adapter.name@version`.
- **An adapter exposes N capabilities** (per A1.1); each with explicit contract + validation.
- **`Payload`** — envelope with `content_type: "application/json"` and `data: json.RawMessage`. Runtime validates envelope only (size, JSON validity); **adapter validates semantic schema**.

### 3.6 Anti-corruption with governance

Runtime receives governance-owned strings (`correlation_id`, `task_id`, `workflow_run_id`, `actor_id`). Runtime **does not type** these beyond `string`. No shared domain types, no imports from governance, no outbound calls runtime → governance.

### 3.7 Capability registry

In-memory `CapabilityRegistry` populated at bootstrap. Maps `(adapter_id, cap_name, version) → Adapter`. Unknown capabilities are rejected at envelope validation (`CapabilityUnknown`, `non_retryable`).

### 3.8 Decisions closed in §3

| # | Decision |
|---|----------|
| D3.1 | One bounded context `execution` |
| D3.2 | One aggregate root: `ExecutionReceipt` |
| D3.3 | `ExecutionReceipt` is the central auditable artifact |
| D3.4 | Request/Result/Handle in `entities/` by project convention; only `ExecutionReceipt` is the aggregate root |
| D3.5 | Rigid separation: raw outcome → normalizer → `ExecutionResult`; raw NEVER crosses to domain |
| D3.6 | `AdapterID` = regex-validated string |
| D3.7 | `Capability` = `(adapter_id, name, version)`, canonical `adapter.name@vX` |
| D3.8 | One adapter → N capabilities |
| D3.9 | `Payload` = `json.RawMessage` opaque; adapter validates semantics; runtime validates envelope |
| D3.10 | `CapabilityRegistry` at bootstrap; unknown capability → normalized failure |
| D3.11 | Runtime does not know governance types; they are opaque strings |
| D3.12 | `ResultNormalizer` is the sole domain service in Phase 1 |

### 3.9 Adjustments (A3)

- **A3.1** Pragmatic folder layout; VO/entity semantics annotated.
- **A3.2** `Payload` = `json.RawMessage` opaque; centralized JSON Schema deferred to Phase 2.
- **A3.3** Git Phase 1 limited to 4 capabilities: `git.status@v1`, `git.clone@v1`, `git.diff@v1`, `git.commit@v1`. No `push`.

---

## Section 4 — Entities / operational models

### 4.1 ExecutionRequest (VO)

| Field | Type | Required | Validation / purpose |
|-------|------|----------|----------------------|
| `correlation_id` | ULID | yes | cross-repo correlation |
| `adapter_id` | `AdapterID` | yes | registered at bootstrap |
| `capability_name` | string | yes | regex `^[a-z][a-z0-9_.]{1,63}$` |
| `capability_version` | string | yes | regex `^v[0-9]+$` |
| `payload` | `json.RawMessage` | yes | non-empty, ≤ `MaxPayloadBytes`, valid JSON |
| `timeout_budget_ms` | int64 | yes (runtime applies default if 0) | > 0, ≤ `MaxTimeoutBudget` |
| `idempotency_key` | string | no | ULID or UUID |
| `actor_id` / `task_id` / `workflow_run_id` | string | no | ≤ 128 chars |
| `retry_attempt` | int | no | informational; runtime does NOT use it for decisions |
| `submitted_at` | timestamp | runtime-set | UTC |

Immutable post-construction.

### 4.2 ExecutionHandle (VO with id)

| Field | Type | Required |
|-------|------|----------|
| `handle_id` | ULID | yes |
| `correlation_id` | ULID | yes |
| `adapter_id` | `AdapterID` | yes |
| `capability` | `Capability` | yes |
| `started_at` | timestamp | yes |

Not independently persisted in Phase 1; embedded in the receipt.

### 4.3 ExecutionResult (VO — normalized output)

| Field | Type | Required | Semantics |
|-------|------|----------|-----------|
| `status` | `ExecutionStatus` | yes | § 4.5 |
| `retryable` | `RetryHint` | yes | `retryable`, `non_retryable`, `unknown` |
| `error_class` | `ErrorClass` | yes if `status != success` | enum (§ 4.6) |
| `error_message` | string | yes if not success | ≤ 4 KiB, no PII (adapter responsibility) |
| `stdout_ref` | `StreamRef` | no | inline up to 16 KiB with `truncated` flag |
| `stderr_ref` | `StreamRef` | no | same |
| `exit_code` | *int | no | applicable only where relevant (shell yes, http no) |
| `artifacts` | `[]Artifact` | yes (can be empty) | durable side effects |
| `adapter_meta` | `map[string]string` | yes (can be empty) | **only small scalars** (A4.2) |
| `bytes_read` / `bytes_written` | int64 | yes | 0 if N/A |
| `duration_ms` | int64 | yes | |
| `completed_at` | timestamp | yes | |

### 4.4 ExecutionReceipt (aggregate root)

| Field | Type | Required |
|-------|------|----------|
| `receipt_id` | ULID | yes (identity) |
| `schema_version` | string | yes (`"v1"` in Phase 1) |
| `request` | `ExecutionRequest` snapshot | yes |
| `handle` | `ExecutionHandle` snapshot | yes |
| `result` | `ExecutionResult` snapshot | yes |
| `provenance` | `Provenance` | yes |
| `timings` | `Timings` | yes |
| `created_at` | timestamp | yes |
| `persisted_at` | timestamp | nullable (set by persistence) |

**Invariants**: unique external identifier, append-only, self-sufficient for audit.

### 4.5 Partial vs Failure — explicit rule (A1.2 + D4.6)

An outcome is **`partial`** if and only if **all four** conditions hold:

1. The capability has **declared multi-step semantics** (`CapabilityRegistry.AllowsPartial = true`).
2. At least **one durable side effect** was applied (`len(artifacts) ≥ 1`).
3. At least **one declared step** did not complete.
4. The adapter **could not revert** the completed steps.

Otherwise → `failure`. If adapter output is ambiguous → `failure` with `error_class = NormalizationFailure`.

### 4.6 ErrorClass (closed enum, 10 values)

`ValidationFailure`, `CapabilityUnknown`, `PayloadSchemaFailure`, `PreconditionFailure`, `ExternalFailure`, `Timeout`, `Cancelled`, `Interrupted`, `NormalizationFailure`, `AdapterInternalError`.

Default `RetryHint` mapping is deterministic (see §5.9).

### 4.7 StreamRef (Phase 1: inline with truncation) — D4.9

- Inline up to `InlineStreamLimit` (default 16 KiB per stream, configurable).
- Overflow → `mode: truncated`, `data` contains first 16 KiB, `truncated_at_byte` marks the cut.
- Phase 2 may add disk-reference hybrid without changing the type shape.

### 4.8 Artifact — durable side effect

```
Artifact { type, uri, size_bytes, checksum (optional), attributes (small scalars only) }
```

Types in Phase 1: `file`, `directory`, `git_ref`, `git_commit`, `http_response_snapshot` (opt-in).

**`shell.exec` produces no artifacts** (per design — it cannot reliably track arbitrary side effects; caller uses specific adapters for tracking).

### 4.9 Persistence-before-return invariant (A4.3)

The receipt is persisted **before** returning to the caller. If persistence fails:

- **The physical side effect may have occurred**, yet the runtime call is considered **failed**.
- The caller receives `error` of class `AdapterInternalError` with a message indicating "side effect may have been applied; persistence failed". The raw outcome is logged but not returned as a successful result.
- This separates the physical outcome from the service outcome, preserving auditability without confusing them.

### 4.10 Decisions closed in §4

| # | Decision |
|---|----------|
| D4.1 | 4 main types, all immutable post-construction |
| D4.2 | Request required fields: correlation_id + adapter_id + capability (name + version) + payload + timeout |
| D4.3 | 5 statuses (success, failure, timeout, cancelled, partial) — first-class from Phase 1 |
| D4.4 | 3 RetryHints (retryable, non_retryable, unknown) |
| D4.5 | Closed `ErrorClass` enum of 10 values with default RetryHint mapping |
| D4.6 | Explicit `partial` rule (4 AND conditions); capability declares `AllowsPartial` |
| D4.7 | Phase 1: `partial` applies only to `git.clone@v1` |
| D4.8 | `shell.exec` never emits artifacts |
| D4.9 | `StreamRef` Phase 1 = inline + truncate flag |
| D4.10 | `Artifact` = type + uri + size + optional checksum + small-scalar attributes |
| D4.11 | Receipt carries `schema_version: "v1"` for forward compat |
| D4.12 | Receipt persisted **before** returning to caller |
| D4.13 | `Provenance` and `Timings` are VOs within the receipt |

### 4.11 Adjustments (A4)

- **A4.1** `timeout_budget` max = **configurable**; 30 min as initial recommendation, not rigid.
- **A4.2** `adapter_meta` = small scalar strings only; no JSON-serialized complex values. Total ≤ 8 KiB preserved.
- **A4.3** Persistence-before-return wording explicit: physical side effect may have occurred, runtime call still marked failed if receipt cannot be persisted.

---

## Section 5 — Value objects

### 5.1 VO philosophy

- **Validated construction**: only `NewXxx()` yields a valid VO; zero-value is invalid.
- **Immutability**: no mutators/setters; unexported fields where applicable.
- **Equality by value**.
- **Stable serialization**: explicit JSON marshalers; wire format is part of the public contract.
- **No side effects** in construction (except `Timings` receives injected `Clock`).

### 5.2 Location

| VO | Path |
|----|------|
| Request/Result/Handle/Receipt VOs | `internal/domain/execution/valueobjects/` (or `entities/` per project convention — semantically clarified) |
| `ReceiptID`, `HandleID`, `CorrelationID`, `IdempotencyKey`, `Clock` | `internal/domain/shared/` |

### 5.3 `AdapterID`

- Regex `^[a-z][a-z0-9_]{1,31}$`; length 2..32; lowercase only.
- Only IDs registered at bootstrap are accepted.

### 5.4 `Capability`

`(adapterID, name, version, allowsPartial, defaultTimeout)`; canonical `adapter.name@version`.

Phase 1 registered set:

| Canonical | AllowsPartial | DefaultTimeout |
|-----------|---------------|----------------|
| `shell.exec@v1` | no | 30s |
| `git.status@v1` | no | 10s |
| `git.clone@v1` | **yes** | 120s |
| `git.diff@v1` | no | 15s |
| `git.commit@v1` | no | 15s |
| `filesystem.read_file@v1` | no | 5s |
| `filesystem.write_file@v1` | no | 10s |
| `http.request@v1` | no | 15s |

Total: **8 capabilities**.

### 5.5 `Payload`

`{ contentType: "application/json", data: json.RawMessage }`. Only `application/json` supported in Phase 1. `data` > 0 and ≤ `MaxPayloadBytes` (default 1 MiB). Runtime runs `json.Valid(data)` only.

### 5.6 `TimeoutBudget`

- Min > 0
- Max configurable (`MaxTimeoutBudget`), default 30 min (A4.1)
- Precision: millisecond
- Wire field: `timeout_budget_ms` (int64)

### 5.7 `RetryHint`

`retryable`, `non_retryable`, `unknown`. Closed enum; forward-compat rule: unknown values treated as `unknown`.

### 5.8 `ExecutionStatus`

`success`, `failure`, `timeout`, `cancelled`, `partial`. Closed enum; all Phase 1 terminal.

### 5.9 `ErrorClass` and default RetryHint mapping

| ErrorClass | Default RetryHint |
|------------|-------------------|
| `ValidationFailure` | `non_retryable` |
| `CapabilityUnknown` | `non_retryable` |
| `PayloadSchemaFailure` | `non_retryable` |
| `PreconditionFailure` | `non_retryable` |
| `ExternalFailure` | `unknown` |
| `Timeout` | `retryable` |
| `Cancelled` | `non_retryable` |
| `Interrupted` | `retryable` |
| `NormalizationFailure` | `non_retryable` |
| `AdapterInternalError` | `non_retryable` |

Consistency with `ExecutionStatus`:
- `success` ⇒ `error_class` empty
- `timeout` ⇒ `Timeout`
- `cancelled` ⇒ `Cancelled`
- `failure` / `partial` ⇒ any of the other 7

### 5.10 `Provenance`

`{ source, sourceVersion, host, runtimeVersion, invocationID (nullable) }`. `source` closed enum: `governance | sdk | http | cli | test`.

### 5.11 `Timings`

`{ submittedAt, startedAt, completedAt, persistedAt (nullable) }`. All UTC, ms precision. Builder pattern enforces order invariant.

### 5.12 `StreamRef`

`{ mode: inline|truncated, data, sizeBytes, truncatedAtByte }`. Inline up to `InlineStreamLimit` (default 16 KiB). JSON encodes `data` as base64 (Go default).

### 5.13 `Artifact` + `ArtifactType`

Types Phase 1: `file`, `directory`, `git_ref`, `git_commit`, `http_response_snapshot`. URI conventions per type (file:// for file/dir, git:// for git refs/commits, etc.). `attributes` = small scalars only, total ≤ 2 KiB.

### 5.14 IDs

`ReceiptID`, `HandleID`, `CorrelationID`, `IdempotencyKey` — nominal types (distinct `string` wrappers) to prevent cross-assignment in compile time.

### 5.15 Serialization conventions

- `snake_case` JSON field names
- Enum values as lowercase literals
- Timestamps RFC 3339 UTC ms
- Durations in `_ms` suffix as int64
- Bytes base64
- Nullable fields explicit `null`
- `DisallowUnknownFields` on input parsing

### 5.16 Decisions closed in §5

| # | Decision |
|---|----------|
| D5.1 | VOs require validated construction; zero-value invalid |
| D5.2 | IDs as distinct nominal types |
| D5.3 | Capability canonical `adapter.name@version` |
| D5.4 | Phase 1 = 8 capabilities (1 shell + 4 git + 2 fs + 1 http) |
| D5.5 | Payload only `application/json` in Phase 1 |
| D5.6 | `TimeoutBudget` max configurable, 30 min default |
| D5.7 | `RetryHint` closed enum of 3 values |
| D5.8 | `ExecutionStatus` closed enum of 5 values |
| D5.9 | `ErrorClass` closed enum of 10 values + deterministic RetryHint default |
| D5.10 | `Provenance.source` closed enum of 5 values |
| D5.11 | `Timings` via builder with temporal invariants |
| D5.12 | `StreamRef` inline + truncate; base64 on wire |
| D5.13 | `Artifact.attributes` = small scalars only |
| D5.14 | JSON conventions: snake_case, RFC 3339 UTC ms, DisallowUnknownFields |
| D5.15 | `schema_version: "v1"` for forward compat |

---

## Section 6 — MVP use cases

### 6.1 Three use cases total in Phase 1

| UC | Name | Surface |
|----|------|---------|
| UC1 | `Execute` | HTTP `POST /api/v1/execute` + SDK `Execute(ctx, req)` |
| UC2 | `ListCapabilities` | HTTP `GET /api/v1/capabilities[?adapter_id=...]` + SDK |
| UC3 | `GetReceipt` | HTTP `GET /api/v1/receipts/{id}[?include_streams=true\|false]` + SDK |

### 6.2 UC1 — Execute (11-step flow)

1. Envelope validation → on failure: `ValidationFailure` + structural receipt.
2. Idempotency lookup (if `idempotency_key` present) → on hit: return cached receipt (including failures — **replay-everything**).
3. Capability resolution → on miss: `CapabilityUnknown` + receipt.
4. Timeout resolution → `min(req.timeout, capability.default, MaxTimeoutBudget)`; overflow → `ValidationFailure`.
5. Handle generation (ULID + correlation_id + started_at).
6. Context setup (timeout + cancellation + OTel span attrs).
7. `adapter.Execute(ctx, capability, payload)` → `AdapterRawOutcome`.
8. Normalize → `ExecutionResult`.
9. Assemble `ExecutionReceipt`.
10. **Persist** (insert-only) → on failure: runtime call marked failed per A4.3.
11. Return `(receipt, nil)` to caller.

### 6.3 UC2 — ListCapabilities

Input: empty or filter `adapter_id`. Output: capability set + runtime metadata. Read-only from in-memory registry.

### 6.4 UC3 — GetReceipt

Input: `receipt_id` + `GetReceiptOptions{IncludeStreams bool}` (default `true`). `include_streams=false` strips `stdout_ref`/`stderr_ref` for lightweight audit fetches. Other errors: 400 (invalid), 404 (not found), 503 (storage unavailable).

### 6.5 Idempotency — replay-everything

`idempotency_key` within window caches **every terminal status**, not just success. Retrying a failure requires a new key.

### 6.6 Observability per use case

Per-use-case counters/histograms; OTel span per request with status/retryable/adapter/capability attributes. Details in §9.7.

### 6.7 Out of Phase 1 use cases

Not in scope for Phase 1: query-multi, cancel, async handle inspection, subscribe-events, redact/delete, bulk-execute, dynamic adapter registration.

### 6.8 Decisions closed in §6

| # | Decision |
|---|----------|
| D6.1 | 3 use cases in Phase 1: Execute, ListCapabilities, GetReceipt |
| D6.2 | UC1 = 11-step flow, persistence-before-return |
| D6.3 | Persistence failure surfaces as `AdapterInternalError` — side-effect-may-have-occurred |
| D6.4 | Idempotency = replay-everything |
| D6.5 | No cancel / query / streaming in Phase 1 |
| D6.6 | Dynamic adapter registration prohibited; compile-time only |

### 6.9 Adjustments (A6)

- `?include_streams=false` query option on UC3 for audit-lightweight fetches.

---

## Section 7 — Inbound and outbound ports

### 7.1 Inbound ports

Location: `internal/ports/inbound/`

```
RuntimeService.Execute(ctx, ExecutionRequest) (ExecutionReceipt, error)       // A7.1
QueryService.ListCapabilities(ctx, CapabilityFilter) (ListCapabilitiesResponse, error)
QueryService.GetReceipt(ctx, ReceiptID, GetReceiptOptions) (ExecutionReceipt, error)
```

**`Execute` returns `(ExecutionReceipt, error)` only** (per A7.1 — the `ExecutionResult` lives inside the receipt; returning both would be duplication and would dilute the message that the receipt is the central artifact).

### 7.2 Outbound ports (active in Phase 1)

Location: `internal/ports/outbound/`

- **`Adapter`** — `ID()`, `Capabilities()`, `Execute(ctx, cap, payload) AdapterOutcome`
- **`ReceiptRepository`** — `Save` (insert-only) + `FindByID`
- **`IdempotencyStore`** — `Lookup(key, window)` + `Record(key, receiptID, expiresAt)`

### 7.3 Reserved ports — NOT in Phase 1 code (A7.2)

`EventPublisher`, `LockManager`, `Mailbox` are **removed from Phase 1 code**. They live in this spec and in `docs/skills-roadmap.md` (or equivalent) as deferred tracks (§11). They will be created as Go interfaces only when their trigger activates.

### 7.4 Dependency inversion

```
HTTP / SDK (adapters/inbound) → RuntimeService / QueryService interfaces
                                  → Application services
                                      → Adapter / ReceiptRepository / IdempotencyStore / Clock
                                          → concrete shell / git / fs / http + pg adapters
```

`bootstrap/wire.go` is the only composition point. Domain/application never import adapters/infrastructure.

### 7.5 Stability contract

Phase 1 freezes the public signatures listed above; additions are permitted in Phase 2, removals/semantic-changes are not.

### 7.6 Structured errors

| Port | Sentinel errors |
|------|-----------------|
| `ReceiptRepository` | `ErrReceiptNotFound`, `ErrReceiptAlreadyExists` |
| `IdempotencyStore` | `ErrIdempotencyKeyNotFound`, `ErrIdempotencyKeyConflict` |
| `Adapter` | No external-failure errors; those travel in `AdapterOutcome`. Go `error` reserved for panic-recovered / invalid ctx. |

### 7.7 Decisions closed in §7

| # | Decision |
|---|----------|
| D7.1 | 2 inbound ports (`RuntimeService`, `QueryService`) |
| D7.2 | 3 outbound ports active in Phase 1 |
| D7.3 | 3 outbound ports reserved in roadmap, NOT in Phase 1 code (A7.2) |
| D7.4 | `Adapter.Execute` returns `AdapterOutcome` (marker interface); `error` only for structural failures |
| D7.5 | `ReceiptRepository.Save` insert-only; duplicate → error, never overwrite |
| D7.6 | `IdempotencyStore` is separate port (may share pg implementation) |
| D7.7 | `Clock` interface required; no direct `time.Now()` in domain/application |
| D7.8 | Phase 1 port signatures frozen as stable contract |
| D7.9 | `bootstrap/wire.go` is the only composition point |

### 7.8 Adjustments (A7)

- **A7.1** `RuntimeService.Execute` returns `(ExecutionReceipt, error)` only.
- **A7.2** `EventPublisher`, `LockManager`, `Mailbox` excluded from Phase 1 code; remain in spec as roadmap / follow-ups.

---

## Section 8 — Phase 1 adapters

### 8.1 Common pattern

Each adapter:
- Location: `internal/adapters/outbound/<adapter_name>/`
- Files: `adapter.go` (implements `Adapter` port), per-capability payload/outcome types, `normalize.go` (or central normalizer registration), tests.
- Unmarshals payload immediately with validation.
- Respects `ctx` (timeout + cancel).
- Never panics as contract.
- Produces concrete raw outcome type; central normalizer does type-switch.

### 8.2 `shell-adapter`

Capability: `shell.exec@v1`.

**Payload**: `command`, `args`, `env` (additive to minimal base), `working_dir`, `stdin`, `exit_success` (configurable default `[0]`).

**Safety**:
- No shell interpolation by default.
- Env isolation (minimal base env, additive merge).
- `AllowedCommandsPath` for non-absolute commands.
- `AllowedWorkingDirs` allowlist.
- **Not a strong sandbox** — Phase 1 offers basic operational limits only (allowlists, env min, timeout/cancel). Strong sandbox (ulimit, seccomp, cgroups) is Phase 2.

**Artifacts**: none (by design — shell cannot reliably track side effects).

### 8.3 `git-adapter` — 4 capabilities

Library: **go-git only** in Phase 1 (D8 closure). Known limitation: **hooks are NOT executed** in `git.commit@v1`.

- **`git.status@v1`** — read-only; `clean`, branch, head, entries. No artifacts.
- **`git.clone@v1`** — **`AllowsPartial=true`**. Payload: `repo_url` (https/SSH), `destination_path`, `ref`, `depth`, `auth_mode`. Auth = SSH agent or none; no embedded credentials. Artifacts: `directory`, `git_ref`, `git_commit`. Partial when fetch succeeds + checkout fails.
- **`git.diff@v1`** — read-only; unified diff up to `max_bytes` with truncate flag. No artifacts.
- **`git.commit@v1`** — stage + commit; atomic (no partial). Empty commit requires `allow_empty=true`. **No hooks in Phase 1** (go-git limitation, documented). Artifacts: `git_commit`, `git_ref`.

### 8.4 `filesystem-adapter` — 2 capabilities

Safety: path allowlist (`AllowedFilesystemRoots`), symlink resolution with escape rejection.

- **`filesystem.read_file@v1`** — read-only; truncate at `max_bytes` with flag.
- **`filesystem.write_file@v1`** — atomic (tmp + fsync + rename). `overwrite` default `false`. `create_dirs` flag creates parent dirs. Never partial. Artifact: `file`.

### 8.5 `http-adapter`

Capability: `http.request@v1`.

**Safety**:
- **Strict SSRF by default**: private IP ranges blocked; override via `HTTPAllowPrivateNetworks=true` for pilot.
- Schemes: `http`/`https` only.
- Max redirect chain = 5.
- TLS verify mandatory (no `InsecureSkipVerify` in Phase 1).
- Optional `HTTPHostAllowlist` (log-only in Phase 1 pilot).
- Max response body size configurable; default 10 MiB with truncate.

**Status mapping**: default success = 2xx; `expected_status` list overrides. 4xx → failure unknown; 5xx → failure retryable (adapter refinement on top of default `unknown`).

### 8.6 Safety matrix

| Adapter | Sandbox | Allowlist | Quotas | Creds |
|---------|---------|-----------|--------|-------|
| shell | minimal env, working_dir allowlist | AllowedCommandsPath | timeout | — |
| git | — | AllowedWorkingDirs for destination | depth, timeout | SSH agent only; no embedded |
| filesystem | path allowlist + symlink resolve | AllowedFilesystemRoots | max_bytes read/write | — |
| http | — | SSRF private-IP block by default; host allowlist optional | body size, redirect chain | — |

### 8.7 Observability per adapter

`runtime.adapter.<id>.execute_duration_ms` histogram, `execute_total{capability, status}` counter, `bytes_read_total`, `bytes_written_total`. OTel spans with adapter-specific attributes.

### 8.8 Decisions closed in §8

| # | Decision |
|---|----------|
| D8.1 | 4 adapters, 8 capabilities total in Phase 1 |
| D8.2 | `shell.exec` = no shell by default; minimal env; working_dir allowlist |
| D8.3 | `git.clone` = only `AllowsPartial=true`; auth via SSH agent only |
| D8.4 | `filesystem.write_file` atomic tmp+rename; no partial |
| D8.5 | `http.request` blocks private IPs by default (SSRF strict); TLS verify mandatory |
| D8.6 | Each adapter concrete raw outcome type; central normalizer type-switch |
| D8.7 | `git.commit@v1` = go-git only in Phase 1; no hooks (documented limitation) |
| D8.8 | Allowlist config at `internal/infrastructure/config/`, bootstrap-loaded |

### 8.9 Adjustments (A8)

- `git.commit@v1` = **go-git only**; hooks documented as explicit Phase 1 limitation.
- `filesystem.write_file.create_dirs` = flag, no separate `mkdir` capability.
- SSRF = **strict by default** with `HTTPAllowPrivateNetworks=true` operational override.
- `shell.exec.exit_success` = configurable list, default `[0]`.
- **Precision note**: `shell` in Phase 1 is NOT a strong sandbox — only basic operational limits. `git.commit@v1` runs **without hooks** as consequence of go-git-only.

---

## Section 9 — Execution / timeout / cancellation / retry hint strategy

### 9.1 `context.Context` as the sole mechanism

Timeout, cancellation, and metadata propagation all travel through Go's `context.Context`. Runtime does NOT invent parallel mechanisms.

### 9.2 Timeout semantics

- Effective timeout = `min(req.timeout_budget_ms, caller_ctx_deadline, MaxTimeoutBudget)`.
- When exceeded: `status=timeout`, `error_class=Timeout`, `retryable=retryable` (default). Adapter invoked cleanup; receipt assembled and persisted.

### 9.3 Cancellation semantics

- **Caller cancels ctx** → `status=cancelled`, `non_retryable`.
- **Runtime shutdown** → same as caller cancel.
- **Budget expired** → `status=timeout` (distinguished from cancel by which deadline fired).

Adapter MUST clean up resources on cancel. Cleanup failures are recorded in `adapter_meta` but do not change outcome class.

### 9.4 Retry hint policy

**Runtime NEVER retries in Phase 1.** `retry_hint` is a signal:
- `retryable`: same input could yield different outcome
- `non_retryable`: same input yields same outcome
- `unknown`: insufficient info

Adapter may override default via raw outcome; normalizer prefers override.

### 9.5 Concurrency and isolation

- **N concurrent executions supported**, bounded by **`MaxConcurrentExecutions` semaphore** (A9.1). No queue — excess requests get HTTP 503 / structured error immediately.
- Adapters MUST be thread-safe and stateless in Phase 1.
- No goroutines escape the request scope.
- Panics in adapter recovered via middleware → `AdapterInternalError`, runtime does not crash. Stack trace to `adapter_meta["panic.stack"]` (truncated 4 KiB) and ERROR log.

### 9.6 Graceful shutdown

On SIGTERM/SIGINT:
1. Stop accepting new requests (HTTP 503).
2. Wait up to `ShutdownGracePeriod` (default **30s**, per D9 closure — aggressive cancel if longer capabilities are in flight).
3. Cancel remaining ctxs; adapters clean up.
4. Persist receipts of cancelled executions.
5. Flush OTel, close DB pool.
6. Exit 0 if clean; 1 if any receipt failed to persist.

During grace period, UC2 and UC3 remain available (read-only).

### 9.7 Observability (OTel)

- Span per execute with attrs (adapter_id, capability_canonical, handle_id, correlation_id, status, retryable).
- Sub-spans: envelope validation, idempotency lookup, adapter invoke, normalize, persist.
- W3C traceparent propagation.
- Default sampler: **`parentbased_traceidratio` ratio 0.1** (Phase 1, honoring the lesson from governance 3.5.B).
- Sampler configurable via `OTEL_TRACES_SAMPLER` + `OTEL_TRACES_SAMPLER_ARG` env vars (with code reading them honors the standard).
- Metrics per §9.7 spec (counters: execute_attempted_total, timeout_total, cancelled_total, panics_recovered_total, idempotency hit/miss; histograms: execute_duration_ms, persist_duration_ms; gauges: bytes_read_total, bytes_written_total).

### 9.8 Configuration summary

| Key | Default | Purpose |
|-----|---------|---------|
| `MaxTimeoutBudget` | 30 min | timeout cap |
| `MaxPayloadBytes` | 1 MiB | envelope cap |
| `InlineStreamLimit` | 16 KiB | stdout/stderr inline threshold |
| `IdempotencyWindow` | 24h | dedup window default |
| `ShutdownGracePeriod` | 30s | drain time on SIGTERM |
| `MaxConcurrentExecutions` | 64 (recommended) | semaphore cap (A9.1) |
| `AllowedCommandsPath` | `/usr/bin:/bin` | shell adapter lookup |
| `AllowedWorkingDirs` | `$HOME` | shell/git working_dir allowlist |
| `AllowedFilesystemRoots` | `$HOME` | filesystem adapter allowlist |
| `HTTPAllowPrivateNetworks` | false | SSRF override |
| `HTTPHostAllowlist` | empty (log-only) | optional host allowlist |
| `OTEL_ENABLED` | false | OTel activation |
| `OTEL_TRACES_SAMPLER` | `parentbased_traceidratio` | sampling strategy |
| `OTEL_TRACES_SAMPLER_ARG` | 0.1 | sampling ratio |

### 9.9 Decisions closed in §9

| # | Decision |
|---|----------|
| D9.1 | `context.Context` is the sole timeout + cancel mechanism |
| D9.2 | Effective timeout = `min(req.budget, caller_deadline, MaxTimeoutBudget)` |
| D9.3 | Runtime NEVER retries; retry_hint is signal only |
| D9.4 | `cancelled` vs `timeout` distinguished by which deadline fires |
| D9.5 | `MaxConcurrentExecutions` semaphore without queue; HTTP 503 on excess |
| D9.6 | Panic in adapter recovered → `AdapterInternalError`; runtime stays up |
| D9.7 | Graceful shutdown with 30s grace period + aggressive cancel |
| D9.8 | OTel sampling via env vars; default 10% |
| D9.9 | Phase 1 adapters thread-safe and stateless |
| D9.10 | Panic in adapter = bug, not crash |

### 9.10 Adjustments (A9)

- **A9.1** Concurrency = **`MaxConcurrentExecutions` semaphore, no queue**. Exceeding requests reject fast with HTTP 503 / structured error.

---

## Section 10 — Testing strategy

### 10.1 Philosophy

Standard pyramid: many unit tests, fewer contract/integration/e2e. Zero mocks in the domain; mocks only on outbound ports. Integration tests use real resources (pg via testcontainers, fs via `t.TempDir`, git repos on-the-fly, http via `httptest.Server`).

### 10.2 Layers

- **Unit** — domain VOs (table-driven, 100% validation coverage), `ResultNormalizer` (table per adapter×condition×expected), application `ExecuteService` with doubles.
- **Contract** — HTTP ≡ SDK equivalence; `AdapterContractTestSuite` applied to all 4 adapters.
- **Integration** — shell against real `echo`/`sleep`/`sh -c`; git against on-the-fly local repos (no network); filesystem against `t.TempDir`; http against `httptest.Server`; persistence via testcontainers.
- **E2E** — 3 smoke scenarios (happy path, git clone partial, idempotency replay).

### 10.3 Determinism

- `Clock` inject obligatory; no direct `time.Now()` outside `RealClock`.
- `IDGenerator` injectable for deterministic asserts where ID identity matters.
- `-race` always on.
- Fresh testcontainer per integration suite.
- httptest.Server with port 0.
- **Git repos constructed dynamically** (A10.1) — no network dependency in CI; static fixture only when a specific topology justifies it.

### 10.4 Coverage gate

- Domain + application ≥ **85%** (gate).
- Adapters outbound ≥ 75%.
- Inbound HTTP+SDK ≥ 70% combined.

### 10.5 CI (Phase 1 light)

- Unit + contract in push; < 60s.
- Integration in PR with `-tags=integration`; < 5 min.
- E2E on main post-merge; < 10 min.
- Linux amd64 only; no multi-OS matrix.
- Security scanning (gosec, govulncheck) included but non-blocking.

### 10.6 Decisions closed in §10

| # | Decision |
|---|----------|
| D10.1 | Standard test pyramid |
| D10.2 | Zero mocks in domain; doubles only for outbound ports |
| D10.3 | Real resources in integration |
| D10.4 | Contract tests guarantee HTTP ≡ SDK and Adapter contract |
| D10.5 | E2E Phase 1 = 3 smoke scenarios |
| D10.6 | Coverage gate 85% domain+application |
| D10.7 | CI light Phase 1 |
| D10.8 | Determinism via injectable Clock + IDGenerator |
| D10.9 | Performance / chaos / fuzz / security-gate = post-Phase 1 follow-ups |

### 10.7 Adjustments (A10)

- **A10.1** Git integration tests = **on-the-fly construction by default**; no network dependency. Static fixtures only for topologies that justify it.

---

## Section 11 — Out of Phase 1

### 11.1 Philosophy

Nothing is built "just in case". Every deferred item has a **trigger** for activation. Tracks are **independent**; Phase 2X prioritization is driven by real operational pull, not fixed order.

### 11.2 Phase 2 near-term (trigger-driven)

| Track | Trigger |
|-------|---------|
| **2A — Async + EventPublisher** | long-running capability degrading sync UX |
| **2B — LockManager** | real concurrent collision on shared resource |
| **2C — Hardening** (load baseline / SLOs / chaos — mirror of governance 3.5.A/B/C) | operational use with SLO requirements |
| **2D — Fuzz + JSON Schema central** | incident from malformed payload |
| **2E — Auth + multi-tenant** | shared / multi-tenant deployment |
| **2F — Git extended** (`git.push`, `git.fetch`, etc.) | CI/CD pipeline requirement |

### 11.3 Phase 3+ far-term

gRPC transport, log aggregation (Loki/ELK), multi-environment templating, GDPR-style delete/tombstone, bidirectional streaming, federated runtimes, external adapter plugin model.

### 11.4 Permanently out of scope (hard contract per A11.1)

These are **identity restrictions** of the repo — runtime-adapters NEVER does these, regardless of phase:

- Deciding which adapter to use → governance (routing)
- Policy / approval evaluation → governance
- Multi-step coordination / workflows → orchestrator
- Automatic internal retry
- Operational-knowledge storage → memory-engine
- Task lifecycle logic
- Strategy selection
- Dynamic adapter registration
- Request composition (batch, pipeline, fan-out)
- Result caching (distinct from idempotency)
- Business-data transformation (ETL)
- UI / frontend

**Any move from this list into runtime-adapters requires a formal ADR with a default-rejected stance until it is proven the functionality does not belong better in governance, orchestrator, or a new service.**

### 11.5 Decisions closed in §11

| # | Decision |
|---|----------|
| D11.1 | Explicit deferral catalogue with triggers per item |
| D11.2 | Nothing built "just in case" |
| D11.3 | `EventPublisher`/`LockManager`/`Mailbox` in roadmap, NOT Phase 1 code |
| D11.4 | 8 Phase 1 capabilities; rest explicitly deferred |
| D11.5 | Hardening tracks are follow-ups (mirror governance 3.5.A/B/C) |
| D11.6 | "Permanently out of scope" = hard contract |
| D11.7 | Phase 2X priorities = trigger-driven, no fixed order (A11.2) |

### 11.6 Adjustments (A11)

- **A11.1** Moves from "permanently out of scope" to runtime-adapters require formal ADR with default-rejected stance.
- **A11.2** No fixed priority among Phase 2X tracks; activation purely trigger-driven.

---

## Section 12 — Repository-level docs and skills

### 12.1 Documentation hierarchy

```
runtime-adapters/
├── CLAUDE.md                       # canonical entry for AI agents
├── AGENTS.md                       # agent operational directives
├── README.md                       # human-facing
├── docs/
│   ├── rules.md                    # hard rules
│   ├── domain-invariants.md        # I1..I22 per §4/§5
│   ├── architecture.md             # derived diagram + flow
│   ├── adr/                        # ADRs, sequential numbering (A12.2)
│   │   ├── 0001-<topic>.md
│   │   └── ...
│   └── superpowers/
│       ├── specs/
│       └── plans/
└── .claude/
    └── skills/
        ├── architecture-guardrails/
        ├── execution-modeling/
        ├── adapter-contracts/
        ├── shell-execution-safety/
        ├── git-file-operations/
        ├── filesystem-safety/
        ├── http-adapter-design/
        ├── resilience-timeouts-retries/
        ├── execution-result-normalization/
        ├── observability-runtime/
        └── testing-quality/
```

### 12.2 CLAUDE.md — canonical entry

Structure (see §12.2 for detail): What this repo is / is not / required mindset / must-read files / core design principles / before-coding checklist / tech stack / output style / never-do list.

### 12.3 AGENTS.md — operational directives

Workflow (brainstorm → spec → plan → exec → verify → archive), skills catalogue with when-to-apply, conventions (conventional commits, no Co-Authored-By), invariants pointer, how to add a capability, how to add an adapter (requires ADR).

### 12.4 rules.md — 15 hard rules

R1 runtime does not decide. R2 receipts do not mutate. R3 port contracts stable. R4 adapters do not panic. R5 raw outcome stays in adapter. R6 runtime does not retry. R7 persistence-before-return. R8 envelope before semantics. R9 no dynamic adapters. R10 config rejected by default. R11 permanent-out-of-scope contract. R12 test determinism. R13 receipts always. R14 MaxConcurrentExecutions required. R15 only 5 statuses; extension requires ADR + schema_version bump.

### 12.5 domain-invariants.md — I1..I22

Request/Handle/Result/Receipt/Adapter/Timeouts invariants extracted from §4/§5. Each numbered and referenceable from tests and commits.

### 12.6 Skills catalogue

11 active skills in Phase 1:

| Skill | Purpose |
|-------|---------|
| architecture-guardrails | enforces hexagonal boundaries + dependency direction |
| execution-modeling | request/result/receipt modeling integrity |
| adapter-contracts | uniform `Adapter` port compliance |
| shell-execution-safety | shell adapter safety within operational limits |
| git-file-operations | git adapter rules (no hooks, no embedded creds, go-git) |
| filesystem-safety | path allowlists, atomic writes, symlink escape |
| http-adapter-design | SSRF, TLS verify, redirect limits |
| resilience-timeouts-retries | ctx handling, timeout, cancel, retry-hint discipline |
| execution-result-normalization | `ResultNormalizer` determinism |
| observability-runtime | metrics / spans / logs conventions |
| testing-quality | pyramid compliance + determinism |

**`mailbox-locking-model` NOT created in Phase 1** (A12.1), not even as a placeholder. It is listed in the roadmap and added only when Phase 2A/B activate.

### 12.7 ADR template and required cases

Sequential numbering `NNNN-topic.md` (A12.2). Required for:
- Port signature change
- New value in a closed enum
- `schema_version` bump
- Moving something from "permanently out of scope"
- Major external dependency change
- Deployment topology change

ADR acceptance requires explicit human approval.

### 12.8 Decisions closed in §12

| # | Decision |
|---|----------|
| D12.1 | CLAUDE.md + AGENTS.md + rules.md + domain-invariants.md + architecture.md as base docs |
| D12.2 | 11 active skills in Phase 1; 1 reserved in roadmap |
| D12.3 | ADR mandatory for cross-boundary changes / enum extensions / schema_version bumps / "permanently out of scope" moves |
| D12.4 | `docs/adr/` with simple template |
| D12.5 | Invariants I1..I22 referenceable from tests/commits |
| D12.6 | Conventional commits; never Co-Authored-By |

### 12.9 Adjustments (A12)

- **A12.1** `mailbox-locking-model` skill NOT created in Phase 1, not even as empty placeholder.
- **A12.2** ADR numbering = **sequential** (`0001-...`), not by date.

---

## Section 13 — SSD + Subagent-driven development preparation

### 13.1 Six-node SSD cycle

`Brainstorm → Spec → Plan → Exec (subagent-driven default or inline on request) → Verify → Archive`

Every significant work item passes through all six. No code without approved spec + plan.

### 13.2 Brainstorming patterns inherited

- Section-by-section approval with explicit `OK`.
- Decision log per section (`Dn.m`).
- Post-approval adjustments tracked as `An.m`.
- Directed choices (2–3 options with recommendation), no open-ended questions.
- Explicit out-of-section references.

### 13.3 Spec conventions

Path: `docs/superpowers/specs/YYYY-MM-DD-<topic>-design.md`.
Header: date / status / scope / baseline / stack / purpose.
Numbered sections with decision log per section.
Consolidated decision table + adjustments at end.
Out-of-scope with triggers. Risks + mitigations. Ready-to-plan checklist.

### 13.4 Plan conventions

Path: `docs/superpowers/plans/YYYY-MM-DD-<topic>.md`.
Header: goal / architecture / stack / spec reference / prerequisites / file structure.
Tasks `T1..TN` with bite-sized steps, complete code per step, exact commands, verbatim commit messages.
Self-review checklist + bundle proposals for subagent-driven execution.

### 13.5 Subagent-driven execution patterns

Typical bundles: A (infra + harness) → B/C (domain + application) → D (docs + execution). Checkpoints between bundles. Dispatch prompts carry: context summary, paths/ports, conventions, task verbatim, injected skills, strict expected-output format.

Lessons from governance work:
- Explicit checkpoint when risk is real (timing, threshold calibration).
- D8 (critical fix) allowed when harness/measurement is broken; never for governance code.
- Fix subagents for surgical corrections.
- Harness thresholds are environment assumptions; stay conservative cross-platform.
- Bash 3.2 on macOS: never `declare -A`; always indexed variables or explicit shebang.

### 13.6 Skill injection pattern

Orchestrator resolves relevant skills per task and injects rules (as `## Project Standards (auto-resolved)`) into subagent prompts. Subagents do NOT read `SKILL.md` directly — they receive pre-digested rules. A skill↔path↔task matrix is cached once per session.

### 13.7 Engram memory

Proactive save on:
- Architectural decisions closed (`Dn.m`).
- Non-obvious bug fixes (root cause + where + learned).
- Established conventions (naming, testing, wiring).

Session close obligatory: `mem_session_summary` with Goal / Discoveries / Accomplished / Next Steps / Relevant Files.

Suggested topic keys: `milestones/<phase>-<track>`, `architecture/execution-receipt-model`, `architecture/adapter-contract`, `observability/otel-baseline`, `chaos/scenarios-matrix`.

### 13.8 ADRs in the cycle

ADRs can be suggested by brainstorming, referenced or authored by spec, required by plan before execution starts. Verify checks that referenced ADRs are `accepted`. ADR acceptance requires explicit human approval.

### 13.9 Phase 2 onwards

Each Phase 2X (A..F) starts with its **own brainstorming**, **own spec**, **own plan**. No assumption of order; activation is **trigger-driven** (A11.2).

### 13.10 Commit conventions

Conventional commits: `feat(scope)`, `fix(scope)`, `chore(scope)`, `docs(scope)`, `test(scope)`. Scope = layer or adapter (`shell`, `normalize`, `config`, `adr`). **Never Co-Authored-By**. Body for non-evident rationale.

### 13.11 Phase 1 opening blueprint

First Phase 1 execution session should: load context via `mem_context` → bootstrap repo (Go mod, Makefile, README) → domain skeleton (VOs + entities + unit tests) → ports (3 active) → application skeleton (ExecuteService with doubles) → adapters one-by-one (shell, git, filesystem, http) → persistence (pg schema + implementers) → inbound HTTP + SDK → E2E smoke → tag `v0.1.0`. Estimation: ~6–8 subagent-driven bundles.

### 13.12 Decisions closed in §13

| # | Decision |
|---|----------|
| D13.1 | Six-node SSD cycle |
| D13.2 | Spec + plan under `docs/superpowers/{specs,plans}/` |
| D13.3 | Subagent-driven default; inline on request |
| D13.4 | Skill injection via compact rules; no `SKILL.md` reads |
| D13.5 | Engram proactive saves; session summary mandatory |
| D13.6 | ADR acceptance requires explicit human approval |
| D13.7 | Phase 2X tracks arrive independent, each with its own brainstorm |
| D13.8 | Conventional commits; never Co-Authored-By |
| D13.9 | Phase 1 opening follows 10-step blueprint |

---

## Appendix A — Consolidated decision log

### Section 1
- D1.1 runtime-adapters = governable execution layer
- D1.2 No decide/route/orchestrate/policy
- D1.3 Normalized outcomes always
- D1.4 ExecutionReceipt is the central auditable artifact
- D1.5 Retryability as signal
- D1.6 Phase 1 = contract + 4 adapters

### Section 2
- D2.1 Primary consumer governance; future orchestrator
- D2.2 Transport HTTP + SDK
- D2.3 Deployment standalone binary, stateless per request except receipts
- D2.4 Sync-only exposed in Phase 1; handle internal
- D2.5 Caller-owned idempotency with configurable window
- D2.6 Auth none in pilot; not apt for shared networks
- D2.7 Multi-tenant no in Phase 1
- D2.8 correlation_id required; task/workflow/actor optional
- D2.9 Contract stability freeze

### Section 3
- D3.1 One bounded context `execution`
- D3.2 One aggregate root ExecutionReceipt
- D3.3 ExecutionReceipt = central auditable artifact
- D3.4 Request/Result/Handle in entities/ by convention
- D3.5 Rigid raw→normalizer→ExecutionResult separation
- D3.6 AdapterID regex-validated string
- D3.7 Capability composite; canonical adapter.name@vX
- D3.8 One adapter → N capabilities
- D3.9 Payload opaque in Phase 1
- D3.10 CapabilityRegistry at bootstrap
- D3.11 Governance types opaque to runtime
- D3.12 ResultNormalizer sole domain service in Phase 1

### Section 4
- D4.1 Four main types immutable
- D4.2 Request fields required/optional set
- D4.3 Five ExecutionStatus values
- D4.4 Three RetryHint values
- D4.5 Closed ErrorClass enum (10 values)
- D4.6 Explicit partial rule (4 AND conditions)
- D4.7 Phase 1 partial applies only to git.clone
- D4.8 shell.exec emits no artifacts
- D4.9 StreamRef inline + truncate in Phase 1
- D4.10 Artifact shape
- D4.11 schema_version "v1"
- D4.12 Persistence-before-return
- D4.13 Provenance + Timings as VOs inside receipt

### Section 5
- D5.1..D5.15 — VO philosophy, locations, Capability set, Payload, TimeoutBudget, RetryHint, ExecutionStatus, ErrorClass, Provenance, Timings, StreamRef, Artifact, IDs, JSON conventions, schema_version

### Section 6
- D6.1 Three Phase 1 use cases
- D6.2 UC1 11-step flow
- D6.3 Persistence failure surfaces as AdapterInternalError
- D6.4 Idempotency replay-everything
- D6.5 No cancel / query / streaming in Phase 1
- D6.6 Dynamic adapter registration prohibited

### Section 7
- D7.1 Two inbound ports
- D7.2 Three outbound ports active in Phase 1
- D7.3 Three outbound ports reserved in roadmap, NOT in code
- D7.4 Adapter.Execute returns AdapterOutcome; error only structural
- D7.5 ReceiptRepository insert-only
- D7.6 IdempotencyStore separate port
- D7.7 Clock interface required
- D7.8 Phase 1 port signatures frozen
- D7.9 bootstrap/wire.go is the only composition point

### Section 8
- D8.1..D8.8 — adapters / capabilities / safety / observability

### Section 9
- D9.1..D9.10 — ctx-based, MaxConcurrentExecutions semaphore, no-retry policy, panic recovery, graceful shutdown, OTel sampling

### Section 10
- D10.1..D10.9 — test pyramid, determinism, coverage gate, CI light

### Section 11
- D11.1..D11.7 — deferral catalogue, permanent-out-of-scope contract, trigger-driven Phase 2X priorities

### Section 12
- D12.1..D12.6 — docs hierarchy, 11 active skills, ADR rules, invariant numbering, commit conventions

### Section 13
- D13.1..D13.9 — SSD cycle, spec/plan conventions, subagent patterns, skill injection, engram usage, ADR integration, Phase 2X independence

---

## Appendix B — Post-approval adjustments

| Ref | Adjustment |
|-----|------------|
| A1.1 | Adapter may expose N capabilities; each with explicit contract + validation + normalized result |
| A1.2 | `partial` is first-class in Phase 1 |
| A1.3 | 4 adapters, `git` scoped tight (no `push`) |
| A2.1 | `tenant_id` NOT in public Phase 1 contract |
| A2.2 | Idempotency window configurable, 24h default |
| A3.1 | Pragmatic folder layout; VO/entity semantics annotated |
| A3.2 | Payload opaque JSON; central Schema deferred |
| A3.3 | Git Phase 1 = 4 capabilities (no push) |
| A4.1 | `timeout_budget` max configurable (30 min recommended) |
| A4.2 | adapter_meta = small scalars only |
| A4.3 | Persistence-before-return wording: physical side effect may have occurred, service outcome still failed |
| A7.1 | `Execute` returns `(ExecutionReceipt, error)` only |
| A7.2 | EventPublisher/LockManager/Mailbox excluded from Phase 1 code |
| A8.1 | git.commit@v1 = go-git only; no hooks in Phase 1 |
| A8.2 | filesystem.write_file.create_dirs as flag, no separate mkdir |
| A8.3 | SSRF strict by default; HTTPAllowPrivateNetworks override |
| A8.4 | shell.exec.exit_success configurable list, default [0] |
| A8.5 | Precision note: shell not a strong sandbox; git.commit without hooks |
| A9.1 | MaxConcurrentExecutions semaphore, no queue, fast 503 reject |
| A10.1 | Git integration tests = on-the-fly construction, no network dependency |
| A11.1 | Moves from "permanently out of scope" require formal ADR with default-rejected stance |
| A11.2 | Phase 2X tracks activated by trigger, no fixed priority |
| A12.1 | mailbox-locking-model skill NOT created in Phase 1 |
| A12.2 | ADR sequential numbering |

---

**End of spec. This document is ready to be moved to the `runtime-adapters` repository and used as input for the Phase 1 implementation plan.**
