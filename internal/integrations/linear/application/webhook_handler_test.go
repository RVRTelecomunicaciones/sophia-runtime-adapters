package application_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sophia-ecosystem/runtime-adapters/internal/integrations/linear/application"
	"github.com/sophia-ecosystem/runtime-adapters/internal/integrations/linear/domain"
)

// stubLifecycle satisfies the LifecycleHandler interface that
// webhook_handler depends on — keeps the handler test independent
// of the real Lifecycle wiring.
type stubLifecycle struct {
	err    error
	last   application.WebhookEvent
	called int
}

func (s *stubLifecycle) Handle(_ context.Context, in application.WebhookEvent) error {
	s.called++
	s.last = in
	return s.err
}

func validPayload() string {
	return `{
      "version": "4",
      "groupKey": "{}/{severity=\"critical\"}/{alertname=\"X\"}",
      "status": "firing",
      "receiver": "ops-warnings",
      "alerts": [
        {"status":"firing","fingerprint":"f1","startsAt":"2026-05-02T12:00:00Z"}
      ],
      "groupLabels":   {"alertname":"X"},
      "commonLabels":  {"alertname":"X","severity":"warning","capability":"shell.exec@v1"},
      "commonAnnotations": {"summary":"a summary","description":"d","runbook":"r"},
      "externalURL": "http://am/"
    }`
}

func TestHandler_FiringValid_Returns200(t *testing.T) {
	stub := &stubLifecycle{}
	h := application.NewWebhookHandler(stub)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(validPayload()))
	req.Header.Set("Content-Type", "application/json")

	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if stub.called != 1 {
		t.Errorf("Lifecycle.Handle called %d times, want 1", stub.called)
	}
	if stub.last.Severity != domain.SeverityWarning {
		t.Errorf("parsed Severity = %q, want warning", stub.last.Severity)
	}
	if stub.last.Capability != "shell.exec@v1" {
		t.Errorf("parsed Capability = %q, want shell.exec@v1", stub.last.Capability)
	}
}

func TestHandler_MalformedJSON_Returns400(t *testing.T) {
	stub := &stubLifecycle{}
	h := application.NewWebhookHandler(stub)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader("not json"))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if stub.called != 0 {
		t.Errorf("Lifecycle.Handle should NOT be called on bad input, got %d", stub.called)
	}
}

func TestHandler_MissingSeverity_Returns400(t *testing.T) {
	payload := `{"status":"firing","groupKey":"gk","alerts":[],"commonLabels":{"alertname":"X"},"commonAnnotations":{"summary":"s"},"externalURL":"u"}`
	stub := &stubLifecycle{}
	h := application.NewWebhookHandler(stub)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(payload))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (missing severity); body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandler_LifecycleReturnsLinearAPI4xx_Returns500(t *testing.T) {
	stub := &stubLifecycle{err: application.ErrLinearClient4xx}
	h := application.NewWebhookHandler(stub)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(validPayload()))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 (Linear 4xx → 500 per §7.6); body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandler_LifecycleReturnsLinearAPI5xx_Returns502(t *testing.T) {
	stub := &stubLifecycle{err: application.ErrLinearClient5xx}
	h := application.NewWebhookHandler(stub)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(validPayload()))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502 (Linear 5xx → 502 per §7.6); body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandler_LifecycleReturnsGenericErr_Returns500(t *testing.T) {
	stub := &stubLifecycle{err: errors.New("unexpected")}
	h := application.NewWebhookHandler(stub)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(validPayload()))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 for generic err; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandler_NonPOST_Returns405(t *testing.T) {
	h := application.NewWebhookHandler(&stubLifecycle{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/webhook", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

func TestHandler_DrainsBody(t *testing.T) {
	// Defensive — the handler must read the full body even on error
	// branches so the underlying connection can be reused.
	body := bytes.NewReader([]byte("not json"))
	h := application.NewWebhookHandler(&stubLifecycle{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhook", body)
	h.ServeHTTP(rec, req)
	if _, err := io.Copy(io.Discard, body); err != nil {
		t.Errorf("body should be drainable, got %v", err)
	}
}
