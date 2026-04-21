package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/entities"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
	"github.com/sophia-ecosystem/runtime-adapters/internal/ports/inbound"
)

// ---- stub implementations ----

type stubRuntime struct{}

func (stubRuntime) Execute(_ context.Context, _ entities.ExecutionRequest) (entities.ExecutionReceipt, error) {
	return entities.ExecutionReceipt{}, nil
}

type stubQuery struct{}

func (stubQuery) ListCapabilities(_ context.Context, _ inbound.CapabilityFilter) (inbound.ListCapabilitiesResponse, error) {
	return inbound.ListCapabilitiesResponse{}, nil
}

func (stubQuery) GetReceipt(_ context.Context, _ shared.ReceiptID, _ inbound.GetReceiptOptions) (entities.ExecutionReceipt, error) {
	return entities.ExecutionReceipt{}, nil
}

// ---- helpers ----

func newTestRouter(t *testing.T) http.Handler {
	t.Helper()
	return NewRouter(stubRuntime{}, stubQuery{})
}

func doRequest(t *testing.T, router http.Handler, method, path string) *http.Response {
	t.Helper()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(method, path, nil)
	router.ServeHTTP(w, r)
	return w.Result()
}

// ---- tests ----

func TestNewRouter_PanicsOnNilSvc(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for nil RuntimeService, got none")
		}
	}()
	NewRouter(nil, stubQuery{})
}

func TestNewRouter_PanicsOnNilQuery(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for nil QueryService, got none")
		}
	}()
	NewRouter(stubRuntime{}, nil)
}

func TestHealthz_Returns200(t *testing.T) {
	router := newTestRouter(t)
	res := doRequest(t, router, http.MethodGet, "/healthz")

	if res.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", res.StatusCode)
	}

	var body map[string]string
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("body is not valid JSON: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("status field = %q, want ok", body["status"])
	}
}

func TestStubRoutes_Return501(t *testing.T) {
	router := newTestRouter(t)

	tests := []struct {
		name   string
		method string
		path   string
	}{
		{"execute stub", http.MethodPost, "/api/v1/execute"},
		{"capabilities stub", http.MethodGet, "/api/v1/capabilities"},
		{"receipts stub", http.MethodGet, "/api/v1/receipts/some-id"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := doRequest(t, router, tt.method, tt.path)

			if res.StatusCode != http.StatusNotImplemented {
				t.Errorf("%s %s: status = %d, want 501", tt.method, tt.path, res.StatusCode)
			}

			var e HTTPError
			if err := json.NewDecoder(res.Body).Decode(&e); err != nil {
				t.Fatalf("body is not valid JSON: %v", err)
			}
			if e.Class != "adapter_internal_error" {
				t.Errorf("error_class = %q, want adapter_internal_error", e.Class)
			}
		})
	}
}

func TestUnknownPath_Returns404(t *testing.T) {
	router := newTestRouter(t)
	res := doRequest(t, router, http.MethodGet, "/does/not/exist")

	if res.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", res.StatusCode)
	}
}

func TestWrongMethod_Returns405(t *testing.T) {
	// /api/v1/execute is POST only — GET should yield 405.
	router := newTestRouter(t)
	res := doRequest(t, router, http.MethodGet, "/api/v1/execute")

	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", res.StatusCode)
	}
}

func TestRouter_ContentTypeOnStubs(t *testing.T) {
	router := newTestRouter(t)
	res := doRequest(t, router, http.MethodPost, "/api/v1/execute")

	ct := res.Header.Get("Content-Type")
	if ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q, want application/json; charset=utf-8", ct)
	}
}
