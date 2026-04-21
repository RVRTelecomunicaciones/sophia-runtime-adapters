package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// panicHandler is a test handler that panics with a controlled value.
func panicHandler(msg string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic(msg)
	})
}

// okHandler is a test handler that writes 200 OK.
func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestPanicRecoverer_CatchesPanic(t *testing.T) {
	panicMsg := "something went terribly wrong"
	handler := panicRecoverer(panicHandler(panicMsg))

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)

	handler.ServeHTTP(w, r)

	res := w.Result()
	if res.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", res.StatusCode)
	}

	var e HTTPError
	if err := json.NewDecoder(res.Body).Decode(&e); err != nil {
		t.Fatalf("body is not valid JSON: %v", err)
	}
	if e.Class != "adapter_internal_error" {
		t.Errorf("error_class = %q, want adapter_internal_error", e.Class)
	}
	if !strings.Contains(e.Message, panicMsg) {
		t.Errorf("error_message %q should contain panic value %q", e.Message, panicMsg)
	}
}

func TestPanicRecoverer_NoPanicPassthrough(t *testing.T) {
	handler := panicRecoverer(okHandler())

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)

	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 when no panic", w.Code)
	}
}

func TestPanicRecoverer_PanicValueInMessage(t *testing.T) {
	tests := []struct {
		name     string
		panicVal string
	}{
		{"string panic", "oops"},
		{"error-like string", "nil pointer dereference"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := panicRecoverer(panicHandler(tt.panicVal))

			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			handler.ServeHTTP(w, r)

			var e HTTPError
			if err := json.NewDecoder(w.Result().Body).Decode(&e); err != nil {
				t.Fatalf("body is not valid JSON: %v", err)
			}
			if !strings.Contains(e.Message, tt.panicVal) {
				t.Errorf("message %q does not contain %q", e.Message, tt.panicVal)
			}
		})
	}
}

func TestRequestIDHeader_PropagatesWhenPresent(t *testing.T) {
	reqID := "test-request-id-123"
	handler := requestIDHeader(okHandler())

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Request-Id", reqID)

	handler.ServeHTTP(w, r)

	got := w.Header().Get("X-Request-Id")
	if got != reqID {
		t.Errorf("X-Request-Id response header = %q, want %q", got, reqID)
	}
}

func TestRequestIDHeader_AbsentWhenNotInRequest(t *testing.T) {
	handler := requestIDHeader(okHandler())

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	// No X-Request-Id header set.

	handler.ServeHTTP(w, r)

	got := w.Header().Get("X-Request-Id")
	if got != "" {
		t.Errorf("X-Request-Id should be absent when not in request, got %q", got)
	}
}
