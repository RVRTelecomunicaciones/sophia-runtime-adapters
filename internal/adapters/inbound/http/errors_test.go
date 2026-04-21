package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteHTTPError_BasicFields(t *testing.T) {
	w := httptest.NewRecorder()
	writeHTTPError(w, http.StatusBadRequest, "validation_failure", "missing field")

	res := w.Result()

	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", res.StatusCode, http.StatusBadRequest)
	}
	if ct := res.Header.Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q, want application/json; charset=utf-8", ct)
	}

	var e HTTPError
	if err := json.NewDecoder(res.Body).Decode(&e); err != nil {
		t.Fatalf("body is not valid JSON: %v", err)
	}
	if e.Class != "validation_failure" {
		t.Errorf("error_class = %q, want %q", e.Class, "validation_failure")
	}
	if e.Message != "missing field" {
		t.Errorf("error_message = %q, want %q", e.Message, "missing field")
	}
	if e.Retryable != "non_retryable" {
		t.Errorf("retryable = %q, want %q", e.Retryable, "non_retryable")
	}
	if e.ReceiptID != "" {
		t.Errorf("receipt_id should be absent on basic error, got %q", e.ReceiptID)
	}
}

func TestWriteHTTPError_StatusCodes(t *testing.T) {
	tests := []struct {
		name string
		code int
	}{
		{"400", http.StatusBadRequest},
		{"404", http.StatusNotFound},
		{"500", http.StatusInternalServerError},
		{"501", http.StatusNotImplemented},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			writeHTTPError(w, tt.code, "test_class", "test msg")
			if w.Code != tt.code {
				t.Errorf("code = %d, want %d", w.Code, tt.code)
			}
		})
	}
}

func TestWriteHTTPErrorWithRetryable_FullControl(t *testing.T) {
	tests := []struct {
		name      string
		retryable string
		receiptID string
	}{
		{"retryable with receipt", "retryable", "01HZXXXXXXXXXXXXXXXXXXXXXXXXXXX"},
		{"non_retryable no receipt", "non_retryable", ""},
		{"unknown no receipt", "unknown", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			writeHTTPErrorWithRetryable(w, http.StatusInternalServerError,
				"adapter_error", "something went wrong",
				tt.retryable, tt.receiptID)

			var e HTTPError
			if err := json.NewDecoder(w.Result().Body).Decode(&e); err != nil {
				t.Fatalf("body is not valid JSON: %v", err)
			}
			if e.Retryable != tt.retryable {
				t.Errorf("retryable = %q, want %q", e.Retryable, tt.retryable)
			}
			if e.ReceiptID != tt.receiptID {
				t.Errorf("receipt_id = %q, want %q", e.ReceiptID, tt.receiptID)
			}
		})
	}
}

func TestWriteJSON_Struct(t *testing.T) {
	type payload struct {
		Status string `json:"status"`
	}

	w := httptest.NewRecorder()
	writeJSON(w, http.StatusOK, payload{Status: "ok"})

	res := w.Result()
	if res.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}

	var p payload
	if err := json.NewDecoder(res.Body).Decode(&p); err != nil {
		t.Fatalf("body is not valid JSON: %v", err)
	}
	if p.Status != "ok" {
		t.Errorf("status = %q, want ok", p.Status)
	}
}

func TestWriteJSON_NilValue(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSON(w, http.StatusOK, nil)

	res := w.Result()
	if res.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", res.StatusCode)
	}
	// nil marshals to JSON null — body must be valid JSON.
	var v interface{}
	if err := json.NewDecoder(res.Body).Decode(&v); err != nil {
		t.Fatalf("body with nil value is not valid JSON: %v", err)
	}
	if v != nil {
		t.Errorf("decoded value = %v, want nil", v)
	}
}
