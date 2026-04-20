// Package valueobjects holds the Phase 1 domain value objects for the
// execution bounded context. All types in this package are immutable after
// construction and equal by value.
package valueobjects

import (
	"encoding/json"
	"fmt"
)

// ExecutionStatus is a closed enum of 5 terminal outcomes per spec §4.3.
// Extending the enum requires an ADR and a schema_version bump (R15).
type ExecutionStatus string

const (
	StatusSuccess   ExecutionStatus = "success"
	StatusFailure   ExecutionStatus = "failure"
	StatusTimeout   ExecutionStatus = "timeout"
	StatusCancelled ExecutionStatus = "cancelled"
	StatusPartial   ExecutionStatus = "partial"
)

var allStatuses = map[ExecutionStatus]struct{}{
	StatusSuccess:   {},
	StatusFailure:   {},
	StatusTimeout:   {},
	StatusCancelled: {},
	StatusPartial:   {},
}

// ParseExecutionStatus validates s and returns the matching ExecutionStatus
// or an error for unknown values.
func ParseExecutionStatus(s string) (ExecutionStatus, error) {
	es := ExecutionStatus(s)
	if _, ok := allStatuses[es]; !ok {
		return "", fmt.Errorf("invalid execution_status %q", s)
	}
	return es, nil
}

// String returns the canonical lowercase form.
func (s ExecutionStatus) String() string { return string(s) }

// MarshalJSON encodes the status as a JSON string.
func (s ExecutionStatus) MarshalJSON() ([]byte, error) { return json.Marshal(string(s)) }

// UnmarshalJSON decodes a JSON string and validates it against the closed enum.
func (s *ExecutionStatus) UnmarshalJSON(b []byte) error {
	var raw string
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	parsed, err := ParseExecutionStatus(raw)
	if err != nil {
		return err
	}
	*s = parsed
	return nil
}
