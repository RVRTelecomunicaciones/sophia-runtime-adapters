package chaos

import (
	"context"
	"fmt"
	"time"

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

// NewChaosAdapter constructs the decorator. real must be non-nil and is
// enforced via panic — a nil wrapped adapter is a programming error
// (chaos wiring must always hand off a real adapter, never an unbound one).
// profile may be nil; Profile methods nil-guard the receiver. clock is
// required for fault kinds that need deterministic timing (latency,
// hang_until_cancel) once Task 1.4 lands.
func NewChaosAdapter(real outbound.Adapter, profile *Profile, clk shared.Clock) *ChaosAdapter {
	if real == nil {
		panic("chaos.NewChaosAdapter: real adapter must not be nil")
	}
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

// dispatchFault implements the closed-enum switch over FaultKind (Task 1.4).
//
// Decorator-native faults (latency, hang_until_cancel, inject_panic) are
// handled here without adapter cooperation. Adapter-synthetic faults are
// delegated to the real adapter via the ChaosCapable interface. FaultPersistFailure
// is defensive-only — it belongs to ChaosReceiptStore; ValidateProfileSupport
// should have rejected any profile that routes it here at bootstrap.
func (c *ChaosAdapter) dispatchFault(
	ctx context.Context,
	cap valueobjects.Capability,
	payload valueobjects.Payload,
	fault FaultConfig,
) (services.AdapterRawOutcome, error) {
	switch fault.Kind {
	case FaultLatency:
		// ctx-aware sleep: if ctx fires before the timer, abort immediately.
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(parseLatency(fault.Match)):
		}
		return c.real.Execute(ctx, cap, payload)

	case FaultHangUntilCancel:
		<-ctx.Done()
		return nil, ctx.Err()

	case FaultInjectPanic:
		// R4: the upstream middleware's panic recoverer catches this and
		// maps it to AdapterInternalError. ChaosAdapter's job is to raise.
		panic(fmt.Sprintf("chaos: injected panic for capability %q", cap.Canonical()))

	case FaultPersistFailure:
		// Defensive: this fault targets ChaosReceiptStore, not an adapter.
		// ValidateProfileSupport (Task 1.4a) should reject this at startup.
		return nil, fmt.Errorf(
			"chaos: persist_failure dispatched to ChaosAdapter (should be ChaosReceiptStore); " +
				"ValidateProfileSupport should reject this at startup",
		)

	default:
		// Adapter-synthetic fault — delegate to ChaosCapable.
		cc, ok := c.real.(ChaosCapable)
		if !ok {
			return nil, fmt.Errorf(
				"chaos: adapter %q does not implement ChaosCapable; "+
					"fault %q dispatch failed (ValidateProfileSupport should have rejected this at startup)",
				c.real.ID(), fault.Kind,
			)
		}
		out, ok := cc.SyntheticOutcome(ctx, cap, payload, fault)
		if !ok {
			return nil, fmt.Errorf(
				"chaos: adapter %q ChaosCapable did not synthesise for fault %q on capability %q",
				c.real.ID(), fault.Kind, cap.Canonical(),
			)
		}
		return out, nil
	}
}

// parseLatency reads fault.Match["duration"] as a Go time.Duration string
// (e.g. "5ms", "1s", "250ms"). Falls back to 100ms on a missing or unparseable value.
// The clock field on ChaosAdapter is preserved for future use (Task 1.5+) but
// dispatchFault uses time.After directly for the ctx-aware sleep pattern.
func parseLatency(match map[string]string) time.Duration {
	if v, ok := match["duration"]; ok {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return 100 * time.Millisecond
}

// Compile-time interface satisfaction.
var _ outbound.Adapter = (*ChaosAdapter)(nil)
