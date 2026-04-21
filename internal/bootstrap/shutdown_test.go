//go:build integration

package bootstrap_test

import (
	"context"
	"net"
	nethttp "net/http"
	"testing"
	"time"
)

// TestGracefulShutdown_ReturnsWithinGracePeriod verifies that
// Runtime.Shutdown unblocks Serve within ShutdownGracePeriod. The
// server serves one healthz request before shutdown to warm up the
// goroutine stack and confirm no in-flight requests are held.
func TestGracefulShutdown_ReturnsWithinGracePeriod(t *testing.T) {
	pool, teardown := startPGForBootstrap(t)
	defer teardown()

	rt := buildSmokeRuntime(t, pool.dsn, 5*time.Second)
	defer rt.Shutdown(context.Background()) //nolint:errcheck

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	errCh := make(chan error, 1)
	go func() { errCh <- rt.Server.Serve(ln) }()

	// Prime the server with a healthz request.
	addr := "http://" + ln.Addr().String()
	resp, err := nethttp.Get(addr + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	resp.Body.Close()

	// Trigger shutdown.
	t0 := time.Now()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := rt.Shutdown(shutdownCtx); err != nil {
		t.Errorf("Shutdown: %v", err)
	}

	// Serve goroutine must return within the grace period.
	select {
	case err := <-errCh:
		if err != nil && err != nethttp.ErrServerClosed {
			t.Errorf("Serve: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Serve did not return within 10s after shutdown")
	}
	elapsed := time.Since(t0)
	if elapsed > 10*time.Second {
		t.Errorf("shutdown took %v, expected < 10s", elapsed)
	}
}
