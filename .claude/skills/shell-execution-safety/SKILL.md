---
name: shell-execution-safety
description: Shell adapter safety within Phase 1 operational limits (allowlists, minimal env, no interpolation).
triggers:
  - "internal/adapters/outbound/shell/**/*.go"
---

# shell-execution-safety

**When this skill applies**: Writing or reviewing the shell adapter, its helpers, or its tests.

## Rules

- **No shell interpolation — ever** (D4.8): Use `exec.CommandContext(ctx, binary, args...)` with a discrete args array. Never build a shell command string and pass it to `sh -c`. This eliminates injection regardless of payload content.
- **Binary must be on `AllowedCommandsPath`** (I8): Before executing, resolve the binary against the colon-separated `RUNTIME_ALLOWED_COMMANDS_PATH` allowlist (`/usr/bin:/bin` by default). Reject with `ErrorClass=NotPermitted` if the resolved binary is not under an allowed prefix.
- **Working directory must be on `AllowedWorkingDirs`** (I8): If a working directory is specified in the payload, resolve it via `filepath.EvalSymlinks` and check it against `RUNTIME_ALLOWED_WORKING_DIRS`. Reject if not covered.
- **Minimal environment base** (D4.8): Pass only a controlled env set to the subprocess — do not inherit the full process environment. At minimum: `PATH`, `HOME`, `LANG`. Callers cannot inject arbitrary env vars.
- **No artifacts from shell commands** (D4.8 / I8): `shell.exec@v1` does not produce artifacts. Stdout/stderr go to streams only. Do not map command output into the `Artifacts` map.
- **Phase 1 is not a strong sandbox** (Phase 1 limitation): The allowlist is an operational control, not a security sandbox. Document this clearly. Phase 2 may introduce seccomp/namespace isolation. Do not promise sandbox-level containment.
- **Context cancellation kills the subprocess** (D9.1): Use `exec.CommandContext` so the OS process receives SIGKILL when the context is cancelled or deadline exceeded. Never ignore context cancellation in the wait loop.
- **Capture streams separately** (§4.3): Stdout and stderr are captured as distinct streams and stored independently on the receipt. Do not merge them.

## Anti-patterns

- **`sh -c "user_input"`**: The classic injection vector — forbidden unconditionally.
- **Inheriting full `os.Environ()`**: Leaks secrets (tokens, DSNs) from the runtime process into the subprocess environment.
- **Ignoring allowlist on binary**: Checking only the filename (not the resolved absolute path) allows symlink escapes to run unauthorized binaries.
- **Blocking wait without context**: `cmd.Wait()` without context propagation leaves orphaned subprocesses after deadline.
- **Treating non-zero exit as a Go error**: Non-zero exit is a business outcome (`ExecutionStatus=Failed`, `ErrorClass=ExternalFailure`), not a structural error.
