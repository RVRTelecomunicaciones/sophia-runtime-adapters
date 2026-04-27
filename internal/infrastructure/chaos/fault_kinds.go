package chaos

const (
	FaultLatency           FaultKind = "latency"
	FaultConnectionReset   FaultKind = "connection_reset"
	FaultHangUntilCancel   FaultKind = "hang_until_cancel"
	FaultEIO               FaultKind = "eio"
	FaultENOSPC            FaultKind = "enospc"
	FaultPermissionDenied  FaultKind = "permission_denied"
	FaultRemoteUnreachable FaultKind = "remote_unreachable"
	FaultAuthFailure       FaultKind = "auth_failure"
	FaultInjectPanic       FaultKind = "inject_panic"
	FaultProcessSignal     FaultKind = "process_signal"
	FaultProcessExit       FaultKind = "process_exit"
	FaultSlowBody          FaultKind = "slow_body"
	FaultRedirectLoop      FaultKind = "redirect_loop"
	FaultTLSHandshakeFail  FaultKind = "tls_handshake_fail"
	// FaultPersistFailure targets the ChaosReceiptStore wrapper, not an adapter.
	// It is in the same enum because the profile loader validates all fault
	// kinds from a single closed set (D2C3.11, R17). The ChaosAdapter dispatch
	// switch (Task 1.4) treats it as an unreachable case — it never reaches
	// adapter dispatch, only the persist boundary.
	FaultPersistFailure FaultKind = "persist_failure"
)

var allFaultKinds = []FaultKind{
	FaultLatency, FaultConnectionReset, FaultHangUntilCancel,
	FaultEIO, FaultENOSPC, FaultPermissionDenied,
	FaultRemoteUnreachable, FaultAuthFailure,
	FaultInjectPanic,
	FaultProcessSignal, FaultProcessExit,
	FaultSlowBody, FaultRedirectLoop, FaultTLSHandshakeFail,
	FaultPersistFailure,
}

// validFaultKinds is a set for O(1) IsValidFaultKind lookup.
// Mirrors the pattern in valueobjects/error_class.go and execution_status.go.
var validFaultKinds = func() map[FaultKind]struct{} {
	m := make(map[FaultKind]struct{}, len(allFaultKinds))
	for _, k := range allFaultKinds {
		m[k] = struct{}{}
	}
	return m
}()

// AllFaultKinds returns the closed set of injectable fault kinds (defensive copy).
func AllFaultKinds() []FaultKind {
	out := make([]FaultKind, len(allFaultKinds))
	copy(out, allFaultKinds)
	return out
}

// IsValidFaultKind reports whether k is in the closed enum.
func IsValidFaultKind(k FaultKind) bool {
	_, ok := validFaultKinds[k]
	return ok
}
