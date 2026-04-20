package valueobjects_test

import (
	"encoding/json"
	"testing"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
)

func TestParseRetryHint_ValidValues(t *testing.T) {
	tests := []struct {
		input    string
		expected valueobjects.RetryHint
	}{
		{"retryable", valueobjects.HintRetryable},
		{"non_retryable", valueobjects.HintNonRetryable},
		{"unknown", valueobjects.HintUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := valueobjects.ParseRetryHint(tt.input)
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

func TestParseRetryHint_InvalidValues(t *testing.T) {
	tests := []string{
		"",
		"RETRYABLE",
		"NonRetryable",
		"non-retryable",
		"yes",
		"no",
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			_, err := valueobjects.ParseRetryHint(input)
			if err == nil {
				t.Errorf("expected error for %q, got nil", input)
			}
		})
	}
}

func TestRetryHint_ZeroValue_Invalid(t *testing.T) {
	var h valueobjects.RetryHint
	_, err := valueobjects.ParseRetryHint(string(h))
	if err == nil {
		t.Error("expected zero-value (empty string) to be invalid")
	}
}

func TestRetryHint_MarshalJSON(t *testing.T) {
	b, err := json.Marshal(valueobjects.HintNonRetryable)
	if err != nil {
		t.Fatalf("MarshalJSON error: %v", err)
	}
	if string(b) != `"non_retryable"` {
		t.Errorf("MarshalJSON = %s, want %q", b, `"non_retryable"`)
	}
}

func TestRetryHint_UnmarshalJSON_Valid(t *testing.T) {
	tests := []struct {
		raw      string
		expected valueobjects.RetryHint
	}{
		{`"retryable"`, valueobjects.HintRetryable},
		{`"non_retryable"`, valueobjects.HintNonRetryable},
		{`"unknown"`, valueobjects.HintUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			var h valueobjects.RetryHint
			if err := json.Unmarshal([]byte(tt.raw), &h); err != nil {
				t.Fatalf("UnmarshalJSON error: %v", err)
			}
			if h != tt.expected {
				t.Errorf("got %q, want %q", h, tt.expected)
			}
		})
	}
}

func TestRetryHint_UnmarshalJSON_Invalid(t *testing.T) {
	tests := []string{
		`""`,
		`"RETRYABLE"`,
		`"non-retryable"`,
		`true`,
	}

	for _, raw := range tests {
		t.Run(raw, func(t *testing.T) {
			var h valueobjects.RetryHint
			if err := json.Unmarshal([]byte(raw), &h); err == nil {
				t.Errorf("expected error for %s, got nil", raw)
			}
		})
	}
}

func TestRetryHint_JSONRoundTrip(t *testing.T) {
	hints := []valueobjects.RetryHint{
		valueobjects.HintRetryable,
		valueobjects.HintNonRetryable,
		valueobjects.HintUnknown,
	}

	for _, original := range hints {
		t.Run(original.String(), func(t *testing.T) {
			b, err := json.Marshal(original)
			if err != nil {
				t.Fatalf("marshal error: %v", err)
			}
			var decoded valueobjects.RetryHint
			if err := json.Unmarshal(b, &decoded); err != nil {
				t.Fatalf("unmarshal error: %v", err)
			}
			if decoded != original {
				t.Errorf("round-trip failed: got %q, want %q", decoded, original)
			}
		})
	}
}
