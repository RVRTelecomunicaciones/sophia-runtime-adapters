package inbound_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/entities"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
	"github.com/sophia-ecosystem/runtime-adapters/internal/ports/inbound"
)

func TestCapabilityFilter_ZeroValue(t *testing.T) {
	var f inbound.CapabilityFilter
	if f.AdapterID != nil {
		t.Errorf("zero CapabilityFilter AdapterID = %v, want nil", f.AdapterID)
	}
}

func TestCapabilityFilter_WithAdapterID(t *testing.T) {
	aid, err := valueobjects.NewAdapterID("shell")
	if err != nil {
		t.Fatalf("NewAdapterID: %v", err)
	}
	f := inbound.CapabilityFilter{AdapterID: &aid}
	if f.AdapterID == nil {
		t.Error("CapabilityFilter.AdapterID is nil after assignment")
	}
	if f.AdapterID.String() != "shell" {
		t.Errorf("AdapterID = %q, want %q", f.AdapterID.String(), "shell")
	}
}

func TestListCapabilitiesResponse_JSONKeys(t *testing.T) {
	resp := inbound.ListCapabilitiesResponse{
		Capabilities:   []valueobjects.Capability{},
		RuntimeVersion: "0.1.0",
		SchemaVersion:  "v1",
	}
	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	for _, key := range []string{`"capabilities":`, `"runtime_version":`, `"schema_version":`} {
		if !strings.Contains(got, key) {
			t.Errorf("JSON missing key %s: got %s", key, got)
		}
	}
}

func TestGetReceiptOptions_ZeroValue(t *testing.T) {
	var o inbound.GetReceiptOptions
	if o.IncludeStreams != false {
		t.Errorf("zero GetReceiptOptions.IncludeStreams = true, want false")
	}
}

type stubQuery struct{}

func (stubQuery) ListCapabilities(ctx context.Context, f inbound.CapabilityFilter) (inbound.ListCapabilitiesResponse, error) {
	return inbound.ListCapabilitiesResponse{}, nil
}

func (stubQuery) GetReceipt(ctx context.Context, id shared.ReceiptID, opts inbound.GetReceiptOptions) (entities.ExecutionReceipt, error) {
	return entities.ExecutionReceipt{}, nil
}

func TestQueryServiceInterface(t *testing.T) {
	var _ inbound.QueryService = stubQuery{}
}
