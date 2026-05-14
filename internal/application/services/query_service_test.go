package services_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sophia-ecosystem/runtime-adapters/internal/application/services"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/entities"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
	"github.com/sophia-ecosystem/runtime-adapters/internal/ports/inbound"
	"github.com/sophia-ecosystem/runtime-adapters/internal/ports/outbound"
	"github.com/sophia-ecosystem/runtime-adapters/internal/ports/outbound/testdoubles"
)

// ---------------------------------------------------------------------------
// Test ULIDs — deterministic IDs for query_service tests.
// ---------------------------------------------------------------------------

const (
	qulid01 = "01HZXK5JC6QK7XV0YQXA0QJ0YA"
	qulid02 = "01HZXK5JC6QK7XV0YQXA0QJ0YB"
	qulid03 = "01HZXK5JC6QK7XV0YQXA0QJ0YC"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newPhase1Registry builds a CapabilityRegistry from the Phase 1 catalog.
func newPhase1Registry(t *testing.T) *valueobjects.CapabilityRegistry {
	t.Helper()
	caps := valueobjects.MustNewPhase1Capabilities()
	reg, err := valueobjects.NewCapabilityRegistry(caps...)
	if err != nil {
		t.Fatalf("NewCapabilityRegistry: %v", err)
	}
	return reg
}

// newQueryService builds a QueryServiceImpl with the Phase 1 registry.
func newQueryService(t *testing.T, repo *testdoubles.InMemoryReceiptRepository) *services.QueryServiceImpl {
	t.Helper()
	svc, err := services.NewQueryService(services.QueryServiceConfig{
		Registry:       newPhase1Registry(t),
		Receipts:       repo,
		RuntimeVersion: "0.1.0-test",
	})
	if err != nil {
		t.Fatalf("NewQueryService: %v", err)
	}
	return svc
}

// buildReceiptWithStreams constructs a minimal valid ExecutionReceipt with
// both stdout_ref and stderr_ref populated, saves it to repo, and returns
// the saved receipt (with PersistedAt stamped).
func buildReceiptWithStreams(t *testing.T, repo *testdoubles.InMemoryReceiptRepository, clk shared.Clock, gen entities.IDGenerator) entities.ExecutionReceipt {
	t.Helper()

	cid, err := shared.NewCorrelationID(qulid01)
	if err != nil {
		t.Fatalf("NewCorrelationID: %v", err)
	}
	aid, err := valueobjects.NewAdapterID("shell")
	if err != nil {
		t.Fatalf("NewAdapterID: %v", err)
	}
	capGot, err := valueobjects.NewCapability(aid, "exec", "v1", false, time.Second)
	if err != nil {
		t.Fatalf("NewCapability: %v", err)
	}
	pl, err := valueobjects.NewPayload("application/json", []byte(`{"command":"echo"}`), 0)
	if err != nil {
		t.Fatalf("NewPayload: %v", err)
	}
	tb, err := valueobjects.NewTimeoutBudget(1000, 0)
	if err != nil {
		t.Fatalf("NewTimeoutBudget: %v", err)
	}
	req, err := entities.NewExecutionRequest(entities.ExecutionRequestInput{
		CorrelationID:     cid,
		AdapterID:         aid,
		CapabilityName:    "exec",
		CapabilityVersion: "v1",
		Payload:           pl,
		TimeoutBudget:     tb,
	}, clk)
	if err != nil {
		t.Fatalf("NewExecutionRequest: %v", err)
	}
	handle, err := entities.NewExecutionHandle(gen, cid, capGot, clk)
	if err != nil {
		t.Fatalf("NewExecutionHandle: %v", err)
	}

	stdout, err := entities.NewStreamRef([]byte("hello"), 5, 1024)
	if err != nil {
		t.Fatalf("NewStreamRef stdout: %v", err)
	}
	stderr, err := entities.NewStreamRef([]byte(""), 0, 1024)
	if err != nil {
		t.Fatalf("NewStreamRef stderr: %v", err)
	}

	now := clk.Now()
	result, err := entities.NewExecutionResult(
		valueobjects.StatusSuccess, valueobjects.HintNonRetryable,
		"", "",
		&stdout, &stderr,
		nil, nil, nil, 5, 0, 0, now,
	)
	if err != nil {
		t.Fatalf("NewExecutionResult: %v", err)
	}

	prov, err := entities.NewProvenance(entities.ProvTest, "0.1.0", "test-host", "0.1.0", "")
	if err != nil {
		t.Fatalf("NewProvenance: %v", err)
	}
	timings := entities.Timings{
		SubmittedAt: req.SubmittedAt(),
		StartedAt:   now,
		CompletedAt: now,
	}
	rcpt, err := entities.NewExecutionReceipt(gen, req, handle, result, prov, timings, clk)
	if err != nil {
		t.Fatalf("NewExecutionReceipt: %v", err)
	}

	saved, err := repo.Save(context.Background(), rcpt)
	if err != nil {
		t.Fatalf("repo.Save: %v", err)
	}
	return saved
}

// ---------------------------------------------------------------------------
// NewQueryService — constructor validation
// ---------------------------------------------------------------------------

func TestNewQueryService_HappyPath(t *testing.T) {
	repo := testdoubles.NewInMemoryReceiptRepository(nil)
	svc, err := services.NewQueryService(services.QueryServiceConfig{
		Registry:       newPhase1Registry(t),
		Receipts:       repo,
		RuntimeVersion: "1.0.0",
	})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
}

func TestNewQueryService_RejectsNilRegistry(t *testing.T) {
	repo := testdoubles.NewInMemoryReceiptRepository(nil)
	_, err := services.NewQueryService(services.QueryServiceConfig{
		Registry:       nil,
		Receipts:       repo,
		RuntimeVersion: "1.0.0",
	})
	if err == nil {
		t.Fatal("expected error for nil Registry, got nil")
	}
}

func TestNewQueryService_RejectsNilReceipts(t *testing.T) {
	_, err := services.NewQueryService(services.QueryServiceConfig{
		Registry:       newPhase1Registry(t),
		Receipts:       nil,
		RuntimeVersion: "1.0.0",
	})
	if err == nil {
		t.Fatal("expected error for nil Receipts, got nil")
	}
}

func TestNewQueryService_RejectsEmptyRuntimeVersion(t *testing.T) {
	repo := testdoubles.NewInMemoryReceiptRepository(nil)
	_, err := services.NewQueryService(services.QueryServiceConfig{
		Registry:       newPhase1Registry(t),
		Receipts:       repo,
		RuntimeVersion: "",
	})
	if err == nil {
		t.Fatal("expected error for empty RuntimeVersion, got nil")
	}
}

// ---------------------------------------------------------------------------
// ListCapabilities
// ---------------------------------------------------------------------------

func TestListCapabilities_NoFilter_ReturnsAll9(t *testing.T) {
	repo := testdoubles.NewInMemoryReceiptRepository(nil)
	svc := newQueryService(t, repo)

	resp, err := svc.ListCapabilities(context.Background(), inbound.CapabilityFilter{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Capabilities) != 9 {
		t.Errorf("expected 9 capabilities, got %d", len(resp.Capabilities))
	}
	if resp.RuntimeVersion != "0.1.0-test" {
		t.Errorf("runtime_version = %q, want %q", resp.RuntimeVersion, "0.1.0-test")
	}
	if resp.SchemaVersion != "v1" {
		t.Errorf("schema_version = %q, want %q", resp.SchemaVersion, "v1")
	}
}

func TestListCapabilities_FilterGit_Returns5(t *testing.T) {
	repo := testdoubles.NewInMemoryReceiptRepository(nil)
	svc := newQueryService(t, repo)

	aid, err := valueobjects.NewAdapterID("git")
	if err != nil {
		t.Fatalf("NewAdapterID: %v", err)
	}
	resp, err := svc.ListCapabilities(context.Background(), inbound.CapabilityFilter{AdapterID: &aid})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Capabilities) != 5 {
		t.Errorf("expected 5 git capabilities, got %d", len(resp.Capabilities))
	}
	for _, c := range resp.Capabilities {
		if c.AdapterID().String() != "git" {
			t.Errorf("expected all capabilities to belong to 'git', got %q", c.AdapterID().String())
		}
	}
}

func TestListCapabilities_FilterFilesystem_Returns2(t *testing.T) {
	repo := testdoubles.NewInMemoryReceiptRepository(nil)
	svc := newQueryService(t, repo)

	aid, err := valueobjects.NewAdapterID("filesystem")
	if err != nil {
		t.Fatalf("NewAdapterID: %v", err)
	}
	resp, err := svc.ListCapabilities(context.Background(), inbound.CapabilityFilter{AdapterID: &aid})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Capabilities) != 2 {
		t.Errorf("expected 2 filesystem capabilities, got %d", len(resp.Capabilities))
	}
}

func TestListCapabilities_FilterUnknownAdapter_ReturnsEmptyNonNil(t *testing.T) {
	repo := testdoubles.NewInMemoryReceiptRepository(nil)
	svc := newQueryService(t, repo)

	aid, err := valueobjects.NewAdapterID("unknown")
	if err != nil {
		t.Fatalf("NewAdapterID: %v", err)
	}
	resp, err := svc.ListCapabilities(context.Background(), inbound.CapabilityFilter{AdapterID: &aid})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Capabilities == nil {
		t.Error("expected non-nil slice for unknown adapter, got nil")
	}
	if len(resp.Capabilities) != 0 {
		t.Errorf("expected 0 capabilities for unknown adapter, got %d", len(resp.Capabilities))
	}
}

func TestListCapabilities_HonoursCancelledCtx(t *testing.T) {
	repo := testdoubles.NewInMemoryReceiptRepository(nil)
	svc := newQueryService(t, repo)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := svc.ListCapabilities(ctx, inbound.CapabilityFilter{})
	if err == nil {
		t.Fatal("expected error from cancelled ctx, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// GetReceipt
// ---------------------------------------------------------------------------

func TestGetReceipt_IncludeStreamsTrue_ReturnsStreams(t *testing.T) {
	clk := &shared.FakeClock{T: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	repo := testdoubles.NewInMemoryReceiptRepository(clk)
	gen := &entities.FakeIDGen{IDs: []string{qulid02, qulid03}}

	saved := buildReceiptWithStreams(t, repo, clk, gen)

	svc := newQueryService(t, repo)
	got, err := svc.GetReceipt(context.Background(), saved.ReceiptID(), inbound.GetReceiptOptions{IncludeStreams: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Result().StdoutRef == nil {
		t.Error("expected stdout_ref to be present with IncludeStreams=true, got nil")
	}
	if got.Result().StderrRef == nil {
		t.Error("expected stderr_ref to be present with IncludeStreams=true, got nil")
	}
}

func TestGetReceipt_IncludeStreamsFalse_StripsStreams(t *testing.T) {
	clk := &shared.FakeClock{T: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	repo := testdoubles.NewInMemoryReceiptRepository(clk)
	gen := &entities.FakeIDGen{IDs: []string{qulid02, qulid03}}

	saved := buildReceiptWithStreams(t, repo, clk, gen)

	svc := newQueryService(t, repo)

	// Fetch with streams stripped.
	stripped, err := svc.GetReceipt(context.Background(), saved.ReceiptID(), inbound.GetReceiptOptions{IncludeStreams: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stripped.Result().StdoutRef != nil {
		t.Error("expected stdout_ref to be nil with IncludeStreams=false")
	}
	if stripped.Result().StderrRef != nil {
		t.Error("expected stderr_ref to be nil with IncludeStreams=false")
	}

	// Verify original in repo is unchanged — fetch again with streams.
	original, err := svc.GetReceipt(context.Background(), saved.ReceiptID(), inbound.GetReceiptOptions{IncludeStreams: true})
	if err != nil {
		t.Fatalf("unexpected error on second fetch: %v", err)
	}
	if original.Result().StdoutRef == nil {
		t.Error("original receipt in repo must retain stdout_ref after StripStreams")
	}
	if original.Result().StderrRef == nil {
		t.Error("original receipt in repo must retain stderr_ref after StripStreams")
	}
}

func TestGetReceipt_UnknownID_ReturnsErrReceiptNotFound(t *testing.T) {
	repo := testdoubles.NewInMemoryReceiptRepository(nil)
	svc := newQueryService(t, repo)

	rid, err := shared.NewReceiptID(qulid01)
	if err != nil {
		t.Fatalf("NewReceiptID: %v", err)
	}

	_, err = svc.GetReceipt(context.Background(), rid, inbound.GetReceiptOptions{})
	if err == nil {
		t.Fatal("expected ErrReceiptNotFound, got nil")
	}
	if !errors.Is(err, outbound.ErrReceiptNotFound) {
		t.Errorf("expected errors.Is(err, ErrReceiptNotFound), got: %v", err)
	}
}

func TestGetReceipt_HonoursCancelledCtx(t *testing.T) {
	repo := testdoubles.NewInMemoryReceiptRepository(nil)
	svc := newQueryService(t, repo)

	rid, err := shared.NewReceiptID(qulid01)
	if err != nil {
		t.Fatalf("NewReceiptID: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = svc.GetReceipt(ctx, rid, inbound.GetReceiptOptions{})
	if err == nil {
		t.Fatal("expected error from cancelled ctx, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Interface satisfaction — compile-time assertion.
// ---------------------------------------------------------------------------

var _ inbound.QueryService = (*services.QueryServiceImpl)(nil)
