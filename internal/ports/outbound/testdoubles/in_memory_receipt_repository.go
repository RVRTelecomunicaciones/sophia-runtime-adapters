package testdoubles

import (
	"context"
	"fmt"
	"sync"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/entities"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
	"github.com/sophia-ecosystem/runtime-adapters/internal/ports/outbound"
)

// InMemoryReceiptRepository is a thread-safe map-backed ReceiptRepository
// for unit tests. Insert-only; Save rejects duplicate ReceiptIDs with
// ErrReceiptAlreadyExists (per D7.5).
type InMemoryReceiptRepository struct {
	clock shared.Clock

	mu sync.RWMutex
	m  map[string]entities.ExecutionReceipt
}

// NewInMemoryReceiptRepository returns an empty repository. clock is
// used to stamp PersistedAt on Save. If nil, shared.RealClock is used.
func NewInMemoryReceiptRepository(clock shared.Clock) *InMemoryReceiptRepository {
	if clock == nil {
		clock = shared.RealClock{}
	}
	return &InMemoryReceiptRepository{
		clock: clock,
		m:     map[string]entities.ExecutionReceipt{},
	}
}

// Save persists the receipt. Returns a copy with PersistedAt stamped.
// Returns ErrReceiptAlreadyExists if the ReceiptID is already present.
func (r *InMemoryReceiptRepository) Save(ctx context.Context, receipt entities.ExecutionReceipt) (entities.ExecutionReceipt, error) {
	if err := ctx.Err(); err != nil {
		return entities.ExecutionReceipt{}, err
	}
	key := receipt.ReceiptID().String()
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.m[key]; exists {
		return entities.ExecutionReceipt{}, fmt.Errorf("%w: %s", outbound.ErrReceiptAlreadyExists, key)
	}
	persisted := receipt.WithPersistedAt(r.clock.Now())
	r.m[key] = persisted
	return persisted, nil
}

// FindByID retrieves a receipt by ID. Returns ErrReceiptNotFound when absent.
func (r *InMemoryReceiptRepository) FindByID(ctx context.Context, id shared.ReceiptID) (entities.ExecutionReceipt, error) {
	if err := ctx.Err(); err != nil {
		return entities.ExecutionReceipt{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	rec, ok := r.m[id.String()]
	if !ok {
		return entities.ExecutionReceipt{}, fmt.Errorf("%w: %s", outbound.ErrReceiptNotFound, id.String())
	}
	return rec, nil
}

// Count returns the number of stored receipts. Test-only helper.
func (r *InMemoryReceiptRepository) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.m)
}

// Compile-time interface satisfaction.
var _ outbound.ReceiptRepository = (*InMemoryReceiptRepository)(nil)
