package grafana

import (
	"fmt"
	"strings"
)

// MapAlertmanagerWebhookToAnnotations transforms one alertmanager v4
// webhook payload into a slice of AnnotationRequest — one per alert
// in payload.alerts[] (V1 scope per D2C4D.15: stateless, append-only,
// no dedup).
//
// Mapping rules per spec §3.2:
//   - Time:    alert.StartsAt.UnixMilli()
//   - TimeEnd: alert.EndsAt.UnixMilli() if non-zero AND alert is resolved.
//     Alertmanager sets EndsAt to a far-future time on firing
//     alerts; we treat that as zero (no range end).
//   - Text:    "[<SEVERITY-PREFIX>] <alertname> — <summary>"
//   - Tags:    [alertname, "severity:<value>", "status:<firing|resolved>", "source:alertmanager"]
//
// Tags are emitted in fixed order (deterministic so tests + LogQL
// queries can rely on the shape).
//
// V1 explicitly omits DashboardUID / PanelID — annotations are global
// (D2C4D.14). Tag-based filtering supported via Grafana
// GET /api/annotations?tags=<value>.
func MapAlertmanagerWebhookToAnnotations(payload AlertmanagerWebhookV4) []AnnotationRequest {
	out := make([]AnnotationRequest, 0, len(payload.Alerts))
	for _, a := range payload.Alerts {
		alertname := a.Labels["alertname"]
		severity := a.Labels["severity"]
		summary := a.Annotations["summary"]
		out = append(out, AnnotationRequest{
			Time:    a.StartsAt.UnixMilli(),
			TimeEnd: zeroOrUnixMilli(a),
			Text:    formatText(alertname, severity, summary),
			Tags: []string{
				alertname,
				"severity:" + severity,
				"status:" + a.Status,
				"source:alertmanager",
			},
		})
	}
	return out
}

// zeroOrUnixMilli returns alert.EndsAt as a UnixMilli ONLY when the
// alert is resolved AND EndsAt is non-zero. Alertmanager sets EndsAt
// to a far-future time for firing alerts; we treat that as zero so
// the annotation is a point-in-time marker, not a range that would
// confuse the dashboard renderer.
func zeroOrUnixMilli(a AlertmanagerAlertV4) int64 {
	if a.EndsAt.IsZero() {
		return 0
	}
	if a.Status != "resolved" {
		return 0
	}
	return a.EndsAt.UnixMilli()
}

// formatText builds the annotation text. Severity is mapped to a short
// prefix (CRIT / WARN / INFO) for readability; unknown severities are
// uppercased verbatim so the operator notices.
func formatText(alertname, severity, summary string) string {
	prefix := severityPrefix(severity)
	if summary == "" {
		return fmt.Sprintf("[%s] %s", prefix, alertname)
	}
	return fmt.Sprintf("[%s] %s — %s", prefix, alertname, summary)
}

// severityPrefix maps known severity values to short prefixes; unknown
// values pass through uppercased.
func severityPrefix(severity string) string {
	switch strings.ToLower(severity) {
	case "critical":
		return "CRIT"
	case "warning":
		return "WARN"
	case "info":
		return "INFO"
	default:
		return strings.ToUpper(severity)
	}
}
