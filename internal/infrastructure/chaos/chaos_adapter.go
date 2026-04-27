package chaos

import (
	"context"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/services"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
	"github.com/sophia-ecosystem/runtime-adapters/internal/ports/outbound"
)

// ChaosAdapter wraps an outbound.Adapter and injects faults defined in
// the immutable Profile loaded at bootstrap. R5 holds: the chaos
// decorator returns the same AdapterRawOutcome shape that the real
// adapter would produce for the equivalent real fault (I24).
//
// Capabilities not listed in the profile pass through to the real
// adapter unchanged. Capabilities listed dispatch via dispatchFault,
// which Task 1.3 stubs as pass-through; Task 1.4 implements the
// closed-enum switch over FaultKind.
type ChaosAdapter struct {
	real    outbound.Adapter
	profile *Profile
	clock   shared.Clock
}

// NewChaosAdapter constructs the decorator. real and profile must be non-nil;
// clock is required for fault kinds that need deterministic timing (latency,
// hang_until_cancel) once Task 1.4 lands.
func NewChaosAdapter(real outbound.Adapter, profile *Profile, clk shared.Clock) *ChaosAdapter {
	return &ChaosAdapter{real: real, profile: profile, clock: clk}
}

// ID delegates to the wrapped adapter.
func (c *ChaosAdapter) ID() valueobjects.AdapterID { return c.real.ID() }

// Capabilities delegates to the wrapped adapter.
func (c *ChaosAdapter) Capabilities() []valueobjects.Capability {
	return c.real.Capabilities()
}

// Execute dispatches to the real adapter when the profile has no fault
// configured for the requested capability; otherwise dispatches to
// dispatchFault.
func (c *ChaosAdapter) Execute(
	ctx context.Context,
	cap valueobjects.Capability,
	payload valueobjects.Payload,
) (services.AdapterRawOutcome, error) {
	fault, ok := c.profile.For(cap.Canonical())
	if !ok {
		return c.real.Execute(ctx, cap, payload)
	}
	return c.dispatchFault(ctx, cap, payload, fault)
}

// dispatchFault is stubbed in Task 1.3: delegates to the real adapter.
// Task 1.4 replaces this body with the closed-enum switch.
func (c *ChaosAdapter) dispatchFault(
	ctx context.Context,
	cap valueobjects.Capability,
	payload valueobjects.Payload,
	_ FaultConfig,
) (services.AdapterRawOutcome, error) {
	return c.real.Execute(ctx, cap, payload)
}

// Compile-time interface satisfaction.
var _ outbound.Adapter = (*ChaosAdapter)(nil)
