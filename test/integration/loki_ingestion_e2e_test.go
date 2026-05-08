//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	tcompose "github.com/testcontainers/testcontainers-go/modules/compose"
)

// TestLokiIngestion_RuntimeRecordReachesLokiViaCollector spins up the
// full B1 stack via compose-testcontainers (base + compose.logs.yaml)
// and asserts a runtime log record arrives in Loki within the timeout
// window with the correct label set per D2C4D.8.
//
// Local docker daemon required. Skipped automatically if `docker info`
// fails so this test does not break test runs on machines without
// docker.
func TestLokiIngestion_RuntimeRecordReachesLokiViaCollector(t *testing.T) {
	if testing.Short() {
		t.Skip("compose stack test; skipping in -short mode")
	}
	if !dockerAvailable() {
		t.Skip("docker daemon not reachable; skipping")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	stack, err := tcompose.NewDockerComposeWith(
		tcompose.WithStackFiles(
			"../../ops/local/compose.yaml",
			"../../ops/local/compose.logs.yaml",
		),
	)
	if err != nil {
		t.Fatalf("compose stack: %v", err)
	}
	t.Cleanup(func() {
		_ = stack.Down(context.Background(),
			tcompose.RemoveOrphans(true),
			tcompose.RemoveImagesLocal,
		)
	})

	if err := stack.Up(ctx, tcompose.Wait(true)); err != nil {
		t.Fatalf("compose up: %v", err)
	}

	// Allow filelog 'start_at: end' to attach + the collector to be
	// fully ready before the runtime emits.
	time.Sleep(5 * time.Second)

	// The marker doubles as the correlation_id sent on the trigger
	// request. CorrelationID validates as a ULID (26-char Crockford
	// base32) so we generate one with ulid.Make() and use it both as
	// the wire-format correlation_id AND as the unique grep token in
	// Loki — execute_service.go enriches the request-scoped logger
	// with the exact correlation_id, so the per-execution log record
	// emitted on the structural-failure path carries this marker.
	marker := ulid.Make().String()

	// Trigger a runtime log emission via the inbound HTTP endpoint.
	// The runtime exposes :8080 via the base compose's port mapping.
	// We POST an envelope that decodes (so we cross the validation
	// boundary) but specifies a non-existent capability — the runtime
	// classifies the failure structurally and persistStructural still
	// calls emitExecutionComplete, which logs an INFO line carrying
	// our correlation_id (= marker).
	if err := triggerRuntimeLog(ctx, marker); err != nil {
		t.Logf("trigger runtime log returned: %v (the runtime middleware logs anyway)", err)
	}

	// Poll Loki for the marker.
	q := fmt.Sprintf(`{service_name="runtime-adapters"} |= "%s"`, marker)
	deadline := time.Now().Add(90 * time.Second)
	var lastErr error
	for {
		if time.Now().After(deadline) {
			t.Fatalf("timeout: Loki did not return the expected record within 90s for query %s; last error: %v", q, lastErr)
		}
		records, streamFields, err := queryLoki(ctx, q, time.Now().Add(-2*time.Minute), time.Now())
		if err != nil {
			lastErr = err
			time.Sleep(2 * time.Second)
			continue
		}
		if len(records) > 0 {
			// Sanity log only — `streamFields` from query_range mixes
			// real index labels with per-line structured metadata (and
			// Loki's auto-derived `detected_level`), so we cannot use it
			// as a cardinality oracle.
			t.Logf("Loki record received; stream payload (labels + structured metadata) = %v", streamFields)

			// The cardinality oracle is /loki/api/v1/labels: it returns
			// ONLY the index-label keys (D2C4D.8 forbids these). We
			// scope the lookup to the same stream selector we just
			// matched against to avoid pulling in unrelated streams.
			indexLabels, err := queryLokiIndexLabels(ctx,
				`{service_name="runtime-adapters"}`,
				time.Now().Add(-2*time.Minute), time.Now())
			if err != nil {
				t.Fatalf("query Loki /labels: %v", err)
			}
			forbidden := []string{
				"trace_id", "span_id", "correlation_id", "capability",
				"adapter", "error_class", "retry_hint", "request_id",
				"user_id", "project_id", "tenant_id",
			}
			for _, k := range forbidden {
				for _, idx := range indexLabels {
					if idx == k {
						t.Errorf("Loki INDEX labels contain forbidden key %q (D2C4D.8 violation); index_labels=%v", k, indexLabels)
					}
				}
			}
			t.Logf("PASS — Loki has %d record(s); index labels = %v", len(records), indexLabels)
			return
		}
		time.Sleep(2 * time.Second)
	}
}

func dockerAvailable() bool {
	cmd := exec.Command("docker", "info")
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run() == nil
}

// queryLokiIndexLabels returns the set of INDEX label keys that Loki
// has stored for streams matching the given selector. Unlike the
// `stream` field of /query_range — which mixes index labels with
// structured metadata + auto-derived fields like `detected_level` —
// the /labels endpoint is authoritative for what Loki has indexed.
//
// This is the correct oracle for D2C4D.8 cardinality assertions.
func queryLokiIndexLabels(ctx context.Context, selector string, start, end time.Time) ([]string, error) {
	u, _ := url.Parse("http://localhost:3100/loki/api/v1/labels")
	v := u.Query()
	v.Set("query", selector)
	v.Set("start", fmt.Sprintf("%d", start.UnixNano()))
	v.Set("end", fmt.Sprintf("%d", end.UnixNano()))
	u.RawQuery = v.Encode()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("loki /labels status %d: %s", resp.StatusCode, body)
	}
	var out struct {
		Data []string `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

func queryLoki(ctx context.Context, q string, start, end time.Time) (records []string, labels map[string]string, err error) {
	u, _ := url.Parse("http://localhost:3100/loki/api/v1/query_range")
	v := u.Query()
	v.Set("query", q)
	v.Set("start", fmt.Sprintf("%d", start.UnixNano()))
	v.Set("end", fmt.Sprintf("%d", end.UnixNano()))
	u.RawQuery = v.Encode()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, nil, fmt.Errorf("loki status %d: %s", resp.StatusCode, body)
	}
	var out struct {
		Data struct {
			Result []struct {
				Stream map[string]string `json:"stream"`
				Values [][]string        `json:"values"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, nil, err
	}
	for _, r := range out.Data.Result {
		for _, v := range r.Values {
			if len(v) >= 2 {
				records = append(records, v[1])
			}
		}
		labels = r.Stream
	}
	return records, labels, nil
}

func triggerRuntimeLog(ctx context.Context, marker string) error {
	// Mirror B1.10's wire format: a structurally-valid envelope that
	// crosses validation but uses a non-existent capability so the
	// runtime takes the structural-failure path. persistStructural
	// still calls emitExecutionComplete, which produces the INFO log
	// line carrying correlation_id=marker.
	body := strings.NewReader(`{
        "correlation_id": "` + marker + `",
        "adapter_id": "shell",
        "capability_name": "no_such_capability",
        "capability_version": "v1",
        "payload": {},
        "timeout_budget_ms": 1000
    }`)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "http://localhost:8080/api/v1/execute", body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}
