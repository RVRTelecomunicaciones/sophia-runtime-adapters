//go:build integration

package pg

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
	outbound "github.com/sophia-ecosystem/runtime-adapters/internal/ports/outbound"
)

// ─── helpers ──────────────────────────────────────────────────────────────────

func mustIdempotencyKey(t *testing.T, s string) shared.IdempotencyKey {
	t.Helper()
	k, err := shared.NewIdempotencyKey(s)
	if err != nil {
		t.Fatalf("NewIdempotencyKey(%q): %v", s, err)
	}
	return k
}

func mustReceiptID(t *testing.T, s string) shared.ReceiptID {
	t.Helper()
	r, err := shared.NewReceiptID(s)
	if err != nil {
		t.Fatalf("NewReceiptID(%q): %v", s, err)
	}
	return r
}

// saveReceiptForKey saves a receipt so the FK in idempotency_keys is satisfied.
func saveReceiptForKey(t *testing.T, repo *ReceiptRepositoryPG, receiptID shared.ReceiptID) {
	t.Helper()
	gen := &fixedIDGen{id: receiptID.String()}
	receipt := buildReceipt(t, gen)
	ctx := context.Background()
	if _, err := repo.Save(ctx, receipt); err != nil {
		t.Fatalf("saveReceiptForKey: %v", err)
	}
}

// fixedIDGen implements entities.IDGenerator returning a single fixed ID.
type fixedIDGen struct{ id string }

func (g *fixedIDGen) New() string { return g.id }

// ─── integration tests ────────────────────────────────────────────────────────

func TestIdempotencyStorePG_RecordAndLookupWithinWindow(t *testing.T) {
	pool, teardown := startPG(t)
	defer teardown()

	repo, err := NewReceiptRepositoryPG(pool)
	if err != nil {
		t.Fatalf("NewReceiptRepositoryPG: %v", err)
	}
	store, err := NewIdempotencyStorePG(pool)
	if err != nil {
		t.Fatalf("NewIdempotencyStorePG: %v", err)
	}

	rid := mustReceiptID(t, "01HZXK5JC6QK7XV0YQXA0QJ0YA")
	saveReceiptForKey(t, repo, rid)

	key := mustIdempotencyKey(t, "01HZXK5JC6QK7XV0YQXA0QJ0YA")
	ctx := context.Background()

	if err := store.Record(ctx, key, rid, 10*time.Minute); err != nil {
		t.Fatalf("Record: %v", err)
	}

	found, ok, err := store.Lookup(ctx, key)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if !ok {
		t.Fatal("Lookup: expected found=true within window, got false")
	}
	if found.String() != rid.String() {
		t.Errorf("Lookup: receipt_id = %q, want %q", found, rid)
	}
}

func TestIdempotencyStorePG_LookupAfterWindowExpiry(t *testing.T) {
	pool, teardown := startPG(t)
	defer teardown()

	repo, err := NewReceiptRepositoryPG(pool)
	if err != nil {
		t.Fatalf("NewReceiptRepositoryPG: %v", err)
	}
	store, err := NewIdempotencyStorePG(pool)
	if err != nil {
		t.Fatalf("NewIdempotencyStorePG: %v", err)
	}

	rid := mustReceiptID(t, "01HZXK5JC6QK7XV0YQXA0QJ0YB")
	saveReceiptForKey(t, repo, rid)

	key := mustIdempotencyKey(t, "01HZXK5JC6QK7XV0YQXA0QJ0YB")
	ctx := context.Background()

	// Record with a 1-second window.
	if err := store.Record(ctx, key, rid, 1*time.Second); err != nil {
		t.Fatalf("Record: %v", err)
	}

	// Wait for window to expire.
	time.Sleep(1100 * time.Millisecond)

	_, ok, err := store.Lookup(ctx, key)
	if err != nil {
		t.Fatalf("Lookup after expiry: %v", err)
	}
	if ok {
		t.Error("Lookup after expiry: expected found=false, got true")
	}
}

func TestIdempotencyStorePG_SameKeySameReceiptID_IdempotentNoOp(t *testing.T) {
	pool, teardown := startPG(t)
	defer teardown()

	repo, err := NewReceiptRepositoryPG(pool)
	if err != nil {
		t.Fatalf("NewReceiptRepositoryPG: %v", err)
	}
	store, err := NewIdempotencyStorePG(pool)
	if err != nil {
		t.Fatalf("NewIdempotencyStorePG: %v", err)
	}

	rid := mustReceiptID(t, "01HZXK5JC6QK7XV0YQXA0QJ0YC")
	saveReceiptForKey(t, repo, rid)

	key := mustIdempotencyKey(t, "01HZXK5JC6QK7XV0YQXA0QJ0YC")
	ctx := context.Background()

	if err := store.Record(ctx, key, rid, 10*time.Minute); err != nil {
		t.Fatalf("first Record: %v", err)
	}
	// Recording the same key+receiptID again must be a no-op.
	if err := store.Record(ctx, key, rid, 10*time.Minute); err != nil {
		t.Errorf("second Record (same key + same receiptID): want nil, got %v", err)
	}
}

func TestIdempotencyStorePG_SameKeyDifferentReceiptID_ReturnsConflict(t *testing.T) {
	pool, teardown := startPG(t)
	defer teardown()

	repo, err := NewReceiptRepositoryPG(pool)
	if err != nil {
		t.Fatalf("NewReceiptRepositoryPG: %v", err)
	}
	store, err := NewIdempotencyStorePG(pool)
	if err != nil {
		t.Fatalf("NewIdempotencyStorePG: %v", err)
	}

	rid1 := mustReceiptID(t, "01HZXK5JC6QK7XV0YQXA0QJ0YD")
	rid2 := mustReceiptID(t, "01HZXK5JC6QK7XV0YQXA0QJ0YE")
	saveReceiptForKey(t, repo, rid1)
	saveReceiptForKey(t, repo, rid2)

	key := mustIdempotencyKey(t, "01HZXK5JC6QK7XV0YQXA0QJ0YD")
	ctx := context.Background()

	if err := store.Record(ctx, key, rid1, 10*time.Minute); err != nil {
		t.Fatalf("first Record: %v", err)
	}

	err = store.Record(ctx, key, rid2, 10*time.Minute)
	if !errors.Is(err, outbound.ErrIdempotencyKeyConflict) {
		t.Errorf("second Record (different receiptID): want ErrIdempotencyKeyConflict, got %v", err)
	}
}

func TestIdempotencyStorePG_Record_ZeroWindow_Rejected(t *testing.T) {
	pool, teardown := startPG(t)
	defer teardown()

	store, err := NewIdempotencyStorePG(pool)
	if err != nil {
		t.Fatalf("NewIdempotencyStorePG: %v", err)
	}

	key := mustIdempotencyKey(t, "01HZXK5JC6QK7XV0YQXA0QJ0YA")
	rid := mustReceiptID(t, "01HZXK5JC6QK7XV0YQXA0QJ0YA")

	if err := store.Record(context.Background(), key, rid, 0); err == nil {
		t.Error("expected error for window=0, got nil")
	}
}

func TestIdempotencyStorePG_FirstWriterWins(t *testing.T) {
	pool, teardown := startPG(t)
	defer teardown()

	repo, err := NewReceiptRepositoryPG(pool)
	if err != nil {
		t.Fatalf("NewReceiptRepositoryPG: %v", err)
	}
	store, err := NewIdempotencyStorePG(pool)
	if err != nil {
		t.Fatalf("NewIdempotencyStorePG: %v", err)
	}

	// Both receipts exist in the receipts table so FK is satisfied.
	rid1 := mustReceiptID(t, "01HZXK5JC6QK7XV0YQXA0QJ0YF")
	rid2 := mustReceiptID(t, "01HZXK5JC6QK7XV0YQXA0QJ0YG")
	saveReceiptForKey(t, repo, rid1)
	saveReceiptForKey(t, repo, rid2)

	key := mustIdempotencyKey(t, "01HZXK5JC6QK7XV0YQXA0QJ0YF")
	ctx := context.Background()

	// First writer records rid1.
	if err := store.Record(ctx, key, rid1, 10*time.Minute); err != nil {
		t.Fatalf("first writer Record: %v", err)
	}

	// Second writer records rid2 for the same key → conflict.
	err = store.Record(ctx, key, rid2, 10*time.Minute)
	if !errors.Is(err, outbound.ErrIdempotencyKeyConflict) {
		t.Errorf("second writer: want ErrIdempotencyKeyConflict, got %v", err)
	}

	// Lookup still returns the first writer's receipt.
	found, ok, err := store.Lookup(ctx, key)
	if err != nil || !ok {
		t.Fatalf("Lookup after first-writer-wins: ok=%v err=%v", ok, err)
	}
	if found.String() != rid1.String() {
		t.Errorf("first-writer-wins: got %q, want %q", found, rid1)
	}
}
