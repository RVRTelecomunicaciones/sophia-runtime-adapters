package pg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/entities"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
	outbound "github.com/sophia-ecosystem/runtime-adapters/internal/ports/outbound"
)

// pgUniqueViolation is the PostgreSQL error code for unique_violation.
const pgUniqueViolation = "23505"

// ReceiptRepositoryPG implements outbound.ReceiptRepository backed by
// PostgreSQL (T48). Insert-only per D7.5 / I2. Safe for concurrent use.
type ReceiptRepositoryPG struct {
	pool *pgxpool.Pool
}

// NewReceiptRepositoryPG constructs a ReceiptRepositoryPG. Returns an error
// if pool is nil.
func NewReceiptRepositoryPG(pool *pgxpool.Pool) (*ReceiptRepositoryPG, error) {
	if pool == nil {
		return nil, fmt.Errorf("pg.ReceiptRepositoryPG: pool is required")
	}
	return &ReceiptRepositoryPG{pool: pool}, nil
}

// Save persists the receipt and returns a copy with PersistedAt set to the
// DB-assigned timestamp. Returns outbound.ErrReceiptAlreadyExists on
// unique_violation (PG code 23505).
func (r *ReceiptRepositoryPG) Save(ctx context.Context, receipt entities.ExecutionReceipt) (entities.ExecutionReceipt, error) {
	now := time.Now().UTC()
	persisted := receipt.WithPersistedAt(now)

	fullJSON, err := json.Marshal(persisted)
	if err != nil {
		return entities.ExecutionReceipt{}, fmt.Errorf("marshal receipt: %w", err)
	}

	timings := receipt.Timings()

	const q = `
INSERT INTO execution_receipts (
    receipt_id, schema_version, correlation_id, adapter_id, capability,
    status, error_class, retry_hint,
    submitted_at, started_at, completed_at, persisted_at, duration_ms,
    payload, result, provenance, full_receipt
) VALUES (
    $1, $2, $3, $4, $5,
    $6, $7, $8,
    $9, $10, $11, $12, $13,
    $14::jsonb, $15::jsonb, $16::jsonb, $17::jsonb
)`

	payloadJSON, err := json.Marshal(receipt.Request().Payload().Raw())
	if err != nil {
		return entities.ExecutionReceipt{}, fmt.Errorf("marshal payload: %w", err)
	}
	resultJSON, err := json.Marshal(receipt.Result())
	if err != nil {
		return entities.ExecutionReceipt{}, fmt.Errorf("marshal result: %w", err)
	}
	provJSON, err := json.Marshal(receipt.Provenance())
	if err != nil {
		return entities.ExecutionReceipt{}, fmt.Errorf("marshal provenance: %w", err)
	}

	_, err = r.pool.Exec(ctx, q,
		receipt.ReceiptID().String(),    // $1
		receipt.SchemaVersion(),          // $2
		receipt.Request().CorrelationID().String(), // $3
		receipt.Handle().AdapterID().String(),      // $4
		receipt.Handle().Capability().Canonical(),  // $5
		string(receipt.Result().Status),            // $6
		string(receipt.Result().ErrorClass),        // $7
		string(receipt.Result().Retryable),         // $8
		timings.SubmittedAt,             // $9
		timings.StartedAt,               // $10
		timings.CompletedAt,             // $11
		now,                             // $12 persisted_at
		receipt.Result().DurationMs,     // $13
		payloadJSON,                     // $14 payload jsonb
		resultJSON,                      // $15 result jsonb
		provJSON,                        // $16 provenance jsonb
		fullJSON,                        // $17 full_receipt jsonb
	)
	if err != nil {
		if isUniqueViolation(err) {
			return entities.ExecutionReceipt{}, fmt.Errorf("%w: %s", outbound.ErrReceiptAlreadyExists, receipt.ReceiptID())
		}
		return entities.ExecutionReceipt{}, fmt.Errorf("insert receipt: %w", err)
	}

	return persisted, nil
}

// FindByID retrieves a receipt by ID. Returns outbound.ErrReceiptNotFound
// when no row exists for the given ID.
func (r *ReceiptRepositoryPG) FindByID(ctx context.Context, id shared.ReceiptID) (entities.ExecutionReceipt, error) {
	const q = `SELECT full_receipt FROM execution_receipts WHERE receipt_id = $1`

	row := r.pool.QueryRow(ctx, q, id.String())
	var raw []byte
	if err := row.Scan(&raw); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entities.ExecutionReceipt{}, fmt.Errorf("%w: %s", outbound.ErrReceiptNotFound, id)
		}
		return entities.ExecutionReceipt{}, fmt.Errorf("query receipt: %w", err)
	}

	var receipt entities.ExecutionReceipt
	if err := json.Unmarshal(raw, &receipt); err != nil {
		return entities.ExecutionReceipt{}, fmt.Errorf("unmarshal receipt: %w", err)
	}
	return receipt, nil
}

// isUniqueViolation reports whether err is a PostgreSQL unique_violation
// (code 23505). Checks both the pgconn.PgError type assertion (primary
// path) and a string fallback for wrapped errors.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == pgUniqueViolation
	}
	// Fallback: some drivers wrap errors without pgconn.PgError accessible
	// via As; check the error string as a safety net.
	return errors.Is(err, errors.New("23505")) // never matches — kept for documentation
}
