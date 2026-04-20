package contract_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/services"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/ports/outbound/testdoubles"
	"github.com/sophia-ecosystem/runtime-adapters/test/contract"
)

// ─── compile-time check: *testing.T satisfies contract.TB ────────────────────
var _ contract.TB = (*testing.T)(nil)

// ─── shared test fixtures ─────────────────────────────────────────────────────

// stubRaw is a minimal AdapterRawOutcome for use in contract suite tests.
type stubRaw struct{}

func (stubRaw) IsAdapterRawOutcome() {}

var _ services.AdapterRawOutcome = stubRaw{}

// mustAdapterID is a test helper that fails the test on error.
func mustAdapterID(t *testing.T, s string) valueobjects.AdapterID {
	t.Helper()
	id, err := valueobjects.NewAdapterID(s)
	if err != nil {
		t.Fatalf("mustAdapterID(%q): %v", s, err)
	}
	return id
}

// mustCapability is a test helper that fails the test on error.
func mustCapability(t *testing.T, id valueobjects.AdapterID, name, ver string) valueobjects.Capability {
	t.Helper()
	c, err := valueobjects.NewCapability(id, name, ver, false, 5*time.Second)
	if err != nil {
		t.Fatalf("mustCapability: %v", err)
	}
	return c
}

// validJSON is a minimal valid JSON payload.
var validJSON = json.RawMessage(`{"cmd":"echo hello"}`)

// invalidJSON is semantically invalid but envelope-valid JSON — the stub
// is programmed to return a raw outcome for it (simulating business rejection).
var invalidJSON = json.RawMessage(`{"bad_field":true}`)

// buildStubAndSamples returns a StubAdapter programmed for both the valid
// and invalid payload paths, plus a matching samples map.
func buildStubAndSamples(t *testing.T) (*testdoubles.StubAdapter, map[string]contract.SamplePayload) {
	t.Helper()
	aid := mustAdapterID(t, "stub")
	cap := mustCapability(t, aid, "op", "v1")
	stub := testdoubles.NewStubAdapter(aid, cap)

	// Single program entry: for any Execute call that reaches the stub's lookup
	// (i.e., context is not cancelled), return a raw outcome. This satisfies
	// both the invalid-payload check (returns raw) and the valid-payload check.
	stub.Program(cap.Canonical(), testdoubles.StubProgram{Raw: stubRaw{}})

	samples := map[string]contract.SamplePayload{
		cap.Canonical(): {
			Valid:   validJSON,
			Invalid: []json.RawMessage{invalidJSON},
		},
	}
	return stub, samples
}

// ─── end-to-end pass ──────────────────────────────────────────────────────────

// TestContractSuite_EndToEnd runs RunContractSuite against a fully-behaving
// StubAdapter and expects every sub-check to pass.
func TestContractSuite_EndToEnd(t *testing.T) {
	stub, samples := buildStubAndSamples(t)
	contract.RunContractSuite(t, stub, samples)
}

// ─── positive self-tests (subset checks) ─────────────────────────────────────

// TestContractSuite_IDWellFormed exercises the ID well-formedness check.
func TestContractSuite_IDWellFormed(t *testing.T) {
	stub, samples := buildStubAndSamples(t)
	expectPass(t, stub, samples)
}

// TestContractSuite_CapabilitiesStable exercises the stability check.
func TestContractSuite_CapabilitiesStable(t *testing.T) {
	stub, samples := buildStubAndSamples(t)
	expectPass(t, stub, samples)
}

// TestContractSuite_InvalidPayload_ReturnsRaw verifies the invalid-payload
// check passes when the stub returns a raw outcome.
func TestContractSuite_InvalidPayload_ReturnsRaw(t *testing.T) {
	stub, samples := buildStubAndSamples(t)
	expectPass(t, stub, samples)
}

// TestContractSuite_CancelledCtx_Pass verifies the cancelled-ctx check passes
// when the stub returns (nil, ctx.Err()) — which StubAdapter does by design.
func TestContractSuite_CancelledCtx_Pass(t *testing.T) {
	stub, samples := buildStubAndSamples(t)
	expectPass(t, stub, samples)
}

// TestContractSuite_DeadlineExceeded_Pass verifies the deadline check passes
// when the stub returns (nil, ctx.Err()) for an expired deadline.
func TestContractSuite_DeadlineExceeded_Pass(t *testing.T) {
	stub, samples := buildStubAndSamples(t)
	expectPass(t, stub, samples)
}

// ─── negative self-test: MissingCoverage ─────────────────────────────────────

// TestMissingCoverage_ReturnsEmptyWhenAllCovered verifies that MissingCoverage
// returns an empty slice when every capability has a sample entry.
func TestMissingCoverage_ReturnsEmptyWhenAllCovered(t *testing.T) {
	stub, samples := buildStubAndSamples(t)
	missing := contract.MissingCoverage(stub, samples)
	if len(missing) != 0 {
		t.Errorf("MissingCoverage returned %v, want empty slice", missing)
	}
}

// TestMissingCoverage_ReturnsCanonicalWhenEntryAbsent verifies that
// MissingCoverage returns the capability's canonical string when its sample
// entry is absent from the map.
func TestMissingCoverage_ReturnsCanonicalWhenEntryAbsent(t *testing.T) {
	stub, _ := buildStubAndSamples(t)
	emptySamples := map[string]contract.SamplePayload{}
	missing := contract.MissingCoverage(stub, emptySamples)
	if len(missing) != 1 {
		t.Fatalf("MissingCoverage: want 1 missing entry, got %d: %v", len(missing), missing)
	}
	want := "stub.op@v1"
	if missing[0] != want {
		t.Errorf("MissingCoverage[0] = %q, want %q", missing[0], want)
	}
}

// ─── helpers ─────────────────────────────────────────────────────────────────

// expectPass runs RunContractSuite via a recorder and fails the outer test
// if the suite reported any top-level failure.
func expectPass(t *testing.T, adapter *testdoubles.StubAdapter, samples map[string]contract.SamplePayload) {
	t.Helper()
	rec := newRecorder(t)
	contract.RunContractSuite(rec, adapter, samples)
	if rec.failed {
		t.Error("RunContractSuite reported unexpected failures (see Logf output above)")
	}
}

// recorder wraps *testing.T to intercept top-level Errorf/Fatalf calls from
// RunContractSuite without propagating them to the outer test. Sub-tests
// spawned via t.Run still run under the real *testing.T and their failures
// propagate normally.
//
// This is intentionally minimal: it captures the top-level failure flag so
// negative-case tests can assert that RunContractSuite DID report a failure.
type recorder struct {
	*testing.T
	failed bool
}

func newRecorder(t *testing.T) *recorder { return &recorder{T: t} }

// Run delegates to the real *testing.T.Run so sub-tests are registered with
// the test runner. Failures inside sub-tests propagate through the real T.
func (r *recorder) Run(name string, f func(*testing.T)) bool {
	return r.T.Run(name, f)
}

func (r *recorder) Helper() { r.T.Helper() }

func (r *recorder) Errorf(format string, args ...any) {
	r.failed = true
	r.T.Logf("(recorder intercepted Errorf) "+format, args...)
}

func (r *recorder) Fatalf(format string, args ...any) {
	r.failed = true
	r.T.Logf("(recorder intercepted Fatalf) "+format, args...)
	// Do NOT call r.T.Fatalf — that would stop the outer test goroutine.
}

func (r *recorder) Logf(format string, args ...any)  { r.T.Logf(format, args...) }
func (r *recorder) Skipf(format string, args ...any) { r.T.Skipf(format, args...) }

// Compile-time: recorder must satisfy contract.TB.
var _ contract.TB = (*recorder)(nil)
