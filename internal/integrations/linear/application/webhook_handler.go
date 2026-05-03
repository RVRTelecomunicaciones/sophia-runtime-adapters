package application

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/sophia-ecosystem/runtime-adapters/internal/integrations/linear/domain"
)

// LifecycleHandler is the application-internal interface the
// HTTP handler depends on. The concrete *Lifecycle (lifecycle.go)
// implements it; tests stub it.
type LifecycleHandler interface {
	Handle(ctx context.Context, in WebhookEvent) error
}

// ErrLinearClient4xx is returned by the LinearAPIClient (or wrapped
// by lifecycle errors) when Linear returned a 4xx (auth failure,
// malformed mutation). Surfaced to HTTP as 500 (caller's error;
// retries won't help, but operator must see it via logs).
var ErrLinearClient4xx = errors.New("linear api 4xx")

// ErrLinearClient5xx is returned when Linear returned 5xx or timed
// out. Surfaced to HTTP as 502 — Alertmanager retries with backoff.
var ErrLinearClient5xx = errors.New("linear api 5xx")

// alertmanagerPayload is the subset of the Alertmanager webhook v4
// payload the adapter needs. Full schema:
//
//	https://prometheus.io/docs/alerting/latest/configuration/#webhook_config
type alertmanagerPayload struct {
	Version           string            `json:"version"`
	GroupKey          string            `json:"groupKey"`
	Status            string            `json:"status"` // "firing" | "resolved"
	Receiver          string            `json:"receiver"`
	Alerts            []alert           `json:"alerts"`
	GroupLabels       map[string]string `json:"groupLabels"`
	CommonLabels      map[string]string `json:"commonLabels"`
	CommonAnnotations map[string]string `json:"commonAnnotations"`
	ExternalURL       string            `json:"externalURL"`
}

type alert struct {
	Status      string            `json:"status"`
	StartsAt    time.Time         `json:"startsAt"`
	Fingerprint string            `json:"fingerprint"`
	Labels      map[string]string `json:"labels"`
}

// WebhookHandler is the http.Handler that parses webhook payloads,
// projects them into WebhookEvent, dispatches to LifecycleHandler,
// and maps errors to HTTP per spec §7.6.
type WebhookHandler struct {
	lc     LifecycleHandler
	logger *slog.Logger
}

// NewWebhookHandler returns a handler that delegates to lc.
// Logger defaults to slog.Default if not set via WithLogger.
func NewWebhookHandler(lc LifecycleHandler) *WebhookHandler {
	return &WebhookHandler{lc: lc, logger: slog.Default()}
}

// WithLogger returns a copy of h that emits to the given logger.
func (h *WebhookHandler) WithLogger(l *slog.Logger) *WebhookHandler {
	return &WebhookHandler{lc: h.lc, logger: l}
}

// ServeHTTP implements http.Handler. Status mapping per spec §7.6:
//
//	200 OK             — normal firing/resolved processed; or resolved-without-prior-firing race (A2C4AB.3.5)
//	400 Bad Request    — JSON parse error or missing required fields; do NOT retry
//	500 Internal Error — Linear API 4xx (auth/malformed) or generic error; operator must investigate
//	502 Bad Gateway    — Linear API 5xx or timeout; Alertmanager retries with backoff
//	405 Method Not Allowed — non-POST
func (h *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	defer func() { _, _ = io.Copy(io.Discard, r.Body); _ = r.Body.Close() }()

	var p alertmanagerPayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		h.logger.Warn("webhook: decode error", "err", err)
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	ev, err := projectPayload(p)
	if err != nil {
		h.logger.Warn("webhook: payload validation failed", "err", err, "groupKey", p.GroupKey)
		http.Error(w, "invalid payload: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.lc.Handle(r.Context(), ev); err != nil {
		switch {
		case errors.Is(err, ErrLinearClient5xx):
			h.logger.Error("webhook: linear 5xx — alertmanager will retry", "err", err)
			http.Error(w, "upstream linear error", http.StatusBadGateway)
		case errors.Is(err, ErrLinearClient4xx):
			h.logger.Error("webhook: linear 4xx — operator action required", "err", err)
			http.Error(w, "linear client error", http.StatusInternalServerError)
		default:
			h.logger.Error("webhook: lifecycle error", "err", err)
			http.Error(w, "lifecycle error", http.StatusInternalServerError)
		}
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// projectPayload validates required fields and maps the raw
// Alertmanager payload into a WebhookEvent. Returns an error if a
// required label is missing or the severity is unsupported.
func projectPayload(p alertmanagerPayload) (WebhookEvent, error) {
	if p.GroupKey == "" {
		return WebhookEvent{}, errors.New("missing groupKey")
	}
	if p.Status != "firing" && p.Status != "resolved" {
		return WebhookEvent{}, errors.New("status must be 'firing' or 'resolved'")
	}
	sev, err := domain.ParseSeverity(p.CommonLabels["severity"])
	if err != nil {
		return WebhookEvent{}, err
	}
	alertname := p.CommonLabels["alertname"]
	if alertname == "" {
		return WebhookEvent{}, errors.New("missing commonLabels.alertname")
	}
	first := time.Now().UTC()
	if len(p.Alerts) > 0 && !p.Alerts[0].StartsAt.IsZero() {
		first = p.Alerts[0].StartsAt
	}
	return WebhookEvent{
		Status:       p.Status,
		GroupKey:     p.GroupKey,
		Severity:     sev,
		Alertname:    alertname,
		Capability:   p.CommonLabels["capability"],
		FirstFiredAt: first,
		ActiveCount:  len(p.Alerts),
		Summary:      p.CommonAnnotations["summary"],
		Description:  p.CommonAnnotations["description"],
		ExternalURL:  p.ExternalURL,
		Runbook:      p.CommonAnnotations["runbook"],
		Dashboard:    p.CommonAnnotations["dashboard"],
	}, nil
}
