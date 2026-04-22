# ADR 0006 — Metric contract invariants (R16 + I23)

## Status

Accepted — 2026-04-21.

## Context

Phase 1's 11-instrument Registry had no formal invariant protecting
it from:

1. Unbounded-cardinality labels (e.g. `error_class`, `receipt_id`)
   leaking into metrics at edit-time.
2. Collector or relabel rules redefining instrument names outside the
   code contract.

Both are recurring failure modes in production Prometheus deployments
— the first blows up cardinality budgets, the second fragments the
metric namespace across teams.

## Decision

- **R16** — Metric cardinality is bounded. Whitelist of label values
  (`capability`, `adapter`, `status`, `signal`). High-cardinality
  labels are prohibited on any declared instrument; they belong in
  logs or exemplars. CI-enforced.
- **I23** — Metric contract is code-defined.
  `internal/infrastructure/obs/metrics.go` (`InstrumentCatalog`) is
  the source of truth. Operational components may not rename,
  filter, or drop declared instruments.

## Consequences

- `error_class` moved from metric label (Phase 1 assumption) to log
  field (§5.3 + §6.4).
- Dashboards refer to exact recording-rule names read from
  `ops/prometheus/generated/*.yaml` — no naming guesswork.
- Rename or remove of an instrument = ADR + schema version bump.
- Operational components (collector, scraper, relabeling) become
  configuration-only — they cannot silently alter the contract.

## References

- Spec §6.4 + §12.
- R16 recorded in `docs/rules.md`.
- I23 recorded in `docs/domain-invariants.md`.
- CI gate: `TestMetricContract_LabelBlacklist`,
  `TestMetricContract_LabelWhitelist`,
  `TestMetricContract_CardinalityBudget`.
