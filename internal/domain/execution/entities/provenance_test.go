package entities_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/entities"
)

// ---------------------------------------------------------------------------
// ProvenanceSource enum
// ---------------------------------------------------------------------------

func TestParseProvenanceSource_AllValidValues(t *testing.T) {
	tests := []struct {
		input    string
		expected entities.ProvenanceSource
	}{
		{"governance", entities.ProvGovernance},
		{"sdk", entities.ProvSDK},
		{"http", entities.ProvHTTP},
		{"cli", entities.ProvCLI},
		{"test", entities.ProvTest},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := entities.ParseProvenanceSource(tt.input)
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

func TestParseProvenanceSource_InvalidValues(t *testing.T) {
	tests := []string{
		"",
		"GOVERNANCE",
		"SDK",
		"HTTP",
		"CLI",
		"TEST",
		"unknown",
		"api",
		"grpc",
	}
	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			_, err := entities.ParseProvenanceSource(input)
			if err == nil {
				t.Errorf("expected error for %q, got nil", input)
			}
		})
	}
}

func TestProvenanceSource_MarshalUnmarshalJSON(t *testing.T) {
	sources := []entities.ProvenanceSource{
		entities.ProvGovernance,
		entities.ProvSDK,
		entities.ProvHTTP,
		entities.ProvCLI,
		entities.ProvTest,
	}
	for _, src := range sources {
		t.Run(src.String(), func(t *testing.T) {
			b, err := json.Marshal(src)
			if err != nil {
				t.Fatalf("marshal error: %v", err)
			}
			var decoded entities.ProvenanceSource
			if err := json.Unmarshal(b, &decoded); err != nil {
				t.Fatalf("unmarshal error: %v", err)
			}
			if decoded != src {
				t.Errorf("round-trip failed: got %q, want %q", decoded, src)
			}
		})
	}
}

func TestProvenanceSource_UnmarshalJSON_InvalidValues(t *testing.T) {
	tests := []string{
		`""`,
		`"unknown"`,
		`"SDK"`,
		`123`,
	}
	for _, raw := range tests {
		t.Run(raw, func(t *testing.T) {
			var src entities.ProvenanceSource
			if err := json.Unmarshal([]byte(raw), &src); err == nil {
				t.Errorf("expected error for %s, got nil", raw)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// NewProvenance construction
// ---------------------------------------------------------------------------

func TestNewProvenance_AllSourcesSucceed(t *testing.T) {
	sources := []entities.ProvenanceSource{
		entities.ProvGovernance,
		entities.ProvSDK,
		entities.ProvHTTP,
		entities.ProvCLI,
		entities.ProvTest,
	}
	for _, src := range sources {
		t.Run(src.String(), func(t *testing.T) {
			p, err := entities.NewProvenance(src, "1.0.0", "host-a", "v2.0.0", "")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if p.Source != src {
				t.Errorf("source = %q, want %q", p.Source, src)
			}
		})
	}
}

func TestNewProvenance_InvalidSourceRejected(t *testing.T) {
	_, err := entities.NewProvenance("grpc", "1.0.0", "host-a", "v2.0.0", "")
	if err == nil {
		t.Error("expected error for invalid source, got nil")
	}
}

func TestNewProvenance_EmptySourceVersionRejected(t *testing.T) {
	_, err := entities.NewProvenance(entities.ProvSDK, "", "host-a", "v2.0.0", "")
	if err == nil {
		t.Error("expected error for empty source_version, got nil")
	}
	if err != nil && !strings.Contains(err.Error(), "source_version") {
		t.Errorf("expected error mentioning source_version, got: %v", err)
	}
}

func TestNewProvenance_EmptyHostRejected(t *testing.T) {
	_, err := entities.NewProvenance(entities.ProvSDK, "1.0.0", "", "v2.0.0", "")
	if err == nil {
		t.Error("expected error for empty host, got nil")
	}
	if err != nil && !strings.Contains(err.Error(), "host") {
		t.Errorf("expected error mentioning host, got: %v", err)
	}
}

func TestNewProvenance_EmptyRuntimeVersionRejected(t *testing.T) {
	_, err := entities.NewProvenance(entities.ProvSDK, "1.0.0", "host-a", "", "")
	if err == nil {
		t.Error("expected error for empty runtime_version, got nil")
	}
	if err != nil && !strings.Contains(err.Error(), "runtime_version") {
		t.Errorf("expected error mentioning runtime_version, got: %v", err)
	}
}

func TestNewProvenance_EmptyInvocationIDOmittedFromJSON(t *testing.T) {
	p, err := entities.NewProvenance(entities.ProvHTTP, "2.1.0", "prod-host", "v3.0.0", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if strings.Contains(string(b), "invocation_id") {
		t.Errorf("expected invocation_id to be omitted from JSON when empty, got: %s", b)
	}
}

func TestNewProvenance_InvocationIDPresentInJSON(t *testing.T) {
	p, err := entities.NewProvenance(entities.ProvGovernance, "1.0.0", "host-a", "v2.0.0", "inv-abc-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if !strings.Contains(string(b), "invocation_id") {
		t.Errorf("expected invocation_id in JSON when set, got: %s", b)
	}
	if !strings.Contains(string(b), "inv-abc-123") {
		t.Errorf("expected invocation_id value in JSON, got: %s", b)
	}
}

func TestNewProvenance_JSONRoundTrip(t *testing.T) {
	original, err := entities.NewProvenance(entities.ProvCLI, "3.2.1", "worker-7", "v1.5.0", "inv-xyz-789")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	b, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	var decoded entities.Provenance
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if decoded.Source != original.Source {
		t.Errorf("source: got %q, want %q", decoded.Source, original.Source)
	}
	if decoded.SourceVersion != original.SourceVersion {
		t.Errorf("source_version: got %q, want %q", decoded.SourceVersion, original.SourceVersion)
	}
	if decoded.Host != original.Host {
		t.Errorf("host: got %q, want %q", decoded.Host, original.Host)
	}
	if decoded.RuntimeVersion != original.RuntimeVersion {
		t.Errorf("runtime_version: got %q, want %q", decoded.RuntimeVersion, original.RuntimeVersion)
	}
	if decoded.InvocationID != original.InvocationID {
		t.Errorf("invocation_id: got %q, want %q", decoded.InvocationID, original.InvocationID)
	}
}

func TestNewProvenance_JSONUnmarshal_UnknownSource(t *testing.T) {
	raw := `{"source":"grpc","source_version":"1.0","host":"h","runtime_version":"v1","invocation_id":""}`
	var p entities.Provenance
	if err := json.Unmarshal([]byte(raw), &p); err == nil {
		t.Error("expected error unmarshalling unknown source, got nil")
	}
}
