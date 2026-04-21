package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sophia-ecosystem/runtime-adapters/internal/application/services"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/entities"
	"github.com/sophia-ecosystem/runtime-adapters/internal/ports/inbound"
)

// ---- helpers ----------------------------------------------------------------

// validExecuteBody is a minimal valid ExecutionRequest JSON.
// Uses a real ULID for correlation_id and a valid adapter_id / capability.
const validExecuteBody = `{
	"correlation_id":      "01HZXK5JC6QK7XV0YQXA0QJ0YZ",
	"adapter_id":          "git",
	"capability_name":     "status",
	"capability_version":  "v1",
	"payload":             {"repo":"/tmp/r"},
	"timeout_budget_ms":   5000,
	"submitted_at":        "2024-01-01T00:00:00.000Z"
}`

// executeRequest builds a POST /api/v1/execute httptest request.
func executeRequest(body string) *http.Request {
	return httptest.NewRequest(http.MethodPost, "/api/v1/execute", strings.NewReader(body))
}

// ---- stub RuntimeService implementations ------------------------------------

type executeStubOK struct {
	receipt entities.ExecutionReceipt
}

func (s executeStubOK) Execute(_ context.Context, _ entities.ExecutionRequest) (entities.ExecutionReceipt, error) {
	return s.receipt, nil
}

type executeStubErr struct{ err error }

func (s executeStubErr) Execute(_ context.Context, _ entities.ExecutionRequest) (entities.ExecutionReceipt, error) {
	return entities.ExecutionReceipt{}, s.err
}

type executeStubCtxCancel struct{}

func (s executeStubCtxCancel) Execute(ctx context.Context, _ entities.ExecutionRequest) (entities.ExecutionReceipt, error) {
	return entities.ExecutionReceipt{}, ctx.Err()
}

// ---- helper to build handler ------------------------------------------------

func newExecuteHandler(svc inbound.RuntimeService, maxBody int) http.HandlerFunc {
	return NewExecuteHandler(svc, ExecuteHandlerConfig{MaxRequestBodyBytes: maxBody})
}

// ---- tests ------------------------------------------------------------------

func TestExecuteHandler_ValidRequest_Returns200(t *testing.T) {
	receipt := mustTestReceipt(t)
	handler := newExecuteHandler(executeStubOK{receipt: receipt}, 0)

	w := httptest.NewRecorder()
	r := executeRequest(validExecuteBody)
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	// Body must be valid JSON, contain receipt_id, and not be an HTTPError.
	var body map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("response body is not valid JSON: %v; raw=%s", err, w.Body.String())
	}
	if _, hasErrClass := body["error_class"]; hasErrClass {
		t.Error("response must not contain error_class on success")
	}
	rid, ok := body["receipt_id"]
	if !ok {
		t.Fatal("response must contain receipt_id")
	}
	var ridStr string
	if err := json.Unmarshal(rid, &ridStr); err != nil || ridStr == "" {
		t.Errorf("receipt_id must be non-empty, got %s", rid)
	}
}

func TestExecuteHandler_EmptyBody_Returns400(t *testing.T) {
	handler := newExecuteHandler(executeStubOK{}, 0)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/execute", http.NoBody)
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
	assertErrorClass(t, w, "validation_failure")
}

func TestExecuteHandler_TrailingData_Returns400(t *testing.T) {
	// Two JSON objects concatenated — the first parses fine, but dec.More()
	// detects leftover data.
	body := validExecuteBody + `{"extra":true}`
	handler := newExecuteHandler(executeStubOK{}, 0)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, executeRequest(body))

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
	assertErrorClass(t, w, "validation_failure")
}

func TestExecuteHandler_MalformedJSON_Returns400(t *testing.T) {
	// A JSON array is not a valid ExecutionRequest object — the custom
	// UnmarshalJSON will fail to decode it. This also exercises the
	// DisallowUnknownFields path on non-object JSON shapes.
	body := `[{"correlation_id":"01HZXK5JC6QK7XV0YQXA0QJ0YZ"}]`
	handler := newExecuteHandler(executeStubOK{}, 0)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, executeRequest(body))

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
	assertErrorClass(t, w, "validation_failure")
}

func TestExecuteHandler_BodyTooLarge_Returns413(t *testing.T) {
	// Limit to 10 bytes — any real body will exceed it.
	handler := newExecuteHandler(executeStubOK{}, 10)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, executeRequest(validExecuteBody))

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", w.Code)
	}
	assertErrorClass(t, w, "payload_schema_failure")
}

func TestExecuteHandler_TooManyExecutions_Returns503(t *testing.T) {
	err := fmt.Errorf("overloaded: %w", services.ErrTooManyExecutions)
	handler := newExecuteHandler(executeStubErr{err: err}, 0)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, executeRequest(validExecuteBody))

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
	var e HTTPError
	if err := json.NewDecoder(w.Body).Decode(&e); err != nil {
		t.Fatalf("body not valid JSON: %v", err)
	}
	if e.Retryable != "retryable" {
		t.Errorf("retryable = %q, want retryable", e.Retryable)
	}
	if e.Class != "adapter_internal_error" {
		t.Errorf("error_class = %q, want adapter_internal_error", e.Class)
	}
}

func TestExecuteHandler_GenericError_Returns500(t *testing.T) {
	handler := newExecuteHandler(executeStubErr{err: fmt.Errorf("db gone")}, 0)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, executeRequest(validExecuteBody))

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
	var e HTTPError
	if err := json.NewDecoder(w.Body).Decode(&e); err != nil {
		t.Fatalf("body not valid JSON: %v", err)
	}
	if e.Retryable != "unknown" {
		t.Errorf("retryable = %q, want unknown", e.Retryable)
	}
}

func TestExecuteHandler_CtxCancelled_Returns499(t *testing.T) {
	handler := newExecuteHandler(executeStubCtxCancel{}, 0)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before the request

	w := httptest.NewRecorder()
	r := executeRequest(validExecuteBody).WithContext(ctx)
	handler.ServeHTTP(w, r)

	if w.Code != 499 {
		t.Errorf("status = %d, want 499", w.Code)
	}
	assertErrorClass(t, w, "cancelled")
}

// ---- helper -----------------------------------------------------------------

func assertErrorClass(t *testing.T, w *httptest.ResponseRecorder, want string) {
	t.Helper()
	var e HTTPError
	if err := json.NewDecoder(w.Body).Decode(&e); err != nil {
		t.Fatalf("body is not valid HTTPError JSON: %v (body=%s)", err, w.Body.String())
	}
	if e.Class != want {
		t.Errorf("error_class = %q, want %q", e.Class, want)
	}
}
