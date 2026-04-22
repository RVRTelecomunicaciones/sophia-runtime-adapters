# Metric contract — runtime-adapters

Authoritative catalog of instruments emitted by `runtime-adapters`.
Source of truth: `internal/infrastructure/obs/metrics.go`
(`InstrumentCatalog`). This file documents the public-facing surface
for Prometheus + Grafana.

## Naming

OTel-native in Go, Prometheus-translated automatically by the SDK:
`runtime_adapters.execution.total` → `runtime_adapters_execution_total`.
The prefix `runtime_adapters_` is mandatory and prevents collision
when the collector shares scrape with other Sophia services.

## Catalog

| Name | Type | Unit | Labels | Purpose |
|---|---|---|---|---|
| `runtime_adapters.execution.total` | counter | `{executions}` | `capability`, `status` | availability SLI |
| `runtime_adapters.execution.duration` | histogram | `s` | `capability` | latency SLI, **success-only** |
| `runtime_adapters.execution.active` | updowncounter | `{executions}` | `capability` | in-flight saturation |
| `runtime_adapters.concurrency.rejects` | counter | `{rejects}` | — | A9.1 fast-reject rate |
| `runtime_adapters.adapter.panics` | counter | `{panics}` | `adapter` | R4 panic signal |
| `runtime_adapters.receipt.persist.failures` | counter | `{failures}` | — | A4.3 violation signal |
| `runtime_adapters.idempotency.replays` | counter | `{replays}` | `capability` | D6.4 replay hit rate |
| `runtime_adapters.pool.connections.acquired.duration` | histogram | `s` | — | pgx pool saturation proxy |
| `runtime_adapters.otel.exporter.queue.size` | gauge | `{items}` | `signal` | collector health |
| `runtime_adapters.migrate.failures` | counter | `{failures}` | — | bootstrap health signal |
| `runtime_adapters.partial.signal` | counter | `{partials}` | `capability` | secondary degradation signal |

## Histogram buckets

`execution.duration` and `pool.connections.acquired.duration` use
explicit bucket boundaries: `[0.1, 0.25, 0.5, 1, 2, 3, 5, 10, 30, 60]`
seconds. Bucket boundaries MUST cover every `le="<value>"` threshold
referenced in `ops/slo/*.yaml`. Enforced by
`TestDurationBuckets_CoverSlothThresholds` in
`internal/infrastructure/obs/metrics_test.go`.

## Label discipline (R16)

**Permitted:** `capability`, `adapter`, `status`, `signal`.

**Prohibited** (high-cardinality or unbounded — belong in logs / exemplars):
`error_class`, `receipt_id`, `handle_id`, `correlation_id`, `trace_id`,
`retry_hint`.

Enforced at test-time by `TestMetricContract_LabelBlacklist` and
`TestMetricContract_LabelWhitelist`.

## Exemplars

`execution.duration` observations attach `{trace_id, receipt_id}` as
exemplars when a span is active. `trace_id` is mandatory when the
span exists; `receipt_id` is best-effort (§6.5).

## Stability contract

- **Rename an instrument** → breaking. ADR + `runtime_adapters_metrics_schema_version` bump.
- **Add a new instrument** → additive. No ADR required.
- **Remove an instrument** → ADR + deprecation period (one release with warning).
- **Add a label respecting the whitelist** → additive, no ADR.
- **Add a label in the blacklist** → rejected in CI.
- **Change unit or type** → breaking. ADR required.

See `docs/superpowers/specs/2026-04-21-phase-2c-observability-slos-design.md`
§6 for the authoritative source.
