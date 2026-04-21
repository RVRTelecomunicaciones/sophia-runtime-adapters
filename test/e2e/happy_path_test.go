//go:build e2e

package e2e_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	nethttp "net/http"
	"testing"
	"time"
)

// TestE2E_ShellExecHappyPath posts a shell.exec@v1 request, asserts the
// receipt reports status=success, stdout contains the expected text, and
// GET /api/v1/receipts/{id} returns the same receipt.
func TestE2E_ShellExecHappyPath(t *testing.T) {
	rt := startRuntime(t, t.TempDir())
	defer rt.Teardown()

	body := map[string]any{
		"correlation_id":     "01HZXK5JC6QK7XV0YQXA0QJ0YZ",
		"adapter_id":         "shell",
		"capability_name":    "exec",
		"capability_version": "v1",
		"payload": map[string]any{
			"command": "echo",
			"args":    []string{"hello-e2e"},
		},
		"timeout_budget_ms": 5000,
	}
	reqBytes, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	req, err := nethttp.NewRequestWithContext(ctx, "POST", rt.Addr+"/api/v1/execute", bytes.NewReader(reqBytes))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := nethttp.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /execute: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != nethttp.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST /execute status=%d body=%s", resp.StatusCode, raw)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}

	var receipt map[string]any
	if err := json.Unmarshal(raw, &receipt); err != nil {
		t.Fatalf("unmarshal receipt: %v  body=%s", err, raw)
	}

	receiptID, _ := receipt["receipt_id"].(string)
	if receiptID == "" {
		t.Fatalf("receipt_id missing: %s", raw)
	}

	result, _ := receipt["result"].(map[string]any)
	if result == nil {
		t.Fatalf("result missing: %s", raw)
	}

	if got, _ := result["status"].(string); got != "success" {
		t.Errorf("status = %q, want success. result=%v", got, result)
	}

	// stdout_ref.data is base64-encoded by encoding/json ([]byte fields).
	// Decode and assert it contains "hello-e2e".
	stdoutRef, _ := result["stdout_ref"].(map[string]any)
	if stdoutRef == nil {
		t.Errorf("stdout_ref missing from result")
	} else {
		dataB64, _ := stdoutRef["data"].(string)
		if dataB64 == "" {
			t.Errorf("stdout_ref.data is empty")
		} else {
			decoded, err := base64.StdEncoding.DecodeString(dataB64)
			if err != nil {
				t.Errorf("decode stdout_ref.data: %v", err)
			} else if !bytes.Contains(decoded, []byte("hello-e2e")) {
				t.Errorf("stdout does not contain \"hello-e2e\": %q", decoded)
			}
		}
	}

	// GET /api/v1/receipts/{id} must return the same receipt_id.
	resp2, err := nethttp.Get(rt.Addr + "/api/v1/receipts/" + receiptID)
	if err != nil {
		t.Fatalf("GET /receipts/%s: %v", receiptID, err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != nethttp.StatusOK {
		raw2, _ := io.ReadAll(resp2.Body)
		t.Fatalf("GET /receipts status=%d body=%s", resp2.StatusCode, raw2)
	}

	var fetched map[string]any
	if err := json.NewDecoder(resp2.Body).Decode(&fetched); err != nil {
		t.Fatal(err)
	}
	if fetched["receipt_id"] != receiptID {
		t.Errorf("fetched receipt_id = %v, want %s", fetched["receipt_id"], receiptID)
	}
}
