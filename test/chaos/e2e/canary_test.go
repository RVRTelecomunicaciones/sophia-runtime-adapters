//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestChaos_Canary_HttpConnectionReset is the per-PR E2E canary that
// validates the full chaos pipeline:
//
//	runtime → otel-collector → prometheus → sloth test rules
//	→ alertmanager → receiver-stub
//
// It drives ~15s of failed HTTP requests through the runtime under the
// ci-http-connection-reset chaos profile, then asserts that the
// HttpRequestAvailabilityBurn alert fires with the correct label set and
// is delivered to receiver-stub within the D2C3.21 tiered budget:
//
//	≤ 60s  → pass
//	60-90s → pass-with-warning
//	> 90s  → fail (real pipeline gap — STOP, do not modify the test)
func TestChaos_Canary_HttpConnectionReset(t *testing.T) {
	// SETUP — chaos profile path is relative to the container's mount; the
	// compose overlay mounts ops/chaos/profiles/ci at this path (D2C3.25).
	profilePath := "/etc/runtime-adapters/chaos/profiles/ci/ci-http-connection-reset.yaml"
	env := map[string]string{
		"RUNTIME_CHAOS_ENABLED": "true",
		"RUNTIME_CHAOS_PROFILE": profilePath,
		"RUNTIME_ENV":           "development",
	}

	ComposeUp(t, env)
	t.Cleanup(func() {
		if t.Failed() {
			DumpDiagnostics(t)
		}
		ComposeDown(t)
	})

	WaitForHealthy(t, 60*time.Second)

	rc := &ReceiverClient{
		BaseURL: "http://localhost:8088",
		HTTP:    &http.Client{Timeout: 10 * time.Second},
	}
	require.NoError(t, rc.Clear(context.Background()), "clear receiver-stub ring buffer")

	// EXERCISE — drive ~15s of failed http.request@v1 executions.
	// Each POST /api/v1/execute with adapter_id=http under the connection_reset
	// chaos profile returns status=failure, incrementing the error counter that
	// feeds the SLI query in test-slo-rules.yaml.
	runtimeURL := "http://localhost:8080/api/v1/execute"
	startExercise := time.Now()

	for i := 0; i < 30; i++ {
		correlationID := ulid.Make().String()
		body, err := json.Marshal(map[string]any{
			"correlation_id":     correlationID,
			"adapter_id":         "http",
			"capability_name":    "request",
			"capability_version": "v1",
			// payload is the raw JSON for the HTTP adapter request
			"payload": json.RawMessage(`{"method":"GET","url":"https://example.invalid/","headers":{},"expected_status":[200]}`),
			"timeout_budget_ms": 1000,
		})
		require.NoError(t, err, "marshal execution request")

		resp, err := http.Post(runtimeURL, "application/json", bytes.NewReader(body)) //nolint:noctx
		if err != nil {
			t.Logf("POST %s error (iteration %d): %v", runtimeURL, i, err)
		} else {
			resp.Body.Close()
			t.Logf("POST %s status=%d (iteration %d)", runtimeURL, resp.StatusCode, i)
		}
		time.Sleep(500 * time.Millisecond)
	}

	t.Logf("exercise phase: %d requests over %s", 30, time.Since(startExercise).Round(time.Millisecond))

	// ASSERT — wait for the alert to fire within budget.
	sloName := readSLOName(t)
	t.Logf("expecting sloth_slo=%q", sloName)

	expected := ExpectedAlert{
		AlertName:  "HttpRequestAvailabilityBurn",
		Capability: "http.request@v1",
		Severity:   "critical",
		SLOName:    sloName,
	}
	target := 60 * time.Second
	deadline := 90 * time.Second

	a, fireLatency, breached, err := rc.WaitForAlert(
		context.Background(),
		expected,
		target,
		deadline,
	)
	require.NoErrorf(t, err,
		"alert not delivered to receiver-stub; exercise started at %s; "+
			"check: (1) runtime metrics visible in Prometheus, "+
			"(2) test-slo-rules.yaml recording rules evaluate, "+
			"(3) alertmanager routing matches, "+
			"(4) receiver-stub reachable from alertmanager",
		startExercise.Format(time.RFC3339),
	)

	labels := labelsFromPayload(a)
	require.NotEmpty(t, labels, "received AlertPayload has no decodable alert labels: body=%s", a.Body)
	t.Logf("received alert labels: %s", labelsString(labels))

	if breached {
		t.Logf("WARNING: fire_latency=%s exceeded target=%s but within deadline=%s (D2C3.21 pass-with-warning)",
			fireLatency.Round(time.Millisecond), target, deadline)
	} else {
		t.Logf("OK: fire_latency=%s (target=%s)", fireLatency.Round(time.Millisecond), target)
	}
}

// readSLOName extracts the sloth_slo label value Sloth assigns to the
// http-request-availability SLO from the rendered test rules YAML.
// Coupling the test to the live rendered output catches Sloth bumps that
// rename recordings (D2C3.29).
func readSLOName(t *testing.T) string {
	t.Helper()

	root := findRepoRoot(t)
	rulesPath := filepath.Join(root, "ops", "prometheus", "generated", "test-slo-rules.yaml")
	data, err := os.ReadFile(rulesPath)
	if err != nil {
		t.Fatalf("readSLOName: read %s: %v", rulesPath, err)
	}

	// The rendered YAML is a multi-document stream (one `---` per adapter file
	// concatenated by `cat`).  We need to find any recording rule that has
	// sloth_slo=http-request-availability in its labels block.
	//
	// Structure we're looking for:
	//   groups:
	//   - name: sloth-slo-sli-recordings-runtime-adapters-chaos-test-http-request-availability
	//     rules:
	//     - record: slo:sli_error:ratio_rate1m
	//       labels:
	//         sloth_slo: http-request-availability
	//
	// We decode the full doc as a generic YAML structure and walk it.
	type ruleYAML struct {
		Record string            `yaml:"record"`
		Alert  string            `yaml:"alert"`
		Labels map[string]string `yaml:"labels"`
	}
	type groupYAML struct {
		Name  string      `yaml:"name"`
		Rules []ruleYAML  `yaml:"rules"`
	}
	type docYAML struct {
		Groups []groupYAML `yaml:"groups"`
	}

	dec := yaml.NewDecoder(bytes.NewReader(data))
	for {
		var doc docYAML
		if err := dec.Decode(&doc); err != nil {
			break
		}
		for _, g := range doc.Groups {
			for _, r := range g.Rules {
				if v, ok := r.Labels["sloth_slo"]; ok && v == "http-request-availability" {
					// Confirm it belongs to the chaos-test service by checking
					// sloth_service label to avoid any accidental match with
					// prod rules if they were ever loaded in the same file.
					if svc, ok := r.Labels["sloth_service"]; ok && svc == "runtime-adapters-chaos-test" {
						return v
					}
				}
			}
		}
	}

	t.Fatalf("readSLOName: could not find sloth_slo label for "+
		"http-request-availability in %s; run `make chaos-render-rules` first", rulesPath)
	return ""
}

// labelsFromPayload decodes the first AMAlert labels from an AlertPayload.
// Exported for use in log lines.
func labelsFromPayload(p AlertPayload) map[string]string {
	if len(p.Body) == 0 {
		return nil
	}
	var wh AMWebhookPayload
	if err := json.Unmarshal(p.Body, &wh); err != nil {
		return nil
	}
	if len(wh.Alerts) == 0 {
		return nil
	}
	return wh.Alerts[0].Labels
}

// labelsString returns a short human-readable representation of label map.
func labelsString(labels map[string]string) string {
	if labels == nil {
		return "{}"
	}
	b, _ := json.Marshal(labels)
	return fmt.Sprintf("%s", b)
}
