// Package valueobjects holds the Phase 1 domain value objects for the
// execution bounded context. All types in this package are immutable after
// construction and equal by value.
package valueobjects_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
)

func TestNewAdapterID_Valid(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"shell", "shell"},
		{"git", "git"},
		{"filesystem", "filesystem"},
		{"http wire name", "http"},
		{"two-char min", "ab"},
		{"thirty-two chars max", "a" + strings.Repeat("b", 31)},
		{"underscore allowed", "adapter_one"},
		{"digits after letter", "adapter2"},
		{"digit in middle", "a1b"},
		{"all lowercase letters", "abcdefghijklmnop"},
		{"trailing underscore", "adapter_"},
		{"multiple underscores", "my_adapter_v2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := valueobjects.NewAdapterID(tt.input)
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tt.input, err)
			}
			if got.String() != tt.input {
				t.Errorf("String() = %q, want %q", got.String(), tt.input)
			}
		})
	}
}

func TestNewAdapterID_Invalid(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"uppercase first char", "Shell"},
		{"uppercase mid", "shEll"},
		{"all uppercase", "SHELL"},
		{"leading digit", "2shell"},
		{"hyphen not allowed", "shell-adapter"},
		{"single char too short", "a"},
		{"over 32 chars", "a" + strings.Repeat("b", 32)},
		{"whitespace", "adapter name"},
		{"dot not allowed", "adapter.name"},
		{"unicode letter first", "αdapter"},
		{"unicode letter mid", "adαpter"},
		{"slash not allowed", "adapter/name"},
		{"colon not allowed", "adapter:v1"},
		{"at sign not allowed", "@adapter"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := valueobjects.NewAdapterID(tt.input)
			if err == nil {
				t.Errorf("expected error for %q, got nil", tt.input)
			}
		})
	}
}

func TestAdapterID_String(t *testing.T) {
	for _, s := range []string{"shell", "git", "filesystem", "http"} {
		t.Run(s, func(t *testing.T) {
			id, err := valueobjects.NewAdapterID(s)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if id.String() != s {
				t.Errorf("String() = %q, want %q", id.String(), s)
			}
		})
	}
}

func TestAdapterID_MarshalJSON(t *testing.T) {
	id, err := valueobjects.NewAdapterID("shell")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	b, err := json.Marshal(id)
	if err != nil {
		t.Fatalf("MarshalJSON error: %v", err)
	}
	if string(b) != `"shell"` {
		t.Errorf("MarshalJSON = %s, want %q", b, `"shell"`)
	}
}

func TestAdapterID_UnmarshalJSON_Valid(t *testing.T) {
	tests := []struct {
		raw      string
		expected string
	}{
		{`"shell"`, "shell"},
		{`"git"`, "git"},
		{`"filesystem"`, "filesystem"},
		{`"http"`, "http"},
		{`"adapter_one"`, "adapter_one"},
	}

	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			var id valueobjects.AdapterID
			if err := json.Unmarshal([]byte(tt.raw), &id); err != nil {
				t.Fatalf("UnmarshalJSON error: %v", err)
			}
			if id.String() != tt.expected {
				t.Errorf("got %q, want %q", id.String(), tt.expected)
			}
		})
	}
}

func TestAdapterID_UnmarshalJSON_Invalid(t *testing.T) {
	tests := []string{
		`""`,
		`"Shell"`,
		`"2shell"`,
		`"shell-adapter"`,
		`"a"`,
		`123`,
		`null`,
	}

	for _, raw := range tests {
		t.Run(raw, func(t *testing.T) {
			var id valueobjects.AdapterID
			if err := json.Unmarshal([]byte(raw), &id); err == nil {
				t.Errorf("expected error for %s, got nil", raw)
			}
		})
	}
}

func TestAdapterID_JSONRoundTrip(t *testing.T) {
	inputs := []string{"shell", "git", "filesystem", "http"}

	for _, s := range inputs {
		t.Run(s, func(t *testing.T) {
			original, err := valueobjects.NewAdapterID(s)
			if err != nil {
				t.Fatalf("NewAdapterID error: %v", err)
			}
			b, err := json.Marshal(original)
			if err != nil {
				t.Fatalf("marshal error: %v", err)
			}
			var decoded valueobjects.AdapterID
			if err := json.Unmarshal(b, &decoded); err != nil {
				t.Fatalf("unmarshal error: %v", err)
			}
			if decoded.String() != original.String() {
				t.Errorf("round-trip failed: got %q, want %q", decoded.String(), original.String())
			}
		})
	}
}

// TestAdapterID_HTTPWireName verifies the Gap-1 resolution: the wire name
// for the HTTP adapter is "http" regardless of the internal Go package name
// (httpreq). AdapterID is just a validated string — no alias machinery needed.
func TestAdapterID_HTTPWireName(t *testing.T) {
	id, err := valueobjects.NewAdapterID("http")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id.String() != "http" {
		t.Errorf("wire name = %q, want %q", id.String(), "http")
	}
}
