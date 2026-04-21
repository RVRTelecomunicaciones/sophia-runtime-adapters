//go:build integration

package bootstrap_test

import (
	"context"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/sophia-ecosystem/runtime-adapters/internal/bootstrap"
	"github.com/sophia-ecosystem/runtime-adapters/internal/infrastructure/config"
)

// pgHandle wraps a running testcontainers Postgres instance with its DSN.
type pgHandle struct {
	dsn string
}

// startPGForBootstrap launches a postgres:15-alpine container, waits for
// readiness, and returns a pgHandle plus a teardown function. It is safe
// to defer teardown.
func startPGForBootstrap(t *testing.T) (*pgHandle, func()) {
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
	h := &pgHandle{dsn: dsn}
	teardown := func() {
		_ = c.Terminate(context.Background())
	}
	return h, teardown
}

// buildSmokeRuntime builds a full Phase 1 Runtime against the given DSN
// using an ephemeral HTTP address. Caller must defer rt.Shutdown.
func buildSmokeRuntime(t *testing.T, dsn string, grace time.Duration) *bootstrap.Runtime {
	t.Helper()
	cfg := config.Config{
		HTTPAddr:                 "127.0.0.1:0",
		MaxTimeoutBudget:         30 * time.Minute,
		MaxPayloadBytes:          1 << 20,
		InlineStreamLimit:        16 * 1024,
		IdempotencyWindow:        24 * time.Hour,
		ShutdownGracePeriod:      grace,
		MaxConcurrentExecutions:  64,
		AllowedCommandsPath:      []string{"/usr/bin", "/bin"},
		AllowedWorkingDirs:       []string{"/tmp"},
		AllowedFilesystemRoots:   []string{"/tmp"},
		HTTPAllowPrivateNetworks: true,
		PostgresDSN:              dsn,
		RuntimeVersion:           "0.1.0-test",
		Hostname:                 "shutdown-test",
		ProvenanceSource:         "test",
	}
	rt, err := bootstrap.BuildRuntime(context.Background(), cfg)
	if err != nil {
		t.Fatalf("BuildRuntime: %v", err)
	}
	return rt
}
