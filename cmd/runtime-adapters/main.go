// runtime-adapters is the Phase 1 binary entrypoint. It loads
// configuration from the environment, wires the runtime, serves HTTP
// with graceful shutdown on SIGTERM/SIGINT, and exits with 0 on clean
// shutdown or 1 on error.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/sophia-ecosystem/runtime-adapters/internal/bootstrap"
	"github.com/sophia-ecosystem/runtime-adapters/internal/infrastructure/config"
)

func main() {
	code := run()
	os.Exit(code)
}

// run returns a process exit code. Split from main so tests can call it
// without os.Exit. Also makes the shutdown/error code paths testable.
func run() int {
	log.SetFlags(log.LstdFlags | log.LUTC | log.Lmicroseconds)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	cfg, err := config.Load(nil)
	if err != nil {
		log.Printf("config: %v", err)
		return 1
	}
	log.Printf("runtime-adapters v%s starting on %s (host=%s)", cfg.RuntimeVersion, cfg.HTTPAddr, cfg.Hostname)

	rt, err := bootstrap.BuildRuntime(ctx, cfg)
	if err != nil {
		log.Printf("bootstrap: %v", err)
		return 1
	}

	// Serve in background; collect error on channel.
	errCh := make(chan error, 1)
	go func() {
		log.Printf("HTTP listen on %s", cfg.HTTPAddr)
		errCh <- rt.Server.ListenAndServe()
	}()

	// Wait for shutdown signal OR server error.
	select {
	case <-ctx.Done():
		log.Printf("shutdown signal received; draining for %v", cfg.ShutdownGracePeriod)
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("HTTP server error: %v", err)
			// Still try to shut down cleanly.
			drainCtx, drainCancel := context.WithTimeout(context.Background(), cfg.ShutdownGracePeriod)
			_ = rt.Shutdown(drainCtx)
			drainCancel()
			return 1
		}
		log.Printf("HTTP server closed unexpectedly (no signal received)")
	}

	drainCtx, drainCancel := context.WithTimeout(context.Background(), cfg.ShutdownGracePeriod)
	defer drainCancel()
	if err := rt.Shutdown(drainCtx); err != nil {
		log.Printf("shutdown: %v", err)
		return 1
	}
	log.Printf("clean shutdown")
	return 0
}
