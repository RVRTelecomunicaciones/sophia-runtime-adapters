//go:build integration

package bootstrap_test

import (
	"context"
	"io"
	"net"
	nethttp "net/http"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/sophia-ecosystem/runtime-adapters/internal/bootstrap"
	"github.com/sophia-ecosystem/runtime-adapters/internal/infrastructure/config"
)

// TestBuildRuntime_EndToEndSmoke wires the full runtime against a testcontainers
// Postgres, hits /healthz and /api/v1/capabilities, then shuts down cleanly.
func TestBuildRuntime_EndToEndSmoke(t *testing.T) {
	ctx := context.Background()

	c, err := tcpostgres.Run(ctx, "postgres:15-alpine",
		tcpostgres.WithDatabase("runtime_adapters"),
		tcpostgres.WithUsername("test"),
		tcpostgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(2*time.Minute),
		),
	)
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}
	defer c.Terminate(context.Background()) //nolint:errcheck

	dsn, err := c.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("get connection string: %v", err)
	}

	cfg := config.Config{
		HTTPAddr:                 "127.0.0.1:0", // ephemeral port; we call Serve(ln) below
		MaxTimeoutBudget:         30 * time.Minute,
		MaxPayloadBytes:          1 << 20,
		InlineStreamLimit:        16 * 1024,
		IdempotencyWindow:        24 * time.Hour,
		ShutdownGracePeriod:      30 * time.Second,
		MaxConcurrentExecutions:  64,
		AllowedCommandsPath:      []string{"/usr/bin", "/bin"},
		AllowedWorkingDirs:       []string{"/tmp"},
		AllowedFilesystemRoots:   []string{"/tmp"},
		HTTPAllowPrivateNetworks: true,
		PostgresDSN:              dsn,
		RuntimeVersion:           "0.1.0",
		Hostname:                 "test-host",
		ProvenanceSource:         "test",
	}

	rt, err := bootstrap.BuildRuntime(ctx, cfg)
	if err != nil {
		t.Fatalf("BuildRuntime: %v", err)
	}

	// Bind ephemeral port before starting the server so the address is known.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		_ = rt.Shutdown(context.Background())
		t.Fatalf("net.Listen: %v", err)
	}

	errCh := make(chan error, 1)
	go func() { errCh <- rt.Server.Serve(ln) }()

	addr := "http://" + ln.Addr().String()

	// --- /healthz ---
	resp, err := nethttp.Get(addr + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != nethttp.StatusOK {
		t.Errorf("healthz: want 200, got %d body=%s", resp.StatusCode, body)
	}

	// --- /api/v1/capabilities ---
	resp, err = nethttp.Get(addr + "/api/v1/capabilities")
	if err != nil {
		t.Fatalf("GET /api/v1/capabilities: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != nethttp.StatusOK {
		t.Fatalf("capabilities: want 200, got %d body=%s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "shell.exec@v1") {
		t.Errorf("capabilities body missing shell.exec@v1: %s", body)
	}

	// --- Clean shutdown ---
	if err := rt.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown: %v", err)
	}
	select {
	case serveErr := <-errCh:
		if serveErr != nil && serveErr != nethttp.ErrServerClosed {
			t.Errorf("Serve returned unexpected error: %v", serveErr)
		}
	case <-time.After(5 * time.Second):
		t.Error("Serve did not return within 5s after Shutdown")
	}
}
