package http

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
	"github.com/sophia-ecosystem/runtime-adapters/internal/ports/inbound"
	"github.com/sophia-ecosystem/runtime-adapters/internal/ports/outbound"
)

// NewReceiptsHandler returns GET /api/v1/receipts/{id}?include_streams=
// true|false. Per spec §6.4 the default is true (streams included);
// the flag exists for lightweight audit fetches that want to skip
// potentially-large stdout/stderr payloads.
func NewReceiptsHandler(query inbound.QueryService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		if idStr == "" {
			writeHTTPError(w, http.StatusBadRequest,
				"validation_failure", "id is required")
			return
		}
		id, err := shared.NewReceiptID(idStr)
		if err != nil {
			writeHTTPError(w, http.StatusBadRequest,
				"validation_failure",
				fmt.Sprintf("invalid receipt id %q: %v", idStr, err))
			return
		}
		// Parse include_streams: absent or empty → true (spec §6.4
		// default); "false" → false; "true" → true; anything else →
		// validation failure.
		includeStreams := true
		if raw := r.URL.Query().Get("include_streams"); raw != "" {
			parsed, perr := strconv.ParseBool(raw)
			if perr != nil {
				writeHTTPError(w, http.StatusBadRequest,
					"validation_failure",
					fmt.Sprintf("invalid include_streams %q: %v", raw, perr))
				return
			}
			includeStreams = parsed
		}
		opts := inbound.GetReceiptOptions{IncludeStreams: includeStreams}

		receipt, err := query.GetReceipt(r.Context(), id, opts)
		if err != nil {
			if errors.Is(err, outbound.ErrReceiptNotFound) {
				writeHTTPError(w, http.StatusNotFound,
					"validation_failure",
					fmt.Sprintf("receipt %q not found", idStr))
				return
			}
			writeHTTPError(w, http.StatusInternalServerError,
				"adapter_internal_error",
				err.Error())
			return
		}
		writeJSON(w, http.StatusOK, receipt)
	}
}
