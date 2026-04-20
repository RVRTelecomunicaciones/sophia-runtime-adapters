package entities_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/entities"
)

func TestParseArtifactType_AllValidValues(t *testing.T) {
	tests := []struct {
		input    string
		expected entities.ArtifactType
	}{
		{"file", entities.ArtifactFile},
		{"directory", entities.ArtifactDirectory},
		{"git_ref", entities.ArtifactGitRef},
		{"git_commit", entities.ArtifactGitCommit},
		{"http_response_snapshot", entities.ArtifactHTTPResponseSnapshot},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := entities.ParseArtifactType(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.expected {
				t.Errorf("got %q, want %q", got, tt.expected)
			}
			if got.String() != tt.input {
				t.Errorf("String() = %q, want %q", got.String(), tt.input)
			}
		})
	}
}

func TestParseArtifactType_InvalidValues(t *testing.T) {
	tests := []string{
		"",
		"FILE",
		"blob",
		"image",
		"archive",
		"unknown",
	}
	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			_, err := entities.ParseArtifactType(input)
			if err == nil {
				t.Errorf("expected error for %q, got nil", input)
			}
		})
	}
}

func TestArtifactType_MarshalUnmarshalJSON(t *testing.T) {
	for _, at := range []entities.ArtifactType{
		entities.ArtifactFile,
		entities.ArtifactDirectory,
		entities.ArtifactGitRef,
		entities.ArtifactGitCommit,
		entities.ArtifactHTTPResponseSnapshot,
	} {
		t.Run(at.String(), func(t *testing.T) {
			b, err := json.Marshal(at)
			if err != nil {
				t.Fatalf("marshal error: %v", err)
			}
			var decoded entities.ArtifactType
			if err := json.Unmarshal(b, &decoded); err != nil {
				t.Fatalf("unmarshal error: %v", err)
			}
			if decoded != at {
				t.Errorf("round-trip failed: got %q, want %q", decoded, at)
			}
		})
	}
}

func TestArtifactType_UnmarshalJSON_InvalidValues(t *testing.T) {
	tests := []string{
		`""`,
		`"unknown"`,
		`"FILE"`,
		`123`,
	}
	for _, raw := range tests {
		t.Run(raw, func(t *testing.T) {
			var at entities.ArtifactType
			if err := json.Unmarshal([]byte(raw), &at); err == nil {
				t.Errorf("expected error for %s, got nil", raw)
			}
		})
	}
}

func TestNewArtifact_AllTypesSucceed(t *testing.T) {
	types := []entities.ArtifactType{
		entities.ArtifactFile,
		entities.ArtifactDirectory,
		entities.ArtifactGitRef,
		entities.ArtifactGitCommit,
		entities.ArtifactHTTPResponseSnapshot,
	}
	for _, at := range types {
		t.Run(at.String(), func(t *testing.T) {
			a, err := entities.NewArtifact(at, "s3://bucket/key", 100, "", nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if a.Type != at {
				t.Errorf("type = %q, want %q", a.Type, at)
			}
		})
	}
}

func TestNewArtifact_InvalidType(t *testing.T) {
	_, err := entities.NewArtifact("blob", "s3://bucket/key", 100, "", nil)
	if err == nil {
		t.Error("expected error for invalid artifact type, got nil")
	}
}

func TestNewArtifact_EmptyURI(t *testing.T) {
	_, err := entities.NewArtifact(entities.ArtifactFile, "", 100, "", nil)
	if err == nil {
		t.Error("expected error for empty URI, got nil")
	}
}

func TestNewArtifact_NegativeSizeBytes(t *testing.T) {
	_, err := entities.NewArtifact(entities.ArtifactFile, "s3://bucket/key", -1, "", nil)
	if err == nil {
		t.Error("expected error for negative size_bytes, got nil")
	}
}

func TestNewArtifact_ZeroSizeBytes(t *testing.T) {
	_, err := entities.NewArtifact(entities.ArtifactFile, "s3://bucket/key", 0, "", nil)
	if err != nil {
		t.Errorf("unexpected error for size_bytes=0: %v", err)
	}
}

func TestNewArtifact_AttributesWithinLimit(t *testing.T) {
	attrs := map[string]string{
		"key": strings.Repeat("a", 1024),
	}
	_, err := entities.NewArtifact(entities.ArtifactFile, "s3://bucket/key", 0, "", attrs)
	if err != nil {
		t.Errorf("unexpected error for attrs within limit: %v", err)
	}
}

func TestNewArtifact_AttributesExceedLimit(t *testing.T) {
	// Total value bytes = 2049 > 2048
	attrs := map[string]string{
		"key": strings.Repeat("a", 2049),
	}
	_, err := entities.NewArtifact(entities.ArtifactFile, "s3://bucket/key", 0, "", attrs)
	if err == nil {
		t.Error("expected error for attrs exceeding 2 KiB limit, got nil")
	}
}

func TestNewArtifact_AttributesExactlyAtLimit(t *testing.T) {
	// 2 * 1024 = 2048 bytes — exactly at the limit, must succeed
	attrs := map[string]string{
		"key": strings.Repeat("a", 2048),
	}
	_, err := entities.NewArtifact(entities.ArtifactFile, "s3://bucket/key", 0, "", attrs)
	if err != nil {
		t.Errorf("unexpected error for attrs at exact limit: %v", err)
	}
}

func TestNewArtifact_NilAttributes(t *testing.T) {
	a, err := entities.NewArtifact(entities.ArtifactFile, "s3://bucket/key", 100, "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.Attributes != nil {
		t.Errorf("expected nil Attributes, got %v", a.Attributes)
	}
	// Verify nil attrs are omitted from JSON
	b, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if strings.Contains(string(b), "attributes") {
		t.Errorf("expected 'attributes' to be omitted from JSON, got: %s", b)
	}
}

func TestNewArtifact_DefensiveCopy(t *testing.T) {
	attrs := map[string]string{"env": "prod"}
	a, err := entities.NewArtifact(entities.ArtifactFile, "s3://bucket/key", 100, "", attrs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Mutate source map after construction.
	attrs["env"] = "staging"
	if a.Attributes["env"] == "staging" {
		t.Error("Artifact.Attributes was not defensively copied — input mutation affected internal state")
	}
}

func TestNewArtifact_JSONRoundTrip(t *testing.T) {
	attrs := map[string]string{"branch": "main", "tag": "v1.2.3"}
	original, err := entities.NewArtifact(
		entities.ArtifactGitCommit,
		"git://github.com/org/repo@abc123",
		0,
		"sha256:deadbeef",
		attrs,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	b, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	var decoded entities.Artifact
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if decoded.Type != original.Type {
		t.Errorf("type: got %q, want %q", decoded.Type, original.Type)
	}
	if decoded.URI != original.URI {
		t.Errorf("uri: got %q, want %q", decoded.URI, original.URI)
	}
	if decoded.SizeBytes != original.SizeBytes {
		t.Errorf("size_bytes: got %d, want %d", decoded.SizeBytes, original.SizeBytes)
	}
	if decoded.Checksum != original.Checksum {
		t.Errorf("checksum: got %q, want %q", decoded.Checksum, original.Checksum)
	}
	if len(decoded.Attributes) != len(original.Attributes) {
		t.Errorf("attributes len: got %d, want %d", len(decoded.Attributes), len(original.Attributes))
	}
	for k, v := range original.Attributes {
		if decoded.Attributes[k] != v {
			t.Errorf("attributes[%q]: got %q, want %q", k, decoded.Attributes[k], v)
		}
	}
}
