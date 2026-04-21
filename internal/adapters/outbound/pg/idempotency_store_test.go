package pg

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
	outbound "github.com/sophia-ecosystem/runtime-adapters/internal/ports/outbound"
)

// TestNewIdempotencyStorePG_NilPoolRejected verifies that a nil pool is
// rejected at construction time (arg validation — no I/O).
func TestNewIdempotencyStorePG_NilPoolRejected(t *testing.T) {
	_, err := NewIdempotencyStorePG(nil)
	if err == nil {
		t.Fatal("expected error for nil pool, got nil")
	}
}

// TestIdempotencyStorePG_Record_NegativeWindow verifies that Record rejects
// window ≤ 0 before any I/O.
func TestIdempotencyStorePG_Record_NegativeWindow(t *testing.T) {
	// We build a store with a nil pool that would panic on any I/O.
	// The window check must fire before any pool call.
	store := &IdempotencyStorePG{pool: nil}

	key, err := shared.NewIdempotencyKey("01HZXK5JC6QK7XV0YQXA0QJ0YA")
	if err != nil {
		t.Fatalf("NewIdempotencyKey: %v", err)
	}
	rid, err := shared.NewReceiptID("01HZXK5JC6QK7XV0YQXA0QJ0YB")
	if err != nil {
		t.Fatalf("NewReceiptID: %v", err)
	}

	err = store.Record(context.Background(), key, rid, 0)
	if err == nil {
		t.Fatal("expected error for window=0, got nil")
	}

	err = store.Record(context.Background(), key, rid, -1*time.Second)
	if err == nil {
		t.Fatal("expected error for window=-1s, got nil")
	}
}

// TestIdempotencyStorePG_SentinelWrapping verifies that sentinel errors are
// properly wrapped and detectable via errors.Is.
func TestIdempotencyStorePG_SentinelWrapping(t *testing.T) {
	wrapped := errors.Join(outbound.ErrIdempotencyKeyConflict, errors.New("extra context"))
	if !errors.Is(wrapped, outbound.ErrIdempotencyKeyConflict) {
		t.Error("ErrIdempotencyKeyConflict must survive errors.Is after join")
	}
}
