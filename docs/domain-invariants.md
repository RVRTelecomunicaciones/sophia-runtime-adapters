# Domain Invariants — runtime-adapters Phase 1

These invariants are enforced at domain / application boundaries. Every code change that touches a receipt, a closed enum, or a request field MUST cite the invariant it preserves or deliberately updates in the commit body.

---

### I1 — `ReceiptID` is ULID and unique

`NewReceiptID` rejects any string that is not a 26-char Crockford-ULID.

**Enforced at:** `internal/domain/shared/ids.go`.
**Source:** D4.1.

---

### I2 — Receipts are append-only

`ReceiptRepository.Save` is insert-only; duplicate `receipt_id` returns `ErrReceiptAlreadyExists`.

**Enforced at:** outbound port + PG implementation.
**Source:** D7.5.

---

### I3 — `ExecutionStatus` ∈ {success, failure, timeout, cancelled, partial}

Closed enum; extension requires ADR + `schema_version` bump.

**Enforced at:** domain VO.
**Source:** D4.3, R15.

---

### I4 — `RetryHint` ∈ {retryable, non_retryable, unknown}

Closed enum.

**Enforced at:** domain VO.
**Source:** D4.4.

---

### I5 — `ErrorClass` is the 10-value closed enum

`ValidationFailure`, `CapabilityUnknown`, `PayloadSchemaFailure`, `PreconditionFailure`, `ExternalFailure`, `Timeout`, `Cancelled`, `Interrupted`, `NormalizationFailure`, `AdapterInternalError`. Each has a deterministic default `RetryHint` per §5.9.

**Enforced at:** domain VO.
**Source:** D4.5, §4.6, §5.9.

---

### I6 — `partial` ⇔ 4-AND rule

An outcome is `partial` iff: capability `AllowsPartial` AND at least one durable artifact AND at least one declared step did not complete AND the adapter did not revert. Any other case → `failure` or `success`.

**Enforced at:** `PartialClassification.Classify()`.
**Source:** D4.6, §4.5.

---

### I7 — `success` ⇒ no `error_class` / `error_message`

`NewExecutionResult` rejects `StatusSuccess` with any non-empty error field.

**Enforced at:** `entities.ExecutionResult`.
**Source:** §5.9.

---

### I8 — `shell.exec` never produces artifacts

Its normalizer emits an empty artifact slice by construction.

**Enforced at:** `internal/adapters/outbound/shell/normalize.go`.
**Source:** D4.8, §1.2 responsibilities table.

---

### I9 — Payload is JSON only and ≤ `MaxPayloadBytes`

`NewPayload` rejects other content types, empty data, non-valid JSON, or oversize data.

**Enforced at:** `internal/domain/execution/valueobjects/payload.go`.
**Source:** D5.5.

---

### I10 — `TimeoutBudget` > 0 and ≤ `MaxTimeoutBudget`

Effective timeout resolves to `min(req.timeout, cap.default, MaxTimeoutBudget)`; zero or negative rejects as `ValidationFailure`.

**Enforced at:** VO constructor + `ExecuteService`.
**Source:** §5.6, A4.1.

---

### I11 — `Capability` canonical = `adapter.name@vX`

`Canonical()` is the stable identifier used throughout normalizer registration and JSON serialization.

**Enforced at:** `valueobjects.Capability`.
**Source:** D3.7.

---

### I12 — `AdapterID` matches `^[a-z][a-z0-9_]{1,31}$`

Construction via `NewAdapterID` is the only valid path.

**Enforced at:** `valueobjects.AdapterID`.
**Source:** D3.6.

---

### I13 — Receipt is persisted before caller gets a response

`ExecuteService.Execute` calls `ReceiptRepository.Save` before returning; persistence failure returns `(zero, error)`.

**Enforced at:** `internal/application/services/execute_service.go`.
**Source:** A4.3, D6.3.

---

### I14 — `ExecutionRequest` is immutable post-construction

No setters exist; all fields are unexported and accessed via getters.

**Enforced at:** `entities.ExecutionRequest`.
**Source:** D4.1.

---

### I15 — `Timings` fields are monotonic

`TimingsBuilder.Build()` rejects out-of-order timestamps (submitted ≤ started ≤ completed; persisted ≥ completed if set).

**Enforced at:** `entities.Timings`.
**Source:** §5.11.

---

### I16 — `StreamRef` inline ≤ `InlineStreamLimit` else `truncated` flag set

`NewStreamRef` handles the cut point and sets `truncated_at_byte`.

**Enforced at:** `entities.StreamRef`.
**Source:** D4.9.

---

### I17 — `Artifact.attributes` = small scalar strings, total ≤ 2 KiB

`NewArtifact` sums the byte length of attribute values and rejects overflow.

**Enforced at:** `entities.Artifact`.
**Source:** D5.13.

---

### I18 — `adapter_meta` = small scalar strings, total ≤ 8 KiB

`NewExecutionResult` validates the size of the provided map.

**Enforced at:** `entities.ExecutionResult`.
**Source:** A4.2.

---

### I19 — `Provenance.source` ∈ {governance, sdk, http, cli, test}

Closed enum.

**Enforced at:** `entities.Provenance`.
**Source:** D5.10.

---

### I20 — `schema_version` = "v1" in Phase 1

Every `NewExecutionReceipt` stamps `"v1"`. Bumping requires ADR + migration plan.

**Enforced at:** `entities.ExecutionReceipt`.
**Source:** D4.11 (see also I3 for the status-coupling rationale).

---

### I21 — Runtime does not retry internally

`ExecuteService` never re-invokes adapter.Execute for any reason (timeout, panic, error). The caller gets the receipt and decides.

**Enforced at:** `internal/application/services/execute_service.go`.
**Source:** D9.3, R6.

---

### I22 — `MaxConcurrentExecutions` enforced by semaphore without queue

`ConcurrencyLimiter.TryAcquire()` is non-blocking; exceeding slots returns `ErrTooManyExecutions` immediately (HTTP 503 downstream).

**Enforced at:** `internal/application/services/concurrency.go`.
**Source:** A9.1, D9.5.

---

### I23 — Metric contract is code-defined

Instrument names, types, units, and label sets live in
`internal/infrastructure/obs/metrics.go` (`InstrumentCatalog`).
Operational components (collector, scraper, relabeling rules) MUST
NOT rename, filter, or drop instruments declared there. Any
rename or drop in the code contract requires an ADR and a
`runtime_adapters_metrics_schema_version` bump.

**Enforced at:** `internal/infrastructure/obs/metrics.go` + CI
contract tests (`TestMetricContract_*`).
**Source:** §4.3 + §12.2 (spec). Added in Phase 2C.1.
