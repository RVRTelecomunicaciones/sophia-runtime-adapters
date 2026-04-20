package valueobjects_test

import (
	"encoding/json"
	"testing"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
)

func TestParseErrorClass_ValidValues(t *testing.T) {
	tests := []struct {
		input    string
		expected valueobjects.ErrorClass
	}{
		{"validation_failure", valueobjects.ErrValidationFailure},
		{"capability_unknown", valueobjects.ErrCapabilityUnknown},
		{"payload_schema_failure", valueobjects.ErrPayloadSchemaFailure},
		{"precondition_failure", valueobjects.ErrPreconditionFailure},
		{"external_failure", valueobjects.ErrExternalFailure},
		{"timeout", valueobjects.ErrTimeout},
		{"cancelled", valueobjects.ErrCancelled},
		{"interrupted", valueobjects.ErrInterrupted},
		{"normalization_failure", valueobjects.ErrNormalizationFailure},
		{"adapter_internal_error", valueobjects.ErrAdapterInternalError},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := valueobjects.ParseErrorClass(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.expected {
				t.Errorf("got %q, want %q", got, tt.expected)
			}
			// round-trip: String() must match input
			if got.String() != tt.input {
				t.Errorf("String() = %q, want %q", got.String(), tt.input)
			}
		})
	}
}

func TestParseErrorClass_InvalidValues(t *testing.T) {
	tests := []string{
		"",
		"ValidationFailure",
		"TIMEOUT",
		"unknown",
		"error",
		"internal",
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			_, err := valueobjects.ParseErrorClass(input)
			if err == nil {
				t.Errorf("expected error for %q, got nil", input)
			}
		})
	}
}

func TestErrorClass_ZeroValue_Invalid(t *testing.T) {
	var e valueobjects.ErrorClass
	_, err := valueobjects.ParseErrorClass(string(e))
	if err == nil {
		t.Error("expected zero-value (empty string) to be invalid")
	}
}

func TestErrorClass_MarshalJSON(t *testing.T) {
	b, err := json.Marshal(valueobjects.ErrAdapterInternalError)
	if err != nil {
		t.Fatalf("MarshalJSON error: %v", err)
	}
	if string(b) != `"adapter_internal_error"` {
		t.Errorf("MarshalJSON = %s, want %q", b, `"adapter_internal_error"`)
	}
}

func TestErrorClass_UnmarshalJSON_Valid(t *testing.T) {
	tests := []struct {
		raw      string
		expected valueobjects.ErrorClass
	}{
		{`"validation_failure"`, valueobjects.ErrValidationFailure},
		{`"capability_unknown"`, valueobjects.ErrCapabilityUnknown},
		{`"payload_schema_failure"`, valueobjects.ErrPayloadSchemaFailure},
		{`"precondition_failure"`, valueobjects.ErrPreconditionFailure},
		{`"external_failure"`, valueobjects.ErrExternalFailure},
		{`"timeout"`, valueobjects.ErrTimeout},
		{`"cancelled"`, valueobjects.ErrCancelled},
		{`"interrupted"`, valueobjects.ErrInterrupted},
		{`"normalization_failure"`, valueobjects.ErrNormalizationFailure},
		{`"adapter_internal_error"`, valueobjects.ErrAdapterInternalError},
	}

	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			var e valueobjects.ErrorClass
			if err := json.Unmarshal([]byte(tt.raw), &e); err != nil {
				t.Fatalf("UnmarshalJSON error: %v", err)
			}
			if e != tt.expected {
				t.Errorf("got %q, want %q", e, tt.expected)
			}
		})
	}
}

func TestErrorClass_UnmarshalJSON_Invalid(t *testing.T) {
	tests := []string{
		`""`,
		`"Timeout"`,
		`"CANCELLED"`,
		`42`,
	}

	for _, raw := range tests {
		t.Run(raw, func(t *testing.T) {
			var e valueobjects.ErrorClass
			if err := json.Unmarshal([]byte(raw), &e); err == nil {
				t.Errorf("expected error for %s, got nil", raw)
			}
		})
	}
}

func TestErrorClass_JSONRoundTrip(t *testing.T) {
	classes := []valueobjects.ErrorClass{
		valueobjects.ErrValidationFailure,
		valueobjects.ErrCapabilityUnknown,
		valueobjects.ErrPayloadSchemaFailure,
		valueobjects.ErrPreconditionFailure,
		valueobjects.ErrExternalFailure,
		valueobjects.ErrTimeout,
		valueobjects.ErrCancelled,
		valueobjects.ErrInterrupted,
		valueobjects.ErrNormalizationFailure,
		valueobjects.ErrAdapterInternalError,
	}

	for _, original := range classes {
		t.Run(original.String(), func(t *testing.T) {
			b, err := json.Marshal(original)
			if err != nil {
				t.Fatalf("marshal error: %v", err)
			}
			var decoded valueobjects.ErrorClass
			if err := json.Unmarshal(b, &decoded); err != nil {
				t.Fatalf("unmarshal error: %v", err)
			}
			if decoded != original {
				t.Errorf("round-trip failed: got %q, want %q", decoded, original)
			}
		})
	}
}

// TestErrorClass_DefaultRetryHint_AllMappings asserts all 10 §5.9 mappings.
func TestErrorClass_DefaultRetryHint_AllMappings(t *testing.T) {
	tests := []struct {
		class    valueobjects.ErrorClass
		expected valueobjects.RetryHint
	}{
		{valueobjects.ErrValidationFailure, valueobjects.HintNonRetryable},
		{valueobjects.ErrCapabilityUnknown, valueobjects.HintNonRetryable},
		{valueobjects.ErrPayloadSchemaFailure, valueobjects.HintNonRetryable},
		{valueobjects.ErrPreconditionFailure, valueobjects.HintNonRetryable},
		{valueobjects.ErrExternalFailure, valueobjects.HintUnknown},
		{valueobjects.ErrTimeout, valueobjects.HintRetryable},
		{valueobjects.ErrCancelled, valueobjects.HintNonRetryable},
		{valueobjects.ErrInterrupted, valueobjects.HintRetryable},
		{valueobjects.ErrNormalizationFailure, valueobjects.HintNonRetryable},
		{valueobjects.ErrAdapterInternalError, valueobjects.HintNonRetryable},
		// Defensive default-branch coverage: an ErrorClass value outside the
		// 10-entry closed set (only constructible by bypassing the Parse
		// constructor) falls through to HintUnknown.
		{valueobjects.ErrorClass("synthetic_unknown_class"), valueobjects.HintUnknown},
		{valueobjects.ErrorClass(""), valueobjects.HintUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.class.String(), func(t *testing.T) {
			got := tt.class.DefaultRetryHint()
			if got != tt.expected {
				t.Errorf("DefaultRetryHint() = %q, want %q", got, tt.expected)
			}
		})
	}
}
