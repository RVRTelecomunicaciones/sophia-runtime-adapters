// Package grafana implements the small annotations-webhook adapter.
// Layout is flat (D2C4D.7) — no domain/ports/application/infrastructure
// subdirs because Grafana annotations have no domain logic worth
// pretending exists (append-only, no lifecycle, no dedup, no state).
package grafana

import "time"

// AnnotationRequest is the body posted to Grafana's
// POST /api/annotations endpoint. V1 always emits global annotations
// (D2C4D.14): DashboardUID and PanelID stay zero so Grafana renders
// the annotation across every dashboard.
type AnnotationRequest struct {
	// Time is the annotation's primary timestamp in UTC milliseconds
	// since epoch. Mapped from alertmanager's startsAt.
	Time int64 `json:"time"`

	// TimeEnd, when non-zero, marks the annotation as a range. Mapped
	// from alertmanager's endsAt when the alert is resolved.
	TimeEnd int64 `json:"timeEnd,omitempty"`

	// Text is the annotation body. Format: "[CRIT|WARN] <alertname> — <summary>".
	Text string `json:"text"`

	// Tags filter / categorize the annotation in dashboards. V1 emits:
	//   [alertname, "severity:<value>", "status:<firing|resolved>",
	//    "source:alertmanager"].
	Tags []string `json:"tags,omitempty"`
}

// AlertmanagerWebhookV4 is the relevant subset of alertmanager's
// webhook payload schema (https://prometheus.io/docs/alerting/latest/notifications/).
// Fields we don't read are intentionally omitted to keep the parse
// surface tight.
type AlertmanagerWebhookV4 struct {
	Receiver string                `json:"receiver"`
	Status   string                `json:"status"`
	Alerts   []AlertmanagerAlertV4 `json:"alerts"`
	GroupKey string                `json:"groupKey"`
}

// AlertmanagerAlertV4 carries the per-alert fields the mapper consumes.
type AlertmanagerAlertV4 struct {
	Status      string            `json:"status"` // "firing" | "resolved"
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	StartsAt    time.Time         `json:"startsAt"`
	EndsAt      time.Time         `json:"endsAt"`
	Fingerprint string            `json:"fingerprint"`
}
