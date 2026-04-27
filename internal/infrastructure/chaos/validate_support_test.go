package chaos

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/services"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/ports/outbound"
	"github.com/sophia-ecosystem/runtime-adapters/internal/ports/outbound/testdoubles"
)

// newFakeChaosCapable builds a fakeChaosCapable for ValidateProfileSupport tests.
func newFakeChaosCapable(t *testing.T, canonical string, faults []FaultKind) *fakeChaosCapable {
	t.Helper()
	cap := mustCapability(t, canonical)
	stub := testdoubles.NewStubAdapter(cap.AdapterID(), cap)
	supported := map[string][]FaultKind{canonical: faults}
	return &fakeChaosCapable{
		StubAdapter: stub,
		supported:   supported,
		synthesizer: map[FaultKind]services.AdapterRawOutcome{},
	}
}

// registryOf builds a registry map from the given adapters.
func registryOf(adapters ...outbound.Adapter) map[valueobjects.AdapterID]outbound.Adapter {
	reg := make(map[valueobjects.AdapterID]outbound.Adapter, len(adapters))
	for _, a := range adapters {
		reg[a.ID()] = a
	}
	return reg
}

func TestValidateProfileSupport_NilProfile_NoError(t *testing.T) {
	err := ValidateProfileSupport(registryOf(), nil)
	require.NoError(t, err)
}

func TestValidateProfileSupport_EmptyAdapters_NoError(t *testing.T) {
	profile := &Profile{
		Version:  1,
		Name:     "empty",
		Adapters: map[string]FaultConfig{},
	}
	err := ValidateProfileSupport(registryOf(), profile)
	require.NoError(t, err)
}

func TestValidateProfileSupport_DecoratorNativeFault_NoCheckNeeded(t *testing.T) {
	// Registry has only a plain StubAdapter (NOT ChaosCapable).
	// Profile asks for inject_panic (decorator-native). Must pass without error.
	shellCap := mustCapability(t, "shell.exec@v1")
	stub := testdoubles.NewStubAdapter(shellCap.AdapterID(), shellCap)
	profile := &Profile{
		Version: 1,
		Name:    "native-fault",
		Adapters: map[string]FaultConfig{
			"shell.exec@v1": {Kind: FaultInjectPanic, Timing: TimingPreDispatch},
		},
	}
	err := ValidateProfileSupport(registryOf(stub), profile)
	require.NoError(t, err)
}

func TestValidateProfileSupport_AdapterSyntheticFault_Supported_OK(t *testing.T) {
	// fakeChaosCapable lists FaultConnectionReset for http.request@v1;
	// profile asks for connection_reset → no error.
	canonical := "http.request@v1"
	fake := newFakeChaosCapable(t, canonical, []FaultKind{FaultConnectionReset})
	profile := &Profile{
		Version: 1,
		Name:    "http-connection-reset",
		Adapters: map[string]FaultConfig{
			canonical: {Kind: FaultConnectionReset, Timing: TimingPostDispatch},
		},
	}
	err := ValidateProfileSupport(registryOf(fake), profile)
	require.NoError(t, err)
}

func TestValidateProfileSupport_AdapterSyntheticFault_NotChaosCapable_Errors(t *testing.T) {
	// Registry has plain StubAdapter; profile asks for FaultConnectionReset.
	// Error must mention adapter ID, capability, and fault kind.
	shellCap := mustCapability(t, "shell.exec@v1")
	stub := testdoubles.NewStubAdapter(shellCap.AdapterID(), shellCap)
	profile := &Profile{
		Version: 1,
		Name:    "non-capable",
		Adapters: map[string]FaultConfig{
			"shell.exec@v1": {Kind: FaultConnectionReset},
		},
	}
	err := ValidateProfileSupport(registryOf(stub), profile)
	require.Error(t, err)
	assert.Contains(t, err.Error(), shellCap.AdapterID().String())
	assert.Contains(t, err.Error(), "shell.exec@v1")
	assert.Contains(t, err.Error(), string(FaultConnectionReset))
}

func TestValidateProfileSupport_AdapterSyntheticFault_Unsupported_Errors(t *testing.T) {
	// fakeChaosCapable lists FaultEIO only; profile asks for FaultConnectionReset.
	canonical := "shell.exec@v1"
	fake := newFakeChaosCapable(t, canonical, []FaultKind{FaultEIO})
	profile := &Profile{
		Version: 1,
		Name:    "wrong-fault",
		Adapters: map[string]FaultConfig{
			canonical: {Kind: FaultConnectionReset},
		},
	}
	err := ValidateProfileSupport(registryOf(fake), profile)
	require.Error(t, err)
	assert.Contains(t, err.Error(), string(FaultConnectionReset))
}

func TestValidateProfileSupport_CapabilityNotInRegistry_Errors(t *testing.T) {
	// Profile mentions a canonical that no adapter owns.
	profile := &Profile{
		Version: 1,
		Name:    "missing-cap",
		Adapters: map[string]FaultConfig{
			"nonexistent.thing@v1": {Kind: FaultEIO},
		},
	}
	err := ValidateProfileSupport(registryOf(), profile)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nonexistent.thing@v1")
	assert.Contains(t, err.Error(), "not registered")
}

func TestValidateProfileSupport_MultipleUnsupportedPairs_AllReported(t *testing.T) {
	// Profile has two unsupported entries; error must contain both messages.
	shellCap := mustCapability(t, "shell.exec@v1")
	stub := testdoubles.NewStubAdapter(shellCap.AdapterID(), shellCap)
	profile := &Profile{
		Version: 1,
		Name:    "multi-error",
		Adapters: map[string]FaultConfig{
			// plain stub → not ChaosCapable → first error
			"shell.exec@v1": {Kind: FaultConnectionReset},
			// capability not in registry → second error
			"unknown.cap@v1": {Kind: FaultEIO},
		},
	}
	err := ValidateProfileSupport(registryOf(stub), profile)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "shell.exec@v1")
	assert.Contains(t, err.Error(), "unknown.cap@v1")
}

func TestValidateProfileSupport_PersistFault_Ignored(t *testing.T) {
	// Profile has only Persist config and empty Adapters.
	// persist_failure is ChaosReceiptStore's concern — ValidateProfileSupport
	// must return nil.
	profile := &Profile{
		Version:  1,
		Name:     "persist-only",
		Adapters: map[string]FaultConfig{},
		Persist:  &PersistFaultConfig{Kind: FaultPersistFailure},
	}
	err := ValidateProfileSupport(registryOf(), profile)
	require.NoError(t, err)
}

func TestValidateProfileSupport_ErrIs_ErrChaosProfileUnsupported(t *testing.T) {
	// When the function returns an error, errors.Is must unwrap to
	// ErrChaosProfileUnsupported.
	profile := &Profile{
		Version: 1,
		Name:    "err-is-check",
		Adapters: map[string]FaultConfig{
			"unknown.cap@v1": {Kind: FaultEIO},
		},
	}
	err := ValidateProfileSupport(registryOf(), profile)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrChaosProfileUnsupported),
		"expected errors.Is(err, ErrChaosProfileUnsupported) to be true, got: %v", err)
}
