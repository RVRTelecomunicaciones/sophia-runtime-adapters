package valueobjects

import (
	"encoding/json"
	"fmt"
	"regexp"
	"time"
)

var (
	capNameRegex = regexp.MustCompile(`^[a-z][a-z0-9_.]{1,63}$`)
	capVerRegex  = regexp.MustCompile(`^v[0-9]+$`)
)

// Capability is an immutable value object describing a single versioned
// capability exposed by an adapter (D3.7, §5.4). Equal by value.
type Capability struct {
	adapterID      AdapterID
	name           string
	version        string
	allowsPartial  bool
	defaultTimeout time.Duration
}

// NewCapability constructs and validates a Capability.
//
//   - name must match ^[a-z][a-z0-9_.]{1,63}$
//   - version must match ^v[0-9]+$
//   - def must be > 0
func NewCapability(aid AdapterID, name, version string, allowsPartial bool, def time.Duration) (Capability, error) {
	if !capNameRegex.MatchString(name) {
		return Capability{}, fmt.Errorf("invalid capability name %q", name)
	}
	if !capVerRegex.MatchString(version) {
		return Capability{}, fmt.Errorf("invalid capability version %q", version)
	}
	if def <= 0 {
		return Capability{}, fmt.Errorf("capability default timeout must be > 0, got %v", def)
	}
	return Capability{
		adapterID:      aid,
		name:           name,
		version:        version,
		allowsPartial:  allowsPartial,
		defaultTimeout: def,
	}, nil
}

// AdapterID returns the owning adapter's identifier.
func (c Capability) AdapterID() AdapterID { return c.adapterID }

// Name returns the capability name segment.
func (c Capability) Name() string { return c.name }

// Version returns the capability version string.
func (c Capability) Version() string { return c.version }

// AllowsPartial reports whether the capability supports partial-result streaming.
func (c Capability) AllowsPartial() bool { return c.allowsPartial }

// DefaultTimeout returns the default execution timeout for this capability.
func (c Capability) DefaultTimeout() time.Duration { return c.defaultTimeout }

// Canonical returns the external identifier "adapter.name@version" used for
// normalizer registration and JSON serialization (e.g. "shell.exec@v1").
func (c Capability) Canonical() string {
	return c.adapterID.String() + "." + c.name + "@" + c.version
}

// capabilityWire is the snake_case wire shape of a Capability. The
// `capability` field carries the canonical "adapter.name@version" form
// used as the human-facing identifier in tags and logs (matches the
// k6 scenario tags and the OTel `capability` attribute). The structured
// fields (adapter_id / name / version) are also emitted so SDK callers
// don't have to re-parse the canonical string. `default_timeout_ms`
// mirrors the request envelope's `timeout_budget_ms` vocabulary.
type capabilityWire struct {
	Canonical        string `json:"capability"`
	AdapterID        string `json:"adapter_id"`
	Name             string `json:"name"`
	Version          string `json:"version"`
	AllowsPartial    bool   `json:"allows_partial"`
	DefaultTimeoutMs int64  `json:"default_timeout_ms"`
}

// MarshalJSON emits the snake_case wire form. Required because all
// fields on the struct are unexported — without this, encoding/json
// silently emits "{}" and HTTP /api/v1/capabilities returns an array
// of empty objects (caught by TestBuildRuntime_EndToEndSmoke).
func (c Capability) MarshalJSON() ([]byte, error) {
	return json.Marshal(capabilityWire{
		Canonical:        c.Canonical(),
		AdapterID:        c.adapterID.String(),
		Name:             c.name,
		Version:          c.version,
		AllowsPartial:    c.allowsPartial,
		DefaultTimeoutMs: c.defaultTimeout.Milliseconds(),
	})
}

// UnmarshalJSON decodes the snake_case wire form and re-runs the same
// validation as NewCapability (identifier regexes, positive timeout).
// Symmetric with MarshalJSON so SDK callers can round-trip a catalog
// fetched over HTTP.
func (c *Capability) UnmarshalJSON(b []byte) error {
	var w capabilityWire
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	aid, err := NewAdapterID(w.AdapterID)
	if err != nil {
		return err
	}
	parsed, err := NewCapability(
		aid, w.Name, w.Version, w.AllowsPartial,
		time.Duration(w.DefaultTimeoutMs)*time.Millisecond,
	)
	if err != nil {
		return err
	}
	*c = parsed
	return nil
}
