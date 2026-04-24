//go:build loadreport

// Package calibrationreports_test validates committed calibration
// artifacts against the schema contracts in spec §8.
//
// Pre-first-calibration state (D2C2.14): both tests skip when the
// artifact is absent so the 2C.2 bundle that introduces them doesn't
// fail its own CI. After the first commit of latest-baseline.json +
// YYYY-MM-DD-baseline-v1.md, absence becomes a failure.
//
// Run: go test -tags loadreport ./ops/slo/calibration-reports/...
package calibrationreports_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// ---- TestLatestBaseline_Schema -----------------------------------------

type latestBaseline struct {
	GeneratedAt              string                        `json:"generated_at"`
	FromReport               string                        `json:"from_report"`
	EnvelopeManifest         string                        `json:"envelope_manifest"`
	ComparisonContext        string                        `json:"comparison_context"`
	RunnerClassBaseline      string                        `json:"runner_class_baseline"`
	RunnerClassSmokeExpected string                        `json:"runner_class_smoke_expected"`
	Core                     map[string]latestBaselineCore `json:"core"`
}
type latestBaselineCore struct {
	P50Ms float64 `json:"p50_ms"`
	P95Ms float64 `json:"p95_ms"`
	P99Ms float64 `json:"p99_ms"`
}

func TestLatestBaseline_Schema(t *testing.T) {
	repoRoot := findRepoRoot(t)
	path := filepath.Join(repoRoot, "ops", "slo", "calibration-reports", "latest-baseline.json")

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skip("latest-baseline.json not yet committed — 2C.2 first calibration pending (D2C2.14)")
	}

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var b latestBaseline
	require.NoError(t, json.Unmarshal(data, &b), "latest-baseline.json must parse as JSON")

	// Required root fields.
	require.NotEmpty(t, b.GeneratedAt, "generated_at required")
	require.NotEmpty(t, b.FromReport, "from_report required")
	require.NotEmpty(t, b.EnvelopeManifest, "envelope_manifest required")
	require.NotEmpty(t, b.ComparisonContext, "comparison_context required (documents envelope mismatch)")
	require.NotEmpty(t, b.RunnerClassBaseline, "runner_class_baseline required")
	require.NotEmpty(t, b.RunnerClassSmokeExpected, "runner_class_smoke_expected required")

	// Core capabilities — must include exactly the 4 core caps from D2C2.1.
	required := []string{
		"shell.exec@v1",
		"filesystem.read_file@v1",
		"filesystem.write_file@v1",
		"http.request@v1",
	}
	for _, cap := range required {
		core, ok := b.Core[cap]
		require.True(t, ok, "core.%q required", cap)
		require.NotZero(t, core.P50Ms, "core.%q.p50_ms should be positive", cap)
		require.NotZero(t, core.P95Ms, "core.%q.p95_ms should be positive", cap)
		require.NotZero(t, core.P99Ms, "core.%q.p99_ms should be positive", cap)
	}
}

// ---- TestCalibrationReport_Structure -----------------------------------

// TestCalibrationReport_Structure checks that every committed report
// has the 6 mandatory top-level sections (§8.2).
func TestCalibrationReport_Structure(t *testing.T) {
	repoRoot := findRepoRoot(t)
	reportsDir := filepath.Join(repoRoot, "ops", "slo", "calibration-reports")

	matches, err := filepath.Glob(filepath.Join(reportsDir, "*-baseline-v*.md"))
	require.NoError(t, err)

	if len(matches) == 0 {
		t.Skip("no calibration reports yet — 2C.2 first calibration pending (D2C2.14)")
	}

	mandatorySections := []string{
		`(?m)^# Calibration report`,
		`(?m)^## Envelope`,
		`(?m)^## Core tier`,
		`(?m)^## Git smoke tier`,
		`(?m)^## Git rough tier`,
		`(?m)^## Summary`,
	}

	for _, report := range matches {
		t.Run(filepath.Base(report), func(t *testing.T) {
			data, err := os.ReadFile(report)
			require.NoError(t, err)
			content := string(data)
			for _, section := range mandatorySections {
				re := regexp.MustCompile(section)
				require.True(t, re.MatchString(content),
					"missing required section matching %q", section)
			}
			// Sanity: no leftover template placeholders.
			require.False(t, strings.Contains(content, "{{"),
				"report has un-substituted {{...}} placeholders")
		})
	}
}

// TestCalibrationReport_TemplateRenderable ensures the template file
// exists and contains the expected placeholders (catches template
// drift vs generate-report.sh).
func TestCalibrationReport_TemplateRenderable(t *testing.T) {
	repoRoot := findRepoRoot(t)
	tmpl := filepath.Join(repoRoot, "ops", "load", "lib", "report-template.md.tmpl")

	data, err := os.ReadFile(tmpl)
	require.NoError(t, err, "template must exist")
	content := string(data)

	// Every placeholder that generate-report.sh expects to substitute.
	required := []string{
		"{{VERSION}}", "{{DATE_UTC}}", "{{GIT_SHA}}", "{{GIT_AUTHOR}}",
		"{{HOST_MACHINE}}", "{{HOST_OS}}", "{{DOCKER_VERSION}}",
		"{{COLLECTOR_VERSION}}", "{{K6_VERSION}}", "{{SLOTH_VERSION}}",
		"{{PROMTOOL_VERSION}}",
		"{{CORE_TIER_SECTIONS}}", "{{GIT_STATUS_TABLES}}",
		"{{GIT_ROUGH_TABLES}}", "{{SUMMARY_ROWS}}",
		"{{CGROUP_VERIFICATION}}", "{{EVIDENCE_DIR}}",
	}
	for _, p := range required {
		require.Contains(t, content, p,
			"template missing required placeholder %q (generate-report.sh expects it)", p)
	}
}

// ---- helper ------------------------------------------------------------

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
