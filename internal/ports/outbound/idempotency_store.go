package outbound

import (
	"context"
	"errors"
	"time"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
)

var (
	// ErrIdempotencyKeyNotFound is returned by Lookup when the key is
	// not present or its window has expired. Lookup also accepts
	// "found=false" as the non-error absence signal; ErrIdempotencyKeyNotFound
	// is reserved for explicit miss-with-error semantics at higher layers.
	ErrIdempotencyKeyNotFound = errors.New("idempotency key not found")

	// ErrIdempotencyKeyConflict is returned by Record when the key
	// already exists with a different ReceiptID. Implementations SHOULD
	// use ON CONFLICT DO NOTHING semantics to let first-writer win;
	// conflict occurs only under race conditions that two callers attempt
	// to register the same key for different receipts.
	ErrIdempotencyKeyConflict = errors.New("idempotency key conflict")
)

// IdempotencyStore is the outbound port for caller-owned idempotency
// (D2.5, D6.4 — replay-everything). Implementations must be safe for
// concurrent use.
type IdempotencyStore interface {
	// Lookup returns (receiptID, true, nil) when the key is present and
	// within the configured window; (zero, false, nil) when absent or
	// expired; or (zero, false, err) for I/O errors.
	Lookup(ctx context.Context, key shared.IdempotencyKey) (shared.ReceiptID, bool, error)

	// Record associates key with receiptID for the given window. Idempotent:
	// first writer wins (ON CONFLICT DO NOTHING). Returns ErrIdempotencyKeyConflict
	// only when the key is already recorded with a DIFFERENT receiptID
	// (true race condition — rare).
	Record(ctx context.Context, key shared.IdempotencyKey, receiptID shared.ReceiptID, window time.Duration) error
}
