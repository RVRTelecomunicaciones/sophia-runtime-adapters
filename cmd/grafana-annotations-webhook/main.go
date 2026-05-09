// Command grafana-annotations-webhook is a small adapter binary that
// receives alertmanager v4 webhook payloads and creates corresponding
// Grafana annotations via Grafana's HTTP /api/annotations endpoint.
//
// HTTP surface:
//
//	POST /webhook  — alertmanager target
//	GET  /health   — liveness probe
//	GET  /ready    — readiness probe (optional; reports config + Grafana reachability)
//
// Per spec §3.2 the binary lives separately from runtime-adapters
// — process boundary preserved (D1.1 / D1.2 / I-AB.6 extended to D).
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/sophia-ecosystem/runtime-adapters/internal/infrastructure/obs/log"
	"github.com/sophia-ecosystem/runtime-adapters/internal/integrations/grafana"
)

// errModeLock signals the Layer 3 mode lock fired (CI=true && RUNTIME_TENANT != "test").
var errModeLock = errors.New("CI must run with RUNTIME_TENANT=test")

// errTenantFingerprint signals Layer 4 fingerprint fired (CI=true && GRAFANA_TENANT_TYPE != "test").
var errTenantFingerprint = errors.New("CI must run with GRAFANA_TENANT_TYPE=test")

// enforceModeLock implements Layer 3 (D2C4AB.16 extended to D, spec §9.3).
func enforceModeLock(getenv func(string) string) error {
	if strings.ToLower(getenv("CI")) == "true" && getenv("RUNTIME_TENANT") != "test" {
		return fmt.Errorf("%w: got RUNTIME_TENANT=%q", errModeLock, getenv("RUNTIME_TENANT"))
	}
	return nil
}

// enforceTenantFingerprint implements Layer 4 (D2C4AB.16, spec §9.4).
func enforceTenantFingerprint(getenv func(string) string) error {
	if strings.ToLower(getenv("CI")) == "true" && getenv("GRAFANA_TENANT_TYPE") != "test" {
		return fmt.Errorf("%w: got GRAFANA_TENANT_TYPE=%q", errTenantFingerprint, getenv("GRAFANA_TENANT_TYPE"))
	}
	return nil
}

func main() {
	// 0. Layer 3 mode lock — pre-empts any side-effecting initialization.
	if err := enforceModeLock(os.Getenv); err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)
		os.Exit(1)
	}
	// 0.5. Layer 4 tenant fingerprint.
	if err := enforceTenantFingerprint(os.Getenv); err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)
		os.Exit(1)
	}

	// 1. Logger.
	logCfg, err := log.LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL log config: %v\n", err)
		os.Exit(1)
	}
	logger, err := log.New(logCfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL log new: %v\n", err)
		os.Exit(1)
	}

	// 2. Adapter config.
	cfg, err := grafana.LoadConfig()
	if err != nil {
		logger.Error(context.Background(), "config load failed",
			slog.String("err", err.Error()))
		os.Exit(1)
	}

	// 3. Layer 4 fingerprint emission — log the tenant identifier so
	// the test-vs-prod boundary is auditable.
	logger.Info(context.Background(), "grafana-annotations-webhook starting",
		slog.String("grafana_tenant_type", cfg.TenantType),
		slog.String("runtime_tenant", cfg.RuntimeTenant),
		slog.String("listen_addr", cfg.ListenAddr),
		slog.String("grafana_url", cfg.GrafanaURL),
	)

	// 4. Wire client + handler. The handler takes the logger so it can
	// emit the structured 4xx error log per A2C4D.4.
	client := grafana.NewHTTPGrafanaClient(cfg.GrafanaURL, cfg.ServiceAccountToken,
		&http.Client{Timeout: 10 * time.Second})
	webhook := grafana.NewWebhookHandler(client, logger)

	mux := http.NewServeMux()
	mux.Handle("/webhook", webhook)
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/ready", func(w http.ResponseWriter, _ *http.Request) {
		// Minimal: report listening + config valid.
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready\n"))
	})

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// 5. Graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	logger.Info(context.Background(), "listening", slog.String("addr", cfg.ListenAddr))
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error(context.Background(), "server error", slog.String("err", err.Error()))
		os.Exit(1)
	}
	logger.Info(context.Background(), "shutdown complete")
}
