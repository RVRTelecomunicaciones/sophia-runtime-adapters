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

_Deferred to Bundle 8 Task T65._ The runtime binary is not yet implemented; current tasks scaffold the repo skeleton, tooling, and documentation.

## License

MIT — see [LICENSE](LICENSE).
