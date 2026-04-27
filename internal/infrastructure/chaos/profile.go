package chaos

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
)

// Catalog is the closed set of canonical capability strings the loader
// validates profile entries against. Phase 1's 8 capabilities form the
// production catalog; tests inject a hand-rolled catalog of the
// canonicals they need.
type Catalog interface {
	Has(canonical string) bool
}

// NewCatalogFromCapabilities builds a Catalog from a capability slice.
// Used by bootstrap to hand the loader the Phase 1 catalog.
func NewCatalogFromCapabilities(caps []valueobjects.Capability) Catalog {
	set := make(map[string]struct{}, len(caps))
	for _, c := range caps {
		set[c.Canonical()] = struct{}{}
	}
	return &catalog{set: set}
}

type catalog struct{ set map[string]struct{} }

func (c *catalog) Has(canonical string) bool {
	_, ok := c.set[canonical]
	return ok
}

// rawProfile mirrors the YAML schema v1 shape for unmarshalling. It is
// converted to the package-public Profile after validation.
type rawProfile struct {
	Version     int                  `yaml:"version"`
	Name        string               `yaml:"name"`
	Description string               `yaml:"description"`
	Adapters    map[string]rawFault  `yaml:"adapters"`
	Persist     *rawPersistFault     `yaml:"persistence"`
	Expected    *rawExpectedOutcome  `yaml:"expected_outcome"`
}

type rawFault struct {
	Fault  string            `yaml:"fault"`
	Timing string            `yaml:"timing"`
	Match  map[string]string `yaml:"match"`
}

type rawPersistFault struct {
	Fault string `yaml:"fault"`
}

type rawExpectedOutcome struct {
	Status     string `yaml:"status"`
	ErrorClass string `yaml:"error_class"`
	RetryHint  string `yaml:"retry_hint"`
}

// parseProfile reads a YAML file from path, parses, and validates against
// the Catalog. This function does NOT do path hardening (Task 1.7 adds
// LoadProfile that wraps parseProfile with allowlist + symlink check).
func parseProfile(path string, cat Catalog) (*Profile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("chaos: read profile %q: %w", path, err)
	}
	return parseProfileBytes(data, cat)
}

// parseProfileBytes parses + validates raw YAML bytes against the Catalog.
func parseProfileBytes(data []byte, cat Catalog) (*Profile, error) {
	var raw rawProfile
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("chaos: decode profile YAML: %w", err)
	}

	if raw.Version != 1 {
		return nil, fmt.Errorf("chaos: profile %q: unsupported schema version %d (only v1 is supported)", raw.Name, raw.Version)
	}
	if raw.Name == "" {
		return nil, fmt.Errorf("chaos: profile name is required")
	}
	if raw.Expected == nil {
		return nil, fmt.Errorf("chaos: profile %q: expected_outcome is required (D2C3.24 stable contract)", raw.Name)
	}

	out := &Profile{
		Version:     raw.Version,
		Name:        raw.Name,
		Description: raw.Description,
		Adapters:    map[string]FaultConfig{},
		Expected: ExpectedOutcome{
			Status:     raw.Expected.Status,
			ErrorClass: raw.Expected.ErrorClass,
			RetryHint:  raw.Expected.RetryHint,
		},
	}

	for canonical, rf := range raw.Adapters {
		if !cat.Has(canonical) {
			return nil, fmt.Errorf("chaos: profile %q: capability %q not in catalog", raw.Name, canonical)
		}
		kind := FaultKind(rf.Fault)
		if !IsValidFaultKind(kind) {
			return nil, fmt.Errorf("chaos: profile %q: unknown fault kind %q for capability %q", raw.Name, rf.Fault, canonical)
		}
		timing := Timing(rf.Timing)
		if timing != "" && timing != TimingPreDispatch && timing != TimingPostDispatch {
			return nil, fmt.Errorf("chaos: profile %q: invalid timing %q for capability %q (want pre_dispatch or post_dispatch)", raw.Name, rf.Timing, canonical)
		}
		out.Adapters[canonical] = FaultConfig{
			Kind:   kind,
			Timing: timing,
			Match:  rf.Match,
		}
	}

	if raw.Persist != nil {
		kind := FaultKind(raw.Persist.Fault)
		if !IsValidFaultKind(kind) {
			return nil, fmt.Errorf("chaos: profile %q: unknown persist fault kind %q", raw.Name, raw.Persist.Fault)
		}
		out.Persist = &PersistFaultConfig{Kind: kind}
	}

	return out, nil
}
