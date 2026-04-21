---
name: architecture-guardrails
description: Enforces hexagonal boundaries (domain/ports/adapters); dependency direction one-way; composition only at bootstrap/wire.go.
triggers:
  - "internal/**/*.go"
---

# architecture-guardrails

**When this skill applies**: Any Go file under `internal/` — writing, reviewing, or refactoring domain, application, ports, or adapter code.

## Rules

- **One-way inward dependency only** (D7.1): `adapters` → `ports` → `application` → `domain`. No layer may import from the layer outside it. `domain` has zero external imports other than stdlib.
- **Composition exclusively at bootstrap** (D7.9): The only file allowed to import both adapters and domain concretions is `internal/bootstrap/wire.go`. No other file wires implementations to interfaces.
- **No adapter imports outside bootstrap or cmd/** (D7.2): `internal/domain/**` and `internal/application/**` must never import `internal/adapters/**`. Violation breaks the hexagonal contract.
- **Ports are interfaces, never structs** (D7.3): All outbound ports in `internal/ports/outbound/` and inbound ports in `internal/ports/inbound/` are Go interfaces. Implementations live under `internal/adapters/`.
- **Inbound adapters own HTTP shape; domain owns execution shape** (D7.5): HTTP request/response DTOs live under `internal/adapters/inbound/http/`. They MUST NOT leak into domain or application packages.
- **No circular imports** — Go enforces this; avoid import cycles that require moving code to break them (symptom: over-shared `util` or `common` packages).
- **`cmd/` is the composition root for the binary** (D7.8): `cmd/runtime/main.go` imports `internal/bootstrap` only, nothing else from `internal/`.
- **Shared value objects belong in `internal/domain/shared/`** (D7.6): Clock, IDGenerator, and similar cross-cutting domain primitives live there, not in application or adapters.

## Anti-patterns

- **Importing an adapter from domain or application**: e.g., `import "sophia-runtime-adapters/internal/adapters/outbound/shell"` in a service — this collapses the boundary and makes testing impossible without real I/O.
- **Wiring in a service file**: instantiating concrete adapters inside `ExecutionService` or any application-layer struct — push it up to `wire.go`.
- **Fat `util` package**: a catch-all `internal/util` that every layer imports creates hidden coupling and breaks the dependency diagram.
- **Test helpers importing production adapters**: test doubles must live in `internal/ports/outbound/testdoubles/` or alongside the test file — never import the real adapter to "make the test easier".
- **Skipping the port interface**: calling a concrete adapter method directly instead of going through the port interface makes the system untestable without the real side effect.
