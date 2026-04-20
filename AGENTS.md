# AGENTS.md — runtime-adapters

## SDD workflow

All significant work follows the six-node cycle (§13.1):

```
Brainstorm → Spec → Plan → Exec → Verify → Archive
```

- **Brainstorm**: explore scope, tradeoffs, and fit. Produces directed choices with a recommendation; no open-ended questions.
- **Spec**: formal design document under `docs/superpowers/specs/YYYY-MM-DD-<topic>-design.md`. Numbered sections, decision log per section (Dn.m), adjustments tracked as An.m.
- **Plan**: implementation task list under `docs/superpowers/plans/YYYY-MM-DD-<topic>.md`. Tasks T1..TN with bite-sized steps, exact commands, verbatim commit messages.
- **Exec**: subagent-driven by default; inline on explicit request. Subagents receive pre-digested compact rules — they do NOT read `SKILL.md` files directly.
- **Verify**: validate implementation against spec + tasks. Reports CRITICAL / WARNING / SUGGESTION.
- **Archive**: sync delta specs to main spec; close the change in engram.

**Phase 1 status**: the approved spec and plan are already in place. Routine Phase 1 work enters at **Plan** (if extending the plan) or **Exec** (if picking up an existing task).

## Skills catalogue

11 active skills for Phase 1. The orchestrator resolves and injects relevant compact rules into each subagent prompt.

| Skill | Purpose |
|-------|---------|
| `architecture-guardrails` | Enforces hexagonal boundaries and dependency direction |
| `execution-modeling` | Request / result / receipt modeling integrity |
| `adapter-contracts` | Uniform `Adapter` port compliance |
| `shell-execution-safety` | Shell adapter safety within operational limits |
| `git-file-operations` | Git adapter rules: no hooks, no embedded credentials, go-git only |
| `filesystem-safety` | Path allowlists, atomic writes, symlink escape prevention |
| `http-adapter-design` | SSRF blocking, TLS verify mandatory, redirect limits |
| `resilience-timeouts-retries` | `context.Context` handling, timeout/cancel discipline, retry-hint policy |
| `execution-result-normalization` | `ResultNormalizer` determinism |
| `observability-runtime` | Metrics, spans, and log conventions |
| `testing-quality` | Test pyramid compliance and determinism requirements |

**`mailbox-locking-model` is NOT active in Phase 1** (A12.1). It is listed in the roadmap and will be created only when Phase 2A or 2B activates.

## Phase 2 reserved tracks

Phase 2 is **trigger-driven** (A11.2) — no fixed priority among tracks; each activates only when its operational trigger fires. Each track starts with its own brainstorm, spec, and plan (§13.9).

| Track | Activation trigger |
|-------|--------------------|
| **2A** — Async execution + `EventPublisher` | Long-running capability degrades sync UX |
| **2B** — `LockManager` | Real concurrent collision on a shared resource |
| **2C** — Hardening (load baseline, SLOs, chaos) | Operational use with SLO requirements |
| **2D** — Fuzz + central JSON Schema | Incident from malformed payload |
| **2E** — Auth + multi-tenant | Shared or multi-tenant deployment |
| **2F** — Git extended (`push`, `fetch`, etc.) | CI/CD pipeline requirement |

A dedicated `docs/skills-roadmap.md` is deferred until a track activates (§7.3 of the spec).

## Conventions

**Commit format**: conventional commits, always.

```
<type>(<scope>): <short imperative subject>

[optional body — non-evident rationale only]
```

- Types: `feat`, `fix`, `chore`, `ci`, `docs`, `test`
- Scope = layer or adapter name: `shell`, `git`, `filesystem`, `httpreq`, `domain`, `ports`, `application`, `bootstrap`, `ci`, `docs`
- Subject: imperative mood, lowercase, ≤ 72 chars, no period
- Body: include only when the rationale is not obvious from the diff

**Never** add `Co-Authored-By` or any AI attribution to commits.

## Invariants pointer

All domain invariants are defined in `docs/domain-invariants.md` (I1..I22).

**Mandatory**: every change that touches receipt shape, enum values, or request fields MUST cite the invariant it preserves or deliberately updates in the commit body. Example: `Preserves I15 (receipts append-only)`.

If a change deliberately updates an invariant, the invariant document must be updated in the same commit.

## How to add a capability

1. Brainstorm scope and fit with the existing adapter that would host it.
2. Update the spec: capability table §5.4, decision table, partial rule if applicable.
3. Write a delta spec under `docs/superpowers/specs/`.
4. Write a plan under `docs/superpowers/plans/`.
5. **ADR required** only if the capability changes an existing port signature, extends a closed enum, or bumps `schema_version`. Otherwise no ADR needed.
6. Implement as a new case in the adapter dispatcher + new payload/outcome types + register the normalizer.
7. Add to `NewPhase1Capabilities()`.
8. Contract tests MUST cover the new capability end-to-end.

## How to add an adapter

1. **ADR REQUIRED before any code** (per §12.7 — qualifies as "major external dependency change" or "port signature change" if a new outcome shape is needed). ADR acceptance requires explicit human approval.
2. Create a new folder: `internal/adapters/outbound/<adapter_name>/`.
3. Implement the `outbound.Adapter` contract (`ID()`, `Capabilities()`, `Execute(ctx, cap, payload)`).
4. Register with bootstrap via `RegisterAllPhase1` in `internal/bootstrap/wire.go`.
5. Must pass `AdapterContractTestSuite` applied to the new adapter.
6. Must have its own safety section in `docs/rules.md` if the adapter introduces new threat surface:
   - `shell` → process execution
   - `http` → SSRF
   - `filesystem` → path traversal
   - `git` → credentials / auth
