package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/entities"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
	"github.com/sophia-ecosystem/runtime-adapters/internal/ports/inbound"
	"github.com/sophia-ecosystem/runtime-adapters/internal/ports/outbound"
)

// ---- stub QueryService implementations for receipts tests ------------------

type receiptsStubOK struct {
	receipt        entities.ExecutionReceipt
	includeStreams  bool // records last call's option
}

func (s *receiptsStubOK) ListCapabilities(_ context.Context, _ inbound.CapabilityFilter) (inbound.ListCapabilitiesResponse, error) {
	return inbound.ListCapabilitiesResponse{}, nil
}

func (s *receiptsStubOK) GetReceipt(_ context.Context, _ shared.ReceiptID, opts inbound.GetReceiptOptions) (entities.ExecutionReceipt, error) {
	s.includeStreams = opts.IncludeStreams
	if opts.IncludeStreams {
		return s.receipt, nil
	}
	return s.receipt.StripStreams(), nil
}

type receiptsStubNotFound struct{}

func (s receiptsStubNotFound) ListCapabilities(_ context.Context, _ inbound.CapabilityFilter) (inbound.ListCapabilitiesResponse, error) {
	return inbound.ListCapabilitiesResponse{}, nil
}

func (s receiptsStubNotFound) GetReceipt(_ context.Context, _ shared.ReceiptID, _ inbound.GetReceiptOptions) (entities.ExecutionReceipt, error) {
	return entities.ExecutionReceipt{}, fmt.Errorf("not found: %w", outbound.ErrReceiptNotFound)
}

type receiptsStubGenericErr struct{}

func (s receiptsStubGenericErr) ListCapabilities(_ context.Context, _ inbound.CapabilityFilter) (inbound.ListCapabilitiesResponse, error) {
	return inbound.ListCapabilitiesResponse{}, nil
}

func (s receiptsStubGenericErr) GetReceipt(_ context.Context, _ shared.ReceiptID, _ inbound.GetReceiptOptions) (entities.ExecutionReceipt, error) {
	return entities.ExecutionReceipt{}, errors.New("db unreachable")
}

// ---- valid test ULID --------------------------------------------------------

// testReceiptULID is a valid Crockford base-32 ULID suitable for ReceiptID.
const testReceiptULID = "01HZXK5JC6QK7XV0YQXA0QJ0YZ"

// ---- helper: serve via chi router so {id} URL param is populated -----------

// serveReceiptsRequest builds a minimal chi router, registers the handler on
// GET /receipts/{id}, and serves a request with the given path+query.
func serveReceiptsRequest(t *testing.T, query inbound.QueryService, path string) *httptest.ResponseRecorder {
	t.Helper()
	handler := NewReceiptsHandler(query)

	r := chi.NewRouter()
	r.Get("/receipts/{id}", handler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	r.ServeHTTP(w, req)
	return w
}

// ---- tests ------------------------------------------------------------------

func TestReceiptsHandler_ValidID_DefaultIncludeStreams_Returns200(t *testing.T) {
	receipt := mustTestReceipt(t)
	stub := &receiptsStubOK{receipt: receipt}
	w := serveReceiptsRequest(t, stub, "/receipts/"+testReceiptULID)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	if !stub.includeStreams {
		t.Error("expected include_streams=true (default)")
	}
	// Verify body is valid JSON and contains receipt_id.
	var body map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not valid JSON: %v; raw=%s", err, w.Body.String())
	}
	if _, ok := body["receipt_id"]; !ok {
		t.Error("response must contain receipt_id")
	}
}

func TestReceiptsHandler_IncludeStreamsTrue_Returns200WithStreams(t *testing.T) {
	receipt := mustTestReceipt(t)
	stub := &receiptsStubOK{receipt: receipt}
	w := serveReceiptsRequest(t, stub, "/receipts/"+testReceiptULID+"?include_streams=true")

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if !stub.includeStreams {
		t.Error("expected include_streams=true")
	}
}

func TestReceiptsHandler_IncludeStreamsFalse_Returns200Stripped(t *testing.T) {
	// The stub returns receipt.StripStreams() when IncludeStreams=false.
	// Verify the handler passes the flag correctly.
	receipt := mustTestReceipt(t)
	stub := &receiptsStubOK{receipt: receipt}
	w := serveReceiptsRequest(t, stub, "/receipts/"+testReceiptULID+"?include_streams=false")

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if stub.includeStreams {
		t.Error("expected include_streams=false")
	}
}

func TestReceiptsHandler_InvalidIncludeStreams_Returns400(t *testing.T) {
	stub := &receiptsStubOK{}
	w := serveReceiptsRequest(t, stub, "/receipts/"+testReceiptULID+"?include_streams=maybe")

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
	assertErrorClass(t, w, "validation_failure")
}

func TestReceiptsHandler_InvalidID_Returns400(t *testing.T) {
	stub := &receiptsStubOK{}
	// "not-a-ulid" is not a valid ULID.
	w := serveReceiptsRequest(t, stub, "/receipts/not-a-ulid")

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
	assertErrorClass(t, w, "validation_failure")
}

func TestReceiptsHandler_NotFound_Returns404(t *testing.T) {
	w := serveReceiptsRequest(t, receiptsStubNotFound{}, "/receipts/"+testReceiptULID)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
	assertErrorClass(t, w, "validation_failure")
}

func TestReceiptsHandler_GenericError_Returns500(t *testing.T) {
	w := serveReceiptsRequest(t, receiptsStubGenericErr{}, "/receipts/"+testReceiptULID)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
	assertErrorClass(t, w, "adapter_internal_error")
}

// ---- compile-time interface checks -----------------------------------------

var _ inbound.QueryService = (*receiptsStubOK)(nil)
var _ inbound.QueryService = receiptsStubNotFound{}
var _ inbound.QueryService = receiptsStubGenericErr{}
