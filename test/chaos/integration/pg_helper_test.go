//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// pgHandle wraps a running testcontainers Postgres instance.
// Mirrors internal/bootstrap/testhelpers_integration_test.go — copied
// because test helpers in _test.go files are not importable across packages.
type pgHandle struct {
	pool *pgxpool.Pool
	dsn  string
}

// startPGForChaos launches a postgres:15-alpine container, waits for
// readiness, applies migrations via pgxpool.New (bootstrap.BuildRuntime
// runs Migrate internally), and returns a pgHandle plus a teardown
// function. The returned pool is the same pool BuildRuntime creates; the
// test suite uses it for CountAll queries.
//
// NOTE: migrations are applied by bootstrap.BuildRuntime, not here. The
// pool is opened post-BuildRuntime via a second pgxpool.New with the same
// DSN. This keeps teardown simple.
func startPGForChaos(t *testing.T) (string, func()) {
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
		t.Fatalf("start pg container: %v", err)
	}
	dsn, err := c.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = c.Terminate(ctx)
		t.Fatalf("container conn string: %v", err)
	}
	teardown := func() {
		_ = c.Terminate(context.Background())
	}
	return dsn, teardown
}

// openPool opens a pgxpool against dsn. Caller must call pool.Close().
func openPool(t *testing.T, dsn string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	return pool
}
