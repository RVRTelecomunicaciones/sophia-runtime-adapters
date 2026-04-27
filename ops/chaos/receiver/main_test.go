package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newTestServer wires a fresh ring buffer and returns its test server + buffer.
func newTestServer(t *testing.T) (*httptest.Server, *ringBuffer) {
	t.Helper()
	rb := &ringBuffer{}
	srv := httptest.NewServer(router(rb))
	t.Cleanup(srv.Close)
	return srv, rb
}

// postAlert sends a JSON payload to POST /alerts.
func postAlert(t *testing.T, baseURL string, body any) *http.Response {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	resp, err := http.Post(baseURL+"/alerts", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("POST /alerts: %v", err)
	}
	return resp
}

// TestStoreAndInspect verifies that a POST /alerts persists the payload
// and GET /inspect returns it.
func TestStoreAndInspect(t *testing.T) {
	srv, _ := newTestServer(t)

	alert := map[string]any{"alertname": "TestAlert", "status": "firing"}
	resp := postAlert(t, srv.URL, alert)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}

	resp2, err := http.Get(srv.URL + "/inspect")
	if err != nil {
		t.Fatalf("GET /inspect: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp2.StatusCode)
	}

	var out struct {
		Received []AlertPayload `json:"received"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Received) != 1 {
		t.Fatalf("want 1 entry, got %d", len(out.Received))
	}
}

// TestDeleteClears verifies that DELETE /alerts empties the buffer.
func TestDeleteClears(t *testing.T) {
	srv, _ := newTestServer(t)

	postAlert(t, srv.URL, map[string]any{"alertname": "A"})
	postAlert(t, srv.URL, map[string]any{"alertname": "B"})

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/alerts", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE /alerts: %v", err)
	}
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("want 204, got %d", resp.StatusCode)
	}

	resp2, err2 := http.Get(srv.URL + "/inspect")
	if err2 != nil {
		t.Fatalf("GET /inspect after delete: %v", err2)
	}
	defer resp2.Body.Close()
	var out struct {
		Received []AlertPayload `json:"received"`
	}
	json.NewDecoder(resp2.Body).Decode(&out) //nolint:errcheck
	if len(out.Received) != 0 {
		t.Fatalf("want 0 entries after delete, got %d", len(out.Received))
	}
}

// TestSinceFilter verifies that GET /inspect?since=<rfc3339> only returns
// entries whose Received timestamp is >= the given time.
func TestSinceFilter(t *testing.T) {
	srv, rb := newTestServer(t)

	// Manually insert an entry in the past.
	past := time.Now().UTC().Add(-2 * time.Hour)
	rb.append(AlertPayload{Received: past, Body: json.RawMessage(`{"old":true}`)})

	// Insert a recent entry via the HTTP endpoint.
	postAlert(t, srv.URL, map[string]any{"new": true})

	// since=1 minute ago — should return only the recent entry.
	since := time.Now().UTC().Add(-1 * time.Minute).Format(time.RFC3339)
	resp, err := http.Get(srv.URL + "/inspect?since=" + since)
	if err != nil {
		t.Fatalf("GET /inspect?since: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}

	var out struct {
		Received []AlertPayload `json:"received"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Received) != 1 {
		t.Fatalf("want 1 recent entry, got %d", len(out.Received))
	}
}

// TestRingBufferCap verifies that adding > 1000 entries drops the oldest.
func TestRingBufferCap(t *testing.T) {
	rb := &ringBuffer{}
	for i := 0; i < ringCap+10; i++ {
		rb.append(AlertPayload{
			Received: time.Now().UTC(),
			Body:     json.RawMessage(fmt.Sprintf(`{"i":%d}`, i)),
		})
	}
	entries := rb.snapshot(time.Time{})
	if len(entries) != ringCap {
		t.Fatalf("want %d entries, got %d", ringCap, len(entries))
	}
	// The oldest surviving entry should be index 10 (the first 10 were dropped).
	var first map[string]int
	if err := json.Unmarshal(entries[0].Body, &first); err != nil {
		t.Fatalf("unmarshal first entry: %v", err)
	}
	if first["i"] != 10 {
		t.Fatalf("want first entry i=10 (oldest remaining), got i=%d", first["i"])
	}
}

// TestMethodNotAllowed verifies that unsupported verbs return 405.
func TestMethodNotAllowed(t *testing.T) {
	srv, _ := newTestServer(t)

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/alerts"},    // GET /alerts not allowed
		{http.MethodPost, "/inspect"},  // POST /inspect not allowed
		{http.MethodPut, "/alerts"},    // PUT /alerts not allowed
		{http.MethodPatch, "/inspect"}, // PATCH /inspect not allowed
	}

	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req, _ := http.NewRequest(tc.method, srv.URL+tc.path, strings.NewReader("{}"))
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			if resp.StatusCode != http.StatusMethodNotAllowed {
				t.Fatalf("want 405, got %d", resp.StatusCode)
			}
		})
	}
}
