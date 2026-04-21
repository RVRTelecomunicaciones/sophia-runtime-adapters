package sdk_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/sophia-ecosystem/runtime-adapters/internal/adapters/inbound/sdk"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/entities"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
	"github.com/sophia-ecosystem/runtime-adapters/internal/ports/inbound"
)

// ─── stubs ───────────────────────────────────────────────────────────────────

// stubRuntime records the last Execute call and returns a pre-set receipt.
type stubRuntime struct {
	called  bool
	lastReq entities.ExecutionRequest
	receipt entities.ExecutionReceipt
	err     error
}

func (s *stubRuntime) Execute(_ context.Context, req entities.ExecutionRequest) (entities.ExecutionReceipt, error) {
	s.called = true
	s.lastReq = req
	return s.receipt, s.err
}

// stubQuery records ListCapabilities and GetReceipt calls.
type stubQuery struct {
	listCalled  bool
	lastFilter  inbound.CapabilityFilter
	listResp    inbound.ListCapabilitiesResponse
	listErr     error
	getCalled   bool
	lastGetID   shared.ReceiptID
	lastGetOpts inbound.GetReceiptOptions
	getReceipt  entities.ExecutionReceipt
	getErr      error
}

func (s *stubQuery) ListCapabilities(_ context.Context, f inbound.CapabilityFilter) (inbound.ListCapabilitiesResponse, error) {
	s.listCalled = true
	s.lastFilter = f
	return s.listResp, s.listErr
}

func (s *stubQuery) GetReceipt(_ context.Context, id shared.ReceiptID, opts inbound.GetReceiptOptions) (entities.ExecutionReceipt, error) {
	s.getCalled = true
	s.lastGetID = id
	s.lastGetOpts = opts
	return s.getReceipt, s.getErr
}

// ─── helpers ─────────────────────────────────────────────────────────────────

const (
	validULID1 = "01HZXK5JC6QK7XV0YQXA0QJ0YZ"
	validULID2 = "01HZXK5JC6QK7XV0YQXA0QJ0YA"
)

func mustBuildReceipt(t *testing.T) entities.ExecutionReceipt {
	t.Helper()

	cid, _ := shared.NewCorrelationID(validULID1)
	aid, _ := valueobjects.NewAdapterID("shell")
	pl, _ := valueobjects.NewPayload(valueobjects.ContentTypeJSON, json.RawMessage(`{"cmd":"echo"}`), 0)
	tb, _ := valueobjects.NewTimeoutBudget(5000, 0)

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := &shared.FakeClock{T: base}

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

	cap, _ := valueobjects.NewCapability(aid, "exec", "v1", false, 5*time.Second)
	handleGen := &entities.FakeIDGen{IDs: []string{validULID2}}
	handleClk := &shared.FakeClock{T: base.Add(time.Millisecond)}
	handle, err := entities.NewExecutionHandle(handleGen, cid, cap, handleClk)
	if err != nil {
		t.Fatalf("NewExecutionHandle: %v", err)
	}

	result, err := entities.NewExecutionResult(
		valueobjects.StatusSuccess, valueobjects.HintNonRetryable,
		"", "", nil, nil, nil, nil, nil,
		0, 0, 100*time.Millisecond, base.Add(101*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("NewExecutionResult: %v", err)
	}

	prov, _ := entities.NewProvenance(entities.ProvSDK, "0.1.0", "test-host", "runtime-0.1.0", "")

	timingsClk := &shared.FakeClock{T: base}
	bld := entities.NewTimingsBuilder(timingsClk).MarkSubmitted()
	timingsClk.Advance(time.Millisecond)
	bld.MarkStarted()
	timingsClk.Advance(100 * time.Millisecond)
	bld.MarkCompleted()
	timings, err := bld.Build()
	if err != nil {
		t.Fatalf("Build timings: %v", err)
	}

	receiptGen := &entities.FakeIDGen{IDs: []string{validULID1}}
	receiptClk := &shared.FakeClock{T: base.Add(102 * time.Millisecond)}
	receipt, err := entities.NewExecutionReceipt(receiptGen, req, handle, result, prov, timings, receiptClk)
	if err != nil {
		t.Fatalf("NewExecutionReceipt: %v", err)
	}
	return receipt
}

// ─── T1: NewClient rejects nil svc ───────────────────────────────────────────

func TestNewClient_RejectsNilService(t *testing.T) {
	q := &stubQuery{}
	_, err := sdk.NewClient(nil, q, sdk.ClientConfig{})
	if err == nil {
		t.Fatal("expected error for nil RuntimeService, got nil")
	}
}

// ─── T2: NewClient rejects nil query ─────────────────────────────────────────

func TestNewClient_RejectsNilQuery(t *testing.T) {
	svc := &stubRuntime{}
	_, err := sdk.NewClient(svc, nil, sdk.ClientConfig{})
	if err == nil {
		t.Fatal("expected error for nil QueryService, got nil")
	}
}

// ─── T3: NewClient defaults clock to RealClock and maxTB to 30 min ───────────

func TestNewClient_DefaultsClockAndMaxTB(t *testing.T) {
	svc := &stubRuntime{}
	q := &stubQuery{}

	// Zero ClientConfig — defaults should kick in; client must be non-nil.
	client, err := sdk.NewClient(svc, q, sdk.ClientConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}

	// Verify the 30-min default indirectly: a TimeoutBudgetMs of 31 min (1860000 ms)
	// should be rejected when the default maxTB is 30 min.
	thirtyOneMin := int64(31 * 60 * 1000)
	_, execErr := client.Execute(context.Background(), sdk.ExecuteInput{
		CorrelationID:     validULID1,
		AdapterID:         "shell",
		CapabilityName:    "exec",
		CapabilityVersion: "v1",
		Payload:           json.RawMessage(`{"cmd":"echo"}`),
		TimeoutBudgetMs:   thirtyOneMin,
	})
	if execErr == nil {
		t.Error("expected error for timeout > 30 min default, got nil")
	}
}

// ─── T4: Execute — validation errors surface as Go errors ────────────────────

func TestExecute_ValidationErrors(t *testing.T) {
	tests := []struct {
		name    string
		input   sdk.ExecuteInput
		wantErr string
	}{
		{
			name: "invalid correlation_id",
			input: sdk.ExecuteInput{
				CorrelationID: "not-a-ulid", AdapterID: "shell",
				CapabilityName: "exec", CapabilityVersion: "v1",
				Payload: json.RawMessage(`{"cmd":"echo"}`), TimeoutBudgetMs: 5000,
			},
			wantErr: "correlation_id",
		},
		{
			name: "invalid adapter_id",
			input: sdk.ExecuteInput{
				CorrelationID: validULID1, AdapterID: "INVALID-CAPS",
				CapabilityName: "exec", CapabilityVersion: "v1",
				Payload: json.RawMessage(`{"cmd":"echo"}`), TimeoutBudgetMs: 5000,
			},
			wantErr: "adapter_id",
		},
		{
			name: "invalid capability_name",
			input: sdk.ExecuteInput{
				CorrelationID: validULID1, AdapterID: "shell",
				CapabilityName: "INVALID NAME!", CapabilityVersion: "v1",
				Payload: json.RawMessage(`{"cmd":"echo"}`), TimeoutBudgetMs: 5000,
			},
			wantErr: "build request",
		},
		{
			name: "empty payload",
			input: sdk.ExecuteInput{
				CorrelationID: validULID1, AdapterID: "shell",
				CapabilityName: "exec", CapabilityVersion: "v1",
				Payload: json.RawMessage(``), TimeoutBudgetMs: 5000,
			},
			wantErr: "payload",
		},
		{
			name: "zero timeout_budget",
			input: sdk.ExecuteInput{
				CorrelationID: validULID1, AdapterID: "shell",
				CapabilityName: "exec", CapabilityVersion: "v1",
				Payload: json.RawMessage(`{"cmd":"echo"}`), TimeoutBudgetMs: 0,
			},
			wantErr: "timeout_budget",
		},
		{
			name: "invalid idempotency_key",
			input: sdk.ExecuteInput{
				CorrelationID: validULID1, AdapterID: "shell",
				CapabilityName: "exec", CapabilityVersion: "v1",
				Payload: json.RawMessage(`{"cmd":"echo"}`), TimeoutBudgetMs: 5000,
				IdempotencyKey: "not-valid-ulid-or-uuid",
			},
			wantErr: "idempotency_key",
		},
	}

	svc := &stubRuntime{}
	q := &stubQuery{}
	clk := &shared.FakeClock{T: time.Unix(1714900000, 0).UTC()}
	client, err := sdk.NewClient(svc, q, sdk.ClientConfig{Clock: clk})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc.called = false
			_, execErr := client.Execute(context.Background(), tt.input)
			if execErr == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !containsStr(execErr.Error(), tt.wantErr) {
				t.Errorf("error %q does not contain %q", execErr.Error(), tt.wantErr)
			}
			if svc.called {
				t.Error("svc.Execute should NOT have been called on validation error")
			}
		})
	}
}

// ─── T5: Execute happy path dispatches to svc.Execute ────────────────────────

func TestExecute_HappyPath_DispatchesToService(t *testing.T) {
	wantReceipt := mustBuildReceipt(t)
	svc := &stubRuntime{receipt: wantReceipt}
	q := &stubQuery{}
	clk := &shared.FakeClock{T: time.Unix(1714900000, 0).UTC()}

	client, err := sdk.NewClient(svc, q, sdk.ClientConfig{Clock: clk})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	got, execErr := client.Execute(context.Background(), sdk.ExecuteInput{
		CorrelationID:     validULID1,
		AdapterID:         "shell",
		CapabilityName:    "exec",
		CapabilityVersion: "v1",
		Payload:           json.RawMessage(`{"cmd":"echo"}`),
		TimeoutBudgetMs:   5000,
	})
	if execErr != nil {
		t.Fatalf("Execute: %v", execErr)
	}
	if !svc.called {
		t.Fatal("svc.Execute was not called")
	}
	// The stub echoes the pre-built receipt; verify it was returned unchanged.
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(wantReceipt)
	if string(gotJSON) != string(wantJSON) {
		t.Errorf("returned receipt differs from stub:\ngot:  %s\nwant: %s", gotJSON, wantJSON)
	}
}

// ─── T6: ListCapabilities with empty filter → nil AdapterID ──────────────────

func TestListCapabilities_EmptyFilter_NilAdapterID(t *testing.T) {
	svc := &stubRuntime{}
	q := &stubQuery{listResp: inbound.ListCapabilitiesResponse{
		Capabilities:   nil,
		RuntimeVersion: "v0.1.0",
		SchemaVersion:  "v1",
	}}
	client, err := sdk.NewClient(svc, q, sdk.ClientConfig{})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, listErr := client.ListCapabilities(context.Background(), sdk.ListCapabilitiesFilter{})
	if listErr != nil {
		t.Fatalf("ListCapabilities: %v", listErr)
	}
	if !q.listCalled {
		t.Fatal("query.ListCapabilities was not called")
	}
	if q.lastFilter.AdapterID != nil {
		t.Errorf("expected nil AdapterID in filter, got %v", q.lastFilter.AdapterID)
	}
}

// ─── T7: ListCapabilities with invalid AdapterID → error, svc NOT called ─────

func TestListCapabilities_InvalidAdapterID_ErrorBeforeCall(t *testing.T) {
	svc := &stubRuntime{}
	q := &stubQuery{}
	client, err := sdk.NewClient(svc, q, sdk.ClientConfig{})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, listErr := client.ListCapabilities(context.Background(), sdk.ListCapabilitiesFilter{
		AdapterID: "INVALID CAPS!",
	})
	if listErr == nil {
		t.Fatal("expected error for invalid adapter_id, got nil")
	}
	if q.listCalled {
		t.Error("query.ListCapabilities should NOT have been called on validation error")
	}
}

// ─── T8: GetReceipt with invalid id → error, query NOT called ────────────────

func TestGetReceipt_InvalidID_ErrorBeforeCall(t *testing.T) {
	svc := &stubRuntime{}
	q := &stubQuery{}
	client, err := sdk.NewClient(svc, q, sdk.ClientConfig{})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, getErr := client.GetReceipt(context.Background(), "not-a-valid-ulid", sdk.GetReceiptOptions{})
	if getErr == nil {
		t.Fatal("expected error for invalid receipt_id, got nil")
	}
	if q.getCalled {
		t.Error("query.GetReceipt should NOT have been called on validation error")
	}
}

// ─── T9: GetReceipt passes IncludeStreams option through ─────────────────────

func TestGetReceipt_PassesOptionsThrough(t *testing.T) {
	svc := &stubRuntime{}
	q := &stubQuery{}
	client, err := sdk.NewClient(svc, q, sdk.ClientConfig{})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	tests := []struct {
		name          string
		opts          sdk.GetReceiptOptions
		wantStreams   bool
	}{
		{"IncludeStreams=false", sdk.GetReceiptOptions{IncludeStreams: false}, false},
		{"IncludeStreams=true", sdk.GetReceiptOptions{IncludeStreams: true}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q.getCalled = false
			q.lastGetOpts = inbound.GetReceiptOptions{}

			_, _ = client.GetReceipt(context.Background(), validULID1, tt.opts)

			if !q.getCalled {
				t.Fatal("query.GetReceipt was not called")
			}
			if q.lastGetOpts.IncludeStreams != tt.wantStreams {
				t.Errorf("IncludeStreams = %v, want %v", q.lastGetOpts.IncludeStreams, tt.wantStreams)
			}
			// Verify the receipt_id was forwarded correctly.
			if q.lastGetID.String() != validULID1 {
				t.Errorf("receipt_id = %q, want %q", q.lastGetID.String(), validULID1)
			}
		})
	}
}

// ─── T10: Execute propagates service errors ───────────────────────────────────

func TestExecute_PropagatesServiceError(t *testing.T) {
	wantErr := errors.New("persistence failure")
	svc := &stubRuntime{err: wantErr}
	q := &stubQuery{}
	clk := &shared.FakeClock{T: time.Unix(1714900000, 0).UTC()}

	client, err := sdk.NewClient(svc, q, sdk.ClientConfig{Clock: clk})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, execErr := client.Execute(context.Background(), sdk.ExecuteInput{
		CorrelationID:     validULID1,
		AdapterID:         "shell",
		CapabilityName:    "exec",
		CapabilityVersion: "v1",
		Payload:           json.RawMessage(`{"cmd":"echo"}`),
		TimeoutBudgetMs:   5000,
	})
	if !errors.Is(execErr, wantErr) {
		t.Errorf("expected wrapped %v, got %v", wantErr, execErr)
	}
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
