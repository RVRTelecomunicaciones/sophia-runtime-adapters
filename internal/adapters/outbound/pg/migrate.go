// Package pg implements the Phase 1 PostgreSQL persistence adapters —
// ReceiptRepositoryPG (T48) and IdempotencyStorePG (T49). T47 provides
// the schema + a Migrate helper that applies the embedded migrations
// via golang-migrate/migrate/v4.
package pg

import (
	"context"
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	pgxmigrate "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sophia-ecosystem/runtime-adapters/internal/adapters/outbound/pg/migrations"
)

// Migrate applies every embedded migration to the database behind pool.
// Idempotent: re-running on an already-migrated database is a no-op.
// Returns nil when migrations reach HEAD or were already at HEAD.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return fmt.Errorf("pg.Migrate: pool is required")
	}

	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("embedded migration source: %w", err)
	}
	defer func() { _ = src.Close() }()

	// Acquire a dedicated connection; golang-migrate's pgx/v5 driver
	// requires a stdlib-compatible *sql.DB OR a pgx.Conn. We adapt
	// via the pgxmigrate.WithInstance path using pgxpool.Pool.
	// Since golang-migrate's pgx/v5 driver takes a connection string
	// in its primary API, the simplest robust path is to acquire the
	// DSN from the pool's config and pass it as the database URL.
	dbURL := pool.Config().ConnString()
	if dbURL == "" {
		return fmt.Errorf("pg.Migrate: pool has no connection string")
	}

	m, err := migrate.NewWithSourceInstance("iofs", src, "pgx5://"+stripScheme(dbURL))
	if err != nil {
		// Fallback: some pool configs don't include scheme — retry with the URL as-is.
		m, err = migrate.NewWithSourceInstance("iofs", src, dbURL)
		if err != nil {
			return fmt.Errorf("new migrate instance: %w", err)
		}
	}
	defer func() {
		_, _ = m.Close()
	}()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}

// stripScheme removes a leading postgres:// or postgresql:// scheme from
// dbURL so callers can re-prefix with pgx5://. migrate's pgx/v5 driver
// registers itself under the "pgx5" scheme; pool ConnStrings use
// "postgres://" or "postgresql://".
func stripScheme(url string) string {
	for _, prefix := range []string{"postgres://", "postgresql://"} {
		if len(url) > len(prefix) && url[:len(prefix)] == prefix {
			return url[len(prefix):]
		}
	}
	return url
}

// MigrateDSN is a convenience wrapper that accepts a raw DSN (postgres://
// or postgresql://) and applies migrations without requiring a pre-opened
// pool. Used by bootstrap/wire.go when the pool doesn't yet exist.
func MigrateDSN(ctx context.Context, dsn string) error {
	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("embedded migration source: %w", err)
	}
	defer func() { _ = src.Close() }()

	m, err := migrate.NewWithSourceInstance("iofs", src, "pgx5://"+stripScheme(dsn))
	if err != nil {
		m, err = migrate.NewWithSourceInstance("iofs", src, dsn)
		if err != nil {
			return fmt.Errorf("new migrate instance: %w", err)
		}
	}
	defer func() {
		_, _ = m.Close()
	}()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}

// Ensure the pgxmigrate driver registers itself at import time.
var _ = pgxmigrate.Postgres{}
