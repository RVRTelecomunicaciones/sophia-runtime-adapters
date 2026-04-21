# ADR 0004 — pgx/v5 for PostgreSQL persistence

- **Status:** accepted
- **Date:** 2026-04-19
- **Deciders:** Russell Vergara
- **Context:** Phase 1 persistence layer (T47-T50) needs a Postgres driver for the receipt + idempotency tables. Options: stdlib `database/sql` with `lib/pq` or `pgx` stdlib mode, native `pgx/v5`. Phase 1 needs explicit types (JSONB, TIMESTAMPTZ), connection pooling, and clear errors for unique_violation detection.
- **Options considered:**
  - Option A — `database/sql` + `github.com/lib/pq`: stdlib-idiomatic, mature, but no native PG types and weaker error mapping (SQL state codes via string parsing).
  - Option B — `database/sql` + `pgx/v5/stdlib`: stdlib-shaped API with pgx under the hood; better than lib/pq but gives up the native pgx benefits.
  - Option C — native `github.com/jackc/pgx/v5` + `pgxpool`: typed *pgconn.PgError (code 23505 for unique_violation), first-class JSONB support, built-in connection pooling.
- **Decision:** Use native `pgx/v5` with `pgxpool` (plan P3). Drops the `database/sql` abstraction layer that adds zero value for our Phase 1 shape (2 tables, append-only writes, narrow read paths).
- **Consequences:**
  - One less adapter between code and Postgres; error handling is explicit (`errors.As(err, &pgErr); pgErr.Code == "23505"`).
  - Connection pool is `*pgxpool.Pool`; test doubles live in `internal/ports/outbound/testdoubles/` for unit tests; real PG reached via testcontainers-go in integration tests.
  - Migrations applied via `golang-migrate/migrate/v4` with its pgx/v5 driver — matches the pool's runtime.
  - No compatibility with `database/sql`-expecting libraries (e.g., some ORM helpers), but Phase 1 has no such consumers.
- **Spec references:** P3 (plan), T47 schema, T48 ReceiptRepositoryPG, T49 IdempotencyStorePG.
