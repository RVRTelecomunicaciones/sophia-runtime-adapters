//go:build e2e

package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	nethttp "net/http"
	"testing"
	"time"
)

// TestE2E_IdempotencyReplay posts the same shell.exec request with the same
// idempotency_key twice. Per D6.4 (replay-everything), the second response
// must carry the same receipt_id. The same started_at timestamp signals that
// the adapter was NOT re-invoked — the cached receipt was returned verbatim.
func TestE2E_IdempotencyReplay(t *testing.T) {
	rt := startRuntime(t, t.TempDir())
	defer rt.Teardown()

	body := map[string]any{
		"correlation_id":     "01HZXK5JC6QK7XV0YQXA0QJ0YZ",
		"adapter_id":         "shell",
		"capability_name":    "exec",
		"capability_version": "v1",
		"payload": map[string]any{
			"command": "echo",
			"args":    []string{"idem-once"},
		},
		"timeout_budget_ms": 5000,
		// Reuse the correlation ULID as the idempotency key — valid ULID per I1.
		"idempotency_key": "01HZXK5JC6QK7XV0YQXA0QJ0YZ",
	}
	reqBytes, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}

	post := func(label string) (receiptID, startedAt string) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		req, err := nethttp.NewRequestWithContext(
			ctx, "POST", rt.Addr+"/api/v1/execute", bytes.NewReader(reqBytes),
		)
		if err != nil {
			t.Fatalf("%s: build request: %v", label, err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := nethttp.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s: POST /execute: %v", label, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != nethttp.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			t.Fatalf("%s: status=%d body=%s", label, resp.StatusCode, raw)
		}

		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("%s: read body: %v", label, err)
		}

		var receipt map[string]any
		if err := json.Unmarshal(raw, &receipt); err != nil {
			t.Fatalf("%s: unmarshal: %v  body=%s", label, err, raw)
		}

		receiptID, _ = receipt["receipt_id"].(string)

		// handle.started_at is present on the receipt wire format (D5.14).
		if handle, ok := receipt["handle"].(map[string]any); ok {
			startedAt, _ = handle["started_at"].(string)
		}

		return receiptID, startedAt
	}

	rid1, start1 := post("first")

	// Brief pause: ensures a DIFFERENT timestamp IF the adapter were re-invoked.
	time.Sleep(150 * time.Millisecond)

	rid2, start2 := post("second")

	t.Logf("first:  receipt_id=%s started_at=%s", rid1, start1)
	t.Logf("second: receipt_id=%s started_at=%s", rid2, start2)

	if rid1 == "" || rid2 == "" {
		t.Fatalf("receipt_id missing: first=%q second=%q", rid1, rid2)
	}

	// Primary assertion: same receipt returned for same idempotency_key.
	if rid1 != rid2 {
		t.Errorf("receipt_ids differ — idempotency failed: %q vs %q", rid1, rid2)
	}

	// Secondary assertion: adapter was NOT re-invoked (started_at identical).
	// started_at equality means the cached receipt is being returned verbatim
	// rather than a fresh execution.
	if start1 == "" || start2 == "" {
		t.Logf("WARN: started_at missing from handle; skipping re-invocation check")
	} else if start1 != start2 {
		t.Errorf("started_at differs between replays — adapter may have been re-invoked: %q vs %q", start1, start2)
	}
}
