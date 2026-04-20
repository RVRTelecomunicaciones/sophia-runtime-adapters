package testdoubles_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/entities"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/services"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
	"github.com/sophia-ecosystem/runtime-adapters/internal/ports/outbound"
	"github.com/sophia-ecosystem/runtime-adapters/internal/ports/outbound/testdoubles"
)

// ─── test fixtures ─────────────────────────────────────────────────────────────

// validULID is a well-formed 26-char Crockford base-32 ULID.
const validULID = "01HZXK5JC6QK7XV0YQXA0QJ0YZ"

// makeULIDN generates a deterministic ULID-like string for index n.
// Uses a base pattern and encodes n into the last 3 chars (0-padded hex-like).
// Works for n 0..999 in tests.
func makeULIDN(n int) string {
	// ULID chars: 0-9 A-H J-N P-T V-Z (Crockford base-32, no I L O U)
	const chars = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	base := []byte("01HZXK5JC6QK7XV0YQXA0QJ0YZ")
	// Encode n into last 4 positions
	v := n
	for i := 25; i >= 22; i-- {
		base[i] = chars[v%32]
		v /= 32
	}
	return string(base)
}

// fakeRaw is a minimal AdapterRawOutcome for use in stub tests.
type fakeRaw struct{ val string }

func (fakeRaw) IsAdapterRawOutcome() {}

var _ services.AdapterRawOutcome = fakeRaw{}

// mustAdapterID is a test helper that panics on error.
func mustAdapterID(t *testing.T, s string) valueobjects.AdapterID {
	t.Helper()
	id, err := valueobjects.NewAdapterID(s)
	if err != nil {
		t.Fatalf("mustAdapterID(%q): %v", s, err)
	}
	return id
}

// mustCapability is a test helper.
func mustCapability(t *testing.T, adapterID valueobjects.AdapterID, name, version string) valueobjects.Capability {
	t.Helper()
	cap, err := valueobjects.NewCapability(adapterID, name, version, false, 5*time.Second)
	if err != nil {
		t.Fatalf("mustCapability: %v", err)
	}
	return cap
}

// mustPayload is a test helper.
func mustPayload(t *testing.T) valueobjects.Payload {
	t.Helper()
	pl, err := valueobjects.NewPayload(valueobjects.ContentTypeJSON, json.RawMessage(`{"k":1}`), 0)
	if err != nil {
		t.Fatalf("mustPayload: %v", err)
	}
	return pl
}

// mustReceiptID is a test helper.
func mustReceiptID(t *testing.T, s string) shared.ReceiptID {
	t.Helper()
	id, err := shared.NewReceiptID(s)
	if err != nil {
		t.Fatalf("mustReceiptID(%q): %v", s, err)
	}
	return id
}

// mustIdempotencyKey is a test helper.
func mustIdempotencyKey(t *testing.T, s string) shared.IdempotencyKey {
	t.Helper()
	k, err := shared.NewIdempotencyKey(s)
	if err != nil {
		t.Fatalf("mustIdempotencyKey(%q): %v", s, err)
	}
	return k
}

// mustReceipt builds a minimal valid ExecutionReceipt using the given ULID
// as its ReceiptID.
func mustReceipt(t *testing.T, receiptULID string) entities.ExecutionReceipt {
	t.Helper()

	// IDs needed: receipt + handle (two distinct ULIDs)
	handleULID := "01HZXK5JC6QK7XV0YQXA0QJ0YA"
	if receiptULID == handleULID {
		handleULID = "01HZXK5JC6QK7XV0YQXA0QJ0YB"
	}
	gen := &entities.FakeIDGen{IDs: []string{receiptULID}}

	baseTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := &shared.FakeClock{T: baseTime}

	// Build correlation ID
	cid, err := shared.NewCorrelationID(validULID)
	if err != nil {
		t.Fatalf("mustReceipt NewCorrelationID: %v", err)
	}

	// Build adapter ID and capability for the request
	aid, err := valueobjects.NewAdapterID("shell")
	if err != nil {
		t.Fatalf("mustReceipt NewAdapterID: %v", err)
	}
	cap, err := valueobjects.NewCapability(aid, "exec", "v1", false, 5*time.Second)
	if err != nil {
		t.Fatalf("mustReceipt NewCapability: %v", err)
	}

	pl, err := valueobjects.NewPayload(valueobjects.ContentTypeJSON, json.RawMessage(`{"cmd":"ls"}`), 0)
	if err != nil {
		t.Fatalf("mustReceipt NewPayload: %v", err)
	}
	tb, err := valueobjects.NewTimeoutBudget(5000, 0)
	if err != nil {
		t.Fatalf("mustReceipt NewTimeoutBudget: %v", err)
	}

	req, err := entities.NewExecutionRequest(entities.ExecutionRequestInput{
		CorrelationID:     cid,
		AdapterID:         aid,
		CapabilityName:    cap.Name(),
		CapabilityVersion: cap.Version(),
		Payload:           pl,
		TimeoutBudget:     tb,
	}, clk)
	if err != nil {
		t.Fatalf("mustReceipt NewExecutionRequest: %v", err)
	}

	// Build handle using a separate FakeIDGen
	handleGen := &entities.FakeIDGen{IDs: []string{handleULID}}
	clk.Advance(time.Millisecond)
	h, err := entities.NewExecutionHandle(handleGen, cid, cap, clk)
	if err != nil {
		t.Fatalf("mustReceipt NewExecutionHandle: %v", err)
	}

	// Build a success result
	clk.Advance(100 * time.Millisecond)
	res, err := entities.NewExecutionResult(
		valueobjects.StatusSuccess,
		valueobjects.HintNonRetryable,
		"", "",
		nil, nil, nil,
		nil, nil,
		0, 0,
		100*time.Millisecond,
		clk.Now(),
	)
	if err != nil {
		t.Fatalf("mustReceipt NewExecutionResult: %v", err)
	}

	prov, err := entities.NewProvenance(entities.ProvTest, "1.0.0", "host-a", "runtime-1.2.3", "")
	if err != nil {
		t.Fatalf("mustReceipt NewProvenance: %v", err)
	}

	// Build timings
	timBase := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	timClk := &shared.FakeClock{T: timBase}
	bld := entities.NewTimingsBuilder(timClk).MarkSubmitted()
	timClk.Advance(10 * time.Millisecond)
	bld.MarkStarted()
	timClk.Advance(100 * time.Millisecond)
	bld.MarkCompleted()
	timings, err := bld.Build()
	if err != nil {
		t.Fatalf("mustReceipt Build timings: %v", err)
	}

	clk.Advance(10 * time.Millisecond)
	receipt, err := entities.NewExecutionReceipt(gen, req, h, res, prov, timings, clk)
	if err != nil {
		t.Fatalf("mustReceipt NewExecutionReceipt: %v", err)
	}
	return receipt
}

// ─── StubAdapter tests ─────────────────────────────────────────────────────────

func TestStubAdapter_InterfaceSatisfaction(t *testing.T) {
	aid := mustAdapterID(t, "shell")
	stub := testdoubles.NewStubAdapter(aid)
	var _ outbound.Adapter = stub

	// ID() is a simple getter — exercise it here so coverage tracks it.
	if stub.ID() != aid {
		t.Errorf("ID() = %v, want %v", stub.ID(), aid)
	}
}

func TestStubAdapter_Capabilities_DefensiveCopy(t *testing.T) {
	aid := mustAdapterID(t, "shell")
	cap := mustCapability(t, aid, "exec", "v1")
	stub := testdoubles.NewStubAdapter(aid, cap)

	caps1 := stub.Capabilities()
	caps2 := stub.Capabilities()

	// Mutating caps1 must not affect caps2 or future calls.
	if len(caps1) == 0 {
		t.Fatal("expected at least one capability")
	}
	// Replace the slice entry — this should not affect the stub's internal state.
	caps1[0] = valueobjects.Capability{}

	caps3 := stub.Capabilities()
	if caps3[0].Canonical() != cap.Canonical() {
		t.Errorf("Capabilities defensive copy broken: got %q, want %q", caps3[0].Canonical(), cap.Canonical())
	}
	_ = caps2
}

func TestStubAdapter_NewStubAdapter_PanicOnAdapterIDMismatch(t *testing.T) {
	shellID := mustAdapterID(t, "shell")
	gitID := mustAdapterID(t, "git")
	gitCap := mustCapability(t, gitID, "clone", "v1")

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic when capability adapter_id mismatches stub ID")
		}
	}()
	testdoubles.NewStubAdapter(shellID, gitCap)
}

func TestStubAdapter_Execute_ReturnsRawOutcome(t *testing.T) {
	aid := mustAdapterID(t, "shell")
	cap := mustCapability(t, aid, "exec", "v1")
	stub := testdoubles.NewStubAdapter(aid, cap)

	raw := fakeRaw{val: "ok"}
	stub.Program(cap.Canonical(), testdoubles.StubProgram{Raw: raw})

	got, err := stub.Execute(context.Background(), cap, mustPayload(t))
	if err != nil {
		t.Fatalf("Execute: unexpected error: %v", err)
	}
	if got != raw {
		t.Errorf("Execute: got %v, want %v", got, raw)
	}
}

func TestStubAdapter_Execute_UnprogrammedCapabilityReturnsError(t *testing.T) {
	aid := mustAdapterID(t, "shell")
	cap := mustCapability(t, aid, "exec", "v1")
	stub := testdoubles.NewStubAdapter(aid, cap)
	// Do NOT program any behavior.

	_, err := stub.Execute(context.Background(), cap, mustPayload(t))
	if err == nil {
		t.Fatal("expected error for unprogrammed capability, got nil")
	}
}

func TestStubAdapter_Execute_ErrWins(t *testing.T) {
	aid := mustAdapterID(t, "shell")
	cap := mustCapability(t, aid, "exec", "v1")
	stub := testdoubles.NewStubAdapter(aid, cap)

	sentinel := errors.New("adapter boom")
	stub.Program(cap.Canonical(), testdoubles.StubProgram{
		Raw: fakeRaw{val: "ignored"},
		Err: sentinel,
	})

	_, err := stub.Execute(context.Background(), cap, mustPayload(t))
	if !errors.Is(err, sentinel) {
		t.Errorf("Execute: expected sentinel error, got %v", err)
	}
}

func TestStubAdapter_Execute_CancelledCtx(t *testing.T) {
	aid := mustAdapterID(t, "shell")
	cap := mustCapability(t, aid, "exec", "v1")
	stub := testdoubles.NewStubAdapter(aid, cap)
	stub.Program(cap.Canonical(), testdoubles.StubProgram{Raw: fakeRaw{val: "ok"}})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := stub.Execute(ctx, cap, mustPayload(t))
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Execute with cancelled ctx: expected context.Canceled, got %v", err)
	}
}

// ─── InMemoryReceiptRepository tests ──────────────────────────────────────────

func TestInMemoryReceiptRepository_InterfaceSatisfaction(t *testing.T) {
	var _ outbound.ReceiptRepository = testdoubles.NewInMemoryReceiptRepository(nil)
}

func TestInMemoryReceiptRepository_Save_FreshKey_PersistedAtSet(t *testing.T) {
	persistTime := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	clk := &shared.FakeClock{T: persistTime}
	repo := testdoubles.NewInMemoryReceiptRepository(clk)

	r := mustReceipt(t, validULID)
	saved, err := repo.Save(context.Background(), r)
	if err != nil {
		t.Fatalf("Save: unexpected error: %v", err)
	}

	pt, ok := saved.PersistedAt()
	if !ok {
		t.Fatal("saved receipt: PersistedAt must be set")
	}
	if !pt.Equal(persistTime) {
		t.Errorf("PersistedAt = %v, want %v", pt, persistTime)
	}
}

func TestInMemoryReceiptRepository_Save_DuplicateReturnsErrReceiptAlreadyExists(t *testing.T) {
	repo := testdoubles.NewInMemoryReceiptRepository(nil)
	r := mustReceipt(t, validULID)

	if _, err := repo.Save(context.Background(), r); err != nil {
		t.Fatalf("first Save: %v", err)
	}

	_, err := repo.Save(context.Background(), r)
	if err == nil {
		t.Fatal("second Save: expected ErrReceiptAlreadyExists, got nil")
	}
	if !errors.Is(err, outbound.ErrReceiptAlreadyExists) {
		t.Errorf("second Save: expected ErrReceiptAlreadyExists, got %v", err)
	}
}

func TestInMemoryReceiptRepository_FindByID_UnknownID_ReturnsErrReceiptNotFound(t *testing.T) {
	repo := testdoubles.NewInMemoryReceiptRepository(nil)
	id := mustReceiptID(t, validULID)

	_, err := repo.FindByID(context.Background(), id)
	if err == nil {
		t.Fatal("FindByID: expected ErrReceiptNotFound, got nil")
	}
	if !errors.Is(err, outbound.ErrReceiptNotFound) {
		t.Errorf("FindByID: expected ErrReceiptNotFound, got %v", err)
	}
}

func TestInMemoryReceiptRepository_FindByID_PersistedKey_ReturnsReceipt(t *testing.T) {
	repo := testdoubles.NewInMemoryReceiptRepository(nil)
	r := mustReceipt(t, validULID)

	saved, err := repo.Save(context.Background(), r)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	found, err := repo.FindByID(context.Background(), saved.ReceiptID())
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}

	// The found receipt must have PersistedAt set and equal to what Save returned.
	ptSaved, _ := saved.PersistedAt()
	ptFound, okFound := found.PersistedAt()
	if !okFound {
		t.Fatal("found receipt: PersistedAt must be set")
	}
	if !ptSaved.Equal(ptFound) {
		t.Errorf("PersistedAt mismatch: saved=%v found=%v", ptSaved, ptFound)
	}
	if found.ReceiptID().String() != r.ReceiptID().String() {
		t.Errorf("ReceiptID mismatch: got %q, want %q", found.ReceiptID(), r.ReceiptID())
	}
}

func TestInMemoryReceiptRepository_ConcurrentSave_AllSucceed(t *testing.T) {
	const n = 100
	repo := testdoubles.NewInMemoryReceiptRepository(nil)

	var wg sync.WaitGroup
	wg.Add(n)
	errs := make([]error, n)

	for i := range n {
		i := i
		go func() {
			defer wg.Done()
			ulid := makeULIDN(i)
			r := mustReceipt(t, ulid)
			_, errs[i] = repo.Save(context.Background(), r)
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("Save[%d]: unexpected error: %v", i, err)
		}
	}
	if repo.Count() != n {
		t.Errorf("Count = %d, want %d", repo.Count(), n)
	}
}

func TestInMemoryReceiptRepository_Count_MatchesSuccessfulSaves(t *testing.T) {
	repo := testdoubles.NewInMemoryReceiptRepository(nil)

	for i := range 5 {
		r := mustReceipt(t, makeULIDN(i))
		if _, err := repo.Save(context.Background(), r); err != nil {
			t.Fatalf("Save[%d]: %v", i, err)
		}
	}
	if repo.Count() != 5 {
		t.Errorf("Count = %d, want 5", repo.Count())
	}
}

func TestInMemoryReceiptRepository_CancelledCtx_Save(t *testing.T) {
	repo := testdoubles.NewInMemoryReceiptRepository(nil)
	r := mustReceipt(t, validULID)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := repo.Save(ctx, r)
	if err == nil {
		t.Fatal("Save with cancelled ctx: expected error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Save with cancelled ctx: expected context.Canceled, got %v", err)
	}
}

func TestInMemoryReceiptRepository_CancelledCtx_FindByID(t *testing.T) {
	repo := testdoubles.NewInMemoryReceiptRepository(nil)
	id := mustReceiptID(t, validULID)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := repo.FindByID(ctx, id)
	if err == nil {
		t.Fatal("FindByID with cancelled ctx: expected error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("FindByID with cancelled ctx: expected context.Canceled, got %v", err)
	}
}

// ─── InMemoryIdempotencyStore tests ───────────────────────────────────────────

func TestInMemoryIdempotencyStore_InterfaceSatisfaction(t *testing.T) {
	var _ outbound.IdempotencyStore = testdoubles.NewInMemoryIdempotencyStore(nil)
}

func TestInMemoryIdempotencyStore_Lookup_UnknownKey_ReturnsFalse(t *testing.T) {
	store := testdoubles.NewInMemoryIdempotencyStore(nil)
	key := mustIdempotencyKey(t, validULID)

	rid, found, err := store.Lookup(context.Background(), key)
	if err != nil {
		t.Fatalf("Lookup: unexpected error: %v", err)
	}
	if found {
		t.Error("Lookup: expected found=false for unknown key")
	}
	if rid.String() != "" {
		t.Errorf("Lookup: expected zero ReceiptID, got %q", rid)
	}
}

func TestInMemoryIdempotencyStore_RecordThenLookup_WithinWindow(t *testing.T) {
	baseTime := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	clk := &shared.FakeClock{T: baseTime}
	store := testdoubles.NewInMemoryIdempotencyStore(clk)

	key := mustIdempotencyKey(t, validULID)
	rid := mustReceiptID(t, "01HZXK5JC6QK7XV0YQXA0QJ0YA")

	if err := store.Record(context.Background(), key, rid, time.Hour); err != nil {
		t.Fatalf("Record: %v", err)
	}

	// Advance by less than the window.
	clk.Advance(30 * time.Minute)

	gotRID, found, err := store.Lookup(context.Background(), key)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if !found {
		t.Fatal("Lookup: expected found=true within window")
	}
	if gotRID != rid {
		t.Errorf("Lookup: got ReceiptID %q, want %q", gotRID, rid)
	}
}

func TestInMemoryIdempotencyStore_Lookup_AfterWindowExpiry_ReturnsFalse(t *testing.T) {
	baseTime := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	clk := &shared.FakeClock{T: baseTime}
	store := testdoubles.NewInMemoryIdempotencyStore(clk)

	key := mustIdempotencyKey(t, validULID)
	rid := mustReceiptID(t, "01HZXK5JC6QK7XV0YQXA0QJ0YA")

	if err := store.Record(context.Background(), key, rid, time.Hour); err != nil {
		t.Fatalf("Record: %v", err)
	}

	// Advance past the window.
	clk.Advance(2 * time.Hour)

	_, found, err := store.Lookup(context.Background(), key)
	if err != nil {
		t.Fatalf("Lookup after expiry: %v", err)
	}
	if found {
		t.Error("Lookup after expiry: expected found=false")
	}
}

func TestInMemoryIdempotencyStore_Record_SameKeySameReceipt_Idempotent(t *testing.T) {
	store := testdoubles.NewInMemoryIdempotencyStore(nil)
	key := mustIdempotencyKey(t, validULID)
	rid := mustReceiptID(t, "01HZXK5JC6QK7XV0YQXA0QJ0YA")

	if err := store.Record(context.Background(), key, rid, time.Hour); err != nil {
		t.Fatalf("first Record: %v", err)
	}
	if err := store.Record(context.Background(), key, rid, time.Hour); err != nil {
		t.Fatalf("second Record (idempotent): %v", err)
	}
}

func TestInMemoryIdempotencyStore_Record_SameKeyDifferentReceipt_ReturnsConflict(t *testing.T) {
	store := testdoubles.NewInMemoryIdempotencyStore(nil)
	key := mustIdempotencyKey(t, validULID)
	rid1 := mustReceiptID(t, "01HZXK5JC6QK7XV0YQXA0QJ0YA")
	rid2 := mustReceiptID(t, "01HZXK5JC6QK7XV0YQXA0QJ0YB")

	if err := store.Record(context.Background(), key, rid1, time.Hour); err != nil {
		t.Fatalf("first Record: %v", err)
	}

	err := store.Record(context.Background(), key, rid2, time.Hour)
	if err == nil {
		t.Fatal("second Record with different receipt: expected ErrIdempotencyKeyConflict, got nil")
	}
	if !errors.Is(err, outbound.ErrIdempotencyKeyConflict) {
		t.Errorf("second Record: expected ErrIdempotencyKeyConflict, got %v", err)
	}
}

func TestInMemoryIdempotencyStore_Record_ZeroWindow_ReturnsError(t *testing.T) {
	store := testdoubles.NewInMemoryIdempotencyStore(nil)
	key := mustIdempotencyKey(t, validULID)
	rid := mustReceiptID(t, "01HZXK5JC6QK7XV0YQXA0QJ0YA")

	err := store.Record(context.Background(), key, rid, 0)
	if err == nil {
		t.Fatal("Record with zero window: expected error, got nil")
	}
}

func TestInMemoryIdempotencyStore_Record_NegativeWindow_ReturnsError(t *testing.T) {
	store := testdoubles.NewInMemoryIdempotencyStore(nil)
	key := mustIdempotencyKey(t, validULID)
	rid := mustReceiptID(t, "01HZXK5JC6QK7XV0YQXA0QJ0YA")

	err := store.Record(context.Background(), key, rid, -time.Second)
	if err == nil {
		t.Fatal("Record with negative window: expected error, got nil")
	}
}

func TestInMemoryIdempotencyStore_ConcurrentRecord_UniqueKeys_AllSucceed(t *testing.T) {
	const n = 100
	store := testdoubles.NewInMemoryIdempotencyStore(nil)

	var wg sync.WaitGroup
	wg.Add(n)
	errs := make([]error, n)

	for i := range n {
		i := i
		go func() {
			defer wg.Done()
			keyStr := makeULIDN(i)
			key := mustIdempotencyKey(t, keyStr)
			ridStr := makeULIDN(i + 1000)
			rid := mustReceiptID(t, ridStr)
			errs[i] = store.Record(context.Background(), key, rid, time.Hour)
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("Record[%d]: unexpected error: %v", i, err)
		}
	}
}

func TestInMemoryIdempotencyStore_CancelledCtx_Lookup(t *testing.T) {
	store := testdoubles.NewInMemoryIdempotencyStore(nil)
	key := mustIdempotencyKey(t, validULID)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := store.Lookup(ctx, key)
	if err == nil {
		t.Fatal("Lookup with cancelled ctx: expected error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Lookup with cancelled ctx: expected context.Canceled, got %v", err)
	}
}

func TestInMemoryIdempotencyStore_CancelledCtx_Record(t *testing.T) {
	store := testdoubles.NewInMemoryIdempotencyStore(nil)
	key := mustIdempotencyKey(t, validULID)
	rid := mustReceiptID(t, "01HZXK5JC6QK7XV0YQXA0QJ0YA")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := store.Record(ctx, key, rid, time.Hour)
	if err == nil {
		t.Fatal("Record with cancelled ctx: expected error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Record with cancelled ctx: expected context.Canceled, got %v", err)
	}
}

// ─── ensure unused imports are used ───────────────────────────────────────────

var _ = fmt.Sprintf
