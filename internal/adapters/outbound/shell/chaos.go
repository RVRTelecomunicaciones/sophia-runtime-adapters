package shell

import (
	"context"
	"fmt"
	"strconv"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/services"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/infrastructure/chaos"
)

// SupportedChaosFaults reports the adapter-synthetic chaos faults this
// adapter can produce for the given capability. Decorator-native faults
// (latency, hang_until_cancel, inject_panic) are universally available
// and not listed here.
//
// Implements chaos.ChaosCapable.
func (a *Adapter) SupportedChaosFaults(cap valueobjects.Capability) []chaos.FaultKind {
	if cap.Canonical() != "shell.exec@v1" {
		return nil
	}
	return []chaos.FaultKind{
		chaos.FaultProcessSignal,
		chaos.FaultProcessExit,
	}
}

// SyntheticOutcome constructs an *execRaw that is shape-identical to
// what the real adapter would produce under the equivalent real fault.
// Implements chaos.ChaosCapable.
func (a *Adapter) SyntheticOutcome(
	_ context.Context,
	cap valueobjects.Capability,
	_ valueobjects.Payload,
	fault chaos.FaultConfig,
) (services.AdapterRawOutcome, bool) {
	if cap.Canonical() != "shell.exec@v1" {
		return nil, false
	}
	switch fault.Kind {
	case chaos.FaultProcessSignal:
		// Process killed by external signal (e.g. SIGKILL not via ctx).
		// Real adapter would produce: exitCode == -1, runErr non-nil,
		// ctxErr nil. The normalizer classifies this as
		// failure / ExternalFailure / unknown via the priority-4 branch.
		signal := fault.Match["signal"]
		if signal == "" {
			signal = "SIGKILL"
		}
		return &execRaw{
			exitCode:       -1,
			runErr:         fmt.Errorf("process killed by signal %s", signal),
			exitSuccess:    []int{0},
			payloadDecoded: true,
		}, true

	case chaos.FaultProcessExit:
		// Process exited with a non-zero code not in exit_success.
		// Real adapter classifies this as failure / ExternalFailure / unknown.
		exitCode := 137 // default mirrors SIGKILL exit (128+9), but is just non-zero
		if v, ok := fault.Match["exit_code"]; ok {
			if n, err := strconv.Atoi(v); err == nil {
				exitCode = n
			}
		}
		return &execRaw{
			exitCode:       exitCode,
			exitSuccess:    []int{0},
			payloadDecoded: true,
		}, true

	default:
		return nil, false
	}
}

// Compile-time assertion that *Adapter implements chaos.ChaosCapable.
var _ chaos.ChaosCapable = (*Adapter)(nil)
