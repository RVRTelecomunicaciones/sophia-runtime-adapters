// Package valueobjects holds the Phase 1 domain value objects for the
// execution bounded context. All types in this package are immutable after
// construction and equal by value.
package valueobjects_test

import (
	"encoding/json"
	"testing"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
)

func TestParseExecutionStatus_ValidValues(t *testing.T) {
	tests := []struct {
		input    string
		expected valueobjects.ExecutionStatus
	}{
		{"success", valueobjects.StatusSuccess},
		{"failure", valueobjects.StatusFailure},
		{"timeout", valueobjects.StatusTimeout},
		{"cancelled", valueobjects.StatusCancelled},
		{"partial", valueobjects.StatusPartial},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := valueobjects.ParseExecutionStatus(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.expected {
				t.Errorf("got %q, want %q", got, tt.expected)
			}
			// round-trip: string representation must match input
			if got.String() != tt.input {
				t.Errorf("String() = %q, want %q", got.String(), tt.input)
			}
		})
	}
}

func TestParseExecutionStatus_InvalidValues(t *testing.T) {
	tests := []string{
		"",
		"SUCCESS",
		"FAILURE",
		"unknown",
		"pending",
		"running",
		"done",
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			_, err := valueobjects.ParseExecutionStatus(input)
			if err == nil {
				t.Errorf("expected error for %q, got nil", input)
			}
		})
	}
}

func TestExecutionStatus_ZeroValue_Invalid(t *testing.T) {
	var s valueobjects.ExecutionStatus
	_, err := valueobjects.ParseExecutionStatus(string(s))
	if err == nil {
		t.Error("expected zero-value (empty string) to be invalid")
	}
}

func TestExecutionStatus_MarshalJSON(t *testing.T) {
	b, err := json.Marshal(valueobjects.StatusPartial)
	if err != nil {
		t.Fatalf("MarshalJSON error: %v", err)
	}
	if string(b) != `"partial"` {
		t.Errorf("MarshalJSON = %s, want %q", b, `"partial"`)
	}
}

func TestExecutionStatus_UnmarshalJSON_Valid(t *testing.T) {
	tests := []struct {
		raw      string
		expected valueobjects.ExecutionStatus
	}{
		{`"success"`, valueobjects.StatusSuccess},
		{`"failure"`, valueobjects.StatusFailure},
		{`"timeout"`, valueobjects.StatusTimeout},
		{`"cancelled"`, valueobjects.StatusCancelled},
		{`"partial"`, valueobjects.StatusPartial},
	}

	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			var s valueobjects.ExecutionStatus
			if err := json.Unmarshal([]byte(tt.raw), &s); err != nil {
				t.Fatalf("UnmarshalJSON error: %v", err)
			}
			if s != tt.expected {
				t.Errorf("got %q, want %q", s, tt.expected)
			}
		})
	}
}

func TestExecutionStatus_UnmarshalJSON_Invalid(t *testing.T) {
	tests := []string{
		`""`,
		`"unknown"`,
		`"SUCCESS"`,
		`123`,
	}

	for _, raw := range tests {
		t.Run(raw, func(t *testing.T) {
			var s valueobjects.ExecutionStatus
			if err := json.Unmarshal([]byte(raw), &s); err == nil {
				t.Errorf("expected error for %s, got nil", raw)
			}
		})
	}
}

func TestExecutionStatus_JSONRoundTrip(t *testing.T) {
	statuses := []valueobjects.ExecutionStatus{
		valueobjects.StatusSuccess,
		valueobjects.StatusFailure,
		valueobjects.StatusTimeout,
		valueobjects.StatusCancelled,
		valueobjects.StatusPartial,
	}

	for _, original := range statuses {
		t.Run(original.String(), func(t *testing.T) {
			b, err := json.Marshal(original)
			if err != nil {
				t.Fatalf("marshal error: %v", err)
			}
			var decoded valueobjects.ExecutionStatus
			if err := json.Unmarshal(b, &decoded); err != nil {
				t.Fatalf("unmarshal error: %v", err)
			}
			if decoded != original {
				t.Errorf("round-trip failed: got %q, want %q", decoded, original)
			}
		})
	}
}
