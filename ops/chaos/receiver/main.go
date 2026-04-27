// Package main is the receiver-stub binary.
//
// It is a diagnostic HTTP server used as an Alertmanager webhook target
// during chaos tests. It does NOT persist state — all data lives in an
// in-memory ring buffer that is reset on process restart or DELETE /alerts.
//
// Endpoints:
//
//	POST /alerts        — Alertmanager webhook; appends to ring buffer
//	GET  /inspect       — Returns {"received": [...]} JSON
//	GET  /inspect?since=<rfc3339> — Filtered view by received timestamp
//	DELETE /alerts      — Clears the ring buffer
package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"sync"
	"time"
)

const ringCap = 1000

// AlertPayload is the envelope Alertmanager sends to webhook receivers.
// We capture the raw JSON and tag each entry with a server-side
// received timestamp so ?since= filtering is stable regardless of
// what timestamps the alert payload itself contains.
type AlertPayload struct {
	Received time.Time       `json:"received"`
	Body     json.RawMessage `json:"body"`
}

// ringBuffer is a fixed-capacity circular buffer for AlertPayload entries.
// When full, the oldest entry is overwritten.
type ringBuffer struct {
	mu   sync.Mutex
	buf  [ringCap]AlertPayload
	head int // next write position
	size int // number of valid entries (0..ringCap)
}

func (r *ringBuffer) append(p AlertPayload) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf[r.head] = p
	r.head = (r.head + 1) % ringCap
	if r.size < ringCap {
		r.size++
	}
}

func (r *ringBuffer) snapshot(since time.Time) []AlertPayload {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.size == 0 {
		return nil
	}
	// Oldest entry is at (head - size + ringCap) % ringCap.
	start := (r.head - r.size + ringCap) % ringCap
	out := make([]AlertPayload, 0, r.size)
	for i := 0; i < r.size; i++ {
		idx := (start + i) % ringCap
		if !since.IsZero() && r.buf[idx].Received.Before(since) {
			continue
		}
		out = append(out, r.buf[idx])
	}
	return out
}

func (r *ringBuffer) clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.head = 0
	r.size = 0
}

// router returns the http.Handler for the receiver-stub.
// Extracted so tests can construct it without starting a real listener.
func router(rb *ringBuffer) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/alerts", func(w http.ResponseWriter, req *http.Request) {
		switch req.Method {
		case http.MethodPost:
			var raw json.RawMessage
			if err := json.NewDecoder(req.Body).Decode(&raw); err != nil {
				http.Error(w, "invalid JSON", http.StatusBadRequest)
				return
			}
			rb.append(AlertPayload{Received: time.Now().UTC(), Body: raw})
			w.WriteHeader(http.StatusOK)
		case http.MethodDelete:
			rb.clear()
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/inspect", func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var since time.Time
		if s := req.URL.Query().Get("since"); s != "" {
			var err error
			since, err = time.Parse(time.RFC3339, s)
			if err != nil {
				http.Error(w, "invalid since: must be RFC3339", http.StatusBadRequest)
				return
			}
		}
		entries := rb.snapshot(since)
		if entries == nil {
			entries = []AlertPayload{}
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{"received": entries}); err != nil {
			log.Printf("encode error: %v", err)
		}
	})

	return mux
}

func main() {
	addr := flag.String("addr", ":8088", "listen address")
	flag.Parse()

	rb := &ringBuffer{}
	log.Printf("receiver-stub listening on %s", *addr)
	srv := &http.Server{
		Addr:         *addr,
		Handler:      router(rb),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("ListenAndServe: %v", err)
	}
}
