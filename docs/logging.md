# Logger contract — runtime-adapters

Canonical reference for the `internal/infrastructure/obs/log`
package. See spec §5 for the authoritative source.

## Principle

Every log emitted within the runtime execution flow carries a **fixed
set of fields** derived from `ExecutionRequest` and
`ExecutionReceipt`. No sprintf. No payload bodies. No `request_id`
as a runtime-level core field (chi's HTTP-layer `X-Request-Id`
stays inside the inbound HTTP adapter; execution logs use
`correlation_id` + `handle_id` for correlation).

## Contract fields

| Field | Presence |
|---|---|
| `capability` | always (Step 3+) |
| `adapter` | always (Step 3+) |
| `correlation_id` | always (Step 0+, sourced from `X-Correlation-Id` or generated ULID) |
| `handle_id` | always (Step 5+) |
| `status` | always (post-normalize) |
| `receipt_id` | always (post-assembly) |
| `duration_ms` | always (final emit) |
| `trace_id` | when span active |
| `error_class` | when `status != success` |

## Level mapping

`LevelFor(status, errClass)` in
`internal/infrastructure/obs/log/levels.go` — spec §5.4:

- `success`, `cancelled` → INFO
- `timeout`, `partial` → WARN
- `failure` + caller-fault errClass (`ValidationFailure`,
  `CapabilityUnknown`, `PayloadSchemaFailure`,
  `PreconditionFailure`) → INFO
- `failure` + transient errClass (`ExternalFailure`, `Interrupted`)
  → WARN
- `failure` + invariant-violation errClass (`AdapterInternalError`,
  `NormalizationFailure`) → ERROR
- Defensive default for unknown status → ERROR

**Orthogonal emissions** (not tied to `ExecutionReceipt` outcome)
are always ERROR:

- `"persist receipt"` failure (A4.3 violation at Step 9)
- `"panic recovered"` from `SafeExecute` or HTTP middleware
- `"migrate failed on startup"` (bootstrap aborts)
- `"otel exporter unhealthy"` — WARN (degradation, runtime keeps running)

## Configuration

Env vars (R10 strict validation — unknown values reject at startup):

| Var | Values | Default |
|---|---|---|
| `RUNTIME_LOG_FORMAT` | `text` \| `json` | `text` |
| `RUNTIME_LOG_LEVEL` | `debug` \| `info` \| `warn` \| `error` | `info` |
| `RUNTIME_LOG_ADD_SOURCE` | `false` \| `true` | `false` |

Production manifests (2C.4) force `RUNTIME_LOG_FORMAT=json` via env.

## Context propagation

- `log.ContextWith(ctx, *Logger)` binds a logger to ctx.
- `log.FromContext(ctx)` retrieves it — **never returns nil** (returns
  a Nop logger if missing, for nil ctx, or for a type-assertion miss).
  Callers always write `log.FromContext(ctx).Info(ctx, ...)` without
  nil guards.

HTTP middleware chain registers `chimw.RequestID` →
`LoggerMiddleware` → `requestIDHeader` → `panicRecoverer` — so
recovered panics always have a logger in ctx.
