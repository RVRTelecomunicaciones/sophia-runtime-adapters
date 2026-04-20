// Package valueobjects holds the Phase 1 domain value objects for the
// execution bounded context. All types in this package are immutable after
// construction and equal by value.
package valueobjects

import (
	"encoding/json"
	"fmt"
	"regexp"
)

var adapterIDRegex = regexp.MustCompile(`^[a-z][a-z0-9_]{1,31}$`)

// AdapterID identifies a registered outbound adapter (shell, git, filesystem,
// http in Phase 1). Only lowercase ASCII letters, digits, and underscore are
// allowed; length 2..32; must start with a letter. Extending the registered
// set requires an ADR per §12.7.
//
// Note (Gap-1): the internal Go package name of an adapter (e.g. httpreq) is
// independent of the AdapterID wire string (e.g. "http"). AdapterID is a pure
// validated string wrapper — no alias or override machinery is needed here.
type AdapterID struct{ v string }

// NewAdapterID validates s against D3.6 regex ^[a-z][a-z0-9_]{1,31}$ and
// returns an AdapterID. Valid values are 2..32 characters, start with a
// lowercase ASCII letter, and contain only lowercase letters, digits, and
// underscores.
func NewAdapterID(s string) (AdapterID, error) {
	if !adapterIDRegex.MatchString(s) {
		return AdapterID{}, fmt.Errorf("invalid adapter_id %q: must match ^[a-z][a-z0-9_]{1,31}$", s)
	}
	return AdapterID{v: s}, nil
}

// String returns the canonical wire form of the AdapterID.
func (a AdapterID) String() string { return a.v }

// MarshalJSON encodes the AdapterID as a JSON string.
func (a AdapterID) MarshalJSON() ([]byte, error) { return json.Marshal(a.v) }

// UnmarshalJSON decodes a JSON string and validates it via NewAdapterID.
func (a *AdapterID) UnmarshalJSON(b []byte) error {
	var raw string
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	parsed, err := NewAdapterID(raw)
	if err != nil {
		return err
	}
	*a = parsed
	return nil
}
