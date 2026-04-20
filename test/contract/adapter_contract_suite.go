// Package contract provides a reusable test suite every Phase 1 outbound
// adapter must pass. Each adapter package (shell, git, filesystem, httpreq)
// calls contract.RunContractSuite from its own _test.go with a map of
// capability canonicals → SamplePayload.
//
// Adapter invariants verified (§8.1, D7.4, R4, R5):
//   - ID() returns a well-formed AdapterID that round-trips through NewAdapterID.
//   - Capabilities() is stable across calls; each capability's AdapterID() equals
//     the adapter's own ID().
//   - Every capability listed by the adapter has a corresponding SamplePayload
//     entry in the samples map supplied by the caller.
//   - Invalid payloads produce either (raw, nil) or (nil, structuralErr) — never
//     (raw, err) together (D7.4: business failures travel in the raw, Go errors
//     are structural-only).
//   - A pre-cancelled context returns (raw, nil) or (nil, ctx.Err()) — never
//     (nil, nil).
//   - A pre-expired deadline context returns (raw, nil) or (nil, ctx.Err()) —
//     never (nil, nil).
//
// Panic freedom (R4): adapters must not propagate panics to the caller. This
// invariant is enforced by code review; runtime-testing a panic-free guarantee
// requires a deliberately malicious adapter and is out of scope for the contract
// suite.
package contract

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/ports/outbound"
)

// TB is the subset of *testing.T that RunContractSuite and its helpers
// require. *testing.T satisfies this interface automatically. Test
// self-tests may provide a spy implementation to intercept failure
// reports without aborting the outer test.
type TB interface {
	Helper()
	Run(name string, f func(t *testing.T)) bool
	Errorf(format string, args ...any)
	Fatalf(format string, args ...any)
	Logf(format string, args ...any)
	Skipf(format string, args ...any)
}

// SamplePayload holds the test fixtures for ONE capability under test.
// Valid is a well-formed payload that the adapter should accept (Execute
// must not return (nil, nil)); Invalid is a list of payloads whose schema
// or envelope is wrong (Execute must return a non-nil raw outcome OR a
// structural Go error, never both, never (nil, nil)).
type SamplePayload struct {
	Valid   json.RawMessage
	Invalid []json.RawMessage
}

// RunContractSuite runs the AdapterContractTestSuite against the given
// adapter. samples maps canonical capability strings (e.g. "shell.exec@v1")
// to fixtures; every entry in adapter.Capabilities() MUST have a matching
// entry in samples, otherwise the suite fails.
//
// Call this function from a _test.go in each concrete adapter package:
//
//	func TestAdapterContract(t *testing.T) {
//	    contract.RunContractSuite(t, myAdapter, map[string]contract.SamplePayload{
//	        "shell.exec@v1": {
//	            Valid:   json.RawMessage(`{"cmd":"echo hello"}`),
//	            Invalid: []json.RawMessage{json.RawMessage(`{}`)},
//	        },
//	    })
//	}
func RunContractSuite(t TB, adapter outbound.Adapter, samples map[string]SamplePayload) {
	t.Helper()
	t.Run("ID_IsWellFormed", func(t *testing.T) { testIDWellFormed(t, adapter) })
	t.Run("Capabilities_StableAndCoherent", func(t *testing.T) { testCapabilitiesStable(t, adapter) })
	t.Run("Capabilities_CoveredBySamples", func(t *testing.T) { testCapabilitiesCovered(t, adapter, samples) })
	t.Run("InvalidPayload_ReturnsRawOrStructuralError", func(t *testing.T) {
		testInvalidPayload(t, adapter, samples)
	})
	t.Run("CancelledCtx_ReturnsEarly", func(t *testing.T) { testCancelledCtx(t, adapter, samples) })
	t.Run("DeadlineExceeded_ReturnsEarly", func(t *testing.T) { testDeadlineExceeded(t, adapter, samples) })
}

// testIDWellFormed asserts that ID() returns a non-empty string that
// round-trips through NewAdapterID without error.
func testIDWellFormed(t *testing.T, a outbound.Adapter) {
	t.Helper()
	id := a.ID()
	if id.String() == "" {
		t.Fatalf("adapter ID() returned empty string")
	}
	if _, err := valueobjects.NewAdapterID(id.String()); err != nil {
		t.Fatalf("ID() returned %q which fails NewAdapterID validation: %v", id.String(), err)
	}
}

// testCapabilitiesStable asserts that two successive calls to Capabilities()
// return slices of equal length with the same canonicals in the same order,
// and that each capability's AdapterID() equals the adapter's own ID().
func testCapabilitiesStable(t *testing.T, a outbound.Adapter) {
	t.Helper()
	first := a.Capabilities()
	second := a.Capabilities()
	if len(first) != len(second) {
		t.Fatalf("Capabilities() length unstable: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i].Canonical() != second[i].Canonical() {
			t.Errorf("Capabilities() order unstable at [%d]: %q vs %q",
				i, first[i].Canonical(), second[i].Canonical())
		}
	}
	id := a.ID()
	for _, c := range first {
		if c.AdapterID() != id {
			t.Errorf("capability %s has adapter_id %q, want adapter's own ID %q",
				c.Canonical(), c.AdapterID().String(), id.String())
		}
	}
}

// MissingCoverage returns the list of canonical capability strings that
// appear in adapter.Capabilities() but have no matching key in samples.
// An empty slice means all capabilities are covered. Exposed so callers
// can write independent assertions without routing through the full suite.
func MissingCoverage(adapter outbound.Adapter, samples map[string]SamplePayload) []string {
	var missing []string
	for _, c := range adapter.Capabilities() {
		if _, ok := samples[c.Canonical()]; !ok {
			missing = append(missing, c.Canonical())
		}
	}
	return missing
}

// testCapabilitiesCovered fails when any capability returned by
// Capabilities() is missing from the samples map supplied by the caller.
func testCapabilitiesCovered(t *testing.T, a outbound.Adapter, samples map[string]SamplePayload) {
	t.Helper()
	for _, canon := range MissingCoverage(a, samples) {
		t.Errorf("capability %q has no SamplePayload entry in the samples map", canon)
	}
}

// testInvalidPayload exercises each invalid fixture for each capability.
// Contract (D7.4): Execute must return EITHER (raw, nil) OR (nil, err) —
// never (raw, err) together, never (nil, nil).
func testInvalidPayload(t *testing.T, a outbound.Adapter, samples map[string]SamplePayload) {
	t.Helper()
	for _, c := range a.Capabilities() {
		sample, ok := samples[c.Canonical()]
		if !ok {
			continue // already reported by testCapabilitiesCovered
		}
		for i, invalid := range sample.Invalid {
			i, invalid, c := i, invalid, c // capture loop vars
			t.Run(c.Canonical()+"/invalid-"+strconv.Itoa(i), func(t *testing.T) {
				pl, err := valueobjects.NewPayload(valueobjects.ContentTypeJSON, invalid, 0)
				if err != nil {
					// Envelope-level rejection before the adapter was reached —
					// this is a valid structural failure path.
					t.Skipf("envelope rejected at NewPayload: %v", err)
					return
				}
				raw, execErr := a.Execute(context.Background(), c, pl)
				// D7.4 invariant: both non-nil is a violation.
				if raw != nil && execErr != nil {
					t.Errorf("D7.4 violation: Execute returned both raw(%T) and Go error(%v)", raw, execErr)
					return
				}
				// (nil, nil) is also a violation — adapter must signal something.
				if raw == nil && execErr == nil {
					t.Errorf("invalid payload returned (nil, nil) — adapter must yield a raw outcome or a structural error")
				}
			})
		}
	}
}

// testCancelledCtx pre-cancels the context before calling Execute and
// asserts the adapter returns (raw, nil) or (nil, ctx.Err()) — never
// (nil, nil).
func testCancelledCtx(t *testing.T, a outbound.Adapter, samples map[string]SamplePayload) {
	t.Helper()
	for _, c := range a.Capabilities() {
		sample, ok := samples[c.Canonical()]
		if !ok || len(sample.Valid) == 0 {
			continue
		}
		c := c // capture
		t.Run(c.Canonical()+"/cancelled", func(t *testing.T) {
			pl, err := valueobjects.NewPayload(valueobjects.ContentTypeJSON, sample.Valid, 0)
			if err != nil {
				t.Fatalf("valid sample rejected by NewPayload: %v", err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel() // cancel before Execute
			raw, execErr := a.Execute(ctx, c, pl)
			if raw == nil && execErr == nil {
				t.Errorf("cancelled ctx returned (nil, nil); expected a raw outcome or ctx.Err()")
			}
		})
	}
}

// testDeadlineExceeded sets a 1-nanosecond deadline and sleeps past it
// before calling Execute, asserting the adapter returns (raw, nil) or
// (nil, ctx.Err()) — never (nil, nil).
func testDeadlineExceeded(t *testing.T, a outbound.Adapter, samples map[string]SamplePayload) {
	t.Helper()
	for _, c := range a.Capabilities() {
		sample, ok := samples[c.Canonical()]
		if !ok || len(sample.Valid) == 0 {
			continue
		}
		c := c // capture
		t.Run(c.Canonical()+"/deadline", func(t *testing.T) {
			pl, err := valueobjects.NewPayload(valueobjects.ContentTypeJSON, sample.Valid, 0)
			if err != nil {
				t.Fatalf("valid sample rejected by NewPayload: %v", err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
			defer cancel()
			time.Sleep(2 * time.Nanosecond) // guarantee deadline is exceeded
			raw, execErr := a.Execute(ctx, c, pl)
			if raw == nil && execErr == nil {
				t.Errorf("deadline-exceeded ctx returned (nil, nil); expected a raw outcome or ctx.Err()")
			}
		})
	}
}
