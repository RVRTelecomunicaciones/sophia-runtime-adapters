package chaos

import (
	"context"
	"errors"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/entities"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
	"github.com/sophia-ecosystem/runtime-adapters/internal/ports/outbound"
)

// ErrChaosPersistFailure is the sentinel error returned by
// ChaosReceiptRepository.Save when the active profile defines a persist fault.
// Callers use errors.Is to detect injected persist failures:
//
//	errors.Is(err, chaos.ErrChaosPersistFailure)
//
// The runtime treats this like any real persist failure (A4.3): the receipt
// never becomes visible and the caller receives 5xx.
var ErrChaosPersistFailure = errors.New("chaos: injected persist failure")

// ChaosReceiptRepository wraps outbound.ReceiptRepository and injects a
// persist failure when the active profile defines a persistence fault.
// FindByID always delegates — chaos targets the persistence boundary
// only, not retrieval.
//
// When profile.PersistFault() is non-nil, Save returns
// ErrChaosPersistFailure without calling the underlying repository.
// The runtime treats this like any other persist failure: A4.3 holds,
// caller receives 5xx, runtime_adapters.receipt.persist.failures
// counter increments.
//
// See spec §5.2, D2C3.8, A4.3.
type ChaosReceiptRepository struct {
	real    outbound.ReceiptRepository
	profile *Profile
}

// NewChaosReceiptRepository constructs the decorator. real must be non-nil
// (matches the precedent set by NewChaosAdapter); profile may be nil and
// PersistFault gracefully returns nil for a nil profile.
func NewChaosReceiptRepository(real outbound.ReceiptRepository, profile *Profile) *ChaosReceiptRepository {
	if real == nil {
		panic("chaos.NewChaosReceiptRepository: real repository must not be nil")
	}
	return &ChaosReceiptRepository{real: real, profile: profile}
}

// Save delegates to the underlying repository, OR returns
// ErrChaosPersistFailure when the active profile defines a persist
// fault. R5/I24 hold: callers see the same shape (an error from Save)
// they would see for a real persist failure.
func (c *ChaosReceiptRepository) Save(ctx context.Context, receipt entities.ExecutionReceipt) (entities.ExecutionReceipt, error) {
	if c.profile.PersistFault() != nil {
		return entities.ExecutionReceipt{}, ErrChaosPersistFailure
	}
	return c.real.Save(ctx, receipt)
}

// FindByID always delegates — chaos targets only the persistence
// boundary (Save), not retrieval.
func (c *ChaosReceiptRepository) FindByID(ctx context.Context, id shared.ReceiptID) (entities.ExecutionReceipt, error) {
	return c.real.FindByID(ctx, id)
}

// Compile-time interface satisfaction.
var _ outbound.ReceiptRepository = (*ChaosReceiptRepository)(nil)
