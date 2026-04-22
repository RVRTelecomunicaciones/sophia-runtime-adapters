//go:build sloth

// Package slo_test validates that the Sloth SLO specs cover every
// Phase 1 capability. Tagged `sloth` so dev machines without the Sloth
// CLI or YAML parse deps can skip without break — CI always runs this
// with -tags sloth per Bundle 8.
//
// Spec §7.6 + plan Task 30.
package slo_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
)

// slothSpec is the minimal subset of the Sloth v1 spec format that this
// test needs to traverse. We rely on the capability label being set on
// every SLO (convention enforced by ops/slo/*.yaml).
type slothSpec struct {
	SLOs []struct {
		Name   string `yaml:"name"`
		Labels struct {
			Capability string `yaml:"capability"`
		} `yaml:"labels"`
	} `yaml:"slos"`
}

// TestSloth_CoversAllPhase1Capabilities asserts: for every capability
// returned by valueobjects.NewPhase1Capabilities() the SLO specs
// declare BOTH an "-availability" and a "-latency" SLO. Adding a new
// capability to Phase 1 without a matching SLO breaks this gate — same
// spirit as VerifyCoversPhase1Catalog in Bundle 4's adapter registry.
func TestSloth_CoversAllPhase1Capabilities(t *testing.T) {
	caps, err := valueobjects.NewPhase1Capabilities()
	require.NoError(t, err, "fetch Phase 1 capabilities")
	require.NotEmpty(t, caps, "Phase 1 catalog must be non-empty")

	has := map[string]map[string]bool{}

	// Resolve ops/slo/*.yaml via repo-root walk instead of CWD-relative
	// glob. Standard `go test ./ops/slo/` sets CWD to the package dir, but
	// compiled test binaries invoked elsewhere (e.g. integration runners)
	// would silently find zero matches. Parity with findRepoRoot pattern
	// in internal/infrastructure/obs/metrics_test.go.
	repoRoot := findRepoRoot(t)
	matches, err := filepath.Glob(filepath.Join(repoRoot, "ops", "slo", "*.yaml"))
	require.NoError(t, err)
	require.NotEmpty(t, matches,
		"no ops/slo/*.yaml files found under %s", repoRoot)

	for _, f := range matches {
		data, err := os.ReadFile(f)
		require.NoError(t, err, "read %s", f)

		var spec slothSpec
		require.NoError(t, yaml.Unmarshal(data, &spec), "parse %s", f)

		for _, s := range spec.SLOs {
			capID := s.Labels.Capability
			require.NotEmpty(t, capID,
				"SLO %q in %s missing labels.capability — every SLO must declare it",
				s.Name, f)

			if has[capID] == nil {
				has[capID] = map[string]bool{}
			}
			switch {
			case strings.HasSuffix(s.Name, "-availability"):
				has[capID]["availability"] = true
			case strings.HasSuffix(s.Name, "-latency"):
				has[capID]["latency"] = true
			default:
				t.Errorf("SLO name %q in %s does not end with -availability or -latency (naming contract)",
					s.Name, f)
			}
		}
	}

	for _, c := range caps {
		id := c.Canonical()
		t.Run(id, func(t *testing.T) {
			require.True(t, has[id]["availability"],
				"missing availability SLO for capability %q", id)
			require.True(t, has[id]["latency"],
				"missing latency SLO for capability %q", id)
		})
	}
}

// findRepoRoot walks up the filesystem until it finds a directory that
// contains go.mod. Fails the test if not found within 10 levels.
// Mirrors the helper in internal/infrastructure/obs/metrics_test.go —
// duplicated locally instead of extracted because the obs package is
// not importable from a _test package under build-tagged ops/ tests.
func findRepoRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	require.NoError(t, err)
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
	t.Fatalf("could not find repo root (go.mod) starting from %s", cwd)
	return ""
}
