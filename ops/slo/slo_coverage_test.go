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

	matches, err := filepath.Glob("*.yaml")
	require.NoError(t, err)
	require.NotEmpty(t, matches, "no ops/slo/*.yaml files found — run from ops/slo/ directory")

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
