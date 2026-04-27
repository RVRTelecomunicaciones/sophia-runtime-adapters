package chaos

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFaultKind_AllKindsValid(t *testing.T) {
	for _, k := range AllFaultKinds() {
		require.True(t, IsValidFaultKind(k), "kind %q must be valid", k)
	}
}

func TestFaultKind_UnknownRejected(t *testing.T) {
	require.False(t, IsValidFaultKind("definitely_not_a_real_fault"))
	require.False(t, IsValidFaultKind(""))
}

func TestFaultKind_ContainsExpected(t *testing.T) {
	expected := []FaultKind{
		FaultLatency, FaultConnectionReset, FaultHangUntilCancel,
		FaultEIO, FaultENOSPC, FaultPermissionDenied,
		FaultRemoteUnreachable, FaultAuthFailure,
		FaultInjectPanic,
		FaultProcessSignal, FaultProcessExit,
		FaultSlowBody, FaultRedirectLoop, FaultTLSHandshakeFail,
		FaultPersistFailure,
	}
	got := AllFaultKinds()
	require.ElementsMatch(t, expected, got)
}
