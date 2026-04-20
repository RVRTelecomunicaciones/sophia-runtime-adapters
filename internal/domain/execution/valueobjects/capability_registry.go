package valueobjects

import (
	"errors"
	"fmt"
	"sort"
)

// ErrUnknownCapability is returned by CapabilityRegistry.Lookup when the
// requested canonical is not registered.
//
// Note: The ErrorClass constant ErrCapabilityUnknown ("capability_unknown") is a
// separate symbol in error_class.go that represents the wire enum value. This
// sentinel is used for Go errors.Is matching on registry misses.
var ErrUnknownCapability = errors.New("capability unknown")

// CapabilityRegistry is an immutable in-memory store of Capability values
// keyed by their canonical string. Dynamic registration is forbidden per D6.6;
// all capabilities must be provided at construction time.
type CapabilityRegistry struct {
	byCanon map[string]Capability
}

// NewCapabilityRegistry builds a registry from the supplied capabilities.
// Returns an error if any two capabilities share the same canonical string.
func NewCapabilityRegistry(caps ...Capability) (*CapabilityRegistry, error) {
	r := &CapabilityRegistry{byCanon: make(map[string]Capability, len(caps))}
	for _, c := range caps {
		canon := c.Canonical()
		if _, dup := r.byCanon[canon]; dup {
			return nil, fmt.Errorf("duplicate capability: %s", canon)
		}
		r.byCanon[canon] = c
	}
	return r, nil
}

// Lookup returns the Capability for the given canonical string.
// Returns ErrUnknownCapability (wrapped) when not found.
func (r *CapabilityRegistry) Lookup(canonical string) (Capability, error) {
	c, ok := r.byCanon[canonical]
	if !ok {
		return Capability{}, fmt.Errorf("%w: %s", ErrUnknownCapability, canonical)
	}
	return c, nil
}

// List returns a snapshot of all registered capabilities in stable canonical
// order (lexicographic ascending).
func (r *CapabilityRegistry) List() []Capability {
	out := make([]Capability, 0, len(r.byCanon))
	for _, c := range r.byCanon {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Canonical() < out[j].Canonical()
	})
	return out
}

// FilterByAdapter returns all capabilities whose AdapterID matches aid, in
// stable canonical order. Returns a non-nil empty slice when no matches
// (aligning with List() for consistent caller handling).
func (r *CapabilityRegistry) FilterByAdapter(aid AdapterID) []Capability {
	out := make([]Capability, 0)
	for _, c := range r.byCanon {
		if c.AdapterID() == aid {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Canonical() < out[j].Canonical()
	})
	return out
}
