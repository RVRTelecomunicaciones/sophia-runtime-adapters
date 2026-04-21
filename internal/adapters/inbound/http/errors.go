// Package http implements the Phase 1 HTTP inbound adapter. It exposes
// three endpoints (POST /api/v1/execute, GET /api/v1/capabilities,
// GET /api/v1/receipts/{id}) per spec §6.1. Routes are registered on
// a chi.Router with panic-recover + request-id middleware.
//
// On the wire, successful responses carry the ExecutionReceipt (or
// ListCapabilitiesResponse) as their JSON body. Structural failures
// use the HTTPError envelope defined here.
package http

import (
	"encoding/json"
	"net/http"
)

// HTTPError is the stable JSON envelope emitted for structural
// failures (bad request, unknown capability, overload, etc.). It is
// distinct from the ExecutionReceipt body — receipts are returned
// whole on successful Execute calls even when the result's status is
// failure/timeout/cancelled/partial. The envelope is used only when
// the runtime can NOT produce a receipt (e.g. the request doesn't
// parse, or persistence itself failed).
//
// Wire format (snake_case per D5.14):
//
//	{
//	  "error_class":   "validation_failure",
//	  "error_message": "...",
//	  "retryable":     "non_retryable",
//	  "receipt_id":    "01HZX..."   // optional, when the failure happened
//	                                  // AFTER a receipt was assembled but
//	                                  // BEFORE it reached the caller.
//	}
type HTTPError struct {
	Code      int    `json:"-"`
	Class     string `json:"error_class"`
	Message   string `json:"error_message"`
	Retryable string `json:"retryable"` // "retryable" | "non_retryable" | "unknown"
	ReceiptID string `json:"receipt_id,omitempty"`
}

// writeHTTPError renders e as JSON with the given HTTP status code.
// If e.Retryable is empty it defaults to "non_retryable".
func writeHTTPError(w http.ResponseWriter, code int, class, msg string) {
	e := HTTPError{
		Code:      code,
		Class:     class,
		Message:   msg,
		Retryable: "non_retryable",
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(e)
}

// writeHTTPErrorWithRetryable is the full-control variant.
func writeHTTPErrorWithRetryable(w http.ResponseWriter, code int, class, msg, retryable, receiptID string) {
	e := HTTPError{
		Code:      code,
		Class:     class,
		Message:   msg,
		Retryable: retryable,
		ReceiptID: receiptID,
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(e)
}

// writeJSON marshals v as JSON with the given status code.
func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
