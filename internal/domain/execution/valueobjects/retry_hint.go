package valueobjects

import (
	"encoding/json"
	"fmt"
)

// RetryHint is a closed enum of 3 retry advisories per spec §4.4.
// Extending the enum requires an ADR and a schema_version bump (R15).
type RetryHint string

const (
	HintRetryable    RetryHint = "retryable"
	HintNonRetryable RetryHint = "non_retryable"
	HintUnknown      RetryHint = "unknown"
)

var allRetryHints = map[RetryHint]struct{}{
	HintRetryable:    {},
	HintNonRetryable: {},
	HintUnknown:      {},
}

// ParseRetryHint validates s and returns the matching RetryHint
// or an error for unknown values.
func ParseRetryHint(s string) (RetryHint, error) {
	rh := RetryHint(s)
	if _, ok := allRetryHints[rh]; !ok {
		return "", fmt.Errorf("invalid retry_hint %q", s)
	}
	return rh, nil
}

// String returns the canonical snake_case form.
func (r RetryHint) String() string { return string(r) }

// MarshalJSON encodes the hint as a JSON string.
func (r RetryHint) MarshalJSON() ([]byte, error) { return json.Marshal(string(r)) }

// UnmarshalJSON decodes a JSON string and validates it against the closed enum.
func (r *RetryHint) UnmarshalJSON(b []byte) error {
	var raw string
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	parsed, err := ParseRetryHint(raw)
	if err != nil {
		return err
	}
	*r = parsed
	return nil
}
