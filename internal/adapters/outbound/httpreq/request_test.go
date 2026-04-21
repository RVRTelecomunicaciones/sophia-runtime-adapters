package httpreq

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
)

// newTestHTTPAdapter builds an adapter with AllowPrivateNetworks=true
// (required for httptest.NewServer which binds to 127.0.0.1).
func newTestHTTPAdapter(t *testing.T, extra ...func(*Config)) *Adapter {
	t.Helper()
	cfg := Config{
		AllowPrivateNetworks: true,
		InlineStreamLimit:    16 * 1024,
	}
	for _, fn := range extra {
		fn(&cfg)
	}
	a, err := NewAdapter(cfg, &shared.FakeClock{T: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func mustHTTPPayload(t *testing.T, v interface{}) valueobjects.Payload {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	pl, err := valueobjects.NewPayload(valueobjects.ContentTypeJSON, b, 0)
	if err != nil {
		t.Fatal(err)
	}
	return pl
}

func capHTTP(t *testing.T, a *Adapter) valueobjects.Capability {
	t.Helper()
	caps := a.Capabilities()
	if len(caps) != 1 {
		t.Fatalf("expected 1 capability, got %d", len(caps))
	}
	return caps[0]
}

// execRequest executes through adapter and type-asserts raw.
func execRequest(t *testing.T, a *Adapter, p interface{}) *httpRaw {
	t.Helper()
	return execRequestCtx(t, context.Background(), a, p)
}

func execRequestCtx(t *testing.T, ctx context.Context, a *Adapter, p interface{}) *httpRaw {
	t.Helper()
	cap := capHTTP(t, a)
	pl := mustHTTPPayload(t, p)
	raw, err := a.Execute(ctx, cap, pl)
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	r, ok := raw.(*httpRaw)
	if !ok {
		t.Fatalf("raw type = %T, want *httpRaw", raw)
	}
	return r
}

// --- Tests ---

func TestHTTPRequest_GET200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hello world"))
	}))
	defer srv.Close()

	a := newTestHTTPAdapter(t)
	r := execRequest(t, a, map[string]interface{}{"method": "GET", "url": srv.URL})
	if r.statusCode != 200 {
		t.Errorf("statusCode = %d, want 200", r.statusCode)
	}
	if string(r.body) != "hello world" {
		t.Errorf("body = %q, want %q", r.body, "hello world")
	}
	if r.validation != "" {
		t.Errorf("unexpected validation: %q", r.validation)
	}
}

func TestHTTPRequest_Normalize_GET200_Meta(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	a := newTestHTTPAdapter(t)
	cap := capHTTP(t, a)
	pl := mustHTTPPayload(t, map[string]interface{}{"method": "GET", "url": srv.URL})
	raw, _ := a.Execute(context.Background(), cap, pl)

	clk := &shared.FakeClock{T: time.Now()}
	result, err := a.Normalize(cap, raw, clk)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != valueobjects.StatusSuccess {
		t.Errorf("status = %q, want success", result.Status)
	}
	if result.AdapterMeta["status_code"] != "200" {
		t.Errorf("meta status_code = %q, want 200", result.AdapterMeta["status_code"])
	}
}

func TestHTTPRequest_GET404_FailureHintUnknown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	a := newTestHTTPAdapter(t)
	cap := capHTTP(t, a)
	pl := mustHTTPPayload(t, map[string]interface{}{"method": "GET", "url": srv.URL})
	raw, _ := a.Execute(context.Background(), cap, pl)

	clk := &shared.FakeClock{T: time.Now()}
	result, err := a.Normalize(cap, raw, clk)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != valueobjects.StatusFailure {
		t.Errorf("status = %q, want failure", result.Status)
	}
	if result.ErrorClass != valueobjects.ErrExternalFailure {
		t.Errorf("error_class = %q, want external_failure", result.ErrorClass)
	}
	if result.Retryable != valueobjects.HintUnknown {
		t.Errorf("retryable = %q, want unknown", result.Retryable)
	}
}

func TestHTTPRequest_GET500_HintRetryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	a := newTestHTTPAdapter(t)
	cap := capHTTP(t, a)
	pl := mustHTTPPayload(t, map[string]interface{}{"method": "GET", "url": srv.URL})
	raw, _ := a.Execute(context.Background(), cap, pl)

	clk := &shared.FakeClock{T: time.Now()}
	result, err := a.Normalize(cap, raw, clk)
	if err != nil {
		t.Fatal(err)
	}
	if result.Retryable != valueobjects.HintRetryable {
		t.Errorf("retryable = %q, want retryable", result.Retryable)
	}
}

func TestHTTPRequest_ExpectedStatus503_IsSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	a := newTestHTTPAdapter(t)
	cap := capHTTP(t, a)
	pl := mustHTTPPayload(t, map[string]interface{}{
		"method":          "GET",
		"url":             srv.URL,
		"expected_status": []int{503},
	})
	raw, _ := a.Execute(context.Background(), cap, pl)

	clk := &shared.FakeClock{T: time.Now()}
	result, err := a.Normalize(cap, raw, clk)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != valueobjects.StatusSuccess {
		t.Errorf("status = %q, want success (503 in expected_status)", result.Status)
	}
}

func TestHTTPRequest_POST_WithBody(t *testing.T) {
	var received []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		received = b
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(b)
	}))
	defer srv.Close()

	a := newTestHTTPAdapter(t)
	r := execRequest(t, a, map[string]interface{}{
		"method": "POST",
		"url":    srv.URL,
		"body":   []byte("request body"),
	})
	if r.statusCode != 200 {
		t.Errorf("statusCode = %d, want 200", r.statusCode)
	}
	if string(received) != "request body" {
		t.Errorf("server received %q, want %q", received, "request body")
	}
}

func TestHTTPRequest_CustomHeaders(t *testing.T) {
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Custom-Header")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	a := newTestHTTPAdapter(t)
	execRequest(t, a, map[string]interface{}{
		"method":  "GET",
		"url":     srv.URL,
		"headers": map[string]string{"X-Custom-Header": "test-value"},
	})
	if gotHeader != "test-value" {
		t.Errorf("server got header %q, want %q", gotHeader, "test-value")
	}
}

func TestHTTPRequest_BodyTruncation(t *testing.T) {
	payload := strings.Repeat("x", 100)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, payload)
	}))
	defer srv.Close()

	a := newTestHTTPAdapter(t, func(cfg *Config) {
		cfg.MaxBodyBytes = 10
	})
	r := execRequest(t, a, map[string]interface{}{"method": "GET", "url": srv.URL})
	if len(r.body) != 10 {
		t.Errorf("body length = %d, want 10", len(r.body))
	}
	if !r.bodyTruncated {
		t.Error("expected bodyTruncated = true")
	}
	if r.totalBytes != 100 {
		t.Errorf("totalBytes = %d, want 100", r.totalBytes)
	}
}

func TestHTTPRequest_Redirect(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()

	mux.HandleFunc("/dest", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("final"))
	})
	mux.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, srv.URL+"/dest", http.StatusFound)
	})

	a := newTestHTTPAdapter(t)
	r := execRequest(t, a, map[string]interface{}{"method": "GET", "url": srv.URL + "/start"})
	if r.statusCode != 200 {
		t.Errorf("statusCode = %d, want 200", r.statusCode)
	}
	if r.redirectCount < 1 {
		t.Errorf("redirectCount = %d, want >= 1", r.redirectCount)
	}
}

func TestHTTPRequest_TooManyRedirects(t *testing.T) {
	counter := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		counter++
		http.Redirect(w, r, fmt.Sprintf("/?n=%d", counter), http.StatusFound)
	}))
	defer srv.Close()

	a := newTestHTTPAdapter(t)
	r := execRequest(t, a, map[string]interface{}{
		"method":        "GET",
		"url":           srv.URL,
		"max_redirects": 2,
	})
	if r.runErr == nil {
		t.Error("expected runErr for too many redirects")
	}
}

func TestHTTPRequest_CancelledCtx(t *testing.T) {
	ready := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(ready)
		<-r.Context().Done()
	}))
	defer srv.Close()

	a := newTestHTTPAdapter(t)
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		<-ready
		cancel()
	}()

	r := execRequestCtx(t, ctx, a, map[string]interface{}{"method": "GET", "url": srv.URL})
	if r.ctxErr == nil {
		t.Error("expected ctxErr for cancelled context")
	}
}

func TestHTTPRequest_TimeoutDeadline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	a := newTestHTTPAdapter(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	r := execRequestCtx(t, ctx, a, map[string]interface{}{"method": "GET", "url": srv.URL})
	if r.ctxErr == nil {
		t.Error("expected ctxErr for deadline exceeded")
	}
}

func TestHTTPRequest_Validation_EmptyMethod(t *testing.T) {
	a := newTestHTTPAdapter(t)
	r := execRequest(t, a, map[string]interface{}{"url": "http://example.com"})
	if r.validation == "" {
		t.Error("expected validation error for empty method")
	}
}

func TestHTTPRequest_Validation_EmptyURL(t *testing.T) {
	a := newTestHTTPAdapter(t)
	r := execRequest(t, a, map[string]interface{}{"method": "GET"})
	if r.validation == "" {
		t.Error("expected validation error for empty url")
	}
}

func TestHTTPRequest_Validation_UnknownField(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	a := newTestHTTPAdapter(t)
	r := execRequest(t, a, map[string]interface{}{"method": "GET", "url": srv.URL, "junk": 1})
	if r.validation == "" {
		t.Error("expected validation error for unknown field")
	}
}

func TestHTTPRequest_Validation_BadScheme(t *testing.T) {
	a := newTestHTTPAdapter(t)
	r := execRequest(t, a, map[string]interface{}{"method": "GET", "url": "ftp://example.com"})
	if r.validation == "" {
		t.Error("expected validation error for ftp scheme")
	}
}

func TestHTTPRequest_Normalize_ValidationBranch(t *testing.T) {
	a := newTestHTTPAdapter(t)
	cap := capHTTP(t, a)

	raw := &httpRaw{validation: "method is required"}
	clk := &shared.FakeClock{T: time.Now()}
	result, err := a.Normalize(cap, raw, clk)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != valueobjects.StatusFailure {
		t.Errorf("status = %q, want failure", result.Status)
	}
	if result.ErrorClass != valueobjects.ErrValidationFailure {
		t.Errorf("error_class = %q, want validation_failure", result.ErrorClass)
	}
}

func TestHTTPRequest_Normalize_RunErrBranch(t *testing.T) {
	a := newTestHTTPAdapter(t)
	cap := capHTTP(t, a)

	raw := &httpRaw{runErr: fmt.Errorf("connection refused")}
	clk := &shared.FakeClock{T: time.Now()}
	result, err := a.Normalize(cap, raw, clk)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != valueobjects.StatusFailure {
		t.Errorf("status = %q, want failure", result.Status)
	}
	if result.ErrorClass != valueobjects.ErrExternalFailure {
		t.Errorf("error_class = %q, want external_failure", result.ErrorClass)
	}
}

func TestHTTPRequest_Normalize_CtxCancelledBranch(t *testing.T) {
	a := newTestHTTPAdapter(t)
	cap := capHTTP(t, a)

	raw := &httpRaw{ctxErr: context.Canceled}
	clk := &shared.FakeClock{T: time.Now()}
	result, err := a.Normalize(cap, raw, clk)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != valueobjects.StatusCancelled {
		t.Errorf("status = %q, want cancelled", result.Status)
	}
}

func TestHTTPRequest_Normalize_CtxDeadlineBranch(t *testing.T) {
	a := newTestHTTPAdapter(t)
	cap := capHTTP(t, a)

	raw := &httpRaw{ctxErr: context.DeadlineExceeded}
	clk := &shared.FakeClock{T: time.Now()}
	result, err := a.Normalize(cap, raw, clk)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != valueobjects.StatusTimeout {
		t.Errorf("status = %q, want timeout", result.Status)
	}
}

func TestHTTPRequest_Normalize_ExpectedStatusMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	a := newTestHTTPAdapter(t)
	cap := capHTTP(t, a)
	pl := mustHTTPPayload(t, map[string]interface{}{
		"method":          "GET",
		"url":             srv.URL,
		"expected_status": []int{404},
	})
	raw, _ := a.Execute(context.Background(), cap, pl)

	clk := &shared.FakeClock{T: time.Now()}
	result, err := a.Normalize(cap, raw, clk)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != valueobjects.StatusFailure {
		t.Errorf("status = %q, want failure (200 not in expected [404])", result.Status)
	}
}

func TestHTTPRequest_Normalize_ForeignRawType(t *testing.T) {
	a := newTestHTTPAdapter(t)
	cap := capHTTP(t, a)
	clk := &shared.FakeClock{T: time.Now()}
	_, err := a.Normalize(cap, &foreignHTTPRaw{}, clk)
	if err == nil {
		t.Error("expected error for foreign raw type")
	}
}

type foreignHTTPRaw struct{}

func (*foreignHTTPRaw) IsAdapterRawOutcome() {}
