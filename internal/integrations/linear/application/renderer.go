// Package application holds the Linear webhook adapter's use cases:
// the HTTP webhook handler, the firing/resolved lifecycle, the
// renderer that builds Linear issue title/body/labels from
// Alertmanager payloads, and the anti-spam logic. Per D2C4AB.5.
package application

import (
	"fmt"
	"strings"
	"time"

	"github.com/sophia-ecosystem/runtime-adapters/internal/integrations/linear/domain"
)

// RenderInput is the projection of an Alertmanager webhook payload
// the renderer needs. The webhook handler builds this from the
// raw payload before calling BuildBody / BuildTitle / BuildLabels.
type RenderInput struct {
	Alertname    string
	Severity     domain.Severity
	Capability   string // empty if the alert grouping has no capability label
	FirstFiredAt time.Time
	LastUpdate   time.Time
	ActiveCount  int
	Summary      string
	Description  string
	ExternalURL  string
	Runbook      string // optional — empty means no runbook link
	Dashboard    string // optional
	GroupKey     string // raw Alertmanager groupKey for the debug HTML comment
}

// BuildTitle renders the Linear issue title per spec §7.4.
//
// Format:
//
//	"[CRIT] <alertname>" or "[WARN] <alertname>" if capability empty
//	"[CRIT] <alertname> — <capability>" or "[WARN] <alertname> — <capability>" if capability present
//
// The em-dash (—) separator matches the spec exactly. Linear titles
// have no length limit relevant here (~256 char max in practice).
func BuildTitle(sev domain.Severity, alertname, capability string) string {
	prefix := "[WARN]"
	if sev == domain.SeverityCritical {
		prefix = "[CRIT]"
	}
	if capability == "" {
		return fmt.Sprintf("%s %s", prefix, alertname)
	}
	return fmt.Sprintf("%s %s — %s", prefix, alertname, capability)
}

// BuildLabels returns the set of label NAMES applied at issue
// creation per spec §7.4. Order:
//  1. domain.DedupLabelConst ("alert-managed") — constant marker
//  2. domain.DedupLabel(groupKey)              — per-grouping dedup key
//  3. "severity:<critical|warning>"
//  4. "capability:<value>"                     — only if capability != ""
func BuildLabels(sev domain.Severity, capability, groupKey string) []string {
	out := []string{
		domain.DedupLabelConst,
		domain.DedupLabel(groupKey),
		"severity:" + string(sev),
	}
	if capability != "" {
		out = append(out, "capability:"+capability)
	}
	return out
}

// BuildBody renders the Markdown body per spec §7.4. Includes the
// debug HTML-comment metadata block at the bottom (groupKey +
// dedup_label) — debug-only; matching uses the LABEL not the
// comment per A2C4AB.3.3.
func BuildBody(in RenderInput) string {
	var b strings.Builder
	fmt.Fprintf(&b, "**Alert:** %s\n", in.Alertname)
	fmt.Fprintf(&b, "**Severity:** %s\n", in.Severity)
	if in.Capability != "" {
		fmt.Fprintf(&b, "**Capability:** %s\n", in.Capability)
	}
	fmt.Fprintf(&b, "**First fired:** %s\n", in.FirstFiredAt.UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "**Last update:** %s\n", in.LastUpdate.UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "**Active alerts in group:** %d\n\n", in.ActiveCount)

	b.WriteString("## Summary\n")
	fmt.Fprintf(&b, "> %s\n\n", in.Summary)

	if in.Description != "" {
		b.WriteString("## Description\n")
		fmt.Fprintf(&b, "> %s\n\n", in.Description)
	}

	b.WriteString("## Links\n")
	fmt.Fprintf(&b, "- [Alertmanager view](%s)\n", in.ExternalURL)
	if in.Runbook != "" {
		fmt.Fprintf(&b, "- [Runbook](%s)\n", in.Runbook)
	}
	if in.Dashboard != "" {
		fmt.Fprintf(&b, "- [Dashboard](%s)\n", in.Dashboard)
	}

	b.WriteString("\n---\n\n")
	b.WriteString("<!-- linear-webhook-adapter dedup metadata. Debug only. Do not edit. -->\n")
	fmt.Fprintf(&b, "<!-- groupKey: %s -->\n", in.GroupKey)
	fmt.Fprintf(&b, "<!-- dedup_label: %s -->\n", domain.DedupLabel(in.GroupKey))
	return b.String()
}
