package http

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/sophia-ecosystem/runtime-adapters/internal/application/services"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/entities"
	"github.com/sophia-ecosystem/runtime-adapters/internal/ports/inbound"
)

// maxRequestBodyBytes bounds the POST /api/v1/execute body at decode
// time. The runtime's MaxPayloadBytes config caps the payload sub-field
// separately in NewPayload; the outer envelope is capped here so a
// malformed client can't ship a 10 GiB JSON blob that fills memory
// before payload validation runs. Configured via NewExecuteHandler.
const defaultMaxRequestBodyBytes = 1 << 20 // 1 MiB

// ExecuteHandlerConfig tunes the handler. Zero values fall back to
// defaults.
type ExecuteHandlerConfig struct {
	MaxRequestBodyBytes int // default 1 MiB
}

// NewExecuteHandler returns the POST /api/v1/execute handler bound to
// the given RuntimeService.
func NewExecuteHandler(svc inbound.RuntimeService, cfg ExecuteHandlerConfig) http.HandlerFunc {
	maxBody := cfg.MaxRequestBodyBytes
	if maxBody <= 0 {
		maxBody = defaultMaxRequestBodyBytes
	}
	return func(w http.ResponseWriter, r *http.Request) {
		// Cap the request body.
		r.Body = http.MaxBytesReader(w, r.Body, int64(maxBody))

		// Decode with DisallowUnknownFields per D5.14. ExecutionRequest's
		// own UnmarshalJSON applies the domain invariants.
		//
		// Known stdlib limitation: DisallowUnknownFields is honored only
		// at THIS decoder level for plain-struct decode paths. It does
		// NOT propagate into types that implement custom UnmarshalJSON
		// (like ExecutionRequest does). Extra fields inside a typed
		// UnmarshalJSON are silently dropped by json.Unmarshal. The
		// outer DisallowUnknownFields still catches top-level shape
		// mismatches (e.g. a JSON array where an object is expected),
		// so malformed bodies are rejected — but nested unknown fields
		// pass through unnoticed. A future Phase 2D hardening task
		// (central JSON Schema validation) will close this gap.
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		var req entities.ExecutionRequest
		if err := dec.Decode(&req); err != nil {
			// Distinguish body-too-large from validation failure.
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				writeHTTPError(w, http.StatusRequestEntityTooLarge,
					"payload_schema_failure",
					fmt.Sprintf("request body exceeds max %d bytes", maxBody))
				return
			}
			if errors.Is(err, io.EOF) {
				writeHTTPError(w, http.StatusBadRequest,
					"validation_failure", "empty request body")
				return
			}
			writeHTTPError(w, http.StatusBadRequest,
				"validation_failure",
				fmt.Sprintf("decode request: %v", err))
			return
		}
		// Reject trailing data (e.g. two JSON objects concatenated).
		if dec.More() {
			writeHTTPError(w, http.StatusBadRequest,
				"validation_failure", "unexpected trailing data in request body")
			return
		}

		receipt, err := svc.Execute(r.Context(), req)
		switch {
		case err == nil:
			writeJSON(w, http.StatusOK, receipt)
			return
		case errors.Is(err, services.ErrTooManyExecutions):
			writeHTTPErrorWithRetryable(w,
				http.StatusServiceUnavailable,
				"adapter_internal_error",
				err.Error(),
				"retryable", // 503 is retryable by convention
				"")
			return
		case errors.Is(err, r.Context().Err()):
			// Caller-cancelled / deadline-exceeded upstream.
			writeHTTPError(w, 499, "cancelled", err.Error())
			return
		default:
			writeHTTPErrorWithRetryable(w,
				http.StatusInternalServerError,
				"adapter_internal_error",
				err.Error(),
				"unknown",
				"")
			return
		}
	}
}
