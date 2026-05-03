// Package domain holds the Linear webhook adapter's pure types —
// no I/O, no HTTP, no Linear API client. Per the hexagonal layout
// established in D2C4AB.5.
package domain

import "fmt"

// Severity is the Alertmanager severity label as it arrives in the
// webhook payload's commonLabels. Only critical and warning are
// valid here — info is silenced at the Alertmanager routing root
// (I-AB.1) and never reaches this adapter. A payload carrying
// info severity is a contract violation upstream and must be
// rejected.
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityWarning  Severity = "warning"
)

// ParseSeverity validates that the incoming severity string is one
// of the two values the adapter handles. Returns an error for info
// (must be silenced upstream), empty (missing label), or anything
// else (malformed).
func ParseSeverity(s string) (Severity, error) {
	switch s {
	case string(SeverityCritical):
		return SeverityCritical, nil
	case string(SeverityWarning):
		return SeverityWarning, nil
	default:
		return "", fmt.Errorf("unsupported severity %q (expected %q or %q)",
			s, SeverityCritical, SeverityWarning)
	}
}

// LinearPriority returns the Linear issue priority that maps to
// this severity per D2C4AB.10:
//
//	critical → P1 (Urgent)  — Linear int code 1
//	warning  → P3 (Medium)  — Linear int code 3
func (s Severity) LinearPriority() int {
	switch s {
	case SeverityCritical:
		return 1
	case SeverityWarning:
		return 3
	default:
		// Defensive — ParseSeverity should have rejected anything else.
		return 0
	}
}
