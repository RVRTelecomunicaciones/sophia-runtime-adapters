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
	FaultPersistFailure    FaultKind = "persist_failure"
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

// AllFaultKinds returns the closed set of injectable fault kinds.
func AllFaultKinds() []FaultKind {
	out := make([]FaultKind, len(allFaultKinds))
	copy(out, allFaultKinds)
	return out
}

// IsValidFaultKind reports whether k is in the closed enum.
func IsValidFaultKind(k FaultKind) bool {
	for _, candidate := range allFaultKinds {
		if candidate == k {
			return true
		}
	}
	return false
}
