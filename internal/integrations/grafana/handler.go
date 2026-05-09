package grafana

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/sophia-ecosystem/runtime-adapters/internal/infrastructure/obs/log"
)

// WebhookHandler implements http.Handler for the alertmanager-driven
// annotations adapter. ServeHTTP enforces the §3.2 + §4.3 contract:
//   - non-POST → 405
//   - malformed JSON → 400
//   - any per-alert 5xx/transport/429 → 502 (retry — alertmanager
//     retries per its notifier retry/backoff)
//   - any per-alert 4xx (non-transient) → 204 (NO retry — D2C4D.12
//     deliberate divergence from linear-webhook). On 4xx the
//     handler MUST log at level=error with grafana_status,
//     alertname, severity, annotation_tags (A2C4D.4).
//   - all-success or 4xx-only → 204
type WebhookHandler struct {
	client GrafanaClient
	logger *log.Logger
}

// NewWebhookHandler constructs a handler. logger is used to emit the
// structured 4xx error log per A2C4D.4. Pass log.NewNop() in tests
// where capture is via an explicit slog.Handler buffer; nil is also
// accepted and substituted with NewNop so callers can omit logger
// in lightweight setups without panic.
func NewWebhookHandler(client GrafanaClient, logger *log.Logger) *WebhookHandler {
	if logger == nil {
		logger = log.NewNop()
	}
	return &WebhookHandler{client: client, logger: logger}
}

// ServeHTTP handles the alertmanager v4 webhook POST.
func (h *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload AlertmanagerWebhookV4
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "malformed alertmanager webhook payload", http.StatusBadRequest)
		return
	}

	annotations := MapAlertmanagerWebhookToAnnotations(payload)
	var anyTransient bool

	// MapAlertmanagerWebhookToAnnotations preserves order — annotations[i]
	// corresponds to payload.Alerts[i]. The 4xx error log uses the
	// matching alert's labels for context.
	for i, ann := range annotations {
		err := h.client.PostAnnotation(r.Context(), ann)
		if err == nil {
			continue
		}
		switch {
		case errors.Is(err, ErrGrafanaClient5xx):
			anyTransient = true
			// keep iterating — partial fan-out is acceptable; final
			// response signals retry-the-whole-group per §4.3.
		case errors.Is(err, ErrGrafanaClient4xx):
			// A2C4D.4: structured error log so token revocation /
			// permission drift / payload bugs are visible in
			// post-mortem. Do NOT flip anyTransient — the handler
			// does NOT respond 5xx for 4xx (retry-looping amplifies
			// the operational issue).
			h.log4xx(r, payload.Alerts[i], ann, err)
		default:
			anyTransient = true
		}
	}

	if anyTransient {
		http.Error(w, "grafana annotations: transient upstream failure", http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// log4xx emits the structured 4xx error log per A2C4D.4. The status
// code is recovered from the typed *ClientError when present; falls
// back to 0 for transport / marshal errors.
//
// Counter increment on annotation failures is deferred — this adapter
// has no OTel meter wiring in V1. If/when metrics are added, the
// counter call lands here.
func (h *WebhookHandler) log4xx(r *http.Request, alert AlertmanagerAlertV4, ann AnnotationRequest, err error) {
	status := 0
	var ce *ClientError
	if errors.As(err, &ce) {
		status = ce.StatusCode
	}
	h.logger.Error(r.Context(), "grafana annotations: 4xx upstream — no retry",
		slog.Int("grafana_status", status),
		slog.String("alertname", alert.Labels["alertname"]),
		slog.String("severity", alert.Labels["severity"]),
		slog.Any("annotation_tags", ann.Tags),
		slog.String("err", err.Error()),
	)
}
