//go:build e2e

package e2e_test

import (
	"context"
	"fmt"
	"net"
	nethttp "net/http"
	"os"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/sophia-ecosystem/runtime-adapters/internal/bootstrap"
	"github.com/sophia-ecosystem/runtime-adapters/internal/infrastructure/config"
)

// runtimeHandle bundles the live HTTP server's address and the teardown
// function needed to release the runtime + the PG container.
type runtimeHandle struct {
	Addr     string
	Teardown func()
}

// startRuntime spins up a PG container, builds the full runtime via
// bootstrap.BuildRuntime, and starts the HTTP server on an ephemeral port.
// Returns the base URL (e.g. http://127.0.0.1:55212) and a teardown that
// shuts everything down.
//
// workingDir is a directory added to the shell/git/filesystem allowlists so
// that tests can exec commands and create files inside it.
func startRuntime(t *testing.T, workingDir string) *runtimeHandle {
	t.Helper()
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
		t.Fatalf("start pg: %v", err)
	}

	dsn, err := c.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = c.Terminate(ctx)
		t.Fatal(err)
	}

	cfg := config.Config{
		HTTPAddr:                "127.0.0.1:0",
		MaxTimeoutBudget:        30 * time.Minute,
		MaxPayloadBytes:         1 << 20,
		InlineStreamLimit:       16 * 1024,
		IdempotencyWindow:       24 * time.Hour,
		ShutdownGracePeriod:     15 * time.Second,
		MaxConcurrentExecutions: 32,
		AllowedCommandsPath:     []string{"/usr/bin", "/bin"},
		AllowedWorkingDirs:      []string{workingDir, os.TempDir()},
		AllowedFilesystemRoots:  []string{workingDir, os.TempDir()},
		// Allow private networks so the test runtime can reach testcontainers PG
		// on loopback (127.x.x.x).
		HTTPAllowPrivateNetworks: true,
		PostgresDSN:              dsn,
		RuntimeVersion:           "0.1.0-e2e",
		Hostname:                 "e2e-test",
		ProvenanceSource:         "test",
	}

	rt, err := bootstrap.BuildRuntime(ctx, cfg)
	if err != nil {
		_ = c.Terminate(ctx)
		t.Fatalf("BuildRuntime: %v", err)
	}

	// Ephemeral listener: bind port before calling Serve so we know the address
	// without having to parse it from server internals.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		_ = rt.Shutdown(context.Background())
		_ = c.Terminate(ctx)
		t.Fatalf("listen: %v", err)
	}

	serveErr := make(chan error, 1)
	go func() { serveErr <- rt.Server.Serve(ln) }()

	addr := "http://" + ln.Addr().String()
	if err := waitHealthy(addr, 5*time.Second); err != nil {
		_ = rt.Shutdown(context.Background())
		_ = c.Terminate(ctx)
		t.Fatalf("wait healthy: %v", err)
	}

	teardown := func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = rt.Shutdown(shutdownCtx)
		select {
		case <-serveErr:
		case <-time.After(5 * time.Second):
			t.Log("Serve did not return after shutdown")
		}
		_ = c.Terminate(context.Background())
	}

	return &runtimeHandle{Addr: addr, Teardown: teardown}
}

// waitHealthy polls GET /healthz until it returns 200 or the timeout
// elapses. Returns an error if the server is still not healthy after
// the deadline.
func waitHealthy(addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := nethttp.Get(addr + "/healthz")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == nethttp.StatusOK {
				return nil
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("runtime did not become healthy within %v", timeout)
}
