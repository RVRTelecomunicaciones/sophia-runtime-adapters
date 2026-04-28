//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// AlertPayload mirrors the shape that receiver-stub stores.
// The stub records the raw Alertmanager webhook body + a server-side
// received timestamp. See ops/chaos/receiver/main.go.
type AlertPayload struct {
	Received time.Time       `json:"received"`
	Body     json.RawMessage `json:"body"`
}

// AMWebhookPayload is the Alertmanager webhook POST body shape.
// We only decode the fields relevant to label inspection.
type AMWebhookPayload struct {
	Alerts []AMAlert `json:"alerts"`
}

// AMAlert is one alert within an Alertmanager webhook payload.
type AMAlert struct {
	Labels map[string]string `json:"labels"`
}

// ExpectedAlert captures the label set the canary asserts must be present.
type ExpectedAlert struct {
	AlertName  string
	Capability string
	Severity   string
	SLOName    string // sloth_slo label value
}

// ReceiverClient is a polling client for the receiver-stub HTTP API.
// BaseURL is the base URL of the receiver-stub (e.g. http://localhost:8088).
type ReceiverClient struct {
	BaseURL string
	HTTP    *http.Client
}

// WaitForAlert polls /inspect every 2s until a matching alert is received
// or deadline elapses.  Returns the matching AlertPayload, the fire latency
// measured from the moment WaitForAlert is called, whether the latency
// exceeded target (pass-with-warning per D2C3.21), and any error.
//
// Latency bands per D2C3.21:
//
//	≤ target          → pass (breached=false)
//	target < t ≤ deadline → pass-with-warning (breached=true, err=nil)
//	> deadline         → fail (err != nil)
func (c *ReceiverClient) WaitForAlert(
	ctx context.Context,
	expected ExpectedAlert,
	target time.Duration,
	deadline time.Duration,
) (AlertPayload, time.Duration, bool, error) {
	start := time.Now()
	deadlineAt := start.Add(deadline)
	const pollEvery = 2 * time.Second

	for {
		if time.Now().After(deadlineAt) {
			elapsed := time.Since(start)
			return AlertPayload{}, elapsed, false, fmt.Errorf(
				"alert %q not received after %s (deadline=%s): "+
					"check Prometheus rules, Alertmanager routing, or receiver-stub connectivity",
				expected.AlertName, elapsed.Round(time.Second), deadline,
			)
		}

		alerts, err := c.AlertsMatching(ctx, expected)
		if err == nil && len(alerts) > 0 {
			latency := time.Since(start)
			return alerts[0], latency, latency > target, nil
		}

		select {
		case <-ctx.Done():
			return AlertPayload{}, time.Since(start), false, ctx.Err()
		case <-time.After(pollEvery):
		}
	}
}

// ListAlerts returns all AlertPayload entries from /inspect.
func (c *ReceiverClient) ListAlerts(ctx context.Context) ([]AlertPayload, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/inspect", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET /inspect: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /inspect: unexpected status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read /inspect body: %w", err)
	}
	var envelope struct {
		Received []AlertPayload `json:"received"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("decode /inspect response: %w", err)
	}
	return envelope.Received, nil
}

// AlertsMatchingSince returns all AlertPayload entries from /inspect?since=<t>
// that contain at least one AMAlert matching exp.
func (c *ReceiverClient) AlertsMatchingSince(ctx context.Context, exp ExpectedAlert, since time.Time) ([]AlertPayload, error) {
	u := c.BaseURL + "/inspect"
	if !since.IsZero() {
		u += "?since=" + url.QueryEscape(since.UTC().Format(time.RFC3339))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET /inspect: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GET /inspect: status %d body=%s", resp.StatusCode, b)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read /inspect body: %w", err)
	}
	var envelope struct {
		Received []AlertPayload `json:"received"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("decode /inspect response: %w", err)
	}
	var matched []AlertPayload
	for _, p := range envelope.Received {
		if payloadMatches(p, exp) {
			matched = append(matched, p)
		}
	}
	return matched, nil
}

// AlertsMatching returns all AlertPayload entries from /inspect that contain
// at least one AMAlert matching exp.
func (c *ReceiverClient) AlertsMatching(ctx context.Context, exp ExpectedAlert) ([]AlertPayload, error) {
	return c.AlertsMatchingSince(ctx, exp, time.Time{})
}

// Clear deletes all entries in the receiver-stub ring buffer via DELETE /alerts.
func (c *ReceiverClient) Clear(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.BaseURL+"/alerts", nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("DELETE /alerts: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("DELETE /alerts: unexpected status %d", resp.StatusCode)
	}
	return nil
}

// payloadMatches returns true if the AlertPayload body contains at least one
// AMAlert whose labels satisfy exp.  All non-empty fields of exp must match.
func payloadMatches(p AlertPayload, exp ExpectedAlert) bool {
	if len(p.Body) == 0 {
		return false
	}
	var wh AMWebhookPayload
	if err := json.Unmarshal(p.Body, &wh); err != nil {
		return false
	}
	for _, a := range wh.Alerts {
		if alertMatches(a, exp) {
			return true
		}
	}
	return false
}

// alertMatches checks whether an individual AMAlert satisfies exp.
// All non-empty fields in exp are checked with exact equality.
func alertMatches(a AMAlert, exp ExpectedAlert) bool {
	if exp.AlertName != "" && a.Labels["alertname"] != exp.AlertName {
		return false
	}
	if exp.Capability != "" && a.Labels["capability"] != exp.Capability {
		return false
	}
	if exp.Severity != "" && a.Labels["severity"] != exp.Severity {
		return false
	}
	if exp.SLOName != "" && a.Labels["sloth_slo"] != exp.SLOName {
		return false
	}
	return true
}
