// Package valueobjects holds the Phase 1 domain value objects for the
// execution bounded context. All types in this package are immutable after
// construction and equal by value.
package valueobjects_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
)

// TestNewPayload_Valid covers all JSON value kinds that must be accepted (§5.5).
func TestNewPayload_Valid(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{"empty object", `{}`},
		{"empty array", `[]`},
		{"object with field", `{"k":"v"}`},
		{"string value", `"string"`},
		{"number value", `123`},
		{"null value", `null`},
		{"bool true", `true`},
		{"bool false", `false`},
		{"nested object", `{"a":{"b":1}}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := valueobjects.NewPayload(valueobjects.ContentTypeJSON, json.RawMessage(tt.data), 0)
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tt.data, err)
			}
			if p.ContentType() != valueobjects.ContentTypeJSON {
				t.Errorf("ContentType() = %q, want %q", p.ContentType(), valueobjects.ContentTypeJSON)
			}
		})
	}
}

// TestNewPayload_RejectWrongContentType checks that only exact "application/json" is accepted.
func TestNewPayload_RejectWrongContentType(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
	}{
		{"empty string", ""},
		{"plain text", "text/plain"},
		{"xml", "application/xml"},
		{"json with charset param", "application/json; charset=utf-8"},
		{"json uppercase", "Application/JSON"},
		{"form encoded", "application/x-www-form-urlencoded"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := valueobjects.NewPayload(tt.contentType, json.RawMessage(`{}`), 0)
			if err == nil {
				t.Errorf("expected error for content_type %q, got nil", tt.contentType)
			}
		})
	}
}

// TestNewPayload_RejectEmptyData verifies that an empty byte slice is rejected.
func TestNewPayload_RejectEmptyData(t *testing.T) {
	_, err := valueobjects.NewPayload(valueobjects.ContentTypeJSON, json.RawMessage{}, 0)
	if err == nil {
		t.Error("expected error for empty data, got nil")
	}
}

// TestNewPayload_RejectOversized verifies that data larger than maxBytes is rejected.
func TestNewPayload_RejectOversized(t *testing.T) {
	// 11 bytes: {"a":"bb"} → exactly 11; limit 10
	data := json.RawMessage(`{"a":"bb"}`) // len = 10, need > 10
	// Build an 11-byte valid JSON object
	oversized := json.RawMessage(`{"a":"bbb"}`) // len = 11
	if len(oversized) != 11 {
		t.Fatalf("test setup: expected 11 bytes, got %d", len(oversized))
	}

	_, err := valueobjects.NewPayload(valueobjects.ContentTypeJSON, oversized, 10)
	if err == nil {
		t.Error("expected error for oversized payload, got nil")
	}

	// Exactly at limit should pass
	_, err = valueobjects.NewPayload(valueobjects.ContentTypeJSON, data, 10)
	if err != nil {
		t.Errorf("unexpected error for payload at exact limit: %v", err)
	}
}

// TestNewPayload_MaxBytesZeroMeansUnlimited verifies that maxBytes=0 accepts any size.
func TestNewPayload_MaxBytesZeroMeansUnlimited(t *testing.T) {
	// Build a large-ish payload to confirm no size rejection when maxBytes=0
	large := make([]byte, 0, 1024+2)
	large = append(large, '[')
	for i := 0; i < 100; i++ {
		if i > 0 {
			large = append(large, ',')
		}
		large = append(large, '1')
	}
	large = append(large, ']')

	_, err := valueobjects.NewPayload(valueobjects.ContentTypeJSON, json.RawMessage(large), 0)
	if err != nil {
		t.Errorf("unexpected error with maxBytes=0: %v", err)
	}
}

// TestNewPayload_RejectInvalidJSON covers syntactically broken inputs.
func TestNewPayload_RejectInvalidJSON(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{"open brace only", `{bad`},
		{"close brace only", `}`},
		{"bare word", `not json`},
		{"unclosed string value", `{"unclosed":`},
		{"trailing comma", `{"a":1,}`},
		{"single quote string", `{'a':'b'}`},
		{"bare identifier", `undefined`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := valueobjects.NewPayload(valueobjects.ContentTypeJSON, json.RawMessage(tt.data), 0)
			if err == nil {
				t.Errorf("expected error for invalid JSON %q, got nil", tt.data)
			}
		})
	}
}

// TestPayload_Raw_DefensiveCopy verifies that mutating the returned slice does
// not affect subsequent calls to Raw().
func TestPayload_Raw_DefensiveCopy(t *testing.T) {
	original := `{"key":"value"}`
	p, err := valueobjects.NewPayload(valueobjects.ContentTypeJSON, json.RawMessage(original), 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	first := p.Raw()
	// Mutate the returned slice entirely
	for i := range first {
		first[i] = 'X'
	}

	// Second call must still return the original bytes
	second := p.Raw()
	if !bytes.Equal(second, []byte(original)) {
		t.Errorf("Raw() after mutation = %q, want %q", second, original)
	}
}

// TestPayload_SizeBytes verifies that SizeBytes equals len(Raw()).
func TestPayload_SizeBytes(t *testing.T) {
	tests := []string{`{}`, `[]`, `{"hello":"world"}`, `null`, `true`, `123`}

	for _, data := range tests {
		t.Run(data, func(t *testing.T) {
			p, err := valueobjects.NewPayload(valueobjects.ContentTypeJSON, json.RawMessage(data), 0)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if p.SizeBytes() != len(p.Raw()) {
				t.Errorf("SizeBytes() = %d, len(Raw()) = %d — mismatch", p.SizeBytes(), len(p.Raw()))
			}
			if p.SizeBytes() != len(data) {
				t.Errorf("SizeBytes() = %d, want %d", p.SizeBytes(), len(data))
			}
		})
	}
}

// TestPayload_ContentType verifies the accessor returns the exact constant.
func TestPayload_ContentType(t *testing.T) {
	p, err := valueobjects.NewPayload(valueobjects.ContentTypeJSON, json.RawMessage(`{}`), 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.ContentType() != "application/json" {
		t.Errorf("ContentType() = %q, want %q", p.ContentType(), "application/json")
	}
}

// TestPayload_ConstructorDefensiveCopy verifies that mutating the input slice
// after construction does not affect the stored payload.
func TestPayload_ConstructorDefensiveCopy(t *testing.T) {
	input := []byte(`{"key":"value"}`)
	original := string(input)

	p, err := valueobjects.NewPayload(valueobjects.ContentTypeJSON, json.RawMessage(input), 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Mutate the original input slice
	for i := range input {
		input[i] = 'Z'
	}

	if string(p.Raw()) != original {
		t.Errorf("constructor did not copy: Raw() = %q, want %q", p.Raw(), original)
	}
}
