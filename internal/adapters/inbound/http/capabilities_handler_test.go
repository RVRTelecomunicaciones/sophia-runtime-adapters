package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/entities"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
	"github.com/sophia-ecosystem/runtime-adapters/internal/ports/inbound"
)

// ---- stub QueryService implementations for capabilities tests ---------------

type capabilitiesStubOK struct {
	caps []valueobjects.Capability
}

func (s capabilitiesStubOK) ListCapabilities(_ context.Context, filter inbound.CapabilityFilter) (inbound.ListCapabilitiesResponse, error) {
	caps := s.caps
	if filter.AdapterID != nil {
		var filtered []valueobjects.Capability
		for _, c := range caps {
			if c.AdapterID().String() == filter.AdapterID.String() {
				filtered = append(filtered, c)
			}
		}
		caps = filtered
	}
	return inbound.ListCapabilitiesResponse{
		Capabilities:   caps,
		RuntimeVersion: "0.1.0",
		SchemaVersion:  "v1",
	}, nil
}

func (s capabilitiesStubOK) GetReceipt(_ context.Context, _ shared.ReceiptID, _ inbound.GetReceiptOptions) (entities.ExecutionReceipt, error) {
	return entities.ExecutionReceipt{}, nil
}

type capabilitiesStubErr struct{}

func (s capabilitiesStubErr) ListCapabilities(_ context.Context, _ inbound.CapabilityFilter) (inbound.ListCapabilitiesResponse, error) {
	return inbound.ListCapabilitiesResponse{}, errors.New("registry unavailable")
}

func (s capabilitiesStubErr) GetReceipt(_ context.Context, _ shared.ReceiptID, _ inbound.GetReceiptOptions) (entities.ExecutionReceipt, error) {
	return entities.ExecutionReceipt{}, nil
}

// ---- helpers ----------------------------------------------------------------

func mustCapability(t *testing.T, adapterID, name, version string) valueobjects.Capability {
	t.Helper()
	aid, err := valueobjects.NewAdapterID(adapterID)
	if err != nil {
		t.Fatalf("NewAdapterID(%q): %v", adapterID, err)
	}
	cap, err := valueobjects.NewCapability(aid, name, version, false, time.Second)
	if err != nil {
		t.Fatalf("NewCapability: %v", err)
	}
	return cap
}

func doCapabilitiesRequest(t *testing.T, query inbound.QueryService, url string) *httptest.ResponseRecorder {
	t.Helper()
	handler := NewCapabilitiesHandler(query)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, url, nil)
	handler.ServeHTTP(w, r)
	return w
}

// ---- tests ------------------------------------------------------------------

func TestCapabilitiesHandler_NoFilter_Returns200WithAll(t *testing.T) {
	caps := []valueobjects.Capability{
		mustCapability(t, "git", "status", "v1"),
		mustCapability(t, "shell", "exec", "v1"),
	}
	stub := capabilitiesStubOK{caps: caps}

	w := doCapabilitiesRequest(t, stub, "/api/v1/capabilities")

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	var resp inbound.ListCapabilitiesResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Capabilities) != 2 {
		t.Errorf("capabilities count = %d, want 2", len(resp.Capabilities))
	}
}

func TestCapabilitiesHandler_AdapterIDFilter_Returns200Filtered(t *testing.T) {
	caps := []valueobjects.Capability{
		mustCapability(t, "git", "status", "v1"),
		mustCapability(t, "git", "clone", "v1"),
		mustCapability(t, "shell", "exec", "v1"),
	}
	stub := capabilitiesStubOK{caps: caps}

	w := doCapabilitiesRequest(t, stub, "/api/v1/capabilities?adapter_id=git")

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	var resp inbound.ListCapabilitiesResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Capabilities) != 2 {
		t.Errorf("capabilities count = %d, want 2 (only git)", len(resp.Capabilities))
	}
}

func TestCapabilitiesHandler_InvalidAdapterID_Returns400(t *testing.T) {
	stub := capabilitiesStubOK{}

	// "INVALID" starts with uppercase — fails AdapterID regex.
	w := doCapabilitiesRequest(t, stub, "/api/v1/capabilities?adapter_id=INVALID")

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
	assertErrorClass(t, w, "validation_failure")
}

func TestCapabilitiesHandler_QueryServiceError_Returns500(t *testing.T) {
	w := doCapabilitiesRequest(t, capabilitiesStubErr{}, "/api/v1/capabilities")

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
	assertErrorClass(t, w, "adapter_internal_error")
}
