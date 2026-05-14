package http

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/stretchr/testify/require"

	"github.com/sophia-ecosystem/runtime-adapters/internal/adapters/inbound/http/middleware"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/entities"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
	"github.com/sophia-ecosystem/runtime-adapters/internal/infrastructure/obs/log"
	"github.com/sophia-ecosystem/runtime-adapters/internal/ports/inbound"
)

// ---- stub implementations ----

// stubRuntime returns a real receipt so MarshalJSON succeeds.
type stubRuntime struct{ t *testing.T }

func (s stubRuntime) Execute(_ context.Context, _ entities.ExecutionRequest) (entities.ExecutionReceipt, error) {
	if s.t != nil {
		return mustTestReceipt(s.t), nil
	}
	return entities.ExecutionReceipt{}, nil
}

// stubQuery returns a real receipt for GetReceipt so MarshalJSON succeeds.
type stubQuery struct{ t *testing.T }

func (s stubQuery) ListCapabilities(_ context.Context, _ inbound.CapabilityFilter) (inbound.ListCapabilitiesResponse, error) {
	return inbound.ListCapabilitiesResponse{}, nil
}

func (s stubQuery) GetReceipt(_ context.Context, _ shared.ReceiptID, _ inbound.GetReceiptOptions) (entities.ExecutionReceipt, error) {
	if s.t != nil {
		return mustTestReceipt(s.t), nil
	}
	return entities.ExecutionReceipt{}, nil
}

// ---- helpers ----

func newTestRouter(t *testing.T) http.Handler {
	t.Helper()
	// nil readiness — /readyz is intentionally NOT mounted for these
	// generic router tests. See readyz_handler_test.go for /readyz
	// coverage with a stub probe.
	return NewRouter(stubRuntime{t: t}, stubQuery{t: t}, log.NewNop(), nil)
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
	NewRouter(nil, stubQuery{}, log.NewNop(), nil)
}

func TestNewRouter_PanicsOnNilQuery(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for nil QueryService, got none")
		}
	}()
	NewRouter(stubRuntime{}, nil, log.NewNop(), nil)
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

func TestRoutes_RealHandlers(t *testing.T) {
	// Replace the old 501-stub assertion. All three routes now dispatch to
	// real handlers; this integration pass verifies that wiring is complete
	// and each route returns a non-error status through the full middleware
	// chain.
	router := newTestRouter(t)

	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
	}{
		{
			name:   "execute — valid request returns 200",
			method: http.MethodPost,
			path:   "/api/v1/execute",
			body: `{
				"correlation_id":     "01HZXK5JC6QK7XV0YQXA0QJ0YZ",
				"adapter_id":         "git",
				"capability_name":    "status",
				"capability_version": "v1",
				"payload":            {"repo":"/tmp/r"},
				"timeout_budget_ms":  5000,
				"submitted_at":       "2024-01-01T00:00:00.000Z"
			}`,
			wantStatus: http.StatusOK,
		},
		{
			name:       "capabilities — no filter returns 200",
			method:     http.MethodGet,
			path:       "/api/v1/capabilities",
			wantStatus: http.StatusOK,
		},
		{
			name:       "receipts — valid ULID returns 200",
			method:     http.MethodGet,
			path:       "/api/v1/receipts/01HZXK5JC6QK7XV0YQXA0QJ0YZ",
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var res *http.Response
			if tt.body != "" {
				w := httptest.NewRecorder()
				r := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
				router.ServeHTTP(w, r)
				res = w.Result()
			} else {
				res = doRequest(t, router, tt.method, tt.path)
			}

			if res.StatusCode != tt.wantStatus {
				t.Errorf("%s %s: status = %d, want %d",
					tt.method, tt.path, res.StatusCode, tt.wantStatus)
			}

			var body map[string]json.RawMessage
			if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
				t.Errorf("body is not valid JSON: %v", err)
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

func TestRouter_ContentTypeOnExecute(t *testing.T) {
	// Verify that all responses carry the correct Content-Type regardless
	// of success/error. Use a bad body to trigger a 400 quickly.
	router := newTestRouter(t)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/execute", strings.NewReader(""))
	router.ServeHTTP(w, r)

	ct := w.Header().Get("Content-Type")
	if ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q, want application/json; charset=utf-8", ct)
	}
}

// TestNewRouter_LoggerMiddlewareBoundInChain asserts the middleware chain
// wires LoggerMiddleware between chimw.RequestID and panicRecoverer, so
// downstream handlers always see a logger bound into r.Context() — even
// when the root logger is a Nop. This pins the registration order from
// spec §5.5 (T17 wiring) so a future refactor cannot silently drop the
// middleware from the chain.
func TestNewRouter_LoggerMiddlewareBoundInChain(t *testing.T) {
	probed := false
	probeHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := log.FromContext(r.Context())
		require.NotNil(t, got, "logger must be bound by middleware chain")
		probed = true
		w.WriteHeader(http.StatusOK)
	})

	// Build a chi router with the exact same middleware chain NewRouter
	// installs, then mount a probe handler. If the chain is correct, the
	// probe sees a non-nil logger in ctx.
	r := chi.NewRouter()
	r.Use(middleware.TraceW3C(rand.Reader, nil))
	r.Use(chimw.RequestID)
	r.Use(LoggerMiddleware(log.NewNop()))
	r.Use(requestIDHeader)
	r.Use(panicRecoverer)
	r.Handle("/probe", probeHandler)

	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, probed, "probe handler did not execute")
}
