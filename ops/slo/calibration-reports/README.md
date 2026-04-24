# ops/slo/calibration-reports

Canonical location for calibration artifacts produced by Phase 2C.2
(and any future re-calibrations in 2C.3 / 2C.4). Single source of
truth — `docs/load-baseline.md` references this path by relative
link (D2C2.16).

## Files

- `README.md` — this file.
- `latest-baseline.json` — machine-readable snapshot of the most
  recent calibration run's per-capability p50/p95/p99, consumed by
  CI smoke for delta comparison. Schema enforced by
  `schema_test.go` (`TestLatestBaseline_Schema`).
- `YYYY-MM-DD-baseline-v<N>.md` — Markdown reports, one per
  calibration run. Never edited after commit; re-runs produce new
  files. Schema enforced by `schema_test.go`
  (`TestCalibrationReport_Structure`).
- `schema_test.go` — Go test validating the above (build tag
  `loadreport`).
- `evidence/YYYY-MM-DD-baseline-v<N>/` — per-run evidence directory
  (manifest.json + summary.json + PromQL outputs + split/rough files).

## Cadence

Calibration is run-on-demand. Trigger a re-run when:

- Code changes land that materially affect performance
  (adapter logic, allocation patterns, new instrumentation).
- The measurement envelope changes (host hardware, Docker version
  bump, compose limits adjustment).
- A smoke regression flag is surfaced by CI (🔴 flag) that the
  operator wants to investigate.

Re-runs automatically auto-increment the `-v<N>` suffix in the
filename. The `latest-baseline.json` always points to the most recent
run — it is the only file overwritten.

## Workflow

```bash
make fixture-git-bench          # regenerate git fixtures
make load-up                    # bring up pinned compose
make load-baseline              # run suite.js + generate report
# Operator reviews evidence + hand-edits ops/slo/*.yaml with proposed
# targets + justifications in the fresh report.
git add ops/slo/calibration-reports/ ops/slo/*.yaml
git commit -m "feat(slo): calibration v<N>"
make load-down
```

## Pre-first-calibration state

Before the first calibration report is committed, `latest-baseline.json`
and `<date>-baseline-v1.md` do not exist. `schema_test.go` tolerates
this via `t.Skip` branches (D2C2.14). After the first commit, their
presence becomes mandatory on every PR.

## Links

- Spec: `docs/superpowers/specs/2026-04-23-phase-2c-load-baseline-design.md`
- Plan: `docs/superpowers/plans/2026-04-23-phase-2c.2-load-baseline.md`
- Human guide: `docs/load-baseline.md`
- Reports: `docs/superpowers/plans/2026-04-21-phase-2c.1-observability-slos.md`
  (the SLO framework that we are calibrating targets for)
