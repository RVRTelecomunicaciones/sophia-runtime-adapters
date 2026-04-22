# SLO framework — runtime-adapters

## Shape

- **Per-capability SLOs** (availability + latency) — 8 capabilities ×
  2 SLO types = 16 SLOs total.
- **Sloth-generated** recording + alert rules from `ops/slo/*.yaml`
  → `ops/prometheus/generated/<adapter>.yaml` (checked in).
- **Burn rate**: 2 windows per SLO — fast (2% budget / 1h) →
  `severity: critical`, slow (5% budget / 6h) → `severity: warning`.
- **Budget horizon**: 30d rolling.
- `cancelled` and `partial` statuses are **neutral** to the
  availability SLI; only `failure` and `timeout` burn budget.
- Latency histogram is **success-only**; threshold is per-capability
  p99 under the configured duration.

## Provisional targets

Targets below are **operational hypotheses** for 2C.1. Quantitative
calibration is deferred to track 2C.2 (load baseline). Changing these
before 2C.2 closes requires explicit justification in PR.

| Capability | Availability | Latency p99 |
|---|---|---|
| `shell.exec@v1` | 99.5% | 5s |
| `git.status@v1` | 99.5% | 2s |
| `git.clone@v1` | 99.0% | 30s |
| `git.diff@v1` | 99.5% | 3s |
| `git.commit@v1` | 99.5% | 2s |
| `filesystem.read_file@v1` | 99.9% | 500ms |
| `filesystem.write_file@v1` | 99.9% | 1s |
| `http.request@v1` | 99.0% | 10s |

## Alert routing

Severity taxonomy (§9):

- **`critical`** → oncall-pager receiver (2C.4 swaps from `null-receiver`).
- **`warning`** → ops-tickets receiver (2C.4 swaps from `null-receiver`).
- **`info`** → dashboard-only, silenced at Alertmanager root.

**Inhibition**: fast-burn (`critical`) suppresses slow-burn
(`warning`) for the same `{sloth_slo, capability}` pair (D2C1.17).

Infra alerts (no `sloth_slo` label) are NOT inhibited cross-tier;
each severity is independent per §9.3.

## Testing contract

- **Sloth idempotency** (CI): regenerate into a temp dir and diff
  against `ops/prometheus/generated/` — drift fails the build.
- **Catalog coverage** (`ops/slo/slo_coverage_test.go`): every Phase 1
  capability must have both `-availability` and `-latency` SLO entries.
- **promtool** (CI): every rule file passes `check rules`; every
  `.test.yaml` fixture passes `test rules`.
- **Alertmanager config** (CI): `amtool check-config` on
  `ops/alertmanager/alertmanager.yaml`.
- **Label coverage** (`ops/alertmanager/alertmanager_routing_test.go`):
  every label referenced in matchers + `equal` is emitted by some
  upstream rule.

## References

- Spec: `docs/superpowers/specs/2026-04-21-phase-2c-observability-slos-design.md` §7.
- Version pins: `ops/slo/.sloth-version`, `ops/prometheus/.prometheus-version`,
  `ops/alertmanager/.alertmanager-version`, `ops/grafana/.dashboard-linter-version`.
