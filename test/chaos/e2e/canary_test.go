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
	"sync"
	"sync/atomic"
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

	// EXERCISE — drive failed http.request@v1 executions CONTINUOUSLY for
	// the full duration of the assertion phase (≈80s).
	//
	// Why sustained, not a one-shot burst: the runtime's OTel SDK uses a 30s
	// PeriodicReader (internal/infrastructure/obs/otel.go), so a 15s burst
	// counter increments only show up in Prometheus as a single flat plateau —
	// rate(counter[1m]) over a flat plateau is 0, ratio = 0/0 = NaN, and
	// no burn-rate alert can ever fire. Sustained traffic across multiple
	// 30s exports lets rate() see real per-second deltas. This also matches
	// the production semantics of multi-window burn-rate alerts: they're
	// designed to fire on sustained failure, not on 15s blips.
	runtimeURL := "http://localhost:8080/api/v1/execute"
	startExercise := time.Now()

	exerciseCtx, cancelExercise := context.WithCancel(context.Background())
	defer cancelExercise()

	var requestCount int64
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-exerciseCtx.Done():
				return
			case <-ticker.C:
				correlationID := ulid.Make().String()
				body, err := json.Marshal(map[string]any{
					"correlation_id":     correlationID,
					"adapter_id":         "http",
					"capability_name":    "request",
					"capability_version": "v1",
					"payload":            json.RawMessage(`{"method":"GET","url":"https://example.invalid/","headers":{},"expected_status":[200]}`),
					"timeout_budget_ms":  1000,
				})
				if err != nil {
					t.Logf("marshal execution request: %v", err)
					continue
				}
				req, err := http.NewRequestWithContext(exerciseCtx, http.MethodPost, runtimeURL, bytes.NewReader(body))
				if err != nil {
					t.Logf("build request: %v", err)
					continue
				}
				req.Header.Set("Content-Type", "application/json")
				resp, err := http.DefaultClient.Do(req)
				n := atomic.AddInt64(&requestCount, 1)
				if err != nil {
					if exerciseCtx.Err() == nil {
						t.Logf("POST %s error (iteration %d): %v", runtimeURL, n, err)
					}
					continue
				}
				resp.Body.Close()
				if n%10 == 0 {
					t.Logf("POST %s status=%d (iteration %d)", runtimeURL, resp.StatusCode, n)
				}
			}
		}
	}()

	// ASSERT — wait for the alert to fire within budget. Exercise keeps
	// running concurrently so the runtime metric stays monotonically
	// increasing across multiple OTel periodic exports.
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
	cancelExercise()
	wg.Wait()
	t.Logf("exercise phase: %d requests over %s", atomic.LoadInt64(&requestCount), time.Since(startExercise).Round(time.Millisecond))
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
	// Per-spec rendered files live under ops/prometheus/generated/test/.
	// http-request-availability lives in http.yaml; read that one
	// directly. (The previous single-file concatenation produced
	// invalid YAML for Prometheus rule_files; D2C3.29 + B6 fix —
	// per-spec files mirror the prod sloth-generate pattern.)
	rulesPath := filepath.Join(root, "ops", "prometheus", "generated", "test", "http.yaml")
	data, err := os.ReadFile(rulesPath)
	if err != nil {
		t.Fatalf("readSLOName: read %s: %v (run `make chaos-render-rules` first)", rulesPath, err)
	}

	// The rendered YAML is a single Sloth output for one spec.
	// We need to find any recording rule that has
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
