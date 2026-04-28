//go:build e2e

package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// DumpDiagnostics invokes ops/chaos/scripts/dump.sh while the chaos compose
// stack is STILL UP and writes the dump to <repo>/chaos-dump/. The CI workflow
// uploads that directory as an artifact (no second invocation post-teardown —
// containers would already be gone).
//
// Call this from t.Cleanup BEFORE ComposeDown when t.Failed() is true.
func DumpDiagnostics(t *testing.T) {
	t.Helper()
	root := findRepoRoot(t)
	dumpDir := filepath.Join(root, "chaos-dump")
	script := filepath.Join(root, "ops", "chaos", "scripts", "dump.sh")
	cmd := exec.Command(script)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "OUT_DIR="+dumpDir)
	out, err := cmd.CombinedOutput()
	t.Logf("chaos-dump (err=%v) → %s:\n%s", err, dumpDir, out)
}
