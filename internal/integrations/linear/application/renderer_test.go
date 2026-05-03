package application_test

import (
	"strings"
	"testing"
	"time"

	"github.com/sophia-ecosystem/runtime-adapters/internal/integrations/linear/application"
	"github.com/sophia-ecosystem/runtime-adapters/internal/integrations/linear/domain"
)

func TestBuildTitle_CriticalWithCapability(t *testing.T) {
	got := application.BuildTitle(domain.SeverityCritical, "ShellExecLatencyBurnRate", "shell.exec@v1")
	want := "[CRIT] ShellExecLatencyBurnRate — shell.exec@v1"
	if got != want {
		t.Errorf("BuildTitle = %q, want %q", got, want)
	}
}

func TestBuildTitle_WarningWithoutCapability(t *testing.T) {
	got := application.BuildTitle(domain.SeverityWarning, "PoolIdleZero", "")
	want := "[WARN] PoolIdleZero"
	if got != want {
		t.Errorf("BuildTitle = %q, want %q", got, want)
	}
}

func TestBuildLabels_AllPresent(t *testing.T) {
	gk := "test-group-key"
	got := application.BuildLabels(domain.SeverityCritical, "shell.exec@v1", gk)
	want := []string{
		domain.DedupLabelConst,
		domain.DedupLabel(gk),
		"severity:critical",
		"capability:shell.exec@v1",
	}
	if len(got) != len(want) {
		t.Fatalf("BuildLabels len = %d, want %d (got %v)", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("BuildLabels[%d] = %q, want %q", i, got[i], w)
		}
	}
}

func TestBuildLabels_NoCapability_ThreeLabels(t *testing.T) {
	got := application.BuildLabels(domain.SeverityWarning, "", "gk")
	if len(got) != 3 {
		t.Errorf("BuildLabels with empty capability len = %d, want 3 (got %v)", len(got), got)
	}
}

func TestBuildBody_ContainsRequiredSections(t *testing.T) {
	in := application.RenderInput{
		Alertname:    "ShellExecLatencyBurnRate",
		Severity:     domain.SeverityCritical,
		Capability:   "shell.exec@v1",
		FirstFiredAt: time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC),
		LastUpdate:   time.Date(2026, 5, 2, 12, 5, 0, 0, time.UTC),
		ActiveCount:  3,
		Summary:      "Burn rate exceeded for shell.exec@v1",
		Description:  "Detected fast burn",
		ExternalURL:  "http://am.example/#/alerts/abc",
		Runbook:      "https://runbooks/shell-burn",
		Dashboard:    "",
		GroupKey:     "gk-12345",
	}
	got := application.BuildBody(in)
	required := []string{
		"**Alert:** ShellExecLatencyBurnRate",
		"**Severity:** critical",
		"**Capability:** shell.exec@v1",
		"**Active alerts in group:** 3",
		"## Summary",
		"Burn rate exceeded for shell.exec@v1",
		"## Description",
		"Detected fast burn",
		"## Links",
		"http://am.example/#/alerts/abc",
		"https://runbooks/shell-burn",
		"<!-- linear-webhook-adapter dedup metadata. Debug only. Do not edit. -->",
		"<!-- groupKey: gk-12345 -->",
		"<!-- dedup_label: " + domain.DedupLabel("gk-12345") + " -->",
	}
	for _, want := range required {
		if !strings.Contains(got, want) {
			t.Errorf("BuildBody missing %q\n--- got ---\n%s", want, got)
		}
	}
}

func TestBuildBody_OmitsEmptyDashboardLink(t *testing.T) {
	in := application.RenderInput{
		Alertname:    "X",
		Severity:     domain.SeverityWarning,
		FirstFiredAt: time.Now().UTC(),
		LastUpdate:   time.Now().UTC(),
		Summary:      "s",
		ExternalURL:  "u",
		GroupKey:     "g",
	}
	got := application.BuildBody(in)
	if strings.Contains(got, "[Dashboard]") {
		t.Errorf("BuildBody should NOT include Dashboard link when empty\n%s", got)
	}
}
