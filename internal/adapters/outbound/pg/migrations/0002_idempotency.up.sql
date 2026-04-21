-- Caller-owned idempotency (D2.5 / D6.4 replay-everything). First-writer-
-- wins semantics via PRIMARY KEY + ON CONFLICT DO NOTHING. Expiry is
-- window-based; consumers (T49) check expires_at on lookup.

CREATE TABLE idempotency_keys (
    key         TEXT PRIMARY KEY,
    receipt_id  TEXT NOT NULL REFERENCES execution_receipts(receipt_id),
    expires_at  TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_idempotency_expires_at ON idempotency_keys (expires_at);
