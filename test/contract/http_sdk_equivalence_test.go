// Package contract_test verifies HTTP and SDK produce byte-identical
// JSON for equivalent inputs.
package contract_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	netHTTP "net/http"
	"net/http/httptest"
	"testing"
	"time"

	inboundhttp "github.com/sophia-ecosystem/runtime-adapters/internal/adapters/inbound/http"
	"github.com/sophia-ecosystem/runtime-adapters/internal/adapters/inbound/sdk"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/entities"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
	"github.com/sophia-ecosystem/runtime-adapters/internal/infrastructure/obs/log"
	"github.com/sophia-ecosystem/runtime-adapters/internal/ports/inbound"
)

// fakeService echoes a deterministic receipt back to both HTTP and SDK
// paths. Using the SAME service instance guarantees that any
// differences are down to serialization, not business logic.
type fakeService struct {
	receipt entities.ExecutionReceipt
	caps    []valueobjects.Capability
}

func (f *fakeService) Execute(_ context.Context, _ entities.ExecutionRequest) (entities.ExecutionReceipt, error) {
	return f.receipt, nil
}

func (f *fakeService) ListCapabilities(_ context.Context, _ inbound.CapabilityFilter) (inbound.ListCapabilitiesResponse, error) {
	return inbound.ListCapabilitiesResponse{
		Capabilities:   f.caps,
		RuntimeVersion: "v0.1.0",
		SchemaVersion:  "v1",
	}, nil
}

func (f *fakeService) GetReceipt(_ context.Context, _ shared.ReceiptID, opts inbound.GetReceiptOptions) (entities.ExecutionReceipt, error) {
	r := f.receipt
	if !opts.IncludeStreams {
		r = r.StripStreams()
	}
	return r, nil
}

func TestHTTPAndSDK_ExecuteProduceEquivalentJSON(t *testing.T) {
	svc := buildFakeService(t)

	// HTTP path.
	server := httptest.NewServer(inboundhttp.NewRouter(svc, svc, log.NewNop()))
	defer server.Close()

	payload := `{"correlation_id":"01HZXK5JC6QK7XV0YQXA0QJ0YZ","adapter_id":"shell","capability_name":"exec","capability_version":"v1","payload":{"cmd":"echo"},"timeout_budget_ms":5000}`
	resp, err := netHTTP.Post(server.URL+"/api/v1/execute", "application/json", bytes.NewReader([]byte(payload)))
	if err != nil {
		t.Fatalf("http POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != netHTTP.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("http status=%d body=%s", resp.StatusCode, string(b))
	}
	httpBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}

	// SDK path.
	clk := &shared.FakeClock{T: time.Unix(1714900000, 0).UTC()}
	client, err := sdk.NewClient(svc, svc, sdk.ClientConfig{Clock: clk, MaxTimeoutBudget: 30 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	sdkReceipt, err := client.Execute(context.Background(), sdk.ExecuteInput{
		CorrelationID:     "01HZXK5JC6QK7XV0YQXA0QJ0YZ",
		AdapterID:         "shell",
		CapabilityName:    "exec",
		CapabilityVersion: "v1",
		Payload:           json.RawMessage(`{"cmd":"echo"}`),
		TimeoutBudgetMs:   5000,
	})
	if err != nil {
		t.Fatal(err)
	}
	sdkBody, err := json.Marshal(sdkReceipt)
	if err != nil {
		t.Fatal(err)
	}

	// Normalize variable fields, then compare.
	httpNorm := normalizeVariable(t, stripTrailingNewline(httpBody))
	sdkNorm := normalizeVariable(t, sdkBody)
	if !bytes.Equal(httpNorm, sdkNorm) {
		t.Errorf("HTTP and SDK produced different JSON after normalization.\n\nHTTP:\n%s\n\nSDK:\n%s", httpNorm, sdkNorm)
	}
}

func TestHTTPAndSDK_ListCapabilitiesProduceEquivalentJSON(t *testing.T) {
	svc := buildFakeService(t)
	server := httptest.NewServer(inboundhttp.NewRouter(svc, svc, log.NewNop()))
	defer server.Close()

	// HTTP.
	resp, err := netHTTP.Get(server.URL + "/api/v1/capabilities")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	httpBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}

	// SDK.
	client, err := sdk.NewClient(svc, svc, sdk.ClientConfig{})
	if err != nil {
		t.Fatal(err)
	}
	sdkResp, err := client.ListCapabilities(context.Background(), sdk.ListCapabilitiesFilter{})
	if err != nil {
		t.Fatal(err)
	}
	sdkBody, err := json.Marshal(sdkResp)
	if err != nil {
		t.Fatal(err)
	}

	// ListCapabilitiesResponse has no variable fields; byte-equal after stripping the
	// trailing newline that json.NewEncoder adds.
	if !bytes.Equal(stripTrailingNewline(httpBody), sdkBody) {
		t.Errorf("HTTP and SDK ListCapabilities differ.\nHTTP: %s\nSDK:  %s", httpBody, sdkBody)
	}
}

// buildFakeService constructs a deterministic receipt and a single
// capability for use by both HTTP and SDK paths.
func buildFakeService(t *testing.T) *fakeService {
	t.Helper()

	const (
		ulid1 = "01HZXK5JC6QK7XV0YQXA0QJ0YZ"
		ulid2 = "01HZXK5JC6QK7XV0YQXA0QJ0YA"
	)

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	cid, err := shared.NewCorrelationID(ulid1)
	if err != nil {
		t.Fatalf("NewCorrelationID: %v", err)
	}
	aid, err := valueobjects.NewAdapterID("shell")
	if err != nil {
		t.Fatalf("NewAdapterID: %v", err)
	}
	pl, err := valueobjects.NewPayload(valueobjects.ContentTypeJSON, json.RawMessage(`{"cmd":"echo"}`), 0)
	if err != nil {
		t.Fatalf("NewPayload: %v", err)
	}
	tb, err := valueobjects.NewTimeoutBudget(5000, 0)
	if err != nil {
		t.Fatalf("NewTimeoutBudget: %v", err)
	}

	reqClk := &shared.FakeClock{T: base}
	req, err := entities.NewExecutionRequest(entities.ExecutionRequestInput{
		CorrelationID:     cid,
		AdapterID:         aid,
		CapabilityName:    "exec",
		CapabilityVersion: "v1",
		Payload:           pl,
		TimeoutBudget:     tb,
	}, reqClk)
	if err != nil {
		t.Fatalf("NewExecutionRequest: %v", err)
	}

	cap, err := valueobjects.NewCapability(aid, "exec", "v1", false, 5*time.Second)
	if err != nil {
		t.Fatalf("NewCapability: %v", err)
	}

	handleGen := &entities.FakeIDGen{IDs: []string{ulid2}}
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

	receiptGen := &entities.FakeIDGen{IDs: []string{ulid1}}
	receiptClk := &shared.FakeClock{T: base.Add(102 * time.Millisecond)}
	receipt, err := entities.NewExecutionReceipt(receiptGen, req, handle, result, prov, timings, receiptClk)
	if err != nil {
		t.Fatalf("NewExecutionReceipt: %v", err)
	}

	return &fakeService{
		receipt: receipt,
		caps:    []valueobjects.Capability{cap},
	}
}

// normalizeVariable zeros out variable fields (IDs + timestamps) so
// HTTP and SDK bodies can be byte-compared.
func normalizeVariable(t *testing.T, raw []byte) []byte {
	t.Helper()
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("normalizeVariable unmarshal: %v (raw=%s)", err, raw)
	}
	zeroFields(m, []string{
		"receipt_id", "handle_id", "created_at", "persisted_at",
		"submitted_at", "started_at", "completed_at",
	})
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("normalizeVariable marshal: %v", err)
	}
	return b
}

func zeroFields(v interface{}, keys []string) {
	switch vv := v.(type) {
	case map[string]interface{}:
		for _, k := range keys {
			if _, ok := vv[k]; ok {
				vv[k] = ""
			}
		}
		for _, child := range vv {
			zeroFields(child, keys)
		}
	case []interface{}:
		for _, c := range vv {
			zeroFields(c, keys)
		}
	}
}

func stripTrailingNewline(b []byte) []byte {
	if len(b) > 0 && b[len(b)-1] == '\n' {
		return b[:len(b)-1]
	}
	return b
}
