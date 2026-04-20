# ADR 0002 — Use `go-git` only for the git adapter in Phase 1 (no hooks)

- **Status:** accepted
- **Date:** 2026-04-19
- **Deciders:** Russell Vergara
- **Context:** The git adapter in Phase 1 exposes 4 capabilities (`git.status@v1`, `git.clone@v1`, `git.diff@v1`, `git.commit@v1`). Two library options were evaluated: (a) go-git (pure-Go, in-process, predictable; no hook execution because it does not invoke the local `git` binary); (b) shelling out to the `git` CLI (runs hooks, but adds process-management complexity, differing system `git` versions across operators, and weaker error normalization).
- **Decision:** Phase 1 uses `github.com/go-git/go-git/v5` exclusively for all four git capabilities. The git adapter does NOT invoke the system `git` binary. As a direct consequence, **client-side hooks are NOT executed** on `git.commit@v1` (and never will be by this adapter).
- **Consequences:**
  - Operators who rely on commit hooks (e.g., `pre-commit`, `commit-msg`) for side effects must run those hooks upstream — `runtime-adapters` will not trigger them.
  - Shell-out parity is deferred to Phase 2F (git-extended track) if and only if a concrete operational trigger demands it (§11.2).
  - The `.claude/skills/git-file-operations/SKILL.md` pack reflects this constraint as an explicit rule for subagents.
  - Error normalization benefits: go-git returns typed Go errors that map cleanly to `ErrorClass` without parsing CLI stderr.
- **Spec references:** A8.1, §8.3, D8.7.
