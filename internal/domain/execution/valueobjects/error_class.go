package valueobjects

import (
	"encoding/json"
	"fmt"
)

// ErrorClass is a closed enum of 10 failure categories per spec §4.6.
// Wire format uses snake_case per §5.14.
// Extending the enum requires an ADR and a schema_version bump (R15).
type ErrorClass string

const (
	ErrValidationFailure    ErrorClass = "validation_failure"
	ErrCapabilityUnknown    ErrorClass = "capability_unknown"
	ErrPayloadSchemaFailure ErrorClass = "payload_schema_failure"
	ErrPreconditionFailure  ErrorClass = "precondition_failure"
	ErrExternalFailure      ErrorClass = "external_failure"
	ErrTimeout              ErrorClass = "timeout"
	ErrCancelled            ErrorClass = "cancelled"
	ErrInterrupted          ErrorClass = "interrupted"
	ErrNormalizationFailure ErrorClass = "normalization_failure"
	ErrAdapterInternalError ErrorClass = "adapter_internal_error"
)

var allErrorClasses = map[ErrorClass]struct{}{
	ErrValidationFailure:    {},
	ErrCapabilityUnknown:    {},
	ErrPayloadSchemaFailure: {},
	ErrPreconditionFailure:  {},
	ErrExternalFailure:      {},
	ErrTimeout:              {},
	ErrCancelled:            {},
	ErrInterrupted:          {},
	ErrNormalizationFailure: {},
	ErrAdapterInternalError: {},
}

// ParseErrorClass validates s and returns the matching ErrorClass
// or an error for unknown values.
func ParseErrorClass(s string) (ErrorClass, error) {
	ec := ErrorClass(s)
	if _, ok := allErrorClasses[ec]; !ok {
		return "", fmt.Errorf("invalid error_class %q", s)
	}
	return ec, nil
}

// String returns the canonical snake_case form.
func (e ErrorClass) String() string { return string(e) }

// MarshalJSON encodes the error class as a JSON string.
func (e ErrorClass) MarshalJSON() ([]byte, error) { return json.Marshal(string(e)) }

// UnmarshalJSON decodes a JSON string and validates it against the closed enum.
func (e *ErrorClass) UnmarshalJSON(b []byte) error {
	var raw string
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	parsed, err := ParseErrorClass(raw)
	if err != nil {
		return err
	}
	*e = parsed
	return nil
}

// DefaultRetryHint returns the deterministic default retry hint for this
// error class, per spec §5.9. Adapters may override with more specific
// information in the raw outcome; the normalizer prefers the override.
func (e ErrorClass) DefaultRetryHint() RetryHint {
	switch e {
	case ErrTimeout, ErrInterrupted:
		return HintRetryable
	case ErrExternalFailure:
		return HintUnknown
	case ErrValidationFailure, ErrCapabilityUnknown, ErrPayloadSchemaFailure,
		ErrPreconditionFailure, ErrCancelled, ErrNormalizationFailure, ErrAdapterInternalError:
		return HintNonRetryable
	default:
		return HintUnknown
	}
}
