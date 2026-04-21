// Package migrations embeds the Phase 1 PostgreSQL schema DDL as a
// golang-migrate/migrate/v4 iofs source. The migrate.go helper in the
// parent pg/ package reads this FS at startup.
package migrations

import "embed"

// FS embeds every .sql file in this directory. Migration files follow
// the golang-migrate naming convention: NNNN_name.{up,down}.sql.
//
//go:embed *.sql
var FS embed.FS
