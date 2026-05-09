package grafana_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sophia-ecosystem/runtime-adapters/internal/integrations/grafana"
)

func TestMapper_FiringSingleAlert(t *testing.T) {
	startsAt := time.Date(2026, 5, 8, 14, 23, 0, 0, time.UTC)
	payload := grafana.AlertmanagerWebhookV4{
		Status: "firing",
		Alerts: []grafana.AlertmanagerAlertV4{{
			Status:      "firing",
			StartsAt:    startsAt,
			Labels:      map[string]string{"alertname": "FastBurn", "severity": "critical"},
			Annotations: map[string]string{"summary": "burn rate elevated"},
		}},
	}

	got := grafana.MapAlertmanagerWebhookToAnnotations(payload)
	if len(got) != 1 {
		t.Fatalf("len: got %d, want 1", len(got))
	}
	if got[0].Time != startsAt.UnixMilli() {
		t.Errorf("Time: got %d, want %d", got[0].Time, startsAt.UnixMilli())
	}
	if got[0].TimeEnd != 0 {
		t.Errorf("TimeEnd: got %d, want 0 (no endsAt for firing alert)", got[0].TimeEnd)
	}
	if got[0].Text != "[CRIT] FastBurn — burn rate elevated" {
		t.Errorf("Text: got %q, want %q", got[0].Text, "[CRIT] FastBurn — burn rate elevated")
	}
	want := []string{"FastBurn", "severity:critical", "status:firing", "source:alertmanager"}
	if !reflect.DeepEqual(got[0].Tags, want) {
		t.Errorf("Tags: got %v, want %v", got[0].Tags, want)
	}
}

func TestMapper_ResolvedSingleAlert(t *testing.T) {
	startsAt := time.Date(2026, 5, 8, 14, 23, 0, 0, time.UTC)
	endsAt := startsAt.Add(5 * time.Minute)
	payload := grafana.AlertmanagerWebhookV4{
		Status: "resolved",
		Alerts: []grafana.AlertmanagerAlertV4{{
			Status:      "resolved",
			StartsAt:    startsAt,
			EndsAt:      endsAt,
			Labels:      map[string]string{"alertname": "FastBurn", "severity": "warning"},
			Annotations: map[string]string{"summary": "ok now"},
		}},
	}

	got := grafana.MapAlertmanagerWebhookToAnnotations(payload)
	if len(got) != 1 {
		t.Fatalf("len: got %d, want 1", len(got))
	}
	if got[0].Time != startsAt.UnixMilli() {
		t.Errorf("Time: got %d, want %d", got[0].Time, startsAt.UnixMilli())
	}
	if got[0].TimeEnd != endsAt.UnixMilli() {
		t.Errorf("TimeEnd: got %d, want %d", got[0].TimeEnd, endsAt.UnixMilli())
	}
	if got[0].Text != "[WARN] FastBurn — ok now" {
		t.Errorf("Text: got %q", got[0].Text)
	}
	want := []string{"FastBurn", "severity:warning", "status:resolved", "source:alertmanager"}
	if !reflect.DeepEqual(got[0].Tags, want) {
		t.Errorf("Tags: got %v, want %v", got[0].Tags, want)
	}
}

func TestMapper_MultipleAlertsInGroup(t *testing.T) {
	now := time.Now().UTC()
	payload := grafana.AlertmanagerWebhookV4{
		Status: "firing",
		Alerts: []grafana.AlertmanagerAlertV4{
			{Status: "firing", StartsAt: now, Labels: map[string]string{"alertname": "A", "severity": "critical"}},
			{Status: "firing", StartsAt: now, Labels: map[string]string{"alertname": "B", "severity": "warning"}},
			{Status: "firing", StartsAt: now, Labels: map[string]string{"alertname": "C", "severity": "critical"}},
		},
	}
	got := grafana.MapAlertmanagerWebhookToAnnotations(payload)
	if len(got) != 3 {
		t.Errorf("len: got %d, want 3 (one annotation per alert per D2C4D.15)", len(got))
	}
}

func TestMapper_MissingOptionalFields_DefaultsApplied(t *testing.T) {
	// No annotations.summary; severity unknown → falls back to UPPER(severity).
	payload := grafana.AlertmanagerWebhookV4{
		Status: "firing",
		Alerts: []grafana.AlertmanagerAlertV4{{
			Status:   "firing",
			StartsAt: time.Now().UTC(),
			Labels:   map[string]string{"alertname": "MissingSummary", "severity": "info"},
		}},
	}
	got := grafana.MapAlertmanagerWebhookToAnnotations(payload)
	if len(got) != 1 {
		t.Fatalf("len: got %d, want 1", len(got))
	}
	if !strings.Contains(got[0].Text, "MissingSummary") {
		t.Errorf("Text must contain alertname even without summary; got %q", got[0].Text)
	}
}

func TestMapper_TagsAreDeterministicOrder(t *testing.T) {
	now := time.Now().UTC()
	payload := grafana.AlertmanagerWebhookV4{
		Status: "firing",
		Alerts: []grafana.AlertmanagerAlertV4{{
			Status:   "firing",
			StartsAt: now,
			Labels:   map[string]string{"alertname": "Z", "severity": "critical"},
		}},
	}
	got1 := grafana.MapAlertmanagerWebhookToAnnotations(payload)
	got2 := grafana.MapAlertmanagerWebhookToAnnotations(payload)
	if !reflect.DeepEqual(got1[0].Tags, got2[0].Tags) {
		t.Errorf("Tags must be deterministic; got1=%v got2=%v", got1[0].Tags, got2[0].Tags)
	}
}

// TestMapper_FiringEndsAtIgnoredEvenWhenNonZero guards against the
// alertmanager quirk where firing alerts carry a far-future EndsAt.
// We must NOT treat that as a real range end.
func TestMapper_FiringEndsAtIgnoredEvenWhenNonZero(t *testing.T) {
	startsAt := time.Date(2026, 5, 8, 14, 0, 0, 0, time.UTC)
	endsAt := startsAt.Add(72 * time.Hour) // far-future, alertmanager-style
	payload := grafana.AlertmanagerWebhookV4{
		Status: "firing",
		Alerts: []grafana.AlertmanagerAlertV4{{
			Status:   "firing",
			StartsAt: startsAt,
			EndsAt:   endsAt,
			Labels:   map[string]string{"alertname": "X", "severity": "critical"},
		}},
	}
	got := grafana.MapAlertmanagerWebhookToAnnotations(payload)
	if got[0].TimeEnd != 0 {
		t.Errorf("TimeEnd: got %d, want 0 (firing alerts must NOT carry timeEnd)", got[0].TimeEnd)
	}
}
