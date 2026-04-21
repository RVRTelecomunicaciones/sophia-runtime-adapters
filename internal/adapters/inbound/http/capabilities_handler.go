package http

import (
	"fmt"
	"net/http"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/ports/inbound"
)

// NewCapabilitiesHandler returns GET /api/v1/capabilities with
// optional ?adapter_id=X filter. Response body is the
// ListCapabilitiesResponse JSON.
func NewCapabilitiesHandler(query inbound.QueryService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var filter inbound.CapabilityFilter
		if aid := r.URL.Query().Get("adapter_id"); aid != "" {
			parsed, err := valueobjects.NewAdapterID(aid)
			if err != nil {
				writeHTTPError(w, http.StatusBadRequest,
					"validation_failure",
					fmt.Sprintf("invalid adapter_id %q: %v", aid, err))
				return
			}
			filter.AdapterID = &parsed
		}
		resp, err := query.ListCapabilities(r.Context(), filter)
		if err != nil {
			writeHTTPError(w, http.StatusInternalServerError,
				"adapter_internal_error",
				err.Error())
			return
		}
		writeJSON(w, http.StatusOK, resp)
	}
}
