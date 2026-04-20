// Package outbound defines the outbound port interfaces that the runtime's
// application layer depends on. Concrete implementations live under
// internal/adapters/outbound/ (Bundle 4+) and are wired at
// internal/bootstrap/wire.go (the only composition point, D7.9).
package outbound

import (
	"context"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/services"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
)

// Adapter is the Phase 1 outbound port implemented by every execution
// adapter (shell, git, filesystem, http). Execute returns an
// AdapterRawOutcome — adapter-specific raw data that the domain
// ResultNormalizer dispatches to a registered closure. The Go error
// return is reserved for STRUCTURAL failures (invalid context, panics
// recovered by middleware). External failures travel inside the raw
// outcome (D7.4).
type Adapter interface {
	// ID returns the adapter's external identifier (e.g. "shell",
	// "git", "filesystem", "http"). Used as the key in the registry
	// returned by RegisterAllPhase1 (Bundle 4).
	ID() valueobjects.AdapterID

	// Capabilities returns the capability set this adapter supports.
	// Each capability's adapter_id must equal ID(). Called once at
	// bootstrap for registry construction.
	Capabilities() []valueobjects.Capability

	// Execute dispatches the given capability + payload, respecting
	// ctx (timeout, cancel, values). Never panics as contract (R4) —
	// adapter middleware recovers and converts panics into raw
	// outcomes. err is non-nil only for structural failures that
	// PRECEDE adapter logic (e.g. the adapter rejects the capability
	// at dispatch); business outcomes are normalized via the raw.
	Execute(ctx context.Context, cap valueobjects.Capability, payload valueobjects.Payload) (services.AdapterRawOutcome, error)
}
