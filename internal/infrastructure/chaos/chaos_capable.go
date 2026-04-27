package chaos

import (
	"context"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/services"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/ports/outbound"
)

// ChaosCapable is the opt-in interface that Phase 1 adapters implement to
// support adapter-synthetic chaos faults. The interface keeps Phase 1's raw
// outcome types unexported (R5) — synthesis happens inside the adapter
// package using the same private type as a real-fault outcome (I24).
//
// Adapters that do not implement ChaosCapable can still be targeted by
// decorator-native faults (latency, hang_until_cancel, inject_panic) which
// the ChaosAdapter handles without any adapter cooperation.
//
// See spec Appendix D and engram topic phase-2c.3/architectural-pivot-chaoscapable.
type ChaosCapable interface {
	outbound.Adapter

	// SupportedChaosFaults returns the chaos fault kinds this adapter can
	// synthesise for the given capability. The list does NOT include
	// decorator-native faults — those are universally available across
	// every adapter regardless of ChaosCapable.
	//
	// Returns an empty (or nil) slice if cap is not owned by this adapter.
	SupportedChaosFaults(cap valueobjects.Capability) []FaultKind

	// SyntheticOutcome constructs an AdapterRawOutcome of the same shape
	// the adapter would produce for the equivalent real fault.
	// Implementations may inspect payload fields they would use in a real
	// execution (e.g. the command string for process_signal, the URL for
	// remote_unreachable) to produce maximally realistic shapes per I24.
	// They MUST NOT execute any side effects — no real subprocesses, no
	// real network calls, no file writes. Returns (outcome, true) on
	// success; (nil, false) if the fault is unsupported for this
	// capability. The (nil, false) path is defensive —
	// ValidateProfileSupport should reject this case at startup.
	SyntheticOutcome(
		ctx context.Context,
		cap valueobjects.Capability,
		payload valueobjects.Payload,
		fault FaultConfig,
	) (services.AdapterRawOutcome, bool)
}

// IsDecoratorNative reports whether the fault is handled directly by
// ChaosAdapter without delegating to ChaosCapable. The three decorator-
// native kinds are universally supported across all adapters.
func IsDecoratorNative(k FaultKind) bool {
	switch k {
	case FaultLatency, FaultHangUntilCancel, FaultInjectPanic:
		return true
	default:
		return false
	}
}
