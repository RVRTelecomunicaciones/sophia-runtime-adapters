package entities

import (
	"encoding/json"
	"fmt"
)

// ProvenanceSource is a closed enum identifying where an ExecutionRequest
// originated (§5.10, D5.10, I19).
type ProvenanceSource string

const (
	ProvGovernance ProvenanceSource = "governance"
	ProvSDK        ProvenanceSource = "sdk"
	ProvHTTP       ProvenanceSource = "http"
	ProvCLI        ProvenanceSource = "cli"
	ProvTest       ProvenanceSource = "test"
)

var allProvenanceSources = map[ProvenanceSource]struct{}{
	ProvGovernance: {},
	ProvSDK:        {},
	ProvHTTP:       {},
	ProvCLI:        {},
	ProvTest:       {},
}

// ParseProvenanceSource validates and returns a ProvenanceSource.
func ParseProvenanceSource(s string) (ProvenanceSource, error) {
	p := ProvenanceSource(s)
	if _, ok := allProvenanceSources[p]; !ok {
		return "", fmt.Errorf("invalid provenance.source %q", s)
	}
	return p, nil
}

// String returns the canonical wire value.
func (p ProvenanceSource) String() string { return string(p) }

// MarshalJSON encodes the ProvenanceSource as a JSON string.
func (p ProvenanceSource) MarshalJSON() ([]byte, error) { return json.Marshal(string(p)) }

// UnmarshalJSON decodes a JSON string and validates it against the closed enum.
func (p *ProvenanceSource) UnmarshalJSON(b []byte) error {
	var raw string
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	parsed, err := ParseProvenanceSource(raw)
	if err != nil {
		return err
	}
	*p = parsed
	return nil
}

// Provenance records where the request originated (source, version) and the
// runtime host/version. InvocationID is optional (empty string when absent
// — omitted from JSON via omitempty).
type Provenance struct {
	Source         ProvenanceSource `json:"source"`
	SourceVersion  string           `json:"source_version"`
	Host           string           `json:"host"`
	RuntimeVersion string           `json:"runtime_version"`
	InvocationID   string           `json:"invocation_id,omitempty"`
}

// NewProvenance validates and constructs a Provenance.
func NewProvenance(source ProvenanceSource, sourceVersion, host, runtimeVersion, invocationID string) (Provenance, error) {
	if _, ok := allProvenanceSources[source]; !ok {
		return Provenance{}, fmt.Errorf("invalid provenance.source %q", source)
	}
	if sourceVersion == "" {
		return Provenance{}, fmt.Errorf("provenance.source_version must not be empty")
	}
	if host == "" {
		return Provenance{}, fmt.Errorf("provenance.host must not be empty")
	}
	if runtimeVersion == "" {
		return Provenance{}, fmt.Errorf("provenance.runtime_version must not be empty")
	}
	return Provenance{
		Source:         source,
		SourceVersion:  sourceVersion,
		Host:           host,
		RuntimeVersion: runtimeVersion,
		InvocationID:   invocationID,
	}, nil
}
