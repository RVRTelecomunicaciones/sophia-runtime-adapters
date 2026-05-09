package grafana_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sophia-ecosystem/runtime-adapters/internal/infrastructure/obs/log"
	"github.com/sophia-ecosystem/runtime-adapters/internal/integrations/grafana"
)

// stubClient lets tests control GrafanaClient responses per call.
type stubClient struct {
	calls atomic.Int32
	resp  func(call int) error
}

func (s *stubClient) PostAnnotation(_ context.Context, _ grafana.AnnotationRequest) error {
	n := int(s.calls.Add(1))
	if s.resp != nil {
		return s.resp(n)
	}
	return nil
}

func mkPayload(t *testing.T, n int, status string) []byte {
	t.Helper()
	p := grafana.AlertmanagerWebhookV4{
		Status: status,
		Alerts: make([]grafana.AlertmanagerAlertV4, n),
	}
	now := time.Now().UTC()
	for i := 0; i < n; i++ {
		p.Alerts[i] = grafana.AlertmanagerAlertV4{
			Status:   status,
			StartsAt: now,
			Labels:   map[string]string{"alertname": "T", "severity": "critical"},
		}
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func TestHandler_HappyPath_AllAlertsAnnotated(t *testing.T) {
	stub := &stubClient{}
	h := grafana.NewWebhookHandler(stub, log.NewNop())

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(mkPayload(t, 3, "firing")))
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("status: got %d, want 204", rr.Code)
	}
	if int(stub.calls.Load()) != 3 {
		t.Errorf("calls: got %d, want 3", stub.calls.Load())
	}
}

func TestHandler_NonPOST_Returns405(t *testing.T) {
	h := grafana.NewWebhookHandler(&stubClient{}, log.NewNop())
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/webhook", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("status: got %d, want 405", rr.Code)
	}
	if rr.Header().Get("Allow") != http.MethodPost {
		t.Errorf("Allow header: got %q, want POST", rr.Header().Get("Allow"))
	}
}

func TestHandler_MalformedJSON_Returns400(t *testing.T) {
	h := grafana.NewWebhookHandler(&stubClient{}, log.NewNop())
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader("not json"))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", rr.Code)
	}
}

func TestHandler_GrafanaError5xx_Returns502(t *testing.T) {
	stub := &stubClient{resp: func(_ int) error { return grafana.ErrGrafanaClient5xx }}
	h := grafana.NewWebhookHandler(stub, log.NewNop())
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(mkPayload(t, 1, "firing")))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadGateway {
		t.Errorf("status: got %d, want 502", rr.Code)
	}
}

func TestHandler_GrafanaError4xx_Returns204_NoRetry(t *testing.T) {
	stub := &stubClient{resp: func(_ int) error { return grafana.ErrGrafanaClient4xx }}
	h := grafana.NewWebhookHandler(stub, log.NewNop())
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(mkPayload(t, 1, "firing")))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Errorf("status: got %d, want 204 (D2C4D.12 deliberate divergence — NO retry on 4xx)", rr.Code)
	}
}

func TestHandler_PartialFailure_AnyFailureReturns502(t *testing.T) {
	// Three alerts: first 2 succeed, third fails 5xx.
	stub := &stubClient{resp: func(call int) error {
		if call == 3 {
			return grafana.ErrGrafanaClient5xx
		}
		return nil
	}}
	h := grafana.NewWebhookHandler(stub, log.NewNop())
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(mkPayload(t, 3, "firing")))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadGateway {
		t.Errorf("status: got %d, want 502 (any 5xx → retry whole group)", rr.Code)
	}
}

func TestHandler_NilLoggerDefaultsToNop(t *testing.T) {
	// Constructor must accept nil and substitute log.NewNop() so callers
	// can pass nil in lightweight test setups without panic.
	stub := &stubClient{resp: func(_ int) error { return grafana.ErrGrafanaClient4xx }}
	h := grafana.NewWebhookHandler(stub, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(mkPayload(t, 1, "firing")))
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("ServeHTTP must NOT panic with nil logger; got %v", r)
		}
	}()
	h.ServeHTTP(rr, req)
}

// TestHandler_GrafanaError4xx_LogsStructuredFields verifies A2C4D.4 —
// when 4xx fires, the handler must emit level=error with grafana_status,
// alertname, severity, annotation_tags so silent failures are visible
// in the log stream.
func TestHandler_GrafanaError4xx_LogsStructuredFields(t *testing.T) {
	var buf bytes.Buffer
	jh := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	logger := log.NewWithHandler(jh)

	stub := &stubClient{resp: func(_ int) error {
		return &grafana.ClientError{StatusCode: 401, Sentinel: grafana.ErrGrafanaClient4xx}
	}}
	h := grafana.NewWebhookHandler(stub, logger)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(mkPayload(t, 1, "firing")))
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("status: got %d, want 204", rr.Code)
	}

	logLine := buf.String()
	for _, want := range []string{
		`"level":"ERROR"`,
		`"grafana_status":401`,
		`"alertname":"T"`,
		`"severity":"critical"`,
	} {
		if !strings.Contains(logLine, want) {
			t.Errorf("4xx log MUST contain %q; got: %s", want, logLine)
		}
	}
}

// Defensive: ensure errors.Is semantics on the typed wrapper still
// work (the handler relies on this for the policy decision).
func TestHandler_TypedClientErrorIsRecognized(t *testing.T) {
	stub := &stubClient{resp: func(_ int) error {
		return &grafana.ClientError{StatusCode: 500, Sentinel: grafana.ErrGrafanaClient5xx}
	}}
	h := grafana.NewWebhookHandler(stub, log.NewNop())
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(mkPayload(t, 1, "firing")))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadGateway {
		t.Errorf("status: got %d, want 502 (typed *ClientError wrapping 5xx)", rr.Code)
	}
	// Defensive use of errors to keep the import non-empty if the test
	// surface is ever simplified.
	_ = errors.Is
}
