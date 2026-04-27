package chaos

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sophia-ecosystem/runtime-adapters/internal/ports/outbound"
	"github.com/sophia-ecosystem/runtime-adapters/internal/ports/outbound/testdoubles"
)

// validReceiptULID is the ULID used by default for receipt test fixtures.
const validReceiptULID = "01HZXK5JC6QK7XV0YQXA0QJ0YZ"

// --- 1. No fault → Save delegates ---

func TestChaosReceiptRepository_NoFault_DelegatesSave(t *testing.T) {
	real := testdoubles.NewInMemoryReceiptRepository(nil)
	profile := &Profile{
		Version:  1,
		Name:     "no-fault",
		Adapters: map[string]FaultConfig{},
		Persist:  nil,
	}
	repo := NewChaosReceiptRepository(real, profile)

	r := mustReceipt(t, validReceiptULID)
	saved, err := repo.Save(context.Background(), r)
	require.NoError(t, err)

	// PersistedAt must be stamped by the underlying repository.
	_, ok := saved.PersistedAt()
	assert.True(t, ok, "PersistedAt must be set after a successful Save")

	// Underlying repository must now hold the receipt.
	assert.Equal(t, 1, real.Count(), "underlying repository must contain the saved receipt")
}

// --- 2. Persist fault active → Save returns ErrChaosPersistFailure ---

func TestChaosReceiptRepository_PersistFaultActive_SaveReturnsErr(t *testing.T) {
	real := testdoubles.NewInMemoryReceiptRepository(nil)
	profile := &Profile{
		Version:  1,
		Name:     "persist-fault",
		Adapters: map[string]FaultConfig{},
		Persist:  &PersistFaultConfig{Kind: FaultPersistFailure},
	}
	repo := NewChaosReceiptRepository(real, profile)

	r := mustReceipt(t, validReceiptULID)
	_, err := repo.Save(context.Background(), r)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrChaosPersistFailure)

	// The underlying repository must remain empty — real Save was never called.
	assert.Equal(t, 0, real.Count(), "underlying repository must be empty when chaos fault fires")
}

// --- 3. errors.Is unwrapping ---

func TestChaosReceiptRepository_ErrorIs_ChaosPersistFailure(t *testing.T) {
	real := testdoubles.NewInMemoryReceiptRepository(nil)
	profile := &Profile{
		Version: 1,
		Name:    "persist-fault",
		Persist: &PersistFaultConfig{Kind: FaultPersistFailure},
	}
	repo := NewChaosReceiptRepository(real, profile)

	r := mustReceipt(t, validReceiptULID)
	_, err := repo.Save(context.Background(), r)
	require.Error(t, err)

	// errors.Is must work for callers that check the sentinel.
	assert.True(t, errors.Is(err, ErrChaosPersistFailure),
		"errors.Is(err, ErrChaosPersistFailure) must be true; got: %v", err)
}

// --- 4. FindByID always delegates (no fault profile) ---

func TestChaosReceiptRepository_FindByID_AlwaysDelegates_NoFaultProfile(t *testing.T) {
	real := testdoubles.NewInMemoryReceiptRepository(nil)
	profile := &Profile{
		Version:  1,
		Name:     "no-fault",
		Adapters: map[string]FaultConfig{},
		Persist:  nil,
	}
	repo := NewChaosReceiptRepository(real, profile)

	// Pre-populate via the wrapper's Save (which delegates when no fault).
	r := mustReceipt(t, validReceiptULID)
	saved, err := repo.Save(context.Background(), r)
	require.NoError(t, err)

	// FindByID must return the saved receipt.
	found, err := repo.FindByID(context.Background(), saved.ReceiptID())
	require.NoError(t, err)
	assert.Equal(t, saved.ReceiptID().String(), found.ReceiptID().String())
}

// --- 5. FindByID always delegates even when persist fault is set ---

func TestChaosReceiptRepository_FindByID_AlwaysDelegates_FaultProfile(t *testing.T) {
	real := testdoubles.NewInMemoryReceiptRepository(nil)
	// Pre-populate the underlying repo directly to bypass the chaos wrapper.
	r := mustReceipt(t, validReceiptULID)
	saved, err := real.Save(context.Background(), r)
	require.NoError(t, err)

	// Now wrap with a persist fault profile.
	profile := &Profile{
		Version: 1,
		Name:    "persist-fault",
		Persist: &PersistFaultConfig{Kind: FaultPersistFailure},
	}
	repo := NewChaosReceiptRepository(real, profile)

	// FindByID must delegate even though Persist fault is set.
	found, err := repo.FindByID(context.Background(), saved.ReceiptID())
	require.NoError(t, err, "FindByID must never fault — chaos targets Save only")
	assert.Equal(t, saved.ReceiptID().String(), found.ReceiptID().String())
}

// --- 6. Nil profile → no fault, delegates normally ---

func TestChaosReceiptRepository_NilProfile_NoFault(t *testing.T) {
	real := testdoubles.NewInMemoryReceiptRepository(nil)
	// profile = nil is explicitly allowed by NewChaosReceiptRepository.
	repo := NewChaosReceiptRepository(real, nil)

	r := mustReceipt(t, validReceiptULID)

	// Save must delegate.
	saved, err := repo.Save(context.Background(), r)
	require.NoError(t, err)
	_, ok := saved.PersistedAt()
	assert.True(t, ok)

	// FindByID must delegate.
	found, err := repo.FindByID(context.Background(), saved.ReceiptID())
	require.NoError(t, err)
	assert.Equal(t, saved.ReceiptID().String(), found.ReceiptID().String())
}

// --- 7. Nil real panics ---

func TestChaosReceiptRepository_NilReal_Panics(t *testing.T) {
	profile := &Profile{Version: 1, Name: "any"}
	require.PanicsWithValue(t,
		"chaos.NewChaosReceiptRepository: real repository must not be nil",
		func() { NewChaosReceiptRepository(nil, profile) },
	)
}

// --- 8. Compile-time + runtime interface satisfaction ---

func TestChaosReceiptRepository_ImplementsReceiptRepository(t *testing.T) {
	// compile-time check via package-level var _ in chaos_receipt_repository.go
	real := testdoubles.NewInMemoryReceiptRepository(nil)
	repo := NewChaosReceiptRepository(real, nil)

	// runtime assertion via interface assignment
	var _ outbound.ReceiptRepository = repo
}
