# Load baseline (Phase 2C.2)

This document describes how runtime-adapters' SLO targets in
`ops/slo/*.yaml` are calibrated from real measurements. It is the
operator-facing counterpart to the design spec
(`docs/superpowers/specs/2026-04-23-phase-2c-load-baseline-design.md`).

## When to run a calibration

- After material performance-affecting code changes land on main.
- When the measurement envelope changes (host hardware, Docker bump,
  compose limits adjustment).
- When CI smoke surfaces a 🔴 regression flag that the operator wants
  to investigate under the full pinned envelope.

## Prerequisites

| Tool | Version | Pin file |
|---|---|---|
| Docker + docker compose v2 | current | — |
| k6 | 0.58.0 | `ops/load/.k6-version` |
| otel-collector-contrib | 0.106.1 | `ops/otel-collector/.collector-version` |
| promtool (via container) | 3.11.2 | `ops/prometheus/.prometheus-version` |
| `jq` + `bash` + `curl` | ubiquitous on host | — |
| `wget` | only inside containers (nginx / k6 alpine / collector contrib) — not needed on host | — |

## Workflow

```bash
# 1. Regenerate git fixtures (deterministic).
make fixture-git-bench

# 2. Full baseline run (~42–45 min).
make load-baseline
```

`load-baseline` does everything end-to-end:

1. Builds the runtime image (`docker build -t runtime-adapters:ci-test .`).
2. Brings up the 7-service pinned compose and waits for health.
3. Runs `verify-limits.sh` to assert cgroup limits are actually applied.
4. Runs `suite.js` via the k6 container (writes `summary.json` to the
   mounted `summaries` volume).
5. Runs `generate-report.sh` which produces:
   - `ops/slo/calibration-reports/YYYY-MM-DD-baseline-v<N>.md`
   - `ops/slo/calibration-reports/evidence/<date>-baseline-v<N>/` with
     `manifest.json`, `summary.json`, per-capability PromQL outputs,
     `git-status-smoke-split.json`, `git-rough-observations.json`
   - `ops/slo/calibration-reports/latest-baseline.json` (CI smoke ref)
6. Tears down the compose (`down -v`).

## Manual review

After `make load-baseline` completes, open the generated report and:

1. For each **core capability** (shell / fs.read / fs.write / http):
   - Review the observed p50 / p95 / p99 against the current
     PROVISIONAL target in `ops/slo/<adapter>.yaml`.
   - Decide: TIGHTEN / LOOSEN / KEEP — fill in the tables in the
     report with the proposed target + justification.
   - Edit `ops/slo/<adapter>.yaml` with the new target.

2. For **git.status@v1** (smoke tier): the report prompts for
   smoke-calibrated targets with `limited` confidence. Decide whether
   you trust the smoke profile enough to tighten the PROVISIONAL
   target; mark `Decision=SMOKE_CALIBRATED`.

3. For **git.clone / diff / commit** (rough tier): document the
   observed ranges in the report. Do NOT edit the YAMLs — leave
   `Decision=ROUGH_NO_CHANGE`. (Exception: if `git.clone` rough
   scenario returned errors because the adapter rejected `file://`,
   mark it `SKIPPED_IN_2C2` instead.)

4. Fill in the `Summary — SLO changes proposed` table at the bottom
   with all decisions + confidence levels.

## Commit

```bash
git add ops/slo/calibration-reports/YYYY-MM-DD-baseline-v<N>.md \
        ops/slo/calibration-reports/latest-baseline.json \
        ops/slo/calibration-reports/evidence/ \
        ops/slo/<adapter>.yaml              # recalibrated YAMLs
git commit -m "feat(slo): calibrate v<N> — envelope 2 CPU / 2 GiB

Report: ops/slo/calibration-reports/YYYY-MM-DD-baseline-v<N>.md"
```

## CI smoke (advisory)

Every PR runs a reduced version of this under
`.github/workflows/ci.yaml#load-smoke`:

- Core 4 only, ~10% RPS, ~2m30s total.
- Runs on GHA `ubuntu-latest` with a stripped 4-service compose
  (`ops/local/compose.ci-smoke.yaml`).
- Output posted as PR comment with delta vs `latest-baseline.json`.
- **Never fails CI** (`continue-on-error: true`). Noisy runner by
  design.

Flag thresholds:

| Flag | Meaning |
|---|---|
| 🟢 | delta ≤ 20% of baseline p99 |
| 🟡 | 20% < delta ≤ 50% |
| 🔴 | delta > 50% — likely real regression, investigate |

## Calibration artifacts location

`ops/slo/calibration-reports/` is the single source of truth
(D2C2.16). No symlinks from `docs/`.

## Troubleshooting

- **`verify-limits.sh` fails** — compose didn't apply cgroup limits.
  Check `ops/local/compose.yaml` for the `cpus:` and `mem_limit:`
  service-level keys (they go directly on the service, NOT under
  `deploy.resources`).
- **k6 can't reach runtime-adapters** — `http://runtime-adapters:8080`
  only resolves inside the compose network. k6 scenarios use
  `__ENV.RUNTIME_URL` with that default; running k6 outside the
  compose requires exporting a different `RUNTIME_URL`.
- **Collector config drift** — `ops/otel-collector/scripts/validate-config.sh`
  uses the same pinned container tag as compose. Version bumps need
  both `.collector-version` and the compose image tag updated.

## Upgrade path (2C.3 / 2C.4)

The compose at `ops/local/compose.yaml` evolves in later tracks:

- **2C.3** adds Alertmanager + Tempo containers, + programmatic E2E
  pipeline tests, + (optionally) soak scenarios if a leak symptom
  appears.
- **2C.4** closes the operational local stack: runbooks, real pager
  receivers, pgx pool Prometheus collector (unblocks `PoolIdleZero`),
  Loki. At that point `ops/local/compose.yaml` may split into a
  baseline-focused and operational-focused pair.

## References

- Spec: `docs/superpowers/specs/2026-04-23-phase-2c-load-baseline-design.md`
- Plan: `docs/superpowers/plans/2026-04-23-phase-2c.2-load-baseline.md`
- ADR 0007 — k6 + pull-based pipeline
- ADR 0008 — pinned compose envelope
- SLO framework (from 2C.1): `docs/slo.md`
- Metric contract (from 2C.1): `docs/metrics.md`
