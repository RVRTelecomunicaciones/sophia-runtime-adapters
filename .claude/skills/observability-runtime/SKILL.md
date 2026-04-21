---
name: observability-runtime
description: OTel spans/metrics conventions, W3C traceparent propagation, sampler via env.
triggers:
  - "internal/infrastructure/observability/**/*.go"
  - "internal/adapters/inbound/http/**/*.go"
  - "internal/application/services/**/*.go"
---

# observability-runtime

**When this skill applies**: Writing or reviewing tracing, metrics, or logging instrumentation across any layer of the runtime.

## Rules

- **OTel opt-in via `OTEL_ENABLED`** (§9.7): OpenTelemetry is disabled by default. When `OTEL_ENABLED=false`, all spans are no-ops (use the no-op tracer provider). Do not create real exporters unless the flag is true.
- **`OTEL_EXPORTER_OTLP_ENDPOINT` required when enabled** (§9.7): If `OTEL_ENABLED=true` and the endpoint is empty, fail startup with a clear error — do not silently drop telemetry.
- **W3C `traceparent` propagation** (§9.6): Extract `traceparent` from inbound HTTP headers at the router middleware layer using the W3C TraceContext propagator. Propagate the span context through `context.Context` to all downstream calls including adapter execution.
- **Sampler via env — default `parentbased_traceidratio 0.1`** (§9.6): Configure the sampler from `OTEL_TRACES_SAMPLER` / `OTEL_TRACES_SAMPLER_ARG`. Default: parent-based with 10% root sampling. Never hardcode a sampler.
- **Registry with 11 named instruments per §9.7**: Define these instruments once in `internal/infrastructure/observability/metrics.go`:
  - `runtime.execute.duration` (histogram)
  - `runtime.execute.attempted` (counter)
  - `runtime.execute.timeout` (counter)
  - `runtime.execute.cancelled` (counter)
  - `runtime.execute.panics` (counter)
  - `runtime.idempotency.hit` (counter)
  - `runtime.idempotency.miss` (counter)
  - `runtime.concurrency.rejected` (counter)
  - `runtime.concurrency.active` (up-down counter)
  - `runtime.http.request.duration` (histogram)
  - `runtime.http.request.count` (counter)
- **Metrics emitted on every execute** (§9.7): `runtime.execute.duration` and `runtime.execute.attempted` are always recorded, even for idempotency hits. Record `runtime.idempotency.hit` before returning the cached receipt.
- **Span names follow `adapter.capability` convention** (§9.6): Span name = `"execute {adapter_id}.{capability_name}"`. Attach `correlation_id`, `adapter_id`, `capability_name`, `status` as span attributes.

## Anti-patterns

- **Real exporter when `OTEL_ENABLED=false`**: Creates background goroutines and network connections unconditionally; breaks no-telemetry deployments.
- **Hardcoded sampler**: Makes it impossible for operators to tune sampling without redeployment.
- **Missing `traceparent` extraction**: Breaks distributed trace correlation when the runtime is called by an OTel-instrumented caller.
- **Omitting metrics on idempotency hit**: Callers need to distinguish "executed" from "deduplicated" — both paths must emit counters.
- **Inventing instrument names**: Deviating from the 11 named instruments breaks dashboards and alerts built against the spec.
