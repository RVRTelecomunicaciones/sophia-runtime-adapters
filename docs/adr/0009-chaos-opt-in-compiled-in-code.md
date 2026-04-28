# ADR 0009 — Chaos as opt-in compiled-in code

## Status

Accepted — 2026-04-26.

## Context

Phase 2C.3 introduces a deliberate fault-injection layer so the runtime's
classification, persistence, metrics, and alert pipeline can be
exercised end-to-end without depending on real upstream outages
(spec §3, §5).

We need to decide WHERE the chaos code lives and HOW operators turn it
on. The available shapes are:

- **a)** A separate binary (`runtime-adapters-chaos`) built from a
  parallel main package, distributed alongside the production binary.
- **b)** A build-tag-gated package (`//go:build chaos`) that is
  excluded from production binaries via `go build` tag selection at
  CI/release time.
- **c)** A runtime plugin loaded dynamically at process start (Go
  plugins / a script interpreter) when chaos is requested.
- **d)** Compiled directly into the binary as an OPT-IN package whose
  effect is gated at runtime by `RUNTIME_CHAOS_ENABLED` plus
  `RUNTIME_ENV != "production"`. The wrapper
  (`MaybeWrapAdaptersWithChaos`) is identity unless the config + env
  pair authorises chaos.

The decision must preserve four invariants:

1. **R5 — raw outcome stays in adapter.** Chaos must wrap the
   `Adapter.Execute` boundary without leaking adapter-internal types
   into domain or application. The chaos package owns the wrapping;
   ports stay frozen.
2. **R17 — fail-closed in production.** Production runs MUST refuse to
   start with chaos enabled, regardless of operator intent. The check
   must be impossible to bypass without re-deploying with a different
   binary or env.
3. **I24 — chaos preserves runtime semantics.** Receipts, metrics, and
   error classes from a chaos-injected execution are shape-identical
   to a real failure (R13 — receipts always; R15 — only 5 statuses).
4. Build/release simplicity. Phase 1 ships one binary, one image, one
   compose stack. Multiplying that surface area for the chaos work is
   discouraged unless it materially improves safety.

## Decision

**Adopt option (d): chaos compiled into the binary as an opt-in,
runtime-gated package.**

- The chaos package lives at `internal/infrastructure/chaos/` and is
  always built with the production binary.
- Adapters opt into chaos by implementing `ChaosCapable`
  (`internal/infrastructure/chaos/chaos_capable.go`). Adapters that do
  not implement the interface are passed through unchanged by the
  wrapper.
- `internal/bootstrap/wire.go` calls `MaybeWrapAdaptersWithChaos`
  before the application service is constructed. The wrapper returns
  the original adapters when `Config.Enabled == false`.
- `internal/infrastructure/chaos/config.go::LoadConfig` rejects
  `RUNTIME_ENV=production` with `RUNTIME_CHAOS_ENABLED=true` at
  bootstrap time, returning an error that aborts startup before any
  adapter is instantiated.
- The config also rejects: missing profile path, schema version != 1,
  unknown fault kind, unknown capability id, path outside the
  allowlist, parent-traversal sequences after `filepath.Clean`,
  symlinks resolving outside the allowlist (D2C3.11).

## Options considered

- **Option a (separate binary)** — rejected. A chaos-only binary
  diverges from the production code path at every release; any
  classification or persistence change must be ported and re-validated
  in two places. R5 and I24 become a continuous integration burden
  rather than a structural property.
- **Option b (build-tag binary)** — rejected. Build-tag gating moves
  the safety check from runtime to release engineering. A misconfigured
  CI that builds with `-tags=chaos` and pushes to production silently
  enables chaos. R17 fails closed only if the release pipeline never
  makes a mistake — which is not the bar we want.
- **Option c (runtime plugin)** — rejected. Go's plugin system is
  brittle (version-locked to the host binary, unsupported on Windows
  and Darwin builds). Script interpreters add a new attack surface and
  defeat R5 (the plugin would need access to internal adapter types).
- **Option d (compiled-in opt-in)** — adopted. R17 is enforced at the
  one place the operator cannot bypass: the bootstrap config loader.
  R5 is preserved because the wrapper sees only the public `Adapter`
  interface plus the opt-in `ChaosCapable` extension. I24 holds because
  the synthesised raw outcome runs through the same `ResultNormalizer`
  as a real failure.

## Consequences

- One binary, one image, one compose stack.
- Production binaries carry the chaos package as inert code (~few KB);
  no measurable runtime cost when disabled.
- Operators turn chaos on by setting two env vars and a profile path —
  no rebuild, no separate image pull.
- The fail-closed contract is testable via unit tests against
  `LoadConfig` and `MaybeWrapAdaptersWithChaos`, not via release-time
  build verification.
- Adding a new fault kind requires touching the chaos package itself
  (decorator switch + enum entry) — see `docs/chaos.md` §7.
- Trade-off accepted: a future high-traffic deployment that wants to
  shave the chaos package out can opt into a build tag later. We do
  not believe that's necessary at Phase 2C.3 scale, and it can be
  added without breaking the contract — the fail-closed gate is at
  runtime, not at compile time.

## Spec references

- §3 (chaos principles), §5 (framework), §8 (bootstrap safety),
  §15 (R17 / I24)
- D2C3.2, D2C3.11, D2C3.13, A2C3.2 (Q2 = d decision)
- R4, R5, R10, R13, R15, R17 — `docs/rules.md`
- I24 — `docs/domain-invariants.md`
