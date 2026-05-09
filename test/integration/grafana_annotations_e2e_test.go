//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestGrafanaAnnotationsWebhook_AlertmanagerPayloadEndToEnd_PostsToGrafana
// builds and runs the grafana-annotations-webhook binary against a
// stub Grafana (httptest), POSTs a synthetic alertmanager v4 payload,
// and asserts the stub received the expected annotation requests.
//
// In-process via httptest + binary spawn — NO compose stack required,
// so this test is stable without the testcontainers race seen in
// loki_ingestion_e2e.
func TestGrafanaAnnotationsWebhook_AlertmanagerPayloadEndToEnd_PostsToGrafana(t *testing.T) {
	// 1. Stub Grafana captures POST /api/annotations payloads.
	var (
		mu       sync.Mutex
		received []map[string]any
	)
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/annotations" || r.Method != http.MethodPost {
			http.Error(w, "stub: unexpected", http.StatusBadRequest)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		received = append(received, body)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":1}`))
	}))
	defer stub.Close()

	// 2. Build the adapter binary.
	repoRoot, err := findRepoRootForGrafana()
	if err != nil {
		t.Fatalf("find repo root: %v", err)
	}
	binPath := filepath.Join(t.TempDir(), "grafana-annotations-webhook")
	build := exec.Command("go", "build", "-o", binPath, "./cmd/grafana-annotations-webhook")
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v: %s", err, out)
	}

	// 3. Pick a free local port.
	listenAddr := pickFreePortForGrafana(t)

	// 4. Launch the adapter.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, binPath)
	cmd.Env = append(os.Environ(),
		"CI=",
		"RUNTIME_TENANT=test",
		"GRAFANA_TENANT_TYPE=test",
		"GRAFANA_URL="+stub.URL,
		"GRAFANA_SERVICE_ACCOUNT_TOKEN=glsa_test",
		"LISTEN_ADDR="+listenAddr,
		"RUNTIME_LOG_FORMAT=json",
		"RUNTIME_LOG_LEVEL=info",
		"RUNTIME_LOG_MIRROR_PATH=", // empty → stdout only
	)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start adapter: %v", err)
	}
	defer func() { _ = cmd.Process.Kill() }()

	// 5. Wait for the adapter's /health to come up.
	if err := waitForHealthGrafana(fmt.Sprintf("http://127.0.0.1%s/health", listenAddr), 5*time.Second); err != nil {
		t.Fatalf("adapter not healthy: %v", err)
	}

	// 6. POST a synthetic alertmanager v4 payload.
	payload := `{
		"receiver": "ops-critical",
		"status": "firing",
		"alerts": [
			{
				"status": "firing",
				"labels": {"alertname":"E2EFastBurn","severity":"critical"},
				"annotations": {"summary":"end-to-end smoke"},
				"startsAt": "2026-05-09T14:23:00Z"
			}
		]
	}`
	resp, err := http.Post(fmt.Sprintf("http://127.0.0.1%s/webhook", listenAddr),
		"application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatalf("post webhook: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 204 {
		t.Errorf("status: got %d, want 204; body=%s", resp.StatusCode, body)
	}

	// 7. Assert stub received the annotation.
	// Allow brief async flush window before reading.
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 {
		t.Fatalf("stub received %d annotations, want 1", len(received))
	}
	text, _ := received[0]["text"].(string)
	if !strings.Contains(text, "E2EFastBurn") {
		t.Errorf("annotation text missing alertname; got %q", text)
	}
	if !strings.Contains(text, "[CRIT]") {
		t.Errorf("annotation text missing CRIT prefix; got %q", text)
	}
}

// findRepoRootForGrafana walks up from the current dir to find a go.mod.
func findRepoRootForGrafana() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found")
		}
		dir = parent
	}
}

func pickFreePortForGrafana(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().(*net.TCPAddr)
	_ = l.Close()
	return fmt.Sprintf(":%d", addr.Port)
}

func waitForHealthGrafana(url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for %s", url)
}
