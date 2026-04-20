package testdoubles

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
	"github.com/sophia-ecosystem/runtime-adapters/internal/ports/outbound"
)

// InMemoryIdempotencyStore is a map-backed IdempotencyStore that uses a
// clock-driven expiry check. Safe for concurrent use.
type InMemoryIdempotencyStore struct {
	clock shared.Clock

	mu sync.RWMutex
	m  map[string]idempotencyEntry
}

type idempotencyEntry struct {
	receiptID shared.ReceiptID
	expiresAt time.Time
}

// NewInMemoryIdempotencyStore returns an empty store. clock is used for
// expiry checks in Lookup and for stamping expiresAt in Record. If nil,
// shared.RealClock is used.
func NewInMemoryIdempotencyStore(clock shared.Clock) *InMemoryIdempotencyStore {
	if clock == nil {
		clock = shared.RealClock{}
	}
	return &InMemoryIdempotencyStore{
		clock: clock,
		m:     map[string]idempotencyEntry{},
	}
}

// Lookup returns (receiptID, true, nil) when the key is present and within
// its window; (zero, false, nil) when absent or expired; (zero, false, err)
// for context errors.
func (s *InMemoryIdempotencyStore) Lookup(ctx context.Context, key shared.IdempotencyKey) (shared.ReceiptID, bool, error) {
	if err := ctx.Err(); err != nil {
		return shared.ReceiptID{}, false, err
	}
	s.mu.RLock()
	entry, ok := s.m[key.String()]
	s.mu.RUnlock()
	if !ok {
		return shared.ReceiptID{}, false, nil
	}
	if !s.clock.Now().Before(entry.expiresAt) {
		// expired
		return shared.ReceiptID{}, false, nil
	}
	return entry.receiptID, true, nil
}

// Record associates key with receiptID for the given window. First writer
// wins. Returns ErrIdempotencyKeyConflict only when the key is already
// recorded with a DIFFERENT receiptID. Returns an error for window <= 0.
func (s *InMemoryIdempotencyStore) Record(ctx context.Context, key shared.IdempotencyKey, id shared.ReceiptID, window time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if window <= 0 {
		return fmt.Errorf("window must be > 0, got %v", window)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.m[key.String()]
	if ok {
		// First-writer-wins UNLESS the existing key maps to a different receipt.
		if existing.receiptID != id {
			return fmt.Errorf("%w: key=%s existing=%s new=%s",
				outbound.ErrIdempotencyKeyConflict,
				key.String(), existing.receiptID.String(), id.String())
		}
		// Same key same receipt = idempotent no-op.
		return nil
	}
	s.m[key.String()] = idempotencyEntry{
		receiptID: id,
		expiresAt: s.clock.Now().Add(window),
	}
	return nil
}

// Compile-time interface satisfaction.
var _ outbound.IdempotencyStore = (*InMemoryIdempotencyStore)(nil)
