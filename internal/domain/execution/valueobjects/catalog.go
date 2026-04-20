package valueobjects

import (
	"fmt"
	"time"
)

// NewPhase1Capabilities returns the 8 capabilities enumerated in spec
// §5.4 — the complete set supported in Phase 1. The returned slice has
// stable ordering (adapter, then capability name within adapter) so
// consumers can assert on index in tests.
//
// External identity (AdapterID) follows the spec exactly: "shell",
// "git", "filesystem", "http". The Go package for the HTTP adapter is
// "httpreq/" internally (avoids net/http stdlib shadowing) but that is
// not observable from this catalog — Gap-1 resolution.
//
// Extending this catalog requires an ADR per §12.7 ("permanently out of
// scope" or enum extension).
func NewPhase1Capabilities() ([]Capability, error) {
	mk := func(aid, name, version string, allowsPartial bool, def time.Duration) (Capability, error) {
		adapter, err := NewAdapterID(aid)
		if err != nil {
			return Capability{}, fmt.Errorf("adapter_id %q: %w", aid, err)
		}
		c, err := NewCapability(adapter, name, version, allowsPartial, def)
		if err != nil {
			return Capability{}, fmt.Errorf("capability %s.%s@%s: %w", aid, name, version, err)
		}
		return c, nil
	}
	entries := []struct {
		aid, name, version string
		partial            bool
		def                time.Duration
	}{
		{"shell", "exec", "v1", false, 30 * time.Second},
		{"git", "status", "v1", false, 10 * time.Second},
		{"git", "clone", "v1", true, 120 * time.Second},
		{"git", "diff", "v1", false, 15 * time.Second},
		{"git", "commit", "v1", false, 15 * time.Second},
		{"filesystem", "read_file", "v1", false, 5 * time.Second},
		{"filesystem", "write_file", "v1", false, 10 * time.Second},
		{"http", "request", "v1", false, 15 * time.Second},
	}
	out := make([]Capability, 0, len(entries))
	for _, e := range entries {
		c, err := mk(e.aid, e.name, e.version, e.partial, e.def)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// MustNewPhase1Capabilities calls NewPhase1Capabilities and panics on
// error. Intended for tests and bootstrap where a catalog build failure
// is a programming error, not a recoverable condition.
func MustNewPhase1Capabilities() []Capability {
	caps, err := NewPhase1Capabilities()
	if err != nil {
		panic(fmt.Sprintf("NewPhase1Capabilities: %v", err))
	}
	return caps
}
