//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
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

// TestChaos_Comprehensive is the nightly comprehensive E2E suite. It is the
// parameterised version of TestChaos_Canary_HttpConnectionReset (spec §13.2)
// running across all six CI chaos profiles. Each subtest brings up a fresh
// chaos compose stack with the scenario-specific RUNTIME_CHAOS_PROFILE,
// drives sustained traffic at the target capability, asserts the expected
// critical alert lands within the D2C3.21 tiered budget (target=60s,
// deadline=90s) with the exact label set, and additionally asserts the
// inhibition contract (§13.3, D2C1.17): no warning alert with the same
// (sloth_slo, capability) is delivered alongside the critical.
//
// All six CI scenarios are active in the suite as of v0.5.0. The two
// scenarios that shipped skipped under v0.4.0 (ci-shell-hang-cancel,
// ci-persist-fail) are now backed by the cancellation-rate and
// persist-availability SLOs introduced in Phase 2C.4 / G — see
// docs/superpowers/specs/2026-04-29-phase-2c.4-g-cancellation-persist-slos-design.md.
//
// Compose lifecycle: ComposeUp + ComposeDown PER SCENARIO. docker compose
// fixes container env at start; the only clean way to swap the chaos
// profile is to bring the stack down and up again. This costs roughly
// 30-60s per scenario in setup and 30s in teardown — well within the 30m
// nightly job timeout and within the suite-level ~15-20m wall budget
// stated in §13.2.
//
// Scenarios:
//
//	ci-fs-readfile-eio          → filesystem.read_file@v1 → FilesystemReadFileAvailabilityBurn
//	ci-git-remote-unreachable   → git.clone@v1            → GitCloneAvailabilityBurn
//	ci-http-connection-reset    → http.request@v1         → HttpRequestAvailabilityBurn
//	ci-shell-panic              → shell.exec@v1           → ShellExecAvailabilityBurn
//	ci-shell-hang-cancel        → shell.exec@v1           → ShellExecCancellationRateBurn
//	ci-persist-fail             → http.request@v1         → PersistAvailabilityBurn (global; http driver)
func TestChaos_Comprehensive(t *testing.T) {
	for _, sc := range comprehensiveScenarios() {
		sc := sc
		t.Run(sc.name, func(t *testing.T) {
			if sc.skipReason != "" {
				t.Skipf("scenario %s skipped: %s", sc.name, sc.skipReason)
				return
			}
			runComprehensiveScenario(t, sc)
		})
	}
}

// comprehensiveScenario describes one end-to-end chaos scenario.
type comprehensiveScenario struct {
	name         string
	profileFile  string // basename under ops/chaos/profiles/ci/
	adapterID    string
	capability   string // value of capability_name
	version      string // value of capability_version
	canonicalCap string // capability label as seen in SLO rules (e.g. http.request@v1)
	payloadJSON  string
	timeoutMs    int64
	useCancel    bool          // true → context.WithCancel + auto-cancel after cancelAfter
	cancelAfter  time.Duration // only used when useCancel is true
	alertName    string        // expected page-tier (severity=critical) alert name
	sloName      string        // sloth_slo label value
	specFile     string        // basename under ops/slo/test/ (used to resolve sloth_slo)
	skipReason   string        // non-empty → t.Skip with this reason
}

// comprehensiveScenarios returns the static list of scenarios. The mapping
// between profile, capability, and SLO is hard-coded here intentionally:
// the test asserts a specific Sloth-rendered alert per profile, and any
// drift between the profile, the SLO spec (ops/slo/test/), and the
// rendered rules is exactly what this nightly is designed to catch — so
// readers can audit the table at a glance.
func comprehensiveScenarios() []comprehensiveScenario {
	return []comprehensiveScenario{
		{
			name:         "fs-readfile-eio",
			profileFile:  "ci-fs-readfile-eio.yaml",
			adapterID:    "filesystem",
			capability:   "read_file",
			version:      "v1",
			canonicalCap: "filesystem.read_file@v1",
			payloadJSON:  `{"path":"/tmp/chaos-fs-read","max_bytes":1024}`,
			timeoutMs:    1000,
			alertName:    "FilesystemReadFileAvailabilityBurn",
			sloName:      "filesystem-read-file-availability",
			specFile:     "filesystem.yaml",
		},
		{
			name:         "git-remote-unreachable",
			profileFile:  "ci-git-remote-unreachable.yaml",
			adapterID:    "git",
			capability:   "clone",
			version:      "v1",
			canonicalCap: "git.clone@v1",
			payloadJSON:  `{"repo_url":"https://example.invalid/repo.git","destination_path":"/tmp/chaos-git-clone","ref":"main","auth_mode":"none"}`,
			timeoutMs:    2000,
			alertName:    "GitCloneAvailabilityBurn",
			sloName:      "git-clone-availability",
			specFile:     "git.yaml",
		},
		{
			name:         "http-connection-reset",
			profileFile:  "ci-http-connection-reset.yaml",
			adapterID:    "http",
			capability:   "request",
			version:      "v1",
			canonicalCap: "http.request@v1",
			payloadJSON:  `{"method":"GET","url":"https://example.invalid/","headers":{},"expected_status":[200]}`,
			timeoutMs:    1000,
			alertName:    "HttpRequestAvailabilityBurn",
			sloName:      "http-request-availability",
			specFile:     "http.yaml",
		},
		{
			name:         "shell-hang-cancel",
			profileFile:  "ci-shell-hang-cancel.yaml",
			adapterID:    "shell",
			capability:   "exec",
			version:      "v1",
			canonicalCap: "shell.exec@v1",
			payloadJSON:  `{"command":"true","args":[],"working_dir":"","env":null,"stdin":null}`,
			timeoutMs:    5000,
			useCancel:    true,
			// 50ms is long enough for the POST to reach the runtime handler
			// and start executing the shell command; short enough that we're
			// well below timeoutMs=5000 so the runtime classifies as
			// cancelled, not timeout.
			cancelAfter: 50 * time.Millisecond,
			alertName:   "ShellExecCancellationRateBurn",
			sloName:     "shell-exec-cancellation-rate",
			specFile:    "shell.yaml",
		},
		{
			name:         "shell-panic",
			profileFile:  "ci-shell-panic.yaml",
			adapterID:    "shell",
			capability:   "exec",
			version:      "v1",
			canonicalCap: "shell.exec@v1",
			payloadJSON:  `{"command":"true","args":[],"working_dir":"","env":null,"stdin":null}`,
			timeoutMs:    1000,
			alertName:    "ShellExecAvailabilityBurn",
			sloName:      "shell-exec-availability",
			specFile:     "shell.yaml",
		},
		{
			name:         "persist-fail",
			profileFile:  "ci-persist-fail.yaml",
			adapterID:    "http",
			capability:   "request",
			version:      "v1",
			canonicalCap: "", // persist SLO is global; capability label intentionally empty
			payloadJSON:  `{"method":"GET","url":"https://example.invalid/","headers":{},"expected_status":[200]}`,
			timeoutMs:    1000,
			alertName:    "PersistAvailabilityBurn",
			sloName:      "persist-availability",
			specFile:     "persist.yaml",
		},
	}
}

// runComprehensiveScenario runs one scenario end-to-end: ComposeUp with the
// scenario's profile, drive sustained traffic, assert the critical alert
// lands within budget, then assert the inhibition contract.
func runComprehensiveScenario(t *testing.T, sc comprehensiveScenario) {
	t.Helper()

	// Resolve the SLO name from the rendered Sloth rules. Coupling the
	// test to the rendered output catches Sloth bumps that rename
	// recordings / alerts (D2C3.29) — the same rationale as the canary's
	// readSLOName helper.
	sloName := readSLONameFromSpec(t, sc.specFile, sc.sloName)
	require.Equal(t, sc.sloName, sloName,
		"sloth_slo label mismatch: spec table says %q but rendered rules emit %q "+
			"(run `make chaos-render-rules` to refresh)", sc.sloName, sloName)

	// SETUP — chaos profile path is relative to the container's mount
	// (D2C3.25); the chaos overlay mounts ops/chaos/profiles/ci at this
	// path.
	profilePath := "/etc/runtime-adapters/chaos/profiles/ci/" + sc.profileFile
	env := map[string]string{
		"RUNTIME_CHAOS_ENABLED": "true",
		"RUNTIME_CHAOS_PROFILE": profilePath,
		"RUNTIME_ENV":           "development",
	}

	ComposeUp(t, env)
	t.Cleanup(func() {
		// IMPORTANT: dump while the stack is still up; tearing down
		// removes the containers and we'd lose all the diagnostic
		// surface. Mirrors the canary's t.Cleanup ordering.
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
	require.NoError(t, rc.Clear(context.Background()),
		"clear receiver-stub ring buffer before scenario %s", sc.name)

	// EXERCISE — drive sustained, failed executions for the full
	// assertion window. Why sustained, not a one-shot burst: the runtime's
	// OTel SDK uses a 30s PeriodicReader, so a 15s burst increments only
	// show up in Prometheus as a single flat plateau. rate(counter[1m])
	// over a flat plateau is 0, ratio = 0/0 = NaN, no burn-rate alert can
	// fire. Sustained traffic across multiple 30s exports lets rate() see
	// real per-second deltas. This is the same fix as canary commit
	// 65bc3d9.
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
				driveOneRequest(t, exerciseCtx, runtimeURL, sc, &requestCount)
			}
		}
	}()

	// ASSERT — wait for the critical alert to fire within the tiered
	// budget. Exercise keeps running concurrently so the runtime metric
	// stays monotonically increasing across multiple OTel periodic
	// exports.
	t.Logf("[%s] expecting alertname=%q, sloth_slo=%q, capability=%q",
		sc.name, sc.alertName, sloName, sc.canonicalCap)

	expected := ExpectedAlert{
		AlertName:  sc.alertName,
		Capability: sc.canonicalCap,
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
	t.Logf("[%s] exercise: %d requests over %s",
		sc.name,
		atomic.LoadInt64(&requestCount),
		time.Since(startExercise).Round(time.Millisecond))
	require.NoErrorf(t, err,
		"[%s] critical alert %q not delivered within %s; "+
			"check (1) runtime metrics in Prometheus, "+
			"(2) ops/prometheus/generated/test/%s rules loaded, "+
			"(3) alertmanager routes match, "+
			"(4) receiver-stub reachable",
		sc.name, sc.alertName, deadline, sc.specFile)

	labels := labelsFromPayload(a)
	require.NotEmpty(t, labels,
		"[%s] received AlertPayload has no decodable labels: body=%s",
		sc.name, a.Body)
	t.Logf("[%s] received critical labels: %s", sc.name, labelsString(labels))

	if breached {
		t.Logf("[%s] WARNING: fire_latency=%s exceeded target=%s but within deadline=%s "+
			"(D2C3.21 pass-with-warning)",
			sc.name, fireLatency.Round(time.Millisecond), target, deadline)
	} else {
		t.Logf("[%s] OK: fire_latency=%s (target=%s)",
			sc.name, fireLatency.Round(time.Millisecond), target)
	}

	// INHIBITION CONTRACT (spec §13.3, D2C1.17). After the critical lands,
	// assert that no warning alert with the same (sloth_slo, capability)
	// has been delivered AFTER the critical's received timestamp.
	//
	// We filter by `since=a.Received` (not the full ring buffer) because
	// multi-window burn-rate alerts cross the warning threshold BEFORE the
	// critical threshold by design — a warning legitimately fires during
	// the early ramp and reaches the receiver. Alertmanager's inhibition
	// rule only suppresses warnings that arrive WHILE a critical is active;
	// it does not retroactively erase warnings already delivered. So the
	// contract under test is: "no NEW warning for the same SLO+capability
	// after the critical fired." Code-quality review on commit ec893a9
	// flagged this as a flakiness vector.
	warnings, werr := rc.AlertsMatchingSince(context.Background(), ExpectedAlert{
		Severity:   "warning",
		SLOName:    expected.SLOName,
		Capability: expected.Capability,
	}, a.Received)
	require.NoError(t, werr,
		"[%s] AlertsMatchingSince(warning) failed: %v", sc.name, werr)
	require.Emptyf(t, warnings,
		"[%s] inhibition violated: warning alert delivered AFTER the critical "+
			"for same (sloth_slo=%s, capability=%s); alertmanager.yml source_match/"+
			"target_match must inhibit warning while critical is active for the "+
			"same SLO+capability (D2C1.17). Got %d warning payloads after %s.",
		sc.name, expected.SLOName, expected.Capability, len(warnings),
		a.Received.Format(time.RFC3339Nano))
}

// driveOneRequest issues a single POST /api/v1/execute against the runtime
// for the given scenario. It honours the scenario's useCancel flag so that
// shell-hang-cancel produces context.Canceled at the runtime side.
func driveOneRequest(
	t *testing.T,
	parentCtx context.Context,
	runtimeURL string,
	sc comprehensiveScenario,
	counter *int64,
) {
	t.Helper()

	correlationID := ulid.Make().String()
	body, err := json.Marshal(map[string]any{
		"correlation_id":     correlationID,
		"adapter_id":         sc.adapterID,
		"capability_name":    sc.capability,
		"capability_version": sc.version,
		"payload":            json.RawMessage(sc.payloadJSON),
		"timeout_budget_ms":  sc.timeoutMs,
	})
	if err != nil {
		t.Logf("[%s] marshal: %v", sc.name, err)
		return
	}

	// For the hang-cancel scenario we drive each request through a
	// short-lived child context that is cancelled (not timed out) shortly
	// after the request goes out. The runtime's chaos hang_until_cancel
	// fault blocks until ctx.Done() — when the client cancels, the runtime
	// observes ctx.Err() == context.Canceled and the shell normalizer maps
	// it to status=cancelled. Using WithCancel + cancel() (NOT
	// WithTimeout) is what differentiates cancelled from timeout.
	reqCtx, reqCancel := context.WithCancel(parentCtx)
	defer reqCancel()
	if sc.useCancel {
		go func() {
			select {
			case <-time.After(sc.cancelAfter):
				reqCancel()
			case <-reqCtx.Done():
			}
		}()
	}

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, runtimeURL, bytes.NewReader(body))
	if err != nil {
		t.Logf("[%s] build request: %v", sc.name, err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	n := atomic.AddInt64(counter, 1)
	if err != nil {
		// In useCancel mode an early cancel manifests as a transport
		// error; that is expected. Don't pollute the log with one
		// message per cancellation.
		if !sc.useCancel && parentCtx.Err() == nil {
			t.Logf("[%s] POST %s err (iter=%d): %v", sc.name, runtimeURL, n, err)
		}
		return
	}
	resp.Body.Close()
	if n%10 == 0 {
		t.Logf("[%s] POST %s status=%d (iter=%d)", sc.name, runtimeURL, resp.StatusCode, n)
	}
}

// readSLONameFromSpec extracts the sloth_slo label value Sloth assigns to
// the given SLO inside the given rendered spec file under
// ops/prometheus/generated/test/. Returns the matching label value or
// fatal-fails the test if the SLO is not present.
//
// This is the parameterised generalisation of the canary's readSLOName
// helper (which is hard-coded to http-request-availability). Coupling the
// test to the rendered output catches Sloth bumps that rename recordings
// (D2C3.29).
func readSLONameFromSpec(t *testing.T, specFile, expectedSLOName string) string {
	t.Helper()

	root := findRepoRoot(t)
	rulesPath := filepath.Join(root, "ops", "prometheus", "generated", "test", specFile)
	data, err := os.ReadFile(rulesPath)
	if err != nil {
		t.Fatalf("readSLONameFromSpec: read %s: %v "+
			"(run `make chaos-render-rules` first)", rulesPath, err)
	}

	type ruleYAML struct {
		Record string            `yaml:"record"`
		Alert  string            `yaml:"alert"`
		Labels map[string]string `yaml:"labels"`
	}
	type groupYAML struct {
		Name  string     `yaml:"name"`
		Rules []ruleYAML `yaml:"rules"`
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
				if v, ok := r.Labels["sloth_slo"]; ok && v == expectedSLOName {
					if svc, ok := r.Labels["sloth_service"]; ok && svc == "runtime-adapters-chaos-test" {
						return v
					}
				}
			}
		}
	}

	t.Fatalf("readSLONameFromSpec: could not find sloth_slo=%q in %s "+
		"(run `make chaos-render-rules` first)",
		expectedSLOName, rulesPath)
	return ""
}
