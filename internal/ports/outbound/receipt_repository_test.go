package outbound_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/sophia-ecosystem/runtime-adapters/internal/ports/outbound"
)

func TestReceiptRepository_SentinelErrors_Exist(t *testing.T) {
	if outbound.ErrReceiptNotFound == nil {
		t.Fatal("ErrReceiptNotFound must not be nil")
	}
	if outbound.ErrReceiptAlreadyExists == nil {
		t.Fatal("ErrReceiptAlreadyExists must not be nil")
	}
}

func TestReceiptRepository_SentinelErrors_AreDistinct(t *testing.T) {
	if errors.Is(outbound.ErrReceiptNotFound, outbound.ErrReceiptAlreadyExists) {
		t.Error("ErrReceiptNotFound must not match ErrReceiptAlreadyExists")
	}
	if errors.Is(outbound.ErrReceiptAlreadyExists, outbound.ErrReceiptNotFound) {
		t.Error("ErrReceiptAlreadyExists must not match ErrReceiptNotFound")
	}
}

func TestReceiptRepository_SentinelErrors_WrappedMatching(t *testing.T) {
	wrappedNotFound := fmt.Errorf("%w: id-123", outbound.ErrReceiptNotFound)
	if !errors.Is(wrappedNotFound, outbound.ErrReceiptNotFound) {
		t.Error("wrapped ErrReceiptNotFound must satisfy errors.Is")
	}

	wrappedAlreadyExists := fmt.Errorf("%w: id-456", outbound.ErrReceiptAlreadyExists)
	if !errors.Is(wrappedAlreadyExists, outbound.ErrReceiptAlreadyExists) {
		t.Error("wrapped ErrReceiptAlreadyExists must satisfy errors.Is")
	}
}

func TestReceiptRepository_SentinelErrors_CrossWrapDoesNotMatch(t *testing.T) {
	wrappedNotFound := fmt.Errorf("%w: id-123", outbound.ErrReceiptNotFound)
	if errors.Is(wrappedNotFound, outbound.ErrReceiptAlreadyExists) {
		t.Error("ErrReceiptNotFound wrapped must not match ErrReceiptAlreadyExists")
	}

	wrappedAlreadyExists := fmt.Errorf("%w: id-456", outbound.ErrReceiptAlreadyExists)
	if errors.Is(wrappedAlreadyExists, outbound.ErrReceiptNotFound) {
		t.Error("ErrReceiptAlreadyExists wrapped must not match ErrReceiptNotFound")
	}
}
