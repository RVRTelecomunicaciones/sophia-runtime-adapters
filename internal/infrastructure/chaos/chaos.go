// Package chaos implements opt-in fault injection for the runtime adapters.
// Chaos-capable code is compiled into the runtime binary but is inert by
// default and fail-closed (R17). Concrete decorators wrap the outbound.Adapter
// port and ports.ReceiptStore; profiles are validated YAML files loaded from
// an allowlist at bootstrap.
//
// See docs/superpowers/specs/2026-04-26-phase-2c.3-chaos-hardening-design.md.
package chaos

// FaultKind enumerates the closed set of injectable fault kinds per spec §5.1.
// Profile validation rejects unknown kinds at startup (R17). Extending the
// enum requires an ADR — chaos faults are a public contract surface that
// integration tests, profile YAMLs, and operators consume; silent additions
// or removals would break that contract.
//
// Members are declared in fault_kinds.go.
type FaultKind string

// Timing controls when the fault is applied relative to the wrapped call.
type Timing string

const (
	TimingPreDispatch  Timing = "pre_dispatch"
	TimingPostDispatch Timing = "post_dispatch"
)

// FaultConfig describes a single per-capability fault.
type FaultConfig struct {
	Kind   FaultKind
	Timing Timing
	// Match constrains the fault to a subset of requests; empty = always match.
	Match map[string]string
}

// PersistFaultConfig describes a persist-layer fault (D2C3.8 ChaosReceiptStore).
type PersistFaultConfig struct {
	Kind FaultKind
}

// ExpectedOutcome is the metadata used by integration tests; not consumed by
// the runtime itself (spec §6.1, D2C3.10/D2C3.24).
type ExpectedOutcome struct {
	Status     string
	ErrorClass string
	RetryHint  string
}

// Profile is the loaded, validated representation of a v1 profile YAML.
// Adapters is keyed by the canonical capability string "adapter.name@version"
// (e.g. "shell.exec@v1") — the same key used by CapabilityRegistry.
// valueobjects.CapabilityID does not exist in Phase 1; the canonical string
// is the established identifier.
type Profile struct {
	Version     int
	Name        string
	Description string
	Adapters    map[string]FaultConfig
	Persist     *PersistFaultConfig
	Expected    ExpectedOutcome
}

// For returns the fault for the given canonical capability string, if any.
func (p *Profile) For(canonical string) (FaultConfig, bool) {
	if p == nil {
		return FaultConfig{}, false
	}
	f, ok := p.Adapters[canonical]
	return f, ok
}

// PersistFault returns the configured persist fault, if any.
func (p *Profile) PersistFault() *PersistFaultConfig {
	if p == nil {
		return nil
	}
	return p.Persist
}

// Config is the bootstrap-resolved chaos configuration.
type Config struct {
	Enabled bool
	Profile *Profile
	Env     string
}
