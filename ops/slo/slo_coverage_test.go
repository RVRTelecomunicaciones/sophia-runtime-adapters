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

// globalSLOs is the allowlist of SLOs that are intentionally not
// per-capability (no labels.capability declaration). They measure
// runtime-wide concerns instead. Each entry is the SLO name and a short
// rationale comment.
//
// Adding to this set is a deliberate decision — every other SLO must
// continue to declare labels.capability so the per-capability coverage
// checks below remain meaningful.
var globalSLOs = map[string]string{
	// persist-availability tracks receipt persistence reliability across
	// ALL capabilities. Adding capability would inflate cardinality and
	// require a metric-contract change (D2C4G.3 in Phase 2C.4 / G).
	"persist-availability": "global runtime-wide receipt persistence (2C.4 / G)",
}

// TestSloth_CoversAllPhase1Capabilities asserts: for every capability
// returned by valueobjects.NewPhase1Capabilities() the SLO specs
// declare an "-availability", "-latency", AND "-cancellation-rate"
// SLO. Adding a new capability to Phase 1 without a matching SLO
// breaks this gate — same spirit as VerifyCoversPhase1Catalog in
// Bundle 4's adapter registry.
//
// Cancellation-rate SLOs were added in Phase 2C.4 / G (per-capability,
// to provide drill-down operationality when callers cancel a specific
// capability disproportionately).
//
// Globally-scoped SLOs (no capability label) are allowed via the
// globalSLOs allowlist — they're skipped from the per-capability
// accounting because they measure runtime-wide concerns.
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
			// Globally-scoped SLOs (e.g. persist-availability) are
			// allowed to skip both the labels.capability requirement
			// and the per-capability accounting. They still must use a
			// recognised suffix (the switch below), and their presence
			// in the allowlist is a deliberate per-spec decision.
			if _, ok := globalSLOs[s.Name]; ok {
				continue
			}

			capID := s.Labels.Capability
			require.NotEmpty(t, capID,
				"SLO %q in %s missing labels.capability — every per-capability SLO must declare it (or be added to globalSLOs allowlist)",
				s.Name, f)

			if has[capID] == nil {
				has[capID] = map[string]bool{}
			}
			switch {
			case strings.HasSuffix(s.Name, "-availability"):
				has[capID]["availability"] = true
			case strings.HasSuffix(s.Name, "-latency"):
				has[capID]["latency"] = true
			case strings.HasSuffix(s.Name, "-cancellation-rate"):
				has[capID]["cancellation-rate"] = true
			default:
				t.Errorf("SLO name %q in %s does not end with -availability, -latency, or -cancellation-rate (naming contract)",
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
			require.True(t, has[id]["cancellation-rate"],
				"missing cancellation-rate SLO for capability %q (added in Phase 2C.4 / G)", id)
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
