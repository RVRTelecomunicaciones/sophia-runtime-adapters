package services

import (
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/entities"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
)

// ErrNoNormalizerRegistered is returned by Normalize when no adapter has
// registered a normalizer for the requested canonical capability.
var ErrNoNormalizerRegistered = errors.New("no normalizer registered")

// AdapterNormalizer is the per-capability closure registered by each
// adapter at bootstrap. It receives the capability it was registered
// for, the adapter-specific raw outcome, and a clock (for completedAt
// stamping), and returns a normalized ExecutionResult. Errors are
// reserved for unexpected structural failures (e.g. raw type mismatch);
// business outcomes travel inside the ExecutionResult itself.
type AdapterNormalizer func(
	cap valueobjects.Capability,
	raw AdapterRawOutcome,
	clk shared.Clock,
) (entities.ExecutionResult, error)

// ResultNormalizer is the domain service that dispatches an adapter raw
// outcome to the per-capability normalizer closure registered at
// bootstrap. It is the sole domain service in Phase 1 (D3.12). No I/O;
// safe for concurrent use after all registrations complete.
//
// Registration happens once at bootstrap (wire.go, Bundle 7). After that
// the Register method should not be called again — it returns an error
// on duplicate registration to catch startup misconfiguration early.
type ResultNormalizer struct {
	inlineLimit int
	mu          sync.RWMutex
	byCanonical map[string]AdapterNormalizer
}

// NewResultNormalizer constructs an empty normalizer. inlineLimit is the
// per-stream byte cap used by adapter normalizers when building
// entities.StreamRef values; it is stored here so normalizer closures
// can query it via InlineLimit().
func NewResultNormalizer(inlineLimit int) *ResultNormalizer {
	if inlineLimit <= 0 {
		panic(fmt.Sprintf("ResultNormalizer inlineLimit must be > 0, got %d", inlineLimit))
	}
	return &ResultNormalizer{
		inlineLimit: inlineLimit,
		byCanonical: map[string]AdapterNormalizer{},
	}
}

// InlineLimit returns the configured stream-inline byte limit. Adapter
// normalizers use this when calling entities.NewStreamRef.
func (n *ResultNormalizer) InlineLimit() int { return n.inlineLimit }

// Register associates a normalizer closure with a canonical capability
// key (e.g. "shell.exec@v1"). Returns an error on duplicate or empty key.
func (n *ResultNormalizer) Register(canonical string, fn AdapterNormalizer) error {
	if canonical == "" {
		return fmt.Errorf("canonical must not be empty")
	}
	if fn == nil {
		return fmt.Errorf("AdapterNormalizer must not be nil")
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if _, exists := n.byCanonical[canonical]; exists {
		return fmt.Errorf("duplicate normalizer registration for %q", canonical)
	}
	n.byCanonical[canonical] = fn
	return nil
}

// Normalize dispatches the raw outcome to the registered closure for
// cap.Canonical(). Returns an error wrapping ErrNoNormalizerRegistered
// when no adapter has registered for this canonical.
func (n *ResultNormalizer) Normalize(
	cap valueobjects.Capability,
	raw AdapterRawOutcome,
	clk shared.Clock,
) (entities.ExecutionResult, error) {
	if clk == nil {
		return entities.ExecutionResult{}, fmt.Errorf("clock is required")
	}
	n.mu.RLock()
	fn, ok := n.byCanonical[cap.Canonical()]
	n.mu.RUnlock()
	if !ok {
		return entities.ExecutionResult{}, fmt.Errorf("%w: %s", ErrNoNormalizerRegistered, cap.Canonical())
	}
	return fn(cap, raw, clk)
}

// Registered returns the sorted list of canonical keys that have
// registered normalizers. Useful for startup sanity checks — wire.go
// calls this to verify the registered set matches NewPhase1Capabilities().
func (n *ResultNormalizer) Registered() []string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	out := make([]string, 0, len(n.byCanonical))
	for k := range n.byCanonical {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
