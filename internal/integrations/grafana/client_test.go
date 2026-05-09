package grafana_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sophia-ecosystem/runtime-adapters/internal/integrations/grafana"
)

func TestClient_PostAnnotation_HappyPath_BearerHeader(t *testing.T) {
	var gotAuth string
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"message":"Annotation added","id":1}`))
	}))
	defer srv.Close()

	c := grafana.NewHTTPGrafanaClient(srv.URL, "glsa_test", http.DefaultClient)
	err := c.PostAnnotation(context.Background(), grafana.AnnotationRequest{
		Time: 1715000000000,
		Text: "[CRIT] TestAlert — boom",
		Tags: []string{"TestAlert", "severity:critical", "status:firing", "source:alertmanager"},
	})
	if err != nil {
		t.Fatalf("PostAnnotation: %v", err)
	}
	if gotAuth != "Bearer glsa_test" {
		t.Errorf("Authorization header: got %q, want %q", gotAuth, "Bearer glsa_test")
	}
	if !strings.Contains(gotBody, `"text":"[CRIT] TestAlert — boom"`) {
		t.Errorf("body: got %q", gotBody)
	}
}

func TestClient_PostAnnotation_5xx_ReturnsErrGrafanaClient5xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := grafana.NewHTTPGrafanaClient(srv.URL, "tok", http.DefaultClient)
	err := c.PostAnnotation(context.Background(), grafana.AnnotationRequest{Time: 1, Text: "x"})
	if !errors.Is(err, grafana.ErrGrafanaClient5xx) {
		t.Errorf("err: got %v, want wraps ErrGrafanaClient5xx", err)
	}
}

func TestClient_PostAnnotation_4xx_ReturnsErrGrafanaClient4xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	c := grafana.NewHTTPGrafanaClient(srv.URL, "tok", http.DefaultClient)
	err := c.PostAnnotation(context.Background(), grafana.AnnotationRequest{Time: 1, Text: "x"})
	if !errors.Is(err, grafana.ErrGrafanaClient4xx) {
		t.Errorf("err: got %v, want wraps ErrGrafanaClient4xx", err)
	}
}

func TestClient_PostAnnotation_429_TreatedAs5xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := grafana.NewHTTPGrafanaClient(srv.URL, "tok", http.DefaultClient)
	err := c.PostAnnotation(context.Background(), grafana.AnnotationRequest{Time: 1, Text: "x"})
	if !errors.Is(err, grafana.ErrGrafanaClient5xx) {
		t.Errorf("429 must wrap ErrGrafanaClient5xx (transient); got %v", err)
	}
}

func TestClient_PostAnnotation_TransportError_TreatedAs5xx(t *testing.T) {
	c := grafana.NewHTTPGrafanaClient("http://127.0.0.1:1", "tok", http.DefaultClient) // port 1 = refused
	err := c.PostAnnotation(context.Background(), grafana.AnnotationRequest{Time: 1, Text: "x"})
	if !errors.Is(err, grafana.ErrGrafanaClient5xx) {
		t.Errorf("transport error must wrap ErrGrafanaClient5xx; got %v", err)
	}
}

// TestClient_PostAnnotation_StatusCodeRecoverableViaErrorsAs verifies
// the typed *ClientError surface required by A2C4D.4 — the handler in
// B2.5 will use errors.As to extract the actual HTTP status for its
// structured 4xx error log.
func TestClient_PostAnnotation_StatusCodeRecoverableViaErrorsAs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := grafana.NewHTTPGrafanaClient(srv.URL, "tok", http.DefaultClient)
	err := c.PostAnnotation(context.Background(), grafana.AnnotationRequest{Time: 1, Text: "x"})

	var ce *grafana.ClientError
	if !errors.As(err, &ce) {
		t.Fatalf("err must be a *ClientError; got %T (%v)", err, err)
	}
	if ce.StatusCode != 401 {
		t.Errorf("StatusCode: got %d, want 401", ce.StatusCode)
	}
	if !errors.Is(err, grafana.ErrGrafanaClient4xx) {
		t.Errorf("err must wrap ErrGrafanaClient4xx; got %v", err)
	}
}
