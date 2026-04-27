package chaos

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/services"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/ports/outbound/testdoubles"
)

// fakeChaosCapable is a test double that implements both outbound.Adapter
// (via embedded *testdoubles.StubAdapter) and ChaosCapable.
// When chaos_receipt_store_test.go lands (Task 1.5), helpers shared across
// test files should move to chaos_test_helpers_test.go.
type fakeChaosCapable struct {
	*testdoubles.StubAdapter
	supported   map[string][]FaultKind
	synthesizer map[FaultKind]services.AdapterRawOutcome
}

func (f *fakeChaosCapable) SupportedChaosFaults(cap valueobjects.Capability) []FaultKind {
	return f.supported[cap.Canonical()]
}

func (f *fakeChaosCapable) SyntheticOutcome(
	_ context.Context,
	_ valueobjects.Capability,
	_ valueobjects.Payload,
	fault FaultConfig,
) (services.AdapterRawOutcome, bool) {
	out, ok := f.synthesizer[fault.Kind]
	return out, ok
}

// Compile-time assertion: fakeChaosCapable satisfies ChaosCapable.
var _ ChaosCapable = (*fakeChaosCapable)(nil)

func TestChaosCapable_InterfaceCompiles(t *testing.T) {
	// Compile-time check via var _ above. Runtime assertion to make the test
	// meaningful in the test output — any type that satisfies the interface
	// must implement every method.
	cap := mustCapability(t, "shell.exec@v1")
	stub := testdoubles.NewStubAdapter(cap.AdapterID(), cap)
	f := &fakeChaosCapable{
		StubAdapter: stub,
		supported:   map[string][]FaultKind{},
		synthesizer: map[FaultKind]services.AdapterRawOutcome{},
	}

	// Verify ChaosCapable is satisfied at runtime via interface assignment.
	var cc ChaosCapable = f
	assert.NotNil(t, cc)
}

func TestIsDecoratorNative_TrueCases(t *testing.T) {
	nativeFaults := []FaultKind{
		FaultLatency,
		FaultHangUntilCancel,
		FaultInjectPanic,
	}
	for _, k := range nativeFaults {
		assert.True(t, IsDecoratorNative(k), "expected %q to be decorator-native", k)
	}
}

func TestIsDecoratorNative_FalseCases(t *testing.T) {
	nonNativeFaults := []FaultKind{
		FaultConnectionReset,
		FaultEIO,
		FaultENOSPC,
		FaultPermissionDenied,
		FaultRemoteUnreachable,
		FaultAuthFailure,
		FaultProcessSignal,
		FaultProcessExit,
		FaultSlowBody,
		FaultRedirectLoop,
		FaultTLSHandshakeFail,
		FaultPersistFailure,
	}
	for _, k := range nonNativeFaults {
		assert.False(t, IsDecoratorNative(k), "expected %q to NOT be decorator-native", k)
	}
}
