// Package shared contains cross-cutting value types used across the
// runtime-adapters domain (IDs, Clock). Types here have no behavior beyond
// validation and equality; they never perform I/O.
package shared

import (
	"errors"
	"fmt"
	"regexp"
)

// ErrInvalidID is returned when any ID constructor receives a string that
// does not match the required format for that ID type.
var ErrInvalidID = errors.New("invalid id")

var (
	// Crockford base-32 ULID (26 chars, uppercase, excludes I/L/O/U).
	ulidRegex = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`)
	// RFC 4122 UUID (lowercase or uppercase hex, hyphenated, 8-4-4-4-12).
	uuidRegex = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
)

// ReceiptID identifies a persisted ExecutionReceipt. Must be a ULID.
type ReceiptID struct{ v string }

// HandleID identifies a running or completed ExecutionHandle. Must be a ULID.
type HandleID struct{ v string }

// CorrelationID cross-references a request across repositories (governance,
// runtime, memory-engine). Must be a ULID supplied by the caller (governance).
type CorrelationID struct{ v string }

// IdempotencyKey is caller-owned and deduplicates retries within a
// configurable window. Accepts ULID or UUID.
type IdempotencyKey struct{ v string }

// NewReceiptID validates s as a ULID and returns a ReceiptID.
func NewReceiptID(s string) (ReceiptID, error) {
	if err := validateULID(s); err != nil {
		return ReceiptID{}, err
	}
	return ReceiptID{v: s}, nil
}

// NewHandleID validates s as a ULID and returns a HandleID.
func NewHandleID(s string) (HandleID, error) {
	if err := validateULID(s); err != nil {
		return HandleID{}, err
	}
	return HandleID{v: s}, nil
}

// NewCorrelationID validates s as a ULID and returns a CorrelationID.
func NewCorrelationID(s string) (CorrelationID, error) {
	if err := validateULID(s); err != nil {
		return CorrelationID{}, err
	}
	return CorrelationID{v: s}, nil
}

// NewIdempotencyKey validates s as a ULID or RFC 4122 UUID and returns an IdempotencyKey.
func NewIdempotencyKey(s string) (IdempotencyKey, error) {
	if ulidRegex.MatchString(s) || uuidRegex.MatchString(s) {
		return IdempotencyKey{v: s}, nil
	}
	return IdempotencyKey{}, fmt.Errorf("%w: idempotency_key must be ULID or UUID: %q", ErrInvalidID, s)
}

// String returns the canonical string form of the ID.
func (r ReceiptID) String() string      { return r.v }
func (h HandleID) String() string       { return h.v }
func (c CorrelationID) String() string  { return c.v }
func (k IdempotencyKey) String() string { return k.v }

func validateULID(s string) error {
	if !ulidRegex.MatchString(s) {
		return fmt.Errorf("%w: expected 26-char ULID, got %q", ErrInvalidID, s)
	}
	return nil
}
