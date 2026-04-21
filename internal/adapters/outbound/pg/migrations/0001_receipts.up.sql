-- Phase 1 receipts table. Insert-only per D7.5 / I2. Stores the full
-- receipt as JSONB for read-back fidelity plus denormalized columns
-- for indexable audit queries.

CREATE TABLE execution_receipts (
    receipt_id       TEXT PRIMARY KEY,
    schema_version   TEXT NOT NULL,
    correlation_id   TEXT NOT NULL,
    adapter_id       TEXT NOT NULL,
    capability       TEXT NOT NULL,       -- canonical e.g. "shell.exec@v1"
    status           TEXT NOT NULL,       -- success|failure|timeout|cancelled|partial
    error_class      TEXT,                -- nullable: empty when status=success
    retry_hint       TEXT NOT NULL,
    submitted_at     TIMESTAMPTZ NOT NULL,
    started_at       TIMESTAMPTZ NOT NULL,
    completed_at     TIMESTAMPTZ NOT NULL,
    persisted_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    duration_ms      BIGINT NOT NULL,
    payload          JSONB NOT NULL,
    result           JSONB NOT NULL,
    provenance       JSONB NOT NULL,
    full_receipt     JSONB NOT NULL       -- authoritative; FindByID unmarshals this
);

CREATE INDEX idx_receipts_correlation_id  ON execution_receipts (correlation_id);
CREATE INDEX idx_receipts_capability       ON execution_receipts (capability);
CREATE INDEX idx_receipts_completed_at     ON execution_receipts (completed_at DESC);
CREATE INDEX idx_receipts_status           ON execution_receipts (status);
