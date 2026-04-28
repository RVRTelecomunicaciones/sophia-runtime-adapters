//go:build e2e

package e2e

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// composeFiles returns the absolute paths to the base and chaos overlay
// compose files, resolving them against the repo root.
func composeFiles(t *testing.T) (base, overlay string) {
	t.Helper()
	root := findRepoRoot(t)
	return filepath.Join(root, "ops", "local", "compose.yaml"),
		filepath.Join(root, "ops", "local", "compose.chaos.yaml")
}

// ComposeUp starts the chaos compose stack (base + overlay) in detached mode.
// env is merged into the current process environment before docker compose runs.
// The test is fatalf'd if the compose up command fails.
func ComposeUp(t *testing.T, env map[string]string) {
	t.Helper()
	base, overlay := composeFiles(t)
	args := []string{
		"compose",
		"-f", base,
		"-f", overlay,
		"up", "-d",
		"--build",
	}
	cmd := exec.Command("docker", args...)
	cmd.Env = mergeEnv(env)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ComposeUp failed: %v\n%s", err, out)
	}
	t.Logf("ComposeUp succeeded:\n%s", out)
}

// ComposeDown tears down the chaos compose stack, removing volumes.
// It is intended to be called from t.Cleanup.
func ComposeDown(t *testing.T) {
	t.Helper()
	base, overlay := composeFiles(t)
	cmd := exec.Command("docker", "compose",
		"-f", base,
		"-f", overlay,
		"down", "-v",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("ComposeDown error (ignored): %v\n%s", err, out)
		return
	}
	t.Logf("ComposeDown succeeded:\n%s", out)
}

// WaitForHealthy polls the health endpoints of all four chaos stack services
// until all return healthy or deadline elapses. Fatalf's the test if any
// service is still unhealthy after deadline.
//
// Health endpoints polled (all host-mapped by compose.yaml + chaos overlay):
//   - Prometheus:       GET http://localhost:9090/-/ready
//   - Alertmanager:     GET http://localhost:9093/-/healthy
//   - Receiver-stub:    GET http://localhost:8088/inspect (200 OK)
//   - Runtime-adapters: GET http://localhost:8080/healthz
func WaitForHealthy(t *testing.T, deadline time.Duration) {
	t.Helper()

	type probe struct {
		name string
		url  string
	}
	probes := []probe{
		{name: "prometheus", url: "http://localhost:9090/-/ready"},
		{name: "alertmanager", url: "http://localhost:9093/-/healthy"},
		{name: "receiver-stub", url: "http://localhost:8088/inspect"},
		{name: "runtime-adapters", url: "http://localhost:8080/healthz"},
	}

	client := &http.Client{Timeout: 3 * time.Second}
	deadlineAt := time.Now().Add(deadline)
	const pollEvery = 2 * time.Second

	for {
		if time.Now().After(deadlineAt) {
			t.Fatalf("WaitForHealthy: services not ready after %s", deadline)
		}

		allReady := true
		for _, p := range probes {
			if !isReady(client, p.url) {
				t.Logf("WaitForHealthy: %s not ready yet (%s)", p.name, p.url)
				allReady = false
			}
		}
		if allReady {
			t.Log("WaitForHealthy: all services ready")
			return
		}
		time.Sleep(pollEvery)
	}
}

// isReady returns true when a GET to url returns HTTP 200.
func isReady(client *http.Client, url string) bool {
	resp, err := client.Get(url) //nolint:noctx
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body) //nolint:errcheck
	return resp.StatusCode == http.StatusOK
}

// mergeEnv builds an environment slice for exec.Cmd by starting with the
// current process's environment and overlaying the provided key=value pairs.
func mergeEnv(extra map[string]string) []string {
	base := os.Environ()
	result := make([]string, 0, len(base)+len(extra))
	overrideKeys := make(map[string]bool, len(extra))
	for k := range extra {
		overrideKeys[strings.ToUpper(k)] = true
	}
	for _, kv := range base {
		key := strings.SplitN(kv, "=", 2)[0]
		if !overrideKeys[strings.ToUpper(key)] {
			result = append(result, kv)
		}
	}
	for k, v := range extra {
		result = append(result, fmt.Sprintf("%s=%s", k, v))
	}
	return result
}
