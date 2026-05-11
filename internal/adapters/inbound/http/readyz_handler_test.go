package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sophia-ecosystem/runtime-adapters/internal/infrastructure/obs/log"
)

// ---- stub Readiness implementations ----

type stubReadiness struct {
	err   error
	calls int
}

func (s *stubReadiness) Ping(_ context.Context) error {
	s.calls++
	return s.err
}

// slowReadiness blocks until ctx is done; used to verify the 2s timeout.
type slowReadiness struct{}

func (slowReadiness) Ping(ctx context.Context) error {
	<-ctx.Done()
	// Surface ctx.Err so the handler renders it in the JSON body — this
	// gives test assertions a concrete error to match against.
	return ctx.Err()
}

// ---- direct handler tests ----

func TestReadyzHandler_OKWhenProbeSucceeds(t *testing.T) {
	probe := &stubReadiness{err: nil}
	handler := NewReadyzHandler(probe)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	handler.ServeHTTP(w, r)

	res := w.Result()
	require.Equal(t, http.StatusOK, res.StatusCode)
	require.Equal(t, "application/json; charset=utf-8", res.Header.Get("Content-Type"))
	require.Equal(t, 1, probe.calls, "probe should be invoked exactly once")

	var body readyResponse
	require.NoError(t, json.NewDecoder(res.Body).Decode(&body))
	require.Equal(t, "ready", body.Status)
	require.Equal(t, "ok", body.Checks["db"])
}

func TestReadyzHandler_503WhenProbeFails(t *testing.T) {
	probe := &stubReadiness{err: errors.New("connection refused")}
	handler := NewReadyzHandler(probe)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	handler.ServeHTTP(w, r)

	res := w.Result()
	require.Equal(t, http.StatusServiceUnavailable, res.StatusCode)
	require.Equal(t, "application/json; charset=utf-8", res.Header.Get("Content-Type"))

	var body readyResponse
	require.NoError(t, json.NewDecoder(res.Body).Decode(&body))
	require.Equal(t, "degraded", body.Status)
	require.Equal(t, "connection refused", body.Checks["db"])
}

func TestReadyzHandler_TimesOutAfter2Seconds(t *testing.T) {
	// slowReadiness blocks on ctx.Done — the handler's 2s timeout must
	// fire and the probe must return ctx.DeadlineExceeded. We cap the
	// test budget at 3s to keep CI snappy even if something hangs.
	handler := NewReadyzHandler(slowReadiness{})

	done := make(chan *http.Response, 1)
	go func() {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/readyz", nil)
		handler.ServeHTTP(w, r)
		done <- w.Result()
	}()

	select {
	case res := <-done:
		require.Equal(t, http.StatusServiceUnavailable, res.StatusCode)
		var body readyResponse
		require.NoError(t, json.NewDecoder(res.Body).Decode(&body))
		require.Equal(t, "degraded", body.Status)
		// ctx.Err() returns context.DeadlineExceeded → "context deadline exceeded".
		require.Equal(t, context.DeadlineExceeded.Error(), body.Checks["db"])
	case <-time.After(3 * time.Second):
		t.Fatal("handler did not honour 2s timeout")
	}
}

func TestNewReadyzHandler_PanicsOnNilProbe(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for nil Readiness, got none")
		}
	}()
	NewReadyzHandler(nil)
}

// ---- router-level tests (mounting / wiring) ----

func TestRouter_ReadyzMountedWhenReadinessProvided(t *testing.T) {
	probe := &stubReadiness{err: nil}
	router := NewRouter(stubRuntime{t: t}, stubQuery{t: t}, log.NewNop(), probe)

	res := doRequest(t, router, http.MethodGet, "/readyz")
	require.Equal(t, http.StatusOK, res.StatusCode)

	var body readyResponse
	require.NoError(t, json.NewDecoder(res.Body).Decode(&body))
	require.Equal(t, "ready", body.Status)
}

func TestRouter_ReadyzReturns503OnDBDown(t *testing.T) {
	probe := &stubReadiness{err: errors.New("dial tcp: connection refused")}
	router := NewRouter(stubRuntime{t: t}, stubQuery{t: t}, log.NewNop(), probe)

	res := doRequest(t, router, http.MethodGet, "/readyz")
	require.Equal(t, http.StatusServiceUnavailable, res.StatusCode)

	var body readyResponse
	require.NoError(t, json.NewDecoder(res.Body).Decode(&body))
	require.Equal(t, "degraded", body.Status)
	require.Equal(t, "dial tcp: connection refused", body.Checks["db"])
}

func TestRouter_ReadyzNotMountedWhenReadinessNil(t *testing.T) {
	// newTestRouter passes nil readiness — /readyz must not be mounted.
	router := newTestRouter(t)

	res := doRequest(t, router, http.MethodGet, "/readyz")
	require.Equal(t, http.StatusNotFound, res.StatusCode,
		"with nil readiness, /readyz must not be mounted (404)")
}

func TestRouter_HealthzStillWorksAlongsideReadyz(t *testing.T) {
	// Pin the spec: /healthz is liveness, dependency-free; adding /readyz
	// must not couple it to the DB probe. We use a deliberately failing
	// probe to prove /healthz still answers 200.
	probe := &stubReadiness{err: errors.New("db down")}
	router := NewRouter(stubRuntime{t: t}, stubQuery{t: t}, log.NewNop(), probe)

	res := doRequest(t, router, http.MethodGet, "/healthz")
	require.Equal(t, http.StatusOK, res.StatusCode,
		"/healthz must remain liveness-only and never observe the DB probe")
	require.Equal(t, 0, probe.calls, "/healthz must not invoke the readiness probe")
}
