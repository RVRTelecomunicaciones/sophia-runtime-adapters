package chaos

import (
	"errors"
	"fmt"
	"strings"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/ports/outbound"
)

// ErrChaosProfileUnsupported is returned (wrapped) by ValidateProfileSupport
// when the profile asks for a fault the wrapped adapter cannot synthesize.
var ErrChaosProfileUnsupported = errors.New("chaos: profile unsupported by registered adapters")

// ValidateProfileSupport ensures every (capability, fault) pair in the
// profile is supported by the wrapped adapter. R17 fail-closed: any
// unsupported pair causes bootstrap to fail with a structured error
// listing every offending pair.
//
// Decorator-native faults (IsDecoratorNative) skip the check — they are
// handled by ChaosAdapter without adapter cooperation. FaultPersistFailure
// is ignored here (handled by ChaosReceiptStore via profile.Persist).
//
// Returns nil if profile is nil or has no per-capability faults.
func ValidateProfileSupport(
	registry map[valueobjects.AdapterID]outbound.Adapter,
	profile *Profile,
) error {
	if profile == nil || len(profile.Adapters) == 0 {
		return nil
	}

	var unsupported []string
	for canonical, fault := range profile.Adapters {
		if IsDecoratorNative(fault.Kind) {
			continue
		}
		if fault.Kind == FaultPersistFailure {
			// FaultPersistFailure targets ChaosReceiptStore via profile.Persist,
			// not an adapter. Profile loader (Task 1.6) should reject this key
			// placement at parse time; this guard is the runtime's defense-in-depth.
			continue
		}
		adapter, cap, ok := lookupAdapterByCanonical(registry, canonical)
		if !ok {
			unsupported = append(unsupported,
				fmt.Sprintf("capability %q is not registered in any adapter", canonical))
			continue
		}
		cc, ok := adapter.(ChaosCapable)
		if !ok {
			unsupported = append(unsupported,
				fmt.Sprintf("adapter %q (capability %q) does not implement ChaosCapable; cannot synthesise fault %q",
					adapter.ID(), canonical, fault.Kind))
			continue
		}
		if !containsFault(cc.SupportedChaosFaults(cap), fault.Kind) {
			unsupported = append(unsupported,
				fmt.Sprintf("adapter %q does not support fault %q for capability %q",
					adapter.ID(), fault.Kind, canonical))
		}
	}
	if len(unsupported) > 0 {
		return fmt.Errorf("%w (profile %q):\n  - %s",
			ErrChaosProfileUnsupported, profile.Name, strings.Join(unsupported, "\n  - "))
	}
	return nil
}

// lookupAdapterByCanonical scans the registry for the adapter whose
// Capabilities() includes the given canonical string. Returns the adapter,
// the matched Capability value (for re-use with ChaosCapable.SupportedChaosFaults),
// and ok=true on hit.
func lookupAdapterByCanonical(
	registry map[valueobjects.AdapterID]outbound.Adapter,
	canonical string,
) (outbound.Adapter, valueobjects.Capability, bool) {
	for _, a := range registry {
		for _, c := range a.Capabilities() {
			if c.Canonical() == canonical {
				return a, c, true
			}
		}
	}
	return nil, valueobjects.Capability{}, false
}

func containsFault(list []FaultKind, target FaultKind) bool {
	for _, k := range list {
		if k == target {
			return true
		}
	}
	return false
}
