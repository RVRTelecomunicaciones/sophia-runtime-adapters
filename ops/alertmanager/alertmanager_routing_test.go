//go:build alertmanager

// Package alertmanager_test validates the alertmanager.yaml routing
// config against contracts the Bundle 4 Sloth rules and Bundle 5
// hand-written infra alerts emit upstream.
//
// amtool check-config covers YAML syntax + receiver refs. This test
// covers: null-receiver declared, every matcher label exists on some
// upstream rule, inhibit_rules.equal labels are emitted upstream,
// group_by includes `capability` (D2C1.16), and inhibit uses
// `sloth_slo` not `alertname` (D2C1.17).
//
// Spec §9 + §11.2. Build-tagged so dev machines without amtool/yaml
// can skip gracefully — CI (Bundle 8) always runs with -tags alertmanager.
package alertmanager_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// amConfig is the minimal subset of the Alertmanager config schema
// this test needs to inspect.
type amConfig struct {
	Route struct {
		Receiver string   `yaml:"receiver"`
		GroupBy  []string `yaml:"group_by"`
		Routes   []struct {
			Matchers []string `yaml:"matchers"`
			Receiver string   `yaml:"receiver"`
			Continue bool     `yaml:"continue"`
		} `yaml:"routes"`
	} `yaml:"route"`
	Receivers []struct {
		Name string `yaml:"name"`
	} `yaml:"receivers"`
	InhibitRules []struct {
		SourceMatchers []string `yaml:"source_matchers"`
		TargetMatchers []string `yaml:"target_matchers"`
		Equal          []string `yaml:"equal"`
	} `yaml:"inhibit_rules"`
}

func TestAlertmanager_NullReceiverDeclared(t *testing.T) {
	cfg := loadAM(t)
	has := false
	for _, r := range cfg.Receivers {
		if r.Name == "null-receiver" {
			has = true
		}
	}
	require.True(t, has, "null-receiver must be declared (2C.1 design)")
}

func TestAlertmanager_AllRouteReceiversDeclared(t *testing.T) {
	cfg := loadAM(t)
	names := map[string]bool{}
	for _, r := range cfg.Receivers {
		names[r.Name] = true
	}
	require.True(t, names[cfg.Route.Receiver],
		"root route receiver %q not declared", cfg.Route.Receiver)
	for _, r := range cfg.Route.Routes {
		require.True(t, names[r.Receiver],
			"sub-route receiver %q not declared", r.Receiver)
	}
}

func TestAlertmanager_GroupByIncludesCapability(t *testing.T) {
	cfg := loadAM(t)
	has := false
	for _, g := range cfg.Route.GroupBy {
		if g == "capability" {
			has = true
		}
	}
	require.True(t, has,
		"group_by must include 'capability' (D2C1.16) — got %v", cfg.Route.GroupBy)
}

// TestAlertmanager_SubRoutesDoNotContinue verifies that every declared
// sub-route has `continue: false` (the YAML zero value). If a future
// edit accidentally sets `continue: true` on the critical sub-route, a
// critical alert would also traverse the warning route — breaking the
// severity-tier isolation promised by §9.1.
func TestAlertmanager_SubRoutesDoNotContinue(t *testing.T) {
	cfg := loadAM(t)
	require.NotEmpty(t, cfg.Route.Routes, "at least one sub-route expected")
	for i, r := range cfg.Route.Routes {
		require.False(t, r.Continue,
			"sub-route %d (matchers=%v, receiver=%q) must have continue=false — "+
				"severity tiers must not cascade", i, r.Matchers, r.Receiver)
	}
}

func TestAlertmanager_InhibitUsesSlothSloNotAlertname(t *testing.T) {
	cfg := loadAM(t)
	require.NotEmpty(t, cfg.InhibitRules, "at least one inhibit rule expected")

	sawSlothSlo := false
	sawAlertname := false
	for _, ir := range cfg.InhibitRules {
		for _, eq := range ir.Equal {
			if eq == "sloth_slo" {
				sawSlothSlo = true
			}
			if eq == "alertname" {
				sawAlertname = true
			}
		}
	}
	require.True(t, sawSlothSlo,
		"inhibit_rules.equal must include 'sloth_slo' (D2C1.17)")
	require.False(t, sawAlertname,
		"inhibit_rules.equal must NOT include 'alertname' (D2C1.17 — that name is unstable across Sloth flavors)")
}

// TestAlertmanager_MatcherLabelsExistUpstream verifies every label
// referenced in route matchers and inhibit_rules.equal is actually
// emitted by some rule under ops/prometheus/. Prevents silent
// drift where the router expects a label that no rule produces.
func TestAlertmanager_MatcherLabelsExistUpstream(t *testing.T) {
	cfg := loadAM(t)
	repoRoot := findRepoRoot(t)

	emitted := collectUpstreamLabels(t, repoRoot)
	emitted["severity"] = true // contract — every rule emits it

	// Matcher label extraction: parse 'key="value"' or 'key=~"..."'.
	reMatcherLabel := regexp.MustCompile(`^\s*([a-zA-Z_][a-zA-Z0-9_]*)\s*(?:=|!=|=~|!~)`)

	checkMatcher := func(m string, context string) {
		matches := reMatcherLabel.FindStringSubmatch(m)
		require.NotEmpty(t, matches,
			"could not parse label from matcher %q (%s)", m, context)
		lbl := matches[1]
		require.True(t, emitted[lbl],
			"matcher label %q in %s is not emitted by any upstream rule", lbl, context)
	}

	for _, r := range cfg.Route.Routes {
		for _, m := range r.Matchers {
			checkMatcher(m, "route.routes")
		}
	}
	for _, ir := range cfg.InhibitRules {
		for _, m := range ir.SourceMatchers {
			checkMatcher(m, "inhibit_rules.source_matchers")
		}
		for _, m := range ir.TargetMatchers {
			checkMatcher(m, "inhibit_rules.target_matchers")
		}
		for _, eq := range ir.Equal {
			require.True(t, emitted[eq],
				"inhibit_rules.equal label %q is not emitted by any upstream rule", eq)
		}
	}
}

// --- helpers ---

func loadAM(t *testing.T) amConfig {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(findRepoRoot(t), "ops", "alertmanager", "alertmanager.yaml"))
	require.NoError(t, err)
	var cfg amConfig
	require.NoError(t, yaml.Unmarshal(data, &cfg))
	return cfg
}

// collectUpstreamLabels scans ops/prometheus/rules/*.yaml and
// ops/prometheus/generated/*.yaml for label keys declared on alert
// or recording rules. Intentionally lax (line-scan) to keep the test
// simple; if a stricter parse is needed later it lives here.
func collectUpstreamLabels(t *testing.T, repoRoot string) map[string]bool {
	t.Helper()
	labels := map[string]bool{}

	ruleFiles, _ := filepath.Glob(filepath.Join(repoRoot, "ops", "prometheus", "rules", "*.yaml"))
	slothFiles, _ := filepath.Glob(filepath.Join(repoRoot, "ops", "prometheus", "generated", "*.yaml"))
	all := append(ruleFiles, slothFiles...)
	require.NotEmpty(t, all, "no upstream rule files found under ops/prometheus/")

	// Walk `labels:` blocks and gather keys. A rule section looks like:
	//   - alert: ...
	//     labels:
	//       severity: critical
	//       capability: shell.exec@v1
	//       ...
	inLabelsBlock := false
	for _, f := range all {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		inLabelsBlock = false
		for _, line := range strings.Split(string(data), "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				continue
			}
			if strings.HasPrefix(trimmed, "labels:") {
				inLabelsBlock = true
				continue
			}
			// A nested block ends when we see another sibling key
			// (annotations:, expr:, alert:, record:, for:) or a dash-entry.
			if strings.HasPrefix(trimmed, "- ") ||
				strings.HasPrefix(trimmed, "annotations:") ||
				strings.HasPrefix(trimmed, "expr:") ||
				strings.HasPrefix(trimmed, "for:") ||
				strings.HasPrefix(trimmed, "alert:") ||
				strings.HasPrefix(trimmed, "record:") ||
				strings.HasPrefix(trimmed, "groups:") ||
				strings.HasPrefix(trimmed, "rules:") ||
				strings.HasPrefix(trimmed, "name:") ||
				strings.HasPrefix(trimmed, "interval:") {
				inLabelsBlock = false
				continue
			}
			if !inLabelsBlock {
				continue
			}
			parts := strings.SplitN(trimmed, ":", 2)
			if len(parts) != 2 {
				continue
			}
			key := strings.TrimSpace(parts[0])
			if key == "" {
				continue
			}
			labels[key] = true
		}
	}
	return labels
}

// findRepoRoot walks up the filesystem until it finds a directory
// containing go.mod. Same pattern as ops/slo and metrics_test.
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
