---
name: filesystem-safety
description: Path allowlists, symlink escape prevention, atomic tmp+fsync+rename writes.
triggers:
  - "internal/adapters/outbound/filesystem/**/*.go"
---

# filesystem-safety

**When this skill applies**: Writing or reviewing the filesystem adapter, its capabilities (`fs.read@v1`, `fs.write@v1`, `fs.list@v1`, `fs.delete@v1`), or its tests.

## Rules

- **Allowlist + EvalSymlinks on every path** (D8.4 / I8): Before any filesystem operation, call `filepath.EvalSymlinks` on the target path and verify the result is under an `RUNTIME_ALLOWED_FILESYSTEM_ROOTS` prefix. Reject with `ErrorClass=NotPermitted` if not covered. This prevents symlink escape regardless of the link depth.
- **Atomic writes via tmp + fsync + rename** (D8.4): For `fs.write@v1`, write to a temp file in the same directory (same filesystem), call `file.Sync()`, then `os.Rename(tmp, target)`. This ensures readers never see a partial write.
- **`overwrite=false` is the default** (D8.4): If the caller does not explicitly pass `overwrite: true`, reject writes to existing files with `ErrorClass=Conflict`. This prevents accidental data loss.
- **Never partial — all-or-nothing writes** (D4.5): `fs.write@v1` has `AllowsPartial=false`. If the write fails mid-stream, delete the temp file and return `ExecutionStatus=Failed`. Do not leave partial files.
- **Path traversal check before allowlist** (I8): Reject any path containing `..` components before resolving — this catches naive traversal before `EvalSymlinks`.
- **`fs.delete@v1` requires explicit confirmation flag** (D8.4): The payload must include `confirm: true` for delete operations. Absent or false → reject with `ErrorClass=InvalidInput`.
- **Context cancellation aborts in-progress writes** (D9.1): Check `ctx.Err()` before and after the write. If cancelled, clean up the temp file.

## Anti-patterns

- **Checking path containment without `EvalSymlinks`**: A symlink `allowed_dir/escape -> /etc` passes a naive prefix check but escapes the allowlist.
- **Non-atomic write (direct `os.WriteFile`)**: A crash between open and close leaves a partial file and no atomic guarantee.
- **Default overwrite=true**: Silent overwrites are data-loss risks; callers must opt in explicitly.
- **Ignoring `..` in path before resolution**: `EvalSymlinks` handles real links but a string check catches injection before syscall overhead.
- **Returning `PartialSuccess` for a write**: The filesystem adapter never partially succeeds on writes — always fail and clean up.
