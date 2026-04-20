package valueobjects

import (
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
