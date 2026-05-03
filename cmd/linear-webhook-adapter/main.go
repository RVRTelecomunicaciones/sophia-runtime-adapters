// linear-webhook-adapter is the Phase 2C.4 / A+B B2 binary that
// translates Alertmanager webhook payloads into Linear GraphQL
// mutations. It is a separate binary from runtime-adapters
// (D2C4AB.4) — same Go module, different process, different
// container, port 9095.
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

	"github.com/sophia-ecosystem/runtime-adapters/internal/integrations/linear/application"
	"github.com/sophia-ecosystem/runtime-adapters/internal/integrations/linear/infrastructure"
)

func main() {
	os.Exit(run(os.Getenv))
}

// run is split from main so tests can call it without os.Exit.
// getenv is injectable so tests can pass a hermetic map-backed env.
func run(getenv func(string) string) int {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	if err := enforceModeLock(getenv); err != nil {
		logger.Error("FATAL: mode lock", "err", err)
		return 1
	}
	if err := enforceTenantFingerprint(getenv); err != nil {
		logger.Error("FATAL: tenant fingerprint", "err", err)
		return 1
	}

	cfg, err := loadConfig(getenv)
	if err != nil {
		logger.Error("FATAL: config", "err", err)
		return 1
	}
	logger.Info("linear-webhook-adapter starting",
		"tenant_team_id", cfg.LinearTeamID,
		"tenant_type", cfg.LinearTenantType,
		"api_url", cfg.LinearAPIURL,
		"listen_addr", cfg.ListenAddr,
	)

	client := infrastructure.NewLinearGraphQLClient(infrastructure.Config{
		APIURL:   cfg.LinearAPIURL,
		APIToken: cfg.LinearAPIToken,
		Timeout:  10 * time.Second,
	})
	lifecycle := application.NewLifecycle(application.LifecycleConfig{
		Client:               client,
		TeamID:               cfg.LinearTeamID,
		RecommentMinInterval: cfg.RecommentMinInterval,
		Now:                  time.Now,
	})
	handler := application.NewWebhookHandler(lifecycle).WithLogger(logger)

	mux := http.NewServeMux()
	mux.Handle("/webhook", handler)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", cfg.ListenAddr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received; draining")
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server error", "err", err)
			return 1
		}
	}

	drainCtx, drainCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer drainCancel()
	if err := srv.Shutdown(drainCtx); err != nil {
		logger.Error("shutdown", "err", err)
		return 1
	}
	logger.Info("clean shutdown")
	return 0
}

// errModeLock is the sentinel error returned when CI=true without
// RUNTIME_TENANT=test (Layer 3 adapter side). Exposed for tests +
// log assertions.
var errModeLock = errors.New("CI must run with RUNTIME_TENANT=test")

// errTenantFingerprint is the sentinel for Layer 4 (adapter side).
var errTenantFingerprint = errors.New("CI must use Linear test tenant only")

// enforceModeLock implements Layer 3 (adapter side) per spec §9.3.
// Aborts startup if CI=true and RUNTIME_TENANT != "test".
func enforceModeLock(getenv func(string) string) error {
	if strings.ToLower(getenv("CI")) == "true" && getenv("RUNTIME_TENANT") != "test" {
		return fmt.Errorf("%w: got RUNTIME_TENANT=%q", errModeLock, getenv("RUNTIME_TENANT"))
	}
	return nil
}

// enforceTenantFingerprint implements Layer 4 (adapter side) per
// spec §7.8 + §9.4. Aborts startup if CI=true and LINEAR_TENANT_TYPE
// != "test".
func enforceTenantFingerprint(getenv func(string) string) error {
	if strings.ToLower(getenv("CI")) == "true" && getenv("LINEAR_TENANT_TYPE") != "test" {
		return fmt.Errorf("%w: got LINEAR_TENANT_TYPE=%q", errTenantFingerprint, getenv("LINEAR_TENANT_TYPE"))
	}
	return nil
}

type config struct {
	LinearAPIToken       string
	LinearTeamID         string
	LinearAPIURL         string
	LinearTenantType     string
	RecommentMinInterval time.Duration
	ListenAddr           string
}

// loadConfig pulls config from getenv with explicit required-field
// validation per spec §7.7.
func loadConfig(getenv func(string) string) (config, error) {
	cfg := config{
		LinearAPIToken:   getenv("LINEAR_API_TOKEN"),
		LinearTeamID:     getenv("LINEAR_TEAM_ID"),
		LinearAPIURL:     getenv("LINEAR_API_URL"),
		LinearTenantType: getenv("LINEAR_TENANT_TYPE"),
		ListenAddr:       getenv("LISTEN_ADDR"),
	}
	if cfg.LinearAPIToken == "" {
		return config{}, errors.New("LINEAR_API_TOKEN is required")
	}
	if cfg.LinearTeamID == "" {
		return config{}, errors.New("LINEAR_TEAM_ID is required")
	}
	if cfg.LinearAPIURL == "" {
		cfg.LinearAPIURL = "https://api.linear.app/graphql"
	}
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = ":9095"
	}
	cfg.RecommentMinInterval = 15 * time.Minute
	if v := getenv("LINEAR_RECOMMENT_MIN_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return config{}, fmt.Errorf("LINEAR_RECOMMENT_MIN_INTERVAL: %w", err)
		}
		cfg.RecommentMinInterval = d
	}
	return cfg, nil
}
