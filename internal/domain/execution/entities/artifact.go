package entities

import (
	"encoding/json"
	"fmt"
)

// ArtifactType is a closed enum of Phase 1 artifact kinds per §5.13.
type ArtifactType string

const (
	ArtifactFile                 ArtifactType = "file"
	ArtifactDirectory            ArtifactType = "directory"
	ArtifactGitRef               ArtifactType = "git_ref"
	ArtifactGitCommit            ArtifactType = "git_commit"
	ArtifactHTTPResponseSnapshot ArtifactType = "http_response_snapshot"
)

var allArtifactTypes = map[ArtifactType]struct{}{
	ArtifactFile:                 {},
	ArtifactDirectory:            {},
	ArtifactGitRef:               {},
	ArtifactGitCommit:            {},
	ArtifactHTTPResponseSnapshot: {},
}

// ParseArtifactType validates and returns an ArtifactType.
func ParseArtifactType(s string) (ArtifactType, error) {
	t := ArtifactType(s)
	if _, ok := allArtifactTypes[t]; !ok {
		return "", fmt.Errorf("invalid artifact_type %q", s)
	}
	return t, nil
}

// String returns the canonical wire value.
func (t ArtifactType) String() string { return string(t) }

// MarshalJSON encodes the ArtifactType as a JSON string.
func (t ArtifactType) MarshalJSON() ([]byte, error) { return json.Marshal(string(t)) }

// UnmarshalJSON decodes a JSON string and validates it against the closed enum.
func (t *ArtifactType) UnmarshalJSON(b []byte) error {
	var raw string
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	parsed, err := ParseArtifactType(raw)
	if err != nil {
		return err
	}
	*t = parsed
	return nil
}

// MaxArtifactAttributesBytes is the total size cap on Artifact.Attributes
// values (I17 / D5.13 — "small scalars only, total ≤ 2 KiB").
const MaxArtifactAttributesBytes = 2 * 1024

// Artifact is a durable side effect produced by capability execution.
// Attributes are small scalar strings only (no JSON-encoded objects);
// total byte length of all attribute values must be ≤ 2 KiB.
type Artifact struct {
	Type       ArtifactType      `json:"type"`
	URI        string            `json:"uri"`
	SizeBytes  int64             `json:"size_bytes"`
	Checksum   string            `json:"checksum,omitempty"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

// NewArtifact validates and constructs an Artifact.
func NewArtifact(t ArtifactType, uri string, sizeBytes int64, checksum string, attrs map[string]string) (Artifact, error) {
	if _, ok := allArtifactTypes[t]; !ok {
		return Artifact{}, fmt.Errorf("invalid artifact_type %q", t)
	}
	if uri == "" {
		return Artifact{}, fmt.Errorf("artifact uri must not be empty")
	}
	if sizeBytes < 0 {
		return Artifact{}, fmt.Errorf("artifact size_bytes must be ≥ 0, got %d", sizeBytes)
	}
	total := 0
	for _, v := range attrs {
		total += len(v)
	}
	if total > MaxArtifactAttributesBytes {
		return Artifact{}, fmt.Errorf("artifact attributes exceed %d bytes (got %d)", MaxArtifactAttributesBytes, total)
	}
	// Defensive copy of attrs.
	var cp map[string]string
	if len(attrs) > 0 {
		cp = make(map[string]string, len(attrs))
		for k, v := range attrs {
			cp[k] = v
		}
	}
	return Artifact{Type: t, URI: uri, SizeBytes: sizeBytes, Checksum: checksum, Attributes: cp}, nil
}
