package chaos

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/services"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
	"github.com/sophia-ecosystem/runtime-adapters/internal/ports/outbound/testdoubles"
)

// makeProfile creates a single-capability fault profile for use in tests.
func makeProfile(canonical string, fault FaultConfig) *Profile {
	return &Profile{
		Version:  1,
		Name:     "test-profile",
		Adapters: map[string]FaultConfig{canonical: fault},
	}
}

// --- Pass-through (from Task 1.3, kept as-is) ---

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

// --- Task 1.4f: dispatchFault closed-enum tests ---

// 1. FaultLatency: ctx-aware sleep then delegate.
func TestChaosAdapter_DispatchFault_Latency_DelegatesAfterSleep(t *testing.T) {
	shellCap := mustCapability(t, "shell.exec@v1")
	stub := testdoubles.NewStubAdapter(shellCap.AdapterID(), shellCap)
	expected := testRawOutcome{tag: "latency-delegate-result"}
	stub.Program("shell.exec@v1", testdoubles.StubProgram{Raw: expected})

	profile := makeProfile("shell.exec@v1", FaultConfig{
		Kind:   FaultLatency,
		Timing: TimingPreDispatch,
		Match:  map[string]string{"duration": "5ms"},
	})

	wrapped := NewChaosAdapter(stub, profile, shared.RealClock{})

	start := time.Now()
	out, err := wrapped.Execute(context.Background(), shellCap, valueobjects.Payload{})
	elapsed := time.Since(start)

	require.NoError(t, err)
	got, ok := out.(testRawOutcome)
	require.True(t, ok, "expected testRawOutcome after latency sleep, got %T", out)
	require.Equal(t, expected.tag, got.tag)
	assert.GreaterOrEqual(t, elapsed, 5*time.Millisecond,
		"latency fault must sleep at least the configured duration before delegating")
}

// 2. FaultLatency: missing duration falls back to 100ms default; ctx cancellation
// proves no panic and that ctx.Err() is returned when the default sleep is interrupted.
func TestChaosAdapter_DispatchFault_Latency_DefaultDuration(t *testing.T) {
	shellCap := mustCapability(t, "shell.exec@v1")
	stub := testdoubles.NewStubAdapter(shellCap.AdapterID(), shellCap)
	stub.Program("shell.exec@v1", testdoubles.StubProgram{Raw: testRawOutcome{tag: "default-latency"}})

	// No "duration" key in Match → parseLatency falls back to 100ms.
	profile := makeProfile("shell.exec@v1", FaultConfig{
		Kind:   FaultLatency,
		Timing: TimingPreDispatch,
		Match:  map[string]string{},
	})

	// 20ms deadline < 100ms default; ctx fires first — verifies fallback path
	// doesn't panic and correctly surfaces ctx.Err().
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	wrapped := NewChaosAdapter(stub, profile, shared.RealClock{})
	out, err := wrapped.Execute(ctx, shellCap, valueobjects.Payload{})

	require.Nil(t, out)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

// 3. FaultLatency: ctx cancel during sleep returns (nil, ctx.Err()) quickly.
func TestChaosAdapter_DispatchFault_Latency_CtxCancelDuringSleep(t *testing.T) {
	shellCap := mustCapability(t, "shell.exec@v1")
	stub := testdoubles.NewStubAdapter(shellCap.AdapterID(), shellCap)
	stub.Program("shell.exec@v1", testdoubles.StubProgram{Raw: testRawOutcome{tag: "should-not-reach"}})

	profile := makeProfile("shell.exec@v1", FaultConfig{
		Kind:   FaultLatency,
		Timing: TimingPreDispatch,
		Match:  map[string]string{"duration": "100ms"},
	})

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()

	wrapped := NewChaosAdapter(stub, profile, shared.RealClock{})
	start := time.Now()
	out, err := wrapped.Execute(ctx, shellCap, valueobjects.Payload{})
	elapsed := time.Since(start)

	require.Nil(t, out)
	require.ErrorIs(t, err, context.Canceled)
	assert.Less(t, elapsed, 50*time.Millisecond,
		"should abort well before the 100ms sleep completes")
}

// 4. FaultHangUntilCancel: blocks until ctx deadline fires.
func TestChaosAdapter_DispatchFault_HangUntilCancel_HonorsCtx(t *testing.T) {
	shellCap := mustCapability(t, "shell.exec@v1")
	stub := testdoubles.NewStubAdapter(shellCap.AdapterID(), shellCap)
	stub.Program("shell.exec@v1", testdoubles.StubProgram{Raw: testRawOutcome{tag: "should-not-reach"}})

	profile := makeProfile("shell.exec@v1", FaultConfig{
		Kind:   FaultHangUntilCancel,
		Timing: TimingPreDispatch,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	wrapped := NewChaosAdapter(stub, profile, shared.RealClock{})
	start := time.Now()
	out, err := wrapped.Execute(ctx, shellCap, valueobjects.Payload{})
	elapsed := time.Since(start)

	require.Nil(t, out)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.GreaterOrEqual(t, elapsed, 10*time.Millisecond)
	assert.Less(t, elapsed, 200*time.Millisecond)
}

// 5. FaultInjectPanic: raises; R4 recoverer is upstream middleware's responsibility.
func TestChaosAdapter_DispatchFault_InjectPanic_Raises(t *testing.T) {
	shellCap := mustCapability(t, "shell.exec@v1")
	stub := testdoubles.NewStubAdapter(shellCap.AdapterID(), shellCap)

	profile := makeProfile("shell.exec@v1", FaultConfig{
		Kind:   FaultInjectPanic,
		Timing: TimingPreDispatch,
	})

	wrapped := NewChaosAdapter(stub, profile, shared.RealClock{})

	require.Panics(t, func() {
		_, _ = wrapped.Execute(context.Background(), shellCap, valueobjects.Payload{})
	})
}

// 6. FaultPersistFailure: defensive error — handled by ChaosReceiptStore, not here.
func TestChaosAdapter_DispatchFault_PersistFailure_DefensiveError(t *testing.T) {
	shellCap := mustCapability(t, "shell.exec@v1")
	stub := testdoubles.NewStubAdapter(shellCap.AdapterID(), shellCap)

	profile := makeProfile("shell.exec@v1", FaultConfig{
		Kind:   FaultPersistFailure,
		Timing: TimingPreDispatch,
	})

	wrapped := NewChaosAdapter(stub, profile, shared.RealClock{})
	out, err := wrapped.Execute(context.Background(), shellCap, valueobjects.Payload{})

	require.Nil(t, out)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "persist_failure")
	assert.Contains(t, err.Error(), "ChaosReceiptStore")
}

// 7. Adapter-synthetic: delegates to ChaosCapable.SyntheticOutcome and surfaces outcome.
func TestChaosAdapter_DispatchFault_AdapterSynthetic_DelegatesToChaosCapable(t *testing.T) {
	httpCap := mustCapability(t, "http.request@v1")
	expected := testRawOutcome{tag: "synthetic-connection-reset"}

	stub := testdoubles.NewStubAdapter(httpCap.AdapterID(), httpCap)
	fakeCC := &fakeChaosCapable{
		StubAdapter: stub,
		supported: map[string][]FaultKind{
			"http.request@v1": {FaultConnectionReset},
		},
		synthesizer: map[FaultKind]services.AdapterRawOutcome{
			FaultConnectionReset: expected,
		},
	}

	profile := makeProfile("http.request@v1", FaultConfig{
		Kind:   FaultConnectionReset,
		Timing: TimingPreDispatch,
	})

	wrapped := NewChaosAdapter(fakeCC, profile, shared.RealClock{})
	out, err := wrapped.Execute(context.Background(), httpCap, valueobjects.Payload{})

	require.NoError(t, err)
	got, ok := out.(testRawOutcome)
	require.True(t, ok, "expected testRawOutcome from ChaosCapable, got %T", out)
	require.Equal(t, expected.tag, got.tag)
}

// 8. Adapter-synthetic: plain StubAdapter (no ChaosCapable) → defensive error.
func TestChaosAdapter_DispatchFault_AdapterSynthetic_NotChaosCapable_Errors(t *testing.T) {
	shellCap := mustCapability(t, "shell.exec@v1")
	// Plain StubAdapter does NOT implement ChaosCapable.
	stub := testdoubles.NewStubAdapter(shellCap.AdapterID(), shellCap)

	profile := makeProfile("shell.exec@v1", FaultConfig{
		Kind:   FaultConnectionReset,
		Timing: TimingPreDispatch,
	})

	wrapped := NewChaosAdapter(stub, profile, shared.RealClock{})
	out, err := wrapped.Execute(context.Background(), shellCap, valueobjects.Payload{})

	require.Nil(t, out)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ChaosCapable")
	assert.Contains(t, err.Error(), "ValidateProfileSupport")
}

// 9. Adapter-synthetic: ChaosCapable returns (nil, false) → defensive error referencing fault.
func TestChaosAdapter_DispatchFault_ChaosCapableReturnsNilFalse_Errors(t *testing.T) {
	shellCap := mustCapability(t, "shell.exec@v1")
	stub := testdoubles.NewStubAdapter(shellCap.AdapterID(), shellCap)
	// Empty synthesizer → SyntheticOutcome always returns (nil, false).
	fakeCC := &fakeChaosCapable{
		StubAdapter: stub,
		supported: map[string][]FaultKind{
			"shell.exec@v1": {FaultEIO},
		},
		synthesizer: map[FaultKind]services.AdapterRawOutcome{},
	}

	profile := makeProfile("shell.exec@v1", FaultConfig{
		Kind:   FaultEIO,
		Timing: TimingPreDispatch,
	})

	wrapped := NewChaosAdapter(fakeCC, profile, shared.RealClock{})
	out, err := wrapped.Execute(context.Background(), shellCap, valueobjects.Payload{})

	require.Nil(t, out)
	require.Error(t, err)
	assert.Contains(t, err.Error(), string(FaultEIO))
}
