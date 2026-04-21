package pg

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
	outbound "github.com/sophia-ecosystem/runtime-adapters/internal/ports/outbound"

	"github.com/jackc/pgx/v5/pgxpool"
)

// IdempotencyStorePG implements outbound.IdempotencyStore backed by
// PostgreSQL (T49). First-writer-wins via ON CONFLICT DO NOTHING.
// Safe for concurrent use.
type IdempotencyStorePG struct {
	pool *pgxpool.Pool
}

// NewIdempotencyStorePG constructs an IdempotencyStorePG. Returns an error
// if pool is nil.
func NewIdempotencyStorePG(pool *pgxpool.Pool) (*IdempotencyStorePG, error) {
	if pool == nil {
		return nil, fmt.Errorf("pg.IdempotencyStorePG: pool is required")
	}
	return &IdempotencyStorePG{pool: pool}, nil
}

// Lookup returns (receiptID, true, nil) when the key is present and within
// its window (expires_at > NOW()); (zero, false, nil) when absent or expired;
// or (zero, false, err) on I/O error.
func (s *IdempotencyStorePG) Lookup(ctx context.Context, key shared.IdempotencyKey) (shared.ReceiptID, bool, error) {
	const q = `SELECT receipt_id FROM idempotency_keys WHERE key = $1 AND expires_at > NOW()`

	row := s.pool.QueryRow(ctx, q, key.String())
	var receiptIDStr string
	if err := row.Scan(&receiptIDStr); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return shared.ReceiptID{}, false, nil
		}
		return shared.ReceiptID{}, false, fmt.Errorf("idempotency lookup: %w", err)
	}

	rid, err := shared.NewReceiptID(receiptIDStr)
	if err != nil {
		return shared.ReceiptID{}, false, fmt.Errorf("idempotency lookup: invalid receipt_id %q: %w", receiptIDStr, err)
	}
	return rid, true, nil
}

// Record associates key with receiptID for the given window. Uses
// ON CONFLICT DO NOTHING for first-writer-wins semantics. Returns
// outbound.ErrIdempotencyKeyConflict when the key is already recorded
// with a DIFFERENT receiptID. Returns nil when the key is absent (inserted)
// or when the key already maps to the same receiptID (idempotent no-op).
func (s *IdempotencyStorePG) Record(ctx context.Context, key shared.IdempotencyKey, receiptID shared.ReceiptID, window time.Duration) error {
	if window <= 0 {
		return fmt.Errorf("idempotency window must be > 0, got %v", window)
	}

	expiresAt := time.Now().Add(window).UTC()

	const q = `
INSERT INTO idempotency_keys (key, receipt_id, expires_at)
VALUES ($1, $2, $3)
ON CONFLICT (key) DO NOTHING`

	ct, err := s.pool.Exec(ctx, q, key.String(), receiptID.String(), expiresAt)
	if err != nil {
		return fmt.Errorf("idempotency record: %w", err)
	}

	if ct.RowsAffected() == 1 {
		// Inserted cleanly — first writer.
		return nil
	}

	// No rows affected means the key already exists. Determine whether
	// it maps to the same receipt (no-op) or a different one (conflict).
	const selQ = `SELECT receipt_id FROM idempotency_keys WHERE key = $1`
	row := s.pool.QueryRow(ctx, selQ, key.String())
	var existing string
	if err := row.Scan(&existing); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Key was deleted between the INSERT and this SELECT — treat as
			// a clean insert race; caller may retry.
			return nil
		}
		return fmt.Errorf("idempotency record follow-up: %w", err)
	}

	if existing == receiptID.String() {
		// Same receipt — idempotent no-op.
		return nil
	}
	return fmt.Errorf("%w: key=%s existing=%s new=%s",
		outbound.ErrIdempotencyKeyConflict, key.String(), existing, receiptID.String())
}
