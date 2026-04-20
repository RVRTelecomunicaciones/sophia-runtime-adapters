// Package testdoubles contains in-memory implementations of the outbound
// ports used exclusively by unit tests in the application layer. They
// live next to the port definitions (not in a separate test module) so
// application tests can import them without cross-module pollution.
//
// These types are NOT production-safe. Do not import from anywhere under
// internal/bootstrap/ or cmd/.
package testdoubles

import (
	"context"
	"fmt"
	"sync"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/services"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/ports/outbound"
)

// StubAdapter is an in-memory Adapter whose ID, Capabilities, and per-
// capability behavior are programmed ahead of time. Execute looks up
// the behavior for the requested capability and returns the
// pre-configured raw outcome. If no behavior is registered, Execute
// returns a Go error (NOT a raw outcome) — test should fail loudly
// when a surprise capability is invoked.
type StubAdapter struct {
	id           valueobjects.AdapterID
	capabilities []valueobjects.Capability

	mu       sync.Mutex
	programs map[string]StubProgram
}

// StubProgram programs StubAdapter.Execute for a single capability.
// Either Raw or Err (or both — Err wins if non-nil) may be set.
type StubProgram struct {
	Raw services.AdapterRawOutcome
	Err error
}

// NewStubAdapter builds a StubAdapter with the given ID and capabilities.
// Panics if any capability's adapter_id != id (test programming error).
func NewStubAdapter(id valueobjects.AdapterID, caps ...valueobjects.Capability) *StubAdapter {
	for _, c := range caps {
		if c.AdapterID() != id {
			panic(fmt.Sprintf("StubAdapter: capability %s belongs to adapter %s, not %s", c.Canonical(), c.AdapterID(), id))
		}
	}
	return &StubAdapter{
		id:           id,
		capabilities: append([]valueobjects.Capability{}, caps...),
		programs:     map[string]StubProgram{},
	}
}

// Program registers the raw outcome that Execute should return for a
// given canonical capability string.
func (s *StubAdapter) Program(canonical string, p StubProgram) {
	s.mu.Lock()
	s.programs[canonical] = p
	s.mu.Unlock()
}

// ID returns the adapter's external identifier.
func (s *StubAdapter) ID() valueobjects.AdapterID { return s.id }

// Capabilities returns a defensive copy of the capability slice.
func (s *StubAdapter) Capabilities() []valueobjects.Capability {
	out := make([]valueobjects.Capability, len(s.capabilities))
	copy(out, s.capabilities)
	return out
}

// Execute returns the pre-programmed outcome for the capability, or an
// error if none is registered. Returns ctx.Err() immediately when the
// context is already cancelled.
func (s *StubAdapter) Execute(ctx context.Context, cap valueobjects.Capability, _ valueobjects.Payload) (services.AdapterRawOutcome, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	s.mu.Lock()
	p, ok := s.programs[cap.Canonical()]
	s.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("StubAdapter: no program for %s", cap.Canonical())
	}
	if p.Err != nil {
		return nil, p.Err
	}
	return p.Raw, nil
}

// Compile-time interface satisfaction.
var _ outbound.Adapter = (*StubAdapter)(nil)
