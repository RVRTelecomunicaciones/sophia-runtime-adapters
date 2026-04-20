# ADR 0001 — Phase 1 spec adoption

- **Status:** accepted
- **Date:** 2026-04-19
- **Deciders:** Russell Vergara
- **Context:** The Phase 1 design spec for `runtime-adapters` was closed after a structured 13-section brainstorming (sections 1 through 13, each with closed decisions `Dn.m` and post-approval adjustments `An.m`). The spec is located at `docs/superpowers/specs/2026-04-19-runtime-adapters-phase1-design.md`.
- **Decision:** The Phase 1 spec is adopted as the authoritative reference for the runtime-adapters execution layer. All `Dn.m` decisions and `An.m` adjustments are binding. Any deviation requires a new ADR.
- **Consequences:**
  - Phase 1 public port contracts freeze as specified in §7 and D2.9.
  - The 8 capabilities enumerated in §5.4 are the complete Phase 1 set (1 shell + 4 git + 2 filesystem + 1 http).
  - `ExecutionStatus`, `RetryHint`, `ErrorClass` enums are closed (§4.3 / §4.4 / §4.6); extending any requires a follow-up ADR plus a `schema_version` bump.
  - The items in §11.4 ("permanently out of scope") are a hard contract; moves from that list require a formal ADR with a default-rejected stance (A11.1).
  - The Phase 1 skill catalog is 11 skills (§12.6); `mailbox-locking-model` is explicitly NOT created in Phase 1 (A12.1).
- **Spec references:** D1.1..D13.9, A1.1..A12.2, §1..§13.
