# ops/local — measurement / calibration environment for Phase 2C.2

This directory contains the docker-compose stacks used to **measure**
runtime-adapters under load and to **calibrate** the SLO targets in
`ops/slo/*.yaml`. It is **NOT** the full operational local stack —
that closure is Phase 2C.4, which will add Alertmanager + Tempo +
real pager/ticket receivers + runbooks.

## Files

| File | Purpose |
|---|---|
| `compose.yaml` | 7-service baseline env with cgroup-pinned limits (2 CPU / 2 GiB runtime). Used by `make load-up` + `make load-baseline`. |
| `compose.ci-smoke.yaml` | 4-service reduced env for GitHub Actions advisory smoke. Runtime runs with `OTEL_ENABLED=false`. Never published to host. |
| `prometheus.yaml` | Scrape config used by both composes' Prometheus service. Scrapes the collector (pull-based, ADR 0007). |
| `mock/default.conf` | nginx static-response config for `http-upstream-mock`. Deterministic ~1 KiB payload, no reflection. |

## Quick start (baseline measurement)

```bash
make fixture-git-bench       # construct git working-tree fixtures (Bundle 4)
make load-up                 # compose up -d --wait
make load-baseline           # runs suite.js + generate-report.sh
make load-down               # compose down -v
```

See `docs/load-baseline.md` for the full workflow including report review.

## Why two compose files

Calibration (`compose.yaml`) uses the full pull-based metrics pipeline
(runtime → collector → Prometheus scrape) so histograms survive the
path intact. CI smoke (`compose.ci-smoke.yaml`) does not care about
Prometheus — it compares k6's own `handleSummary()` output against
the committed `latest-baseline.json` snapshot. Two compose files =
two clear envelopes, two clear purposes.

## Operational path

This compose will evolve in Phase 2C.4 to include Alertmanager +
Tempo + real receivers. Until then, alerts fire into the Alertmanager
that 2C.1 Bundle 6 defined but there is no active receiver path — by
design. Dashboards load and are visually inspected, but not asserted
programmatically; E2E programmatic assertions are Phase 2C.3.
