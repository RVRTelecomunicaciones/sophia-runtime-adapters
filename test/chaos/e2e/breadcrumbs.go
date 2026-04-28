//go:build e2e

package e2e

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// DumpDiagnostics invokes ops/chaos/scripts/dump.sh and logs the combined
// stdout+stderr output. Call from t.Cleanup when t.Failed() is true so that
// CI artifacts capture the Prometheus/Alertmanager/receiver-stub state at the
// moment of failure.
func DumpDiagnostics(t *testing.T) {
	t.Helper()
	root := findRepoRoot(t)
	script := filepath.Join(root, "ops", "chaos", "scripts", "dump.sh")
	cmd := exec.Command(script)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	t.Logf("chaos-dump (err=%v):\n%s", err, out)
}
