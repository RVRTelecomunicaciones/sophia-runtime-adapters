// Package valueobjects holds the Phase 1 domain value objects for the
// execution bounded context. All types in this package are immutable after
// construction and equal by value.
package valueobjects

import (
	"encoding/json"
	"fmt"
)

// ContentTypeJSON is the only supported payload content type in Phase 1 (D5.5).
const ContentTypeJSON = "application/json"

// Payload is the opaque input envelope for capability execution. Phase 1 allows
// only application/json; adapters parse and validate the semantic schema of
// their own payload types (per D3.9). Runtime validates only the envelope:
// content type, non-emptiness, size bound, and JSON validity.
type Payload struct {
	contentType string
	data        json.RawMessage
}

// NewPayload constructs a Payload, validating the envelope constraints.
// Rejects non-JSON content types, empty data, data exceeding maxBytes,
// and data that is not valid JSON.
func NewPayload(contentType string, data json.RawMessage, maxBytes int) (Payload, error) {
	if contentType != ContentTypeJSON {
		return Payload{}, fmt.Errorf("unsupported content_type %q (Phase 1: %s only)", contentType, ContentTypeJSON)
	}
	if len(data) == 0 {
		return Payload{}, fmt.Errorf("payload data is empty")
	}
	if maxBytes > 0 && len(data) > maxBytes {
		return Payload{}, fmt.Errorf("payload exceeds max %d bytes (got %d)", maxBytes, len(data))
	}
	if !json.Valid(data) {
		return Payload{}, fmt.Errorf("payload is not valid JSON")
	}
	// Defensive copy so callers can't mutate internal state.
	cp := make(json.RawMessage, len(data))
	copy(cp, data)
	return Payload{contentType: contentType, data: cp}, nil
}

// ContentType returns the envelope content type ("application/json" in Phase 1).
func (p Payload) ContentType() string { return p.contentType }

// Raw returns a defensive copy of the JSON bytes. Callers may safely mutate
// the returned slice without affecting the Payload.
func (p Payload) Raw() json.RawMessage {
	out := make(json.RawMessage, len(p.data))
	copy(out, p.data)
	return out
}

// SizeBytes returns the length of the underlying JSON payload in bytes.
func (p Payload) SizeBytes() int { return len(p.data) }
