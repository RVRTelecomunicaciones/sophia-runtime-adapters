package chaos

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/services"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
	"github.com/sophia-ecosystem/runtime-adapters/internal/ports/outbound"
	"github.com/sophia-ecosystem/runtime-adapters/internal/ports/outbound/testdoubles"
)

// ─── Test-local helpers ───────────────────────────────────────────────────────

// newFakeClock returns a deterministic clock for wire tests.
func newFakeClock() *shared.FakeClock {
	return &shared.FakeClock{T: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
}

// newStore returns a fresh InMemoryReceiptRepository using the given clock.
func newStore(clk shared.Clock) outbound.ReceiptRepository {
	return testdoubles.NewInMemoryReceiptRepository(clk)
}

// singleStubRegistry builds a registry with one plain StubAdapter for canonical.
func singleStubRegistry(t *testing.T, canonical string) map[valueobjects.AdapterID]outbound.Adapter {
	t.Helper()
	cap := mustCapability(t, canonical)
	stub := testdoubles.NewStubAdapter(cap.AdapterID(), cap)
	return map[valueobjects.AdapterID]outbound.Adapter{
		cap.AdapterID(): stub,
	}
}

// singleFakeCCRegistry builds a registry with one fakeChaosCapable adapter
// that advertises faultKind as supported for canonical.
func singleFakeCCRegistry(t *testing.T, canonical string, faultKind FaultKind) map[valueobjects.AdapterID]outbound.Adapter {
	t.Helper()
	cap := mustCapability(t, canonical)
	stub := testdoubles.NewStubAdapter(cap.AdapterID(), cap)
	fcc := &fakeChaosCapable{
		StubAdapter: stub,
		supported: map[string][]FaultKind{
			canonical: {faultKind},
		},
		synthesizer: map[FaultKind]services.AdapterRawOutcome{},
	}
	return map[valueobjects.AdapterID]outbound.Adapter{
		cap.AdapterID(): fcc,
	}
}

// enabledCfg returns a *Config with Enabled=true and the given profile.
func enabledCfg(profile *Profile) *Config {
	return &Config{Enabled: true, Profile: profile, Env: "development"}
}

// emptyProfile returns a Profile with no adapter faults and no persist fault.
func emptyProfile() *Profile {
	return &Profile{
		Version:  1,
		Name:     "empty",
		Adapters: map[string]FaultConfig{},
		Expected: ExpectedOutcome{Status: "success"},
	}
}

// persistProfile returns a Profile that includes a persist fault only.
func persistProfile() *Profile {
	return &Profile{
		Version:  1,
		Name:     "persist-only",
		Adapters: map[string]FaultConfig{},
		Persist:  &PersistFaultConfig{Kind: FaultPersistFailure},
		Expected: ExpectedOutcome{Status: "failure"},
	}
}

// adapterFaultProfile returns a Profile specifying faultKind for canonical.
func adapterFaultProfile(canonical string, faultKind FaultKind) *Profile {
	return &Profile{
		Version: 1,
		Name:    "adapter-fault",
		Adapters: map[string]FaultConfig{
			canonical: {Kind: faultKind},
		},
		Expected: ExpectedOutcome{Status: "failure"},
	}
}

// ─── Tests ────────────────────────────────────────────────────────────────────

// 1. nil cfg → identity, no error.
func TestMaybeWrapAdaptersWithChaos_NilConfig_Identity(t *testing.T) {
	clk := newFakeClock()
	reg := singleStubRegistry(t, "shell.exec@v1")
	store := newStore(clk)

	outReg, outStore, err := MaybeWrapAdaptersWithChaos(reg, store, nil, clk)

	require.NoError(t, err)
	assert.Equal(t, reg, outReg, "registry must be the same map when chaos is disabled")
	assert.Equal(t, store, outStore, "store must be the same instance when chaos is disabled")
}

// 2. Enabled=false → identity, no error.
func TestMaybeWrapAdaptersWithChaos_DisabledConfig_Identity(t *testing.T) {
	clk := newFakeClock()
	reg := singleStubRegistry(t, "shell.exec@v1")
	store := newStore(clk)
	cfg := &Config{Enabled: false, Env: "development"}

	outReg, outStore, err := MaybeWrapAdaptersWithChaos(reg, store, cfg, clk)

	require.NoError(t, err)
	assert.Equal(t, reg, outReg)
	assert.Equal(t, store, outStore)
}

// 3. Enabled, empty profile (no faults, no persist) → adapters wrapped, store unchanged.
func TestMaybeWrapAdaptersWithChaos_EmptyProfile_NoPersist_WrapsAdaptersOnly(t *testing.T) {
	clk := newFakeClock()
	reg := singleStubRegistry(t, "shell.exec@v1")
	store := newStore(clk)
	cfg := enabledCfg(emptyProfile())

	outReg, outStore, err := MaybeWrapAdaptersWithChaos(reg, store, cfg, clk)

	require.NoError(t, err)
	require.Len(t, outReg, 1, "output registry must have same length")
	for _, a := range outReg {
		assert.IsType(t, (*ChaosAdapter)(nil), a, "each adapter must be wrapped in *ChaosAdapter")
	}
	assert.Equal(t, store, outStore, "store must be unchanged (no persist fault)")
}

// 4. Enabled, profile with persist fault → adapters wrapped, store wrapped.
func TestMaybeWrapAdaptersWithChaos_ProfileWithPersist_WrapsBoth(t *testing.T) {
	clk := newFakeClock()
	reg := singleStubRegistry(t, "shell.exec@v1")
	store := newStore(clk)
	cfg := enabledCfg(persistProfile())

	outReg, outStore, err := MaybeWrapAdaptersWithChaos(reg, store, cfg, clk)

	require.NoError(t, err)
	require.Len(t, outReg, 1)
	for _, a := range outReg {
		assert.IsType(t, (*ChaosAdapter)(nil), a)
	}
	assert.IsType(t, (*ChaosReceiptRepository)(nil), outStore,
		"store must be wrapped in *ChaosReceiptRepository when persist fault is set")
}

// 5. connection_reset on http.request@v1 with a ChaosCapable adapter that
// supports it → validation passes, registry wrapped, nil error.
func TestMaybeWrapAdaptersWithChaos_ProfileWithAdapterFault_ValidatesAndWraps(t *testing.T) {
	clk := newFakeClock()
	const canonical = "http.request@v1"
	reg := singleFakeCCRegistry(t, canonical, FaultConnectionReset)
	store := newStore(clk)
	cfg := enabledCfg(adapterFaultProfile(canonical, FaultConnectionReset))

	outReg, outStore, err := MaybeWrapAdaptersWithChaos(reg, store, cfg, clk)

	require.NoError(t, err)
	require.Len(t, outReg, 1)
	for _, a := range outReg {
		assert.IsType(t, (*ChaosAdapter)(nil), a)
	}
	// No persist fault in this profile.
	assert.Equal(t, store, outStore)
}

// 6. connection_reset on http.request@v1 with a plain StubAdapter (NOT ChaosCapable)
// → ValidateProfileSupport fails, returns (nil, nil, error).
func TestMaybeWrapAdaptersWithChaos_ProfileFaultUnsupported_FailsClosed(t *testing.T) {
	clk := newFakeClock()
	const canonical = "http.request@v1"
	reg := singleStubRegistry(t, canonical)
	store := newStore(clk)
	cfg := enabledCfg(adapterFaultProfile(canonical, FaultConnectionReset))

	outReg, outStore, err := MaybeWrapAdaptersWithChaos(reg, store, cfg, clk)

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrChaosProfileUnsupported),
		"error must wrap ErrChaosProfileUnsupported; got: %v", err)
	assert.Nil(t, outReg, "registry must be nil on validation error — no partial wrap")
	assert.Nil(t, outStore, "store must be nil on validation error")
}

// 7. inject_panic (decorator-native) on shell.exec@v1 with plain StubAdapter →
// ChaosCapable check is skipped for decorator-native faults; validation passes.
func TestMaybeWrapAdaptersWithChaos_DecoratorNativeFault_AlwaysOK(t *testing.T) {
	clk := newFakeClock()
	const canonical = "shell.exec@v1"
	reg := singleStubRegistry(t, canonical)
	store := newStore(clk)
	cfg := enabledCfg(adapterFaultProfile(canonical, FaultInjectPanic))

	outReg, outStore, err := MaybeWrapAdaptersWithChaos(reg, store, cfg, clk)

	require.NoError(t, err, "decorator-native faults bypass ChaosCapable requirement")
	require.Len(t, outReg, 1)
	for _, a := range outReg {
		assert.IsType(t, (*ChaosAdapter)(nil), a)
	}
	assert.Equal(t, store, outStore)
}

// 8. Profile mixes a supported fault on one capability and an unsupported fault
// on another → returned registry is nil (no partial wrap observable).
func TestMaybeWrapAdaptersWithChaos_PartialWrapNotObservable_OnError(t *testing.T) {
	clk := newFakeClock()

	capShell := mustCapability(t, "shell.exec@v1")
	capHTTP := mustCapability(t, "http.request@v1")

	// shell.exec: plain stub (NOT ChaosCapable) → connection_reset will fail validation.
	stubShell := testdoubles.NewStubAdapter(capShell.AdapterID(), capShell)
	// http.request: fakeChaosCapable supporting connection_reset → would pass.
	stubHTTP := testdoubles.NewStubAdapter(capHTTP.AdapterID(), capHTTP)
	fccHTTP := &fakeChaosCapable{
		StubAdapter: stubHTTP,
		supported: map[string][]FaultKind{
			"http.request@v1": {FaultConnectionReset},
		},
		synthesizer: map[FaultKind]services.AdapterRawOutcome{},
	}

	reg := map[valueobjects.AdapterID]outbound.Adapter{
		capShell.AdapterID(): stubShell,
		capHTTP.AdapterID():  fccHTTP,
	}
	store := newStore(clk)
	profile := &Profile{
		Version: 1,
		Name:    "mixed",
		Adapters: map[string]FaultConfig{
			"shell.exec@v1":   {Kind: FaultConnectionReset},
			"http.request@v1": {Kind: FaultConnectionReset},
		},
		Expected: ExpectedOutcome{Status: "failure"},
	}
	cfg := enabledCfg(profile)

	outReg, outStore, err := MaybeWrapAdaptersWithChaos(reg, store, cfg, clk)

	require.Error(t, err)
	assert.Nil(t, outReg, "registry must be nil — no partial wrap may be observed")
	assert.Nil(t, outStore, "store must be nil on validation error")
}

// 9. Wrapping preserves every original AdapterID in the output map.
func TestMaybeWrapAdaptersWithChaos_PreservesRegistryKeys(t *testing.T) {
	clk := newFakeClock()

	capShell := mustCapability(t, "shell.exec@v1")
	capHTTP := mustCapability(t, "http.request@v1")
	capFS := mustCapability(t, "filesystem.read_file@v1")

	reg := map[valueobjects.AdapterID]outbound.Adapter{
		capShell.AdapterID(): testdoubles.NewStubAdapter(capShell.AdapterID(), capShell),
		capHTTP.AdapterID():  testdoubles.NewStubAdapter(capHTTP.AdapterID(), capHTTP),
		capFS.AdapterID():    testdoubles.NewStubAdapter(capFS.AdapterID(), capFS),
	}
	store := newStore(clk)
	cfg := enabledCfg(emptyProfile())

	outReg, _, err := MaybeWrapAdaptersWithChaos(reg, store, cfg, clk)

	require.NoError(t, err)
	require.Len(t, outReg, len(reg))
	for id := range reg {
		_, ok := outReg[id]
		assert.True(t, ok, "AdapterID %q missing from wrapped registry", id)
	}
}

// 10. Every output adapter satisfies outbound.Adapter; output store satisfies
// outbound.ReceiptRepository.
func TestMaybeWrapAdaptersWithChaos_WrappersImplementInterfaces(t *testing.T) {
	clk := newFakeClock()
	reg := singleStubRegistry(t, "shell.exec@v1")
	store := newStore(clk)
	cfg := enabledCfg(persistProfile())

	outReg, outStore, err := MaybeWrapAdaptersWithChaos(reg, store, cfg, clk)

	require.NoError(t, err)
	for _, a := range outReg {
		var _ = a
	}
	var _ = outStore
}
