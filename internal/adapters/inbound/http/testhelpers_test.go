package http

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/entities"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
)

// Two distinct ULIDs used across handler tests.
const (
	testULID1 = "01HZXK5JC6QK7XV0YQXA0QJ0YZ"
	testULID2 = "01HZXK5JC6QK7XV0YQXA0QJ0YA"
)

// mustTestReceipt builds a minimal but valid ExecutionReceipt that can be
// JSON-marshaled without error. Used by handler stubs that need to return a
// non-zero receipt.
func mustTestReceipt(t *testing.T) entities.ExecutionReceipt {
	t.Helper()

	cid, err := shared.NewCorrelationID(testULID1)
	if err != nil {
		t.Fatalf("NewCorrelationID: %v", err)
	}

	aid, err := valueobjects.NewAdapterID("git")
	if err != nil {
		t.Fatalf("NewAdapterID: %v", err)
	}

	pl, err := valueobjects.NewPayload(valueobjects.ContentTypeJSON, json.RawMessage(`{"repo":"/tmp/r"}`), 0)
	if err != nil {
		t.Fatalf("NewPayload: %v", err)
	}

	tb, err := valueobjects.NewTimeoutBudget(5000, 0)
	if err != nil {
		t.Fatalf("NewTimeoutBudget: %v", err)
	}

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	reqClk := &shared.FakeClock{T: base}

	req, err := entities.NewExecutionRequest(entities.ExecutionRequestInput{
		CorrelationID:     cid,
		AdapterID:         aid,
		CapabilityName:    "status",
		CapabilityVersion: "v1",
		Payload:           pl,
		TimeoutBudget:     tb,
	}, reqClk)
	if err != nil {
		t.Fatalf("NewExecutionRequest: %v", err)
	}

	cap, err := valueobjects.NewCapability(aid, "status", "v1", false, 5*time.Second)
	if err != nil {
		t.Fatalf("NewCapability: %v", err)
	}

	handleGen := &entities.FakeIDGen{IDs: []string{testULID2}}
	handleClk := &shared.FakeClock{T: base.Add(time.Millisecond)}
	handle, err := entities.NewExecutionHandle(handleGen, cid, cap, handleClk)
	if err != nil {
		t.Fatalf("NewExecutionHandle: %v", err)
	}

	result, err := entities.NewExecutionResult(
		valueobjects.StatusSuccess,
		valueobjects.HintNonRetryable,
		"", "",
		nil, nil, nil,
		nil, nil,
		0, 0,
		100*time.Millisecond,
		base.Add(101*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("NewExecutionResult: %v", err)
	}

	prov, err := entities.NewProvenance(entities.ProvHTTP, "0.1.0", "test-host", "runtime-0.1.0", "")
	if err != nil {
		t.Fatalf("NewProvenance: %v", err)
	}

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

	receiptGen := &entities.FakeIDGen{IDs: []string{testULID1}}
	receiptClk := &shared.FakeClock{T: base.Add(102 * time.Millisecond)}

	receipt, err := entities.NewExecutionReceipt(
		receiptGen, req, handle, result, prov, timings, receiptClk,
	)
	if err != nil {
		t.Fatalf("NewExecutionReceipt: %v", err)
	}
	return receipt
}
