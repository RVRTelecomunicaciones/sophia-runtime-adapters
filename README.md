# runtime-adapters

> The governable execution layer of the Sophia ecosystem — materializes real side effects (processes, files, network, git) from typed requests and returns normalized, auditable results.

**Status:** Phase 1 — greenfield. The spec is approved; implementation is in progress under `feat/bundle-1-infra-harness` and subsequent feature branches.

## Documentation

- [Phase 1 Spec](docs/superpowers/specs/2026-04-19-runtime-adapters-phase1-design.md) — authoritative design with closed decisions (`Dn.m`) and adjustments (`An.m`).
- [Phase 1 Plan](docs/superpowers/plans/2026-04-19-runtime-adapters-phase1.md) — 8-bundle, 68-task implementation roadmap.
- [CLAUDE.md](CLAUDE.md) — canonical entry for AI agents.
- [AGENTS.md](AGENTS.md) — operational directives + skill catalogue.
- [Architecture](docs/architecture.md) — dependency diagram + request flow.
- [Hard rules (R1..R15)](docs/rules.md)
- [Domain invariants (I1..I22)](docs/domain-invariants.md)
- [ADRs](docs/adr/) — start with `0001-phase-1-spec-adoption.md`.

## Quick start (developer)

### Prerequisites

- Go 1.26+ (the `toolchain` directive in `go.mod` auto-downloads `go1.26.2` if you have Go 1.21+ installed).
- Docker daemon running — required for integration tests and the dev quickstart below (PostgreSQL via testcontainers / docker run).
- `golangci-lint` — optional but recommended for local lint parity with CI.
- `make` — GNU make works fine.

### Run the full test suite

```bash
# Unit + contract tests (fast, no external deps).
make test

# Integration tests — spins up ephemeral Postgres via testcontainers.
# Requires Docker running.
make test-integration

# E2E smoke scenarios — full runtime + Postgres + HTTP round trip.
make test-e2e

# Coverage for domain + application.
make cover
```

### Run the runtime locally

```bash
# 1. Start Postgres in the background (replace the tag if you prefer).
docker run --rm -d --name runtime-pg \
  -p 5432:5432 \
  -e POSTGRES_PASSWORD=dev \
  -e POSTGRES_USER=dev \
  -e POSTGRES_DB=runtime_adapters \
  postgres:15-alpine

# 2. Set required env + run.
export RUNTIME_POSTGRES_DSN="postgres://dev:dev@localhost:5432/runtime_adapters?sslmode=disable"
make run
```

You should see:

```
runtime-adapters v0.1.0 starting on :8080 (host=<hostname>)
HTTP listen on :8080
```

### Call the API

```bash
# Health check.
curl -s localhost:8080/healthz
# → {"status":"ok"}

# List the 8 Phase 1 capabilities.
curl -s localhost:8080/api/v1/capabilities | jq

# Execute a shell.exec command (adjust correlation_id — must be a valid ULID).
curl -s -X POST localhost:8080/api/v1/execute \
  -H 'Content-Type: application/json' \
  -d '{
    "correlation_id": "01HZXK5JC6QK7XV0YQXA0QJ0YZ",
    "adapter_id": "shell",
    "capability_name": "exec",
    "capability_version": "v1",
    "payload": { "command": "echo", "args": ["hello"] },
    "timeout_budget_ms": 5000
  }' | jq

# Fetch the resulting receipt (replace the id).
curl -s "localhost:8080/api/v1/receipts/<receipt_id>?include_streams=true" | jq
```

### Configuration reference

All runtime settings come from environment variables with sane defaults — see `internal/infrastructure/config/config.go` godoc or the Phase 1 spec §9.8 table. Key overrides:

| Variable | Default | Purpose |
|---|---|---|
| `RUNTIME_HTTP_ADDR` | `:8080` | HTTP listen address |
| `RUNTIME_POSTGRES_DSN` | **required** | Postgres connection string |
| `RUNTIME_MAX_TIMEOUT_BUDGET` | `30m` | Cap on per-request timeout (A4.1) |
| `RUNTIME_MAX_PAYLOAD_BYTES` | `1048576` | 1 MiB payload cap |
| `RUNTIME_MAX_CONCURRENT_EXECUTIONS` | `64` | Fast-reject semaphore (A9.1) |
| `RUNTIME_IDEMPOTENCY_WINDOW` | `24h` | Caller-owned idempotency window |
| `RUNTIME_SHUTDOWN_GRACE_PERIOD` | `30s` | Graceful drain on SIGTERM |
| `RUNTIME_ALLOWED_COMMANDS_PATH` | `/usr/bin:/bin` | Shell adapter command allowlist (colon-sep) |
| `RUNTIME_ALLOWED_WORKING_DIRS` | `$HOME` | Shell + git working-dir allowlist |
| `RUNTIME_ALLOWED_FILESYSTEM_ROOTS` | `$HOME` | Filesystem adapter path allowlist |
| `RUNTIME_HTTP_ALLOW_PRIVATE_NETWORKS` | `false` | Disable SSRF strict block (A8.3) |
| `OTEL_ENABLED` | `false` | Opt into OpenTelemetry export |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | (empty) | Required when OTel enabled |

## License

MIT — see [LICENSE](LICENSE).
