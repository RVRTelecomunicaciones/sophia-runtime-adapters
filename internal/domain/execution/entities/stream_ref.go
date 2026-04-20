// Package entities contains the aggregate root (ExecutionReceipt) and its
// inner value objects (Request, Handle, Result, Artifact, StreamRef,
// Provenance, Timings) for the execution bounded context. All types are
// immutable after construction and validated in their constructors.
package entities

import (
	"fmt"
)

// StreamMode indicates whether a StreamRef carries inline data or a
// truncated prefix.
type StreamMode string

const (
	// StreamInline means the StreamRef Data contains the complete stream
	// (size_bytes == len(Data)).
	StreamInline StreamMode = "inline"
	// StreamTruncated means the stream was larger than the inline limit
	// and only the first truncated_at_byte bytes are present in Data.
	StreamTruncated StreamMode = "truncated"
)

// StreamRef is a stdout/stderr reference. In Phase 1 the data is always
// inline (up to InlineStreamLimit, default 16 KiB) with a truncated flag
// when the original stream was larger. Phase 2 may introduce disk-backed
// references without changing this shape.
type StreamRef struct {
	Mode            StreamMode `json:"mode"`
	Data            []byte     `json:"data,omitempty"`
	SizeBytes       int64      `json:"size_bytes"`
	TruncatedAtByte int64      `json:"truncated_at_byte,omitempty"`
}

// NewStreamRef builds a StreamRef, truncating to inlineLimit if total size
// exceeds it. inlineLimit must be > 0. Returns a zero StreamRef (Mode "")
// when data is nil and totalSize is 0 — use this to represent "no stream".
//
// totalSize is the original stream length in bytes. data may contain
// either the full stream (totalSize ≤ inlineLimit) or a prefix (totalSize
// > inlineLimit). The constructor defensively copies data.
func NewStreamRef(data []byte, totalSize int64, inlineLimit int) (StreamRef, error) {
	if inlineLimit <= 0 {
		return StreamRef{}, fmt.Errorf("inlineLimit must be > 0, got %d", inlineLimit)
	}
	if totalSize < 0 {
		return StreamRef{}, fmt.Errorf("totalSize must be ≥ 0, got %d", totalSize)
	}
	if int64(len(data)) > totalSize {
		return StreamRef{}, fmt.Errorf("data length %d exceeds totalSize %d", len(data), totalSize)
	}
	if totalSize == 0 && len(data) == 0 {
		return StreamRef{Mode: StreamInline, Data: nil, SizeBytes: 0}, nil
	}
	if totalSize <= int64(inlineLimit) {
		cp := make([]byte, len(data))
		copy(cp, data)
		return StreamRef{Mode: StreamInline, Data: cp, SizeBytes: totalSize}, nil
	}
	// Truncated: keep up to inlineLimit bytes.
	n := inlineLimit
	if len(data) < n {
		n = len(data)
	}
	cp := make([]byte, n)
	copy(cp, data[:n])
	return StreamRef{
		Mode:            StreamTruncated,
		Data:            cp,
		SizeBytes:       totalSize,
		TruncatedAtByte: int64(n),
	}, nil
}

// Note: StreamRef uses EXPORTED fields and Go's default JSON encoding.
// Unlike most VOs in this project, its behavior is pure data and the JSON
// wire format aligns perfectly with struct tags. This avoids hand-written
// MarshalJSON. base64 encoding of Data is provided automatically by
// encoding/json for []byte fields.
