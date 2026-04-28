//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"testing"
)

// findRepoRoot walks up the directory tree from the current working directory
// until it finds a directory containing go.mod.  Fatalf's the test if not
// found within 10 levels.  Mirrors the pattern used in ops/slo/slo_coverage_test.go.
func findRepoRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("findRepoRoot: getwd: %v", err)
	}
	dir := cwd
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("findRepoRoot: could not find go.mod starting from %s", cwd)
	return ""
}
