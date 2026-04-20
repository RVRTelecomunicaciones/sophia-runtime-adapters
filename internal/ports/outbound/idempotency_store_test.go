package outbound_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/sophia-ecosystem/runtime-adapters/internal/ports/outbound"
)

func TestIdempotencyStore_SentinelErrors_Exist(t *testing.T) {
	if outbound.ErrIdempotencyKeyNotFound == nil {
		t.Fatal("ErrIdempotencyKeyNotFound must not be nil")
	}
	if outbound.ErrIdempotencyKeyConflict == nil {
		t.Fatal("ErrIdempotencyKeyConflict must not be nil")
	}
}

func TestIdempotencyStore_SentinelErrors_AreDistinct(t *testing.T) {
	if errors.Is(outbound.ErrIdempotencyKeyNotFound, outbound.ErrIdempotencyKeyConflict) {
		t.Error("ErrIdempotencyKeyNotFound must not match ErrIdempotencyKeyConflict")
	}
	if errors.Is(outbound.ErrIdempotencyKeyConflict, outbound.ErrIdempotencyKeyNotFound) {
		t.Error("ErrIdempotencyKeyConflict must not match ErrIdempotencyKeyNotFound")
	}
}

func TestIdempotencyStore_SentinelErrors_WrappedMatching(t *testing.T) {
	wrappedNotFound := fmt.Errorf("%w: key-abc", outbound.ErrIdempotencyKeyNotFound)
	if !errors.Is(wrappedNotFound, outbound.ErrIdempotencyKeyNotFound) {
		t.Error("wrapped ErrIdempotencyKeyNotFound must satisfy errors.Is")
	}

	wrappedConflict := fmt.Errorf("%w: key-abc existing=X new=Y", outbound.ErrIdempotencyKeyConflict)
	if !errors.Is(wrappedConflict, outbound.ErrIdempotencyKeyConflict) {
		t.Error("wrapped ErrIdempotencyKeyConflict must satisfy errors.Is")
	}
}

func TestIdempotencyStore_SentinelErrors_CrossWrapDoesNotMatch(t *testing.T) {
	wrappedNotFound := fmt.Errorf("%w: key-abc", outbound.ErrIdempotencyKeyNotFound)
	if errors.Is(wrappedNotFound, outbound.ErrIdempotencyKeyConflict) {
		t.Error("ErrIdempotencyKeyNotFound wrapped must not match ErrIdempotencyKeyConflict")
	}

	wrappedConflict := fmt.Errorf("%w: key-abc existing=X new=Y", outbound.ErrIdempotencyKeyConflict)
	if errors.Is(wrappedConflict, outbound.ErrIdempotencyKeyNotFound) {
		t.Error("ErrIdempotencyKeyConflict wrapped must not match ErrIdempotencyKeyNotFound")
	}
}
