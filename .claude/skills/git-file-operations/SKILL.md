---
name: git-file-operations
description: Git adapter rules: go-git only, no hooks, SSH-agent auth, no embedded credentials.
triggers:
  - "internal/adapters/outbound/git/**/*.go"
---

# git-file-operations

**When this skill applies**: Writing or reviewing the git adapter, its capabilities (`git.status@v1`, `git.clone@v1`, `git.diff@v1`, `git.commit@v1`), or its tests.

## Rules

- **go-git only — no shell-out to the system git binary** (D8.7 / ADR-0002): All git operations use `github.com/go-git/go-git/v5`. The adapter must never invoke `/usr/bin/git` or any system binary. This is a hard constraint, not a preference.
- **No client-side hooks** (A8.1): go-git does not execute `.git/hooks/*` scripts. This is intentional and documented (ADR-0002). Do not add logic to manually invoke hooks — defer to upstream callers if hook side effects are needed.
- **SSH-agent auth only, no embedded credentials** (D8.3): For authenticated clone/push, use `ssh.DefaultAuthBuilder` backed by the user's SSH agent socket. Never accept or store SSH private keys, passwords, or tokens in the payload or in config.
- **`git.clone@v1` is the only capability with `AllowsPartial=true`** (D4.6): A clone interrupted mid-transfer may leave a partial working tree. The adapter must report `ExecutionStatus=PartialSuccess` in this case and populate the `Artifacts` map with the bytes transferred.
- **Working directory must be on `AllowedWorkingDirs`** (I8): Resolve the target path via `filepath.EvalSymlinks` and check against the allowlist before any write operation (clone, commit).
- **Context cancellation terminates go-git operations** (D9.1): Pass `ctx` to all go-git calls that accept it. go-git respects `context.Done` on network operations.
- **Typed go-git errors for normalization** (ADR-0002): Translate `*git.NoErrAlreadyUpToDate`, `plumbing.ErrObjectNotFound`, `transport.ErrAuthorizationFailed`, etc. to the appropriate `ErrorClass` — do not return raw go-git errors as business failures.

## Anti-patterns

- **`exec.Command("git", ...)` anywhere in the adapter**: Violates D8.7 and ADR-0002; creates platform-dependent behavior and hook execution side effects.
- **Accepting credentials in payload**: Tokens or passwords in the execution payload are a secret-leakage risk; reject with `ErrorClass=NotPermitted`.
- **Ignoring `AllowsPartial` for clone**: Treating an interrupted clone as a hard failure discards potentially useful partial output; report `PartialSuccess` and include transfer bytes in artifacts.
- **Hardcoding hook invocation**: Any attempt to replicate hook behavior inside the adapter re-introduces the go-git bypass rationale described in ADR-0002.
- **Swallowing go-git errors as generic**: Returning `ErrorClass=Unknown` when a typed go-git error is available makes debugging and retry decisions impossible.
