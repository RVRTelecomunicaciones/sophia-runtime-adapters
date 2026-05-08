//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// startPGForLogPipeline launches a postgres:15-alpine testcontainer for
// the log pipeline integration tests and returns a DSN plus a teardown
// function. Migrations are applied by bootstrap.BuildRuntime, not here.
//
// Mirror of test/chaos/integration/pg_helper_test.go (which is package-
// scoped to that directory and therefore not importable). The duplication
// is deliberate: integration helpers live next to the tests that consume
// them.
func startPGForLogPipeline(t *testing.T) (string, func()) {
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
