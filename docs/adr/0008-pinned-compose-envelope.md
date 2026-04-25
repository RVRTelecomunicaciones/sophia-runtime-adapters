# ADR 0008 — Pinned compose envelope for calibration

## Status

Accepted — 2026-04-23.

## Context

Phase 2C.2 calibrates SLO targets in `ops/slo/*.yaml` from measured
distributions. A measurement is only meaningful when the environment
that produced it is declarable, stable, and reproducible. We need
an explicit envelope — hardware/container limits + software stack
versions — so that:

1. The numbers in the YAMLs are anchored to a known baseline.
2. A second run under the same envelope can confirm or refute drift.
3. Code changes that shift performance are caught by re-running
   against the same envelope.

The alternative — "run it on my laptop and trust the number" —
fails all three requirements: laptops thermal-throttle, other
processes compete, and cross-engineer machines diverge.

## Decision

The calibration envelope is **declared** via `ops/local/compose.yaml`
with service-level `cpus:` and `mem_limit:` keys applied through
Docker Compose v2 non-swarm cgroup enforcement. The envelope is
**verified** on every run by `ops/load/lib/verify-limits.sh`, which
asserts `docker inspect` reports the expected `NanoCpus` + `Memory`
before k6 starts. Any drift aborts the run.

### Envelope (Phase 2C.2 initial)

- `runtime-adapters`: 2.0 CPU, 2 GiB mem_limit, 1 GiB mem_reservation
- `postgres`: 1.0 CPU, 512 MiB
- `otel-collector-contrib:0.106.1`: 0.5 CPU, 256 MiB
- `prometheus:v3.11.2`: 1.0 CPU, 1 GiB
- `grafana:11.3.0`: 0.5 CPU, 512 MiB
- `nginx:1.27-alpine` (http-upstream-mock): 0.5 CPU, 256 MiB
- `grafana/k6:0.58.0`: 1.0 CPU, 512 MiB

Nominal total: ~6.5 CPU / ~5 GiB.

### Envelope-as-attribute

Every YAML target and every calibration report carries the envelope
identifier (`"2cpu-2gib-pinned"`) in:

- The report's Envelope section (machine-readable via
  `evidence/<dir>/manifest.json`).
- `latest-baseline.json`'s `runner_class_baseline` field.
- Prometheus `external_labels: envelope: "2cpu-2gib-pinned"` (so
  scraped metrics carry the envelope tag too).

### CI smoke has a different, explicit envelope

`compose.ci-smoke.yaml` uses reduced limits (0.75 CPU / 768 MiB
runtime; 0.4 CPU / 256 MiB k6) sized for the private-repo floor
(2 CPU / 8 GiB) even though this repo is actually public (4 CPU /
16 GiB `ubuntu-latest`). Conservative sizing = robust to visibility
changes + noisy co-tenants. The `latest-baseline.json` records
`runner_class_smoke_expected: "github-actions-ubuntu-latest-public-4cpu-16gib"`
so the CI smoke delta comparison knows it is **not** apples-to-apples.
Delta flag thresholds (🟢/🟡/🔴) are accordingly loose (>50% drift =
🔴, not 5%).

### Operational note: re-runs trigger on envelope or code change

- Any change to `ops/local/compose.yaml` service limits or image tags
  → re-run calibration before shipping.
- Any code change that materially affects hot-path performance
  (adapter rewrites, new instrumentation, syscall additions) →
  re-run calibration before shipping.
- Auto-increment `-v<N>` suffix in calibration report filename
  prevents in-place mutation.

## Consequences

- `docker` + `docker compose v2` become mandatory dev + CI
  prerequisites for running any calibration (they already were for
  2C.1 integration tests).
- Switching to a different envelope (e.g., running calibration on a
  dedicated VM in 2C.4) requires a new report version + updating
  `latest-baseline.json` + accepting that comparison to prior
  versions is not direct.
- The `deploy.resources.limits` block (swarm-only) is explicitly
  NOT used — it would be silently ignored by `docker compose up`.
  A2C2.5 records this.
- `prometheusremotewrite` is NOT in the metrics path (ADR 0007)
  because its histogram handling can silently corrupt calibration
  evidence.

## References

- Spec: `docs/superpowers/specs/2026-04-23-phase-2c-load-baseline-design.md`
  §6 + §6.3 envelope verification + §6.7 CI smoke envelope.
- Plan: Bundle 2 lands the compose; Bundle 7 runs the first
  calibration against it.
- `ops/load/lib/verify-limits.sh` is the pre-run enforcement.
- `latest-baseline.json` records envelope class; CI smoke compares
  against it with loose flag thresholds.
- ADR 0007 records the linked decisions on tooling + pipeline.
