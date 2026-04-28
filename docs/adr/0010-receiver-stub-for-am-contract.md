# ADR 0010 — Receiver stub for the Alertmanager webhook contract

## Status

Accepted — 2026-04-26.

## Context

Phase 2C.3 needs to assert that the alerting pipeline actually delivers
a critical alert to the configured webhook receiver within a tiered
timing budget (D2C3.21 — 60s target / 90s deadline) AND that
inhibition rules suppress duplicate warning pages for the same
`(sloth_slo, capability)` (D2C1.17). The assertion runs in CI on every
PR (canary, single profile) and nightly (comprehensive, all profiles
plus inhibition).

The available shapes for "what receives the webhook in CI" are:

- **a)** Mock the webhook at the Alertmanager HTTP boundary using
  `nginx -L /dev/stdout` or a `curl --output`-style logging proxy.
- **b)** Use a third-party receiver (e.g., Slack-bot stub, PagerDuty
  test endpoint) that the test then queries.
- **c)** Configure Alertmanager with a webhook target that points at a
  small purpose-built HTTP service we maintain in this repo
  (`ops/chaos/receiver/`), running in the chaos compose overlay.

The test must be able to:

1. Verify a webhook actually arrives — not just that AM's internal
   state shows a fired alert.
2. Inspect the webhook BODY — Alertmanager's POST shape with the
   labels block — so the test can assert on `(alertname,
   sloth_slo, capability, severity)` as delivered, not as Prometheus
   evaluated.
3. Filter by timestamp so post-critical inhibition checks
   (`AlertsMatchingSince`) do not race the early-ramp warning fires.
4. Be reset between scenarios so the comprehensive nightly's six
   subtests do not contaminate each other.
5. Run inside the same compose network as Alertmanager so test wiring
   uses the production AM webhook config shape — no test-only forks.

## Decision

**Adopt option (c): a small purpose-built HTTP receiver-stub in
`ops/chaos/receiver/` that the chaos compose overlay runs at port
8088. The stub records every Alertmanager webhook POST in a ring
buffer and exposes `GET /inspect`, `GET /inspect?since=<rfc3339>`, and
`POST /clear`.**

- Source: `ops/chaos/receiver/main.go`. Built into a small image via
  `ops/chaos/receiver/Dockerfile` and assembled by the chaos compose
  overlay (`ops/local/compose.chaos.yaml`).
- The chaos `alertmanager.chaos.yaml` config routes ALL alerts to the
  receiver-stub via a `webhook_configs:` entry pointing at
  `http://receiver-stub:8088/`.
- Test code interacts via `test/chaos/e2e/receiver_client.go`:
  `WaitForAlert`, `AlertsMatching`, `AlertsMatchingSince`, `Clear`.
- The buffer carries a server-side `Received time.Time` per payload
  so timestamp-filtering is deterministic.

## Options considered

- **Option a (nginx + log)** — rejected. Logs are unstructured;
  parsing them inside the Go test is fragile. There is no API to
  filter by timestamp or clear between scenarios. AM's webhook contract
  is JSON; reducing it to log lines loses fidelity.
- **Option b (third-party receiver)** — rejected. Each adds a network
  dependency, an authentication surface, and rate limits. Tests must
  not flake because a Slack/PD endpoint slowed down. Test fixtures
  should not require a real external account.
- **Option c (purpose-built receiver-stub)** — adopted. Owns the data
  shape, runs inside the chaos compose network, has an explicit query
  API, supports a deterministic clear operation, and is reusable
  between the per-PR canary and the nightly comprehensive without
  modification. Stub source is small (~150 LOC of Go), reviewed in
  the same PR (B5).

## Consequences

- One additional service in the chaos compose stack
  (`receiver-stub`). Its presence is gated by the chaos overlay; it
  is absent from the production compose path (D2C3.26 — deterministic
  file swap, no conditional rendering).
- Test code talks to a real HTTP service over a real socket — same
  shape AM uses to deliver in production. The contract under test is
  the actual one AM enforces.
- Failure mode if the receiver-stub becomes unreachable mid-test:
  `WaitForAlert`'s context times out at the GHA job level (10 min for
  canary, 30 min for nightly) instead of the 90s deadline. Acceptable
  because the receiver-stub is a tiny Go program with a healthcheck;
  in practice it does not flake.
- The `/inspect?since=t` filter is the load-bearing mechanism for the
  inhibition contract assertion (commit 7c0ec99 in B7 Task 7.1).
  Without it, warnings that legitimately fire during the critical
  ramp would false-positive the inhibition check.
- Future use: the same stub is a candidate for any future test that
  needs to introspect AM webhook deliveries — e.g. a test that asserts
  a non-chaos production-style alert reaches the right route — without
  re-introducing the patterns rejected above.

## Spec references

- §11 (receiver + compose overlay), §12 (canary), §13 (nightly + 13.3 inhibition)
- §13.4 ¶2 (auto-issue creation independent of receiver state)
- D2C1.17 (AM inhibition uses sloth_slo + capability)
- D2C3.21 (tiered timing budget)
- D2C3.26 (deterministic file swap)
- I24 — `docs/domain-invariants.md`
