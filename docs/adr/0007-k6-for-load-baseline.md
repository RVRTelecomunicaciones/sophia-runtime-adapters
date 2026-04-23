# ADR 0007 — k6 for load baseline + pull-based metrics pipeline

## Status

Accepted — 2026-04-23.

## Context

Phase 2C.1 shipped the observability layer with **provisional** SLO
targets. Every spec file under `ops/slo/*.yaml` carries a
PROVISIONAL header naming 2C.2 as the track that replaces those
numbers with measured ones.

Phase 2C.2 must therefore:

1. Generate load against the HTTP surface of `runtime-adapters`.
2. Capture distributions (p50/p95/p99) + throughput + saturation
   breakpoints under a declared resource envelope.
3. Feed those numbers back into `ops/slo/*.yaml` via a review-driven
   manual flow with an auditable report.

Two core decisions drive this ADR: **which tool** generates the
load, and **how the runtime's metrics reach Prometheus** during
measurement.

## Decision — Load tool: k6 (primary) + opt-in Go bench (SDK only)

We adopt **Grafana k6 0.58.0** as the primary HTTP load tool.
Scenarios live under `ops/load/scenarios/*.js`. Shared helpers
(correlation-id generation, payload builders) live in
`ops/load/lib/common.js`.

The SDK peer of runtime-adapters is NOT a 2C.2 deliverable under
load. If a concrete question about SDK performance surfaces later,
it will be answered with `go test -bench` ad-hoc — not with k6, and
not as a gating artifact of any track.

### k6 alternatives considered

- **vegeta** — Go CLI, simpler setup, but poorer native Prometheus
  integration and weaker scenario DSL.
- **hey / bombardier** — lightweight, HTTP-only, no scenario model.
  Fine for smoke, not for tiered/multi-scenario calibration.
- **Custom Go harness** — full control, reuses internal packages,
  but burns track time on tooling construction. Rejected as scope
  creep: 2C.2 is a calibration track, not a tooling-building track.

k6 wins because (a) it is the SRE 2026 standard, (b) Grafana Labs
alignment fits our existing dashboard tooling family, (c) JavaScript
scenarios are review-friendly, (d) GitHub Actions integration is
first-class, (e) `handleSummary(data)` produces a structured
machine-readable summary we can feed into report generation without
parsing stdout.

## Decision — Metrics pipeline: pull-based, reject `prometheusremotewrite`

Runtime metrics flow via:

```
runtime-adapters → OTLP gRPC → otel-collector (Prometheus exporter :8889)
                                      ↑
                             Prometheus scrapes every 5s
```

We deliberately **reject** the `prometheusremotewrite` exporter in
the collector. Its OTel histogram translation has recurring problems:

- Delta/cumulative mismatches between OTel-native histograms and
  Prometheus-format histograms during translation.
- Exemplar support varies by exporter version; attribute preservation
  is fragile.
- Timestamp alignment with the rest of the scrape interval is not
  guaranteed.

For a track whose output is **numerical calibration of SLO targets
that depend on `execution.duration` bucket shape**, any of these
drift classes would invalidate the measurements silently. The
pull-based approach is a known-clean translation path: the
collector's `prometheus` exporter emits standard OpenMetrics format
with exemplars enabled, and Prometheus scrapes it like any other
target.

Pull-based is also closer to how the team expects to deploy in
production (every future 2C.4 envelope will involve a scrape-based
monitoring setup anyway), so the calibration env matches production
shape for free.

## Consequences

- k6 becomes a pinned tool in the repo (`ops/load/.k6-version`
  → 0.58.0), following the same pattern as sloth / promtool / amtool /
  dashboard-linter from 2C.1.
- The collector is a mandatory part of the baseline compose — not
  optional. Its config is validated in CI against the same pinned
  container tag that runs during baseline (D2C2.15).
- SDK performance is deliberately out of scope for 2C.2. If a
  concrete question arises later, `go test -bench` is the escape
  hatch, not k6.
- Adding a new capability to the Phase 1 catalog in the future
  requires (a) a Sloth spec update, (b) a new k6 scenario under
  `ops/load/scenarios/`. The scenario layout (one file per
  capability) keeps that incremental.

## References

- Spec: `docs/superpowers/specs/2026-04-23-phase-2c-load-baseline-design.md`
  §3 (approach) + §3.1 (why pull-based) + §7 (metrics pipeline).
- Plan: `docs/superpowers/plans/2026-04-23-phase-2c.2-load-baseline.md`
  Bundle 2 lands compose + collector config.
- Pin files: `ops/load/.k6-version` (tool),
  `ops/otel-collector/.collector-version` (image tag).
