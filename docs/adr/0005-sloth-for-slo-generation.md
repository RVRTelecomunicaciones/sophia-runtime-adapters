# ADR 0005 — Sloth for SLO generation

## Status

Accepted — 2026-04-21.

## Context

Phase 2C.1 requires SLO specs and burn-rate alert rules for 8 Phase 1
capabilities. Candidates evaluated during brainstorming:

- **A** — hand-write all 16 recording rules + 32 burn-rate alert rules.
- **B** — use Sloth (`slok/sloth`) for declarative generation.
- **C** — production-grade Helm + codegen + chaos hooks.

## Decision

**Hybrid: Sloth for SLOs (B), hand-written for everything else (A).**

Sloth for SLOs — one YAML spec per adapter under `ops/slo/*.yaml`;
Sloth generates recording + burn-rate alert rules at build-time.
Output committed under `ops/prometheus/generated/`.

Hand-written for: infra alerts, Alertmanager routing, Grafana
dashboards, runbooks. Team continues to own alert math outside the
SLO happy path.

C is rejected: overshoot for 2C.1 scope; Helm/Kustomize land in 2C.4
(operational readiness) when real operator demand exists.

## Consequences

- Sloth becomes a build-time dep (binary, pinned via
  `ops/slo/.sloth-version`).
- CI idempotency gate (`sloth generate` + diff against checked-in
  output) prevents drift between spec and generated rules.
- Adding a new capability = adding one YAML spec; Sloth regenerates
  recording + alert rules automatically.
- Drift between Sloth versions is a conscious upgrade: bump the pin,
  regenerate, review diff, commit.

## References

- Spec: `docs/superpowers/specs/2026-04-21-phase-2c-observability-slos-design.md` §3 + §7.
- Pin file: `ops/slo/.sloth-version`.
