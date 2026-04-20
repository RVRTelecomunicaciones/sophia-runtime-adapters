package entities_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/entities"
)

func TestNewStreamRef_InvalidInlineLimit(t *testing.T) {
	tests := []struct {
		name        string
		inlineLimit int
	}{
		{"zero", 0},
		{"negative", -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := entities.NewStreamRef(nil, 0, tt.inlineLimit)
			if err == nil {
				t.Errorf("expected error for inlineLimit=%d, got nil", tt.inlineLimit)
			}
		})
	}
}

func TestNewStreamRef_NegativeTotalSize(t *testing.T) {
	_, err := entities.NewStreamRef(nil, -1, 16)
	if err == nil {
		t.Error("expected error for totalSize=-1, got nil")
	}
}

func TestNewStreamRef_DataExceedsTotalSize(t *testing.T) {
	_, err := entities.NewStreamRef([]byte("hello"), 3, 16)
	if err == nil {
		t.Error("expected error when len(data) > totalSize, got nil")
	}
}

func TestNewStreamRef_EmptyStream(t *testing.T) {
	ref, err := entities.NewStreamRef(nil, 0, 16)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ref.Mode != entities.StreamInline {
		t.Errorf("mode = %q, want %q", ref.Mode, entities.StreamInline)
	}
	if ref.SizeBytes != 0 {
		t.Errorf("size_bytes = %d, want 0", ref.SizeBytes)
	}
	if ref.Data != nil {
		t.Errorf("data = %v, want nil", ref.Data)
	}
}

func TestNewStreamRef_InlineMode(t *testing.T) {
	data := []byte("hello world")
	ref, err := entities.NewStreamRef(data, int64(len(data)), 16384)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ref.Mode != entities.StreamInline {
		t.Errorf("mode = %q, want %q", ref.Mode, entities.StreamInline)
	}
	if ref.SizeBytes != int64(len(data)) {
		t.Errorf("size_bytes = %d, want %d", ref.SizeBytes, len(data))
	}
	if !bytes.Equal(ref.Data, data) {
		t.Errorf("data = %v, want %v", ref.Data, data)
	}
	if ref.TruncatedAtByte != 0 {
		t.Errorf("truncated_at_byte = %d, want 0", ref.TruncatedAtByte)
	}
}

func TestNewStreamRef_TruncatedMode(t *testing.T) {
	const inlineLimit = 4
	// totalSize exceeds inlineLimit; data contains full stream
	data := []byte("hello world") // 11 bytes > 4
	ref, err := entities.NewStreamRef(data, int64(len(data)), inlineLimit)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ref.Mode != entities.StreamTruncated {
		t.Errorf("mode = %q, want %q", ref.Mode, entities.StreamTruncated)
	}
	if int(ref.TruncatedAtByte) != inlineLimit {
		t.Errorf("truncated_at_byte = %d, want %d", ref.TruncatedAtByte, inlineLimit)
	}
	if len(ref.Data) != inlineLimit {
		t.Errorf("len(data) = %d, want %d", len(ref.Data), inlineLimit)
	}
	if !bytes.Equal(ref.Data, data[:inlineLimit]) {
		t.Errorf("data = %v, want %v", ref.Data, data[:inlineLimit])
	}
	if ref.SizeBytes != int64(len(data)) {
		t.Errorf("size_bytes = %d, want %d", ref.SizeBytes, len(data))
	}
}

func TestNewStreamRef_TruncatedMode_DataShorterThanLimit(t *testing.T) {
	// Caller only passes a prefix of the stream (e.g. only first 2 bytes),
	// but reports totalSize as 100 (the actual stream size).
	const inlineLimit = 16
	data := []byte("ab") // only 2 bytes available to caller
	ref, err := entities.NewStreamRef(data, 100, inlineLimit)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ref.Mode != entities.StreamTruncated {
		t.Errorf("mode = %q, want %q", ref.Mode, entities.StreamTruncated)
	}
	// TruncatedAtByte should be min(len(data), inlineLimit) = 2
	if ref.TruncatedAtByte != 2 {
		t.Errorf("truncated_at_byte = %d, want 2", ref.TruncatedAtByte)
	}
	if len(ref.Data) != 2 {
		t.Errorf("len(data) = %d, want 2", len(ref.Data))
	}
}

func TestNewStreamRef_DefensiveCopy(t *testing.T) {
	data := []byte("original")
	ref, err := entities.NewStreamRef(data, int64(len(data)), 16384)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Mutate input slice after construction.
	data[0] = 'X'
	if ref.Data[0] == 'X' {
		t.Error("StreamRef.Data was not defensively copied — input mutation affected internal state")
	}
}

func TestNewStreamRef_JSONRoundTrip(t *testing.T) {
	data := []byte("stdout output")
	original, err := entities.NewStreamRef(data, int64(len(data)), 16384)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	b, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	var decoded entities.StreamRef
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if decoded.Mode != original.Mode {
		t.Errorf("mode: got %q, want %q", decoded.Mode, original.Mode)
	}
	if decoded.SizeBytes != original.SizeBytes {
		t.Errorf("size_bytes: got %d, want %d", decoded.SizeBytes, original.SizeBytes)
	}
	if !bytes.Equal(decoded.Data, original.Data) {
		t.Errorf("data mismatch after JSON round-trip")
	}
}

func TestNewStreamRef_JSONRoundTrip_Truncated(t *testing.T) {
	data := []byte("hello world truncated stream")
	original, err := entities.NewStreamRef(data, 1000, 4)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	b, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	var decoded entities.StreamRef
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if decoded.Mode != entities.StreamTruncated {
		t.Errorf("mode: got %q, want %q", decoded.Mode, entities.StreamTruncated)
	}
	if decoded.TruncatedAtByte != original.TruncatedAtByte {
		t.Errorf("truncated_at_byte: got %d, want %d", decoded.TruncatedAtByte, original.TruncatedAtByte)
	}
	if !bytes.Equal(decoded.Data, original.Data) {
		t.Errorf("data mismatch after JSON round-trip")
	}
}
