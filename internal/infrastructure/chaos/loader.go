package chaos

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// allowlistRoots are the directory roots from which profiles may be
// loaded (D2C3.17 / D2C3.25 / R17). Includes both host source paths
// (used when running locally or in tests) and the runtime-container
// mount root (used when the binary runs in the chaos compose overlay).
//
// Relative entries are resolved against the module root (the directory
// containing go.mod) at LoadProfile time; absolute entries are matched
// as-is. Module-root resolution makes the allowlist correct whether the
// caller is the production binary (CWD = repo root) or a `go test` run
// (CWD = package directory).
var allowlistRoots = []string{
	"ops/chaos/profiles/ci",
	"ops/chaos/profiles/local",
	"internal/infrastructure/chaos/testdata",
	"/etc/runtime-adapters/chaos/profiles",
}

// findModuleRoot walks up from cwd until it finds a directory containing
// go.mod, returning that directory. If go.mod is not found within 20
// levels the function returns cwd as a safe fallback (allowlist will
// not match, keeping the fail-closed property).
func findModuleRoot(cwd string) string {
	dir := cwd
	for i := 0; i < 20; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return cwd
}

// LoadProfile reads, validates, and parses a profile from path. Path is
// hardened: filepath.Clean → no parent traversal → must be inside the
// allowlist → resolved symlink target must also be inside the allowlist.
//
// Relative paths are resolved against the module root (directory containing
// go.mod). Absolute paths are used as-is. This makes the function correct
// whether the caller is the production binary (CWD = repo root) or a
// `go test` run (CWD = package directory).
//
// Returns the immutable *Profile on success, or a descriptive error
// indicating which guard rejected the path. R17 fail-closed: any guard
// failure returns error; bootstrap aborts.
func LoadProfile(path string, cat Catalog) (*Profile, error) {
	cleaned := filepath.Clean(path)
	if hasParentTraversal(cleaned) {
		return nil, fmt.Errorf("chaos: profile path %q contains parent traversal", path)
	}

	var abs string
	if filepath.IsAbs(cleaned) {
		abs = cleaned
	} else {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("chaos: resolve working directory: %w", err)
		}
		modRoot := findModuleRoot(cwd)
		abs = filepath.Join(modRoot, cleaned)
	}

	if !inAllowlist(abs) {
		return nil, fmt.Errorf("chaos: profile path %q not in allowlist", abs)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("chaos: resolve symlinks for %q: %w", abs, err)
	}
	if !inAllowlist(resolved) {
		return nil, fmt.Errorf("chaos: profile path %q escapes allowlist via symlink", abs)
	}
	// parseProfile already prefixes its errors with "chaos:"; do not
	// double-wrap.
	return parseProfile(resolved, cat)
}

// hasParentTraversal reports whether p contains a ".." path component
// after filepath.Clean. Clean already reduces "a/../b" to "b" and
// "a/b/.." to "a", so a remaining ".." can only appear at the start
// (e.g. "../foo" or "../../bar") which means the path tries to escape
// the working directory.
func hasParentTraversal(cleaned string) bool {
	parts := strings.Split(cleaned, string(filepath.Separator))
	for _, p := range parts {
		if p == ".." {
			return true
		}
	}
	return false
}

// inAllowlist reports whether absPath is inside one of the allowlistRoots.
// Relative roots are resolved against the module root (directory containing
// go.mod), which makes the check correct both for production binaries (where
// CWD is the repo root) and for `go test` runs (where CWD is the package dir).
func inAllowlist(absPath string) bool {
	cwd, err := os.Getwd()
	if err != nil {
		return false
	}
	modRoot := findModuleRoot(cwd)

	for _, root := range allowlistRoots {
		var rootAbs string
		if filepath.IsAbs(root) {
			rootAbs = root
		} else {
			rootAbs = filepath.Join(modRoot, root)
		}
		rel, err := filepath.Rel(rootAbs, absPath)
		if err != nil {
			continue
		}
		if rel == "." {
			return true
		}
		if !strings.HasPrefix(rel, "..") {
			return true
		}
	}
	return false
}
