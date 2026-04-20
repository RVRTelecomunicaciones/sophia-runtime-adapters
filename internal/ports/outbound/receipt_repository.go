package outbound

import (
	"context"
	"errors"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/entities"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
)

// Sentinel errors for receipt repository operations. Callers use
// errors.Is to distinguish semantic conditions from transient
// I/O failures.
var (
	// ErrReceiptNotFound is returned by FindByID when no receipt
	// exists for the given ID.
	ErrReceiptNotFound = errors.New("receipt not found")

	// ErrReceiptAlreadyExists is returned by Save when a receipt with
	// the same ReceiptID has already been persisted. The repository is
	// insert-only (D7.5 / I2).
	ErrReceiptAlreadyExists = errors.New("receipt already exists")
)

// ReceiptRepository is the outbound port for persisting and retrieving
// execution receipts. Insert-only per D7.5. Implementations must be
// safe for concurrent use.
type ReceiptRepository interface {
	// Save persists the receipt. On success the returned receipt has
	// its PersistedAt field populated (via ExecutionReceipt.WithPersistedAt).
	// Returns ErrReceiptAlreadyExists if a receipt with the same
	// ReceiptID is already persisted.
	Save(ctx context.Context, receipt entities.ExecutionReceipt) (entities.ExecutionReceipt, error)

	// FindByID retrieves a receipt. Returns ErrReceiptNotFound when no
	// receipt exists for the given ID.
	FindByID(ctx context.Context, id shared.ReceiptID) (entities.ExecutionReceipt, error)
}
