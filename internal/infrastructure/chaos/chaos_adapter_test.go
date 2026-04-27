package chaos

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
	"github.com/sophia-ecosystem/runtime-adapters/internal/ports/outbound/testdoubles"
)

// testRawOutcome is a minimal AdapterRawOutcome used only in chaos_adapter_test.go.
type testRawOutcome struct{ tag string }

func (testRawOutcome) IsAdapterRawOutcome() {}

// mustCapability returns the Phase 1 capability with the given canonical or fails.
func mustCapability(t *testing.T, canonical string) valueobjects.Capability {
	t.Helper()
	caps, err := valueobjects.NewPhase1Capabilities()
	require.NoError(t, err)
	for _, c := range caps {
		if c.Canonical() == canonical {
			return c
		}
	}
	t.Fatalf("capability %q not in Phase 1 catalog", canonical)
	return valueobjects.Capability{}
}

func TestChaosAdapter_PassThrough_NoFaultForCapability(t *testing.T) {
	shellCap := mustCapability(t, "shell.exec@v1")
	stub := testdoubles.NewStubAdapter(shellCap.AdapterID(), shellCap)
	expected := testRawOutcome{tag: "real-stub-result"}
	stub.Program("shell.exec@v1", testdoubles.StubProgram{Raw: expected})

	emptyProfile := &Profile{
		Version:  1,
		Name:     "empty",
		Adapters: map[string]FaultConfig{},
	}

	wrapped := NewChaosAdapter(stub, emptyProfile, shared.RealClock{})
	require.Equal(t, stub.ID(), wrapped.ID())
	require.Equal(t, stub.Capabilities(), wrapped.Capabilities())

	out, err := wrapped.Execute(context.Background(), shellCap, valueobjects.Payload{})
	require.NoError(t, err)
	// With no fault for the capability, the stub's programmed outcome must
	// surface unchanged — proving the decorator delegated to the real adapter.
	got, ok := out.(testRawOutcome)
	require.True(t, ok, "expected pass-through to surface testRawOutcome, got %T", out)
	require.Equal(t, expected.tag, got.tag)
}

func TestChaosAdapter_DispatchFault_StubDelegates(t *testing.T) {
	// Task 1.3 dispatchFault is stubbed (delegates to real). Real switch lands in 1.4.
	// This test asserts the stub still delegates when a fault is configured —
	// i.e. behavior is identical to pass-through until 1.4. The test must FAIL
	// when 1.4 lands (because dispatchFault then synthesizes its own outcome),
	// signalling that the test must be reworked at that point.
	shellCap := mustCapability(t, "shell.exec@v1")
	stub := testdoubles.NewStubAdapter(shellCap.AdapterID(), shellCap)
	expected := testRawOutcome{tag: "stub-still-runs-in-1.3"}
	stub.Program("shell.exec@v1", testdoubles.StubProgram{Raw: expected})

	profile := &Profile{
		Version: 1,
		Name:    "fault-configured",
		Adapters: map[string]FaultConfig{
			"shell.exec@v1": {Kind: FaultLatency, Timing: TimingPreDispatch},
		},
	}

	wrapped := NewChaosAdapter(stub, profile, shared.RealClock{})
	out, err := wrapped.Execute(context.Background(), shellCap, valueobjects.Payload{})
	require.NoError(t, err)
	got, ok := out.(testRawOutcome)
	require.True(t, ok)
	require.Equal(t, expected.tag, got.tag, "Task 1.3 dispatchFault is stubbed; rework this test in 1.4")
}
