package chaos

import (
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
	"github.com/sophia-ecosystem/runtime-adapters/internal/ports/outbound"
)

// MaybeWrapAdaptersWithChaos returns the adapter registry and receipt
// repository, either unchanged (chaos disabled / nil cfg) or wrapped
// with ChaosAdapter and possibly ChaosReceiptRepository per the active
// profile.
//
// When chaos is enabled, ValidateProfileSupport is called first to
// enforce R17 fail-closed: if any (capability, fault) pair in the
// profile names a fault the wrapped adapter does not support, this
// function returns an error and bootstrap aborts. No partial wrap is
// observable.
//
// Pure function: no side effects beyond constructing the wrappers.
// Fully testable in isolation (no DB, no I/O).
//
// (D2C3.12, Appendix D wiring path.)
func MaybeWrapAdaptersWithChaos(
	registry map[valueobjects.AdapterID]outbound.Adapter,
	store outbound.ReceiptRepository,
	cfg *Config,
	clk shared.Clock,
) (map[valueobjects.AdapterID]outbound.Adapter, outbound.ReceiptRepository, error) {
	if cfg == nil || !cfg.Enabled {
		return registry, store, nil
	}
	if err := ValidateProfileSupport(registry, cfg.Profile); err != nil {
		return nil, nil, err
	}
	wrapped := make(map[valueobjects.AdapterID]outbound.Adapter, len(registry))
	for id, real := range registry {
		wrapped[id] = NewChaosAdapter(real, cfg.Profile, clk)
	}
	if cfg.Profile.PersistFault() != nil {
		store = NewChaosReceiptRepository(store, cfg.Profile)
	}
	return wrapped, store, nil
}
