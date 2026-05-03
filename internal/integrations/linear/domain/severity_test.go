package domain_test

import (
	"testing"

	"github.com/sophia-ecosystem/runtime-adapters/internal/integrations/linear/domain"
)

func TestSeverity_LinearPriority(t *testing.T) {
	tests := []struct {
		name     string
		input    domain.Severity
		wantPrio int
	}{
		{"critical → P1 Urgent", domain.SeverityCritical, 1},
		{"warning → P3 Medium", domain.SeverityWarning, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.input.LinearPriority()
			if got != tt.wantPrio {
				t.Errorf("Severity(%q).LinearPriority() = %d, want %d",
					tt.input, got, tt.wantPrio)
			}
		})
	}
}

func TestParseSeverity(t *testing.T) {
	tests := []struct {
		input   string
		want    domain.Severity
		wantErr bool
	}{
		{"critical", domain.SeverityCritical, false},
		{"warning", domain.SeverityWarning, false},
		{"info", "", true},  // info is silenced upstream — adapter must reject
		{"", "", true},
		{"CRITICAL", "", true}, // case-sensitive
		{"unknown", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := domain.ParseSeverity(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseSeverity(%q) err = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("ParseSeverity(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
