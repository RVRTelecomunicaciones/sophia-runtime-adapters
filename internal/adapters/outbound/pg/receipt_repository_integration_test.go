//go:build integration

package pg

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/entities"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
	outbound "github.com/sophia-ecosystem/runtime-adapters/internal/ports/outbound"
)

// ─── helpers ──────────────────────────────────────────────────────────────────

func buildReceipt(t *testing.T, gen entities.IDGenerator) entities.ExecutionReceipt {
	t.Helper()
	cid, err := shared.NewCorrelationID("01HZXK5JC6QK7XV0YQXA0QJ0YA")
	if err != nil {
		t.Fatalf("NewCorrelationID: %v", err)
	}
	aid, err := valueobjects.NewAdapterID("shell")
	if err != nil {
		t.Fatalf("NewAdapterID: %v", err)
	}
	cap, err := valueobjects.NewCapability(aid, "exec", "v1", false, 5*time.Second)
	if err != nil {
		t.Fatalf("NewCapability: %v", err)
	}
	clk := &shared.FakeClock{T: time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)}
	h, err := entities.NewExecutionHandle(gen, cid, cap, clk)
	if err != nil {
		t.Fatalf("NewExecutionHandle: %v", err)
	}

	raw := []byte(`{"cmd":"echo hello"}`)
	payload, err := valueobjects.NewPayload(valueobjects.ContentTypeJSON, raw, 0)
	if err != nil {
		t.Fatalf("NewPayload: %v", err)
	}
	tb, err := valueobjects.NewTimeoutBudget(5000, 0)
	if err != nil {
		t.Fatalf("NewTimeoutBudget: %v", err)
	}
	req, err := entities.NewExecutionRequest(entities.ExecutionRequestInput{
		CorrelationID:     cid,
		AdapterID:         aid,
		CapabilityName:    "exec",
		CapabilityVersion: "v1",
		Payload:           payload,
		TimeoutBudget:     tb,
	}, clk)
	if err != nil {
		t.Fatalf("NewExecutionRequest: %v", err)
	}

	res, err := entities.NewExecutionResult(
		valueobjects.StatusSuccess,
		valueobjects.HintNonRetryable,
		"", "",
		nil, nil, nil,
		nil, nil,
		0, 0,
		100*time.Millisecond,
		time.Date(2026, 4, 19, 12, 0, 0, 100000000, time.UTC),
	)
	if err != nil {
		t.Fatalf("NewExecutionResult: %v", err)
	}

	prov, err := entities.NewProvenance(entities.ProvTest, "1.0.0", "host-a", "runtime-1.2.3", "")
	if err != nil {
		t.Fatalf("NewProvenance: %v", err)
	}

	base := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	tc := &shared.FakeClock{T: base}
	bld := entities.NewTimingsBuilder(tc).MarkSubmitted()
	tc.Advance(10 * time.Millisecond)
	bld.MarkStarted()
	tc.Advance(100 * time.Millisecond)
	bld.MarkCompleted()
	tim, err := bld.Build()
	if err != nil {
		t.Fatalf("Build timings: %v", err)
	}

	receiptGen := gen
	r, err := entities.NewExecutionReceipt(receiptGen, req, h, res, prov, tim, clk)
	if err != nil {
		t.Fatalf("NewExecutionReceipt: %v", err)
	}
	return r
}

// newULIDGen creates a new production IDGenerator.
func newULIDGen() entities.IDGenerator {
	return entities.ULIDGen{}
}

// ─── integration tests ────────────────────────────────────────────────────────

func TestReceiptRepositoryPG_SaveAndFindByID(t *testing.T) {
	pool, teardown := startPG(t)
	defer teardown()

	repo, err := NewReceiptRepositoryPG(pool)
	if err != nil {
		t.Fatalf("NewReceiptRepositoryPG: %v", err)
	}

	receipt := buildReceipt(t, newULIDGen())
	ctx := context.Background()

	saved, err := repo.Save(ctx, receipt)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	// PersistedAt must be set after Save.
	if _, ok := saved.PersistedAt(); !ok {
		t.Error("saved receipt should have PersistedAt set")
	}

	found, err := repo.FindByID(ctx, saved.ReceiptID())
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}

	// Key fields must round-trip.
	if found.ReceiptID().String() != saved.ReceiptID().String() {
		t.Errorf("ReceiptID: got %q, want %q", found.ReceiptID(), saved.ReceiptID())
	}
	if found.SchemaVersion() != saved.SchemaVersion() {
		t.Errorf("SchemaVersion: got %q, want %q", found.SchemaVersion(), saved.SchemaVersion())
	}
	if found.Result().Status != saved.Result().Status {
		t.Errorf("Result.Status: got %q, want %q", found.Result().Status, saved.Result().Status)
	}
	if found.Request().CorrelationID().String() != saved.Request().CorrelationID().String() {
		t.Errorf("Request.CorrelationID: got %q, want %q", found.Request().CorrelationID(), saved.Request().CorrelationID())
	}
	if !found.Timings().CompletedAt.Equal(saved.Timings().CompletedAt) {
		t.Errorf("Timings.CompletedAt: got %v, want %v", found.Timings().CompletedAt, saved.Timings().CompletedAt)
	}
}

func TestReceiptRepositoryPG_DuplicateSave_ReturnsErrReceiptAlreadyExists(t *testing.T) {
	pool, teardown := startPG(t)
	defer teardown()

	repo, err := NewReceiptRepositoryPG(pool)
	if err != nil {
		t.Fatalf("NewReceiptRepositoryPG: %v", err)
	}

	receipt := buildReceipt(t, newULIDGen())
	ctx := context.Background()

	if _, err := repo.Save(ctx, receipt); err != nil {
		t.Fatalf("first Save: %v", err)
	}

	_, err = repo.Save(ctx, receipt)
	if !errors.Is(err, outbound.ErrReceiptAlreadyExists) {
		t.Errorf("second Save: want ErrReceiptAlreadyExists, got %v", err)
	}
}

func TestReceiptRepositoryPG_FindByID_Unknown_ReturnsErrReceiptNotFound(t *testing.T) {
	pool, teardown := startPG(t)
	defer teardown()

	repo, err := NewReceiptRepositoryPG(pool)
	if err != nil {
		t.Fatalf("NewReceiptRepositoryPG: %v", err)
	}

	unknownID, err := shared.NewReceiptID("01HZXK5JC6QK7XV0YQXA0QJ0YA")
	if err != nil {
		t.Fatalf("NewReceiptID: %v", err)
	}

	_, err = repo.FindByID(context.Background(), unknownID)
	if !errors.Is(err, outbound.ErrReceiptNotFound) {
		t.Errorf("FindByID unknown: want ErrReceiptNotFound, got %v", err)
	}
}

func TestReceiptRepositoryPG_ConcurrencySmoke(t *testing.T) {
	pool, teardown := startPG(t)
	defer teardown()

	repo, err := NewReceiptRepositoryPG(pool)
	if err != nil {
		t.Fatalf("NewReceiptRepositoryPG: %v", err)
	}

	const n = 10
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			r := buildReceipt(t, newULIDGen())
			_, errs[idx] = repo.Save(context.Background(), r)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: Save failed: %v", i, err)
		}
	}
}
