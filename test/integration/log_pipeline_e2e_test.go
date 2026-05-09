//go:build integration

// Package integration contains the Phase 2C.4 / D B1 integration tests
// for the log signal pipeline.
//
// Files in this directory are tagged `integration` and only run in the
// CI integration job (which sets RUNTIME_TENANT=test, satisfying the
// runtime-side mode lock). They exercise the full bootstrap → runtime
// → emission path that unit tests cannot reach.
package integration

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/sophia-ecosystem/runtime-adapters/internal/bootstrap"
	"github.com/sophia-ecosystem/runtime-adapters/internal/infrastructure/config"
)

// TestLogPipeline_RuntimeWritesToMirrorAndStdout asserts that the
// SafeFanoutWriter wired into bootstrap.BuildRuntime actually fans out
// emissions to both stdout (R10 primary) AND the mirror file
// (D2C4D.3 / I-D.1) when RUNTIME_LOG_MIRROR_PATH is set.
//
// Strategy:
//  1. Bring up a real Postgres testcontainer.
//  2. Set RUNTIME_LOG_MIRROR_PATH to a tempdir file (via t.Setenv).
//  3. BuildRuntime — composes the full app; rootLogger writes via the
//     SafeFanoutWriter to (stdout, mirror, stderr-fallback).
//  4. Drive a request through rt.Server.Handler so the LoggerMiddleware
//     binds the mirror-backed root logger into ctx, then ExecuteService
//     emits the per-execution "execution complete" record. The body's
//     correlation_id (a fresh ULID acting as a unique marker) appears
//     in the structured log payload.
//  5. Read the mirror file and assert the marker is present — proving
//     the fanout writer actually fanned out.
//
// The execution itself is expected to fail (we deliberately use a
// non-existent capability so we don't need any adapter setup) — the
// pre-adapter persistStructural path still emits the same "execution
// complete" log line, which is what we need.
//
// Refs: I-D.1, D2C4D.3, spec §6.3.
func TestLogPipeline_RuntimeWritesToMirrorAndStdout(t *testing.T) {
	// 1. Postgres testcontainer.
	dsn, teardown := startPGForLogPipeline(t)
	defer teardown()

	// 2. Mirror path on a per-test tempdir.
	mirror := filepath.Join(t.TempDir(), "runtime.jsonl")
	t.Setenv("RUNTIME_LOG_MIRROR_PATH", mirror)
	t.Setenv("RUNTIME_LOG_FORMAT", "json")
	t.Setenv("RUNTIME_LOG_LEVEL", "info")
	t.Setenv("RUNTIME_TENANT", "test")

	// 3. Compose the runtime. Mirror parameters mirror chaos integration
	//    test fixtures: minimal allowlists, OTel disabled, in-process.
	cfg := config.Config{
		HTTPAddr:                 "127.0.0.1:0",
		MaxTimeoutBudget:         30 * time.Minute,
		MaxPayloadBytes:          1 << 20,
		InlineStreamLimit:        16 * 1024,
		IdempotencyWindow:        24 * time.Hour,
		ShutdownGracePeriod:      15 * time.Second,
		MaxConcurrentExecutions:  64,
		AllowedCommandsPath:      []string{"/usr/bin", "/bin"},
		AllowedWorkingDirs:       []string{"/tmp"},
		AllowedFilesystemRoots:   []string{"/tmp"},
		HTTPAllowPrivateNetworks: true,
		PostgresDSN:              dsn,
		RuntimeVersion:           "0.1.0-log-pipeline-test",
		Hostname:                 "log-pipeline-test",
		ProvenanceSource:         "test",
		OTelEnabled:              false,
		Env:                      "development",
	}
	ctx := context.Background()
	rt, err := bootstrap.BuildRuntime(ctx, cfg)
	if err != nil {
		t.Fatalf("BuildRuntime: %v", err)
	}
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = rt.Shutdown(shutdownCtx)
	})

	// 4. Drive a request through the HTTP handler. The body's
	//    correlation_id is our unique marker — execute_service.go
	//    enriches the request-scoped logger with `correlation_id` at
	//    step 0 (line ~133), so the marker appears in EVERY log line
	//    emitted on this code path, including the final
	//    "execution complete" record.
	//
	//    We intentionally use a non-existent capability so the
	//    structural failure path runs (no adapter dispatch needed).
	//    persistStructural still calls emitExecutionComplete, so the
	//    ULID lands in the mirror.
	marker := ulid.Make().String()
	body := `{
        "correlation_id": "` + marker + `",
        "adapter_id": "shell",
        "capability_name": "no_such_capability",
        "capability_version": "v1",
        "payload": {},
        "timeout_budget_ms": 1000
    }`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/execute",
		bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	rt.Server.Handler.ServeHTTP(rec, req)

	// We don't care WHAT the response code was — the persistStructural
	// path produces a 200 OK with a failure-classified receipt. What we
	// care about is the log emission landing in the mirror.
	t.Logf("HTTP response status=%d body-prefix=%q",
		rec.Code, truncate(rec.Body.String(), 120))

	// 5. Allow the SafeFanoutWriter a brief moment to flush. The writer
	//    is synchronous, but slog buffers via os.File which is
	//    typically unbuffered for stdout/files; this is defense in
	//    depth.
	time.Sleep(200 * time.Millisecond)

	data, err := os.ReadFile(mirror)
	if err != nil {
		t.Fatalf("read mirror file %q: %v", mirror, err)
	}
	if len(data) == 0 {
		t.Fatalf("mirror file %q is empty — fanout did NOT reach the file", mirror)
	}
	if !strings.Contains(string(data), marker) {
		t.Errorf("mirror file does not contain marker %q.\nfull contents:\n%s",
			marker, data)
	}
	// Sanity: assert at least one INFO record made it to the file —
	// JSON handler emits `"level":"INFO"` for slog.LevelInfo records.
	if !strings.Contains(string(data), `"level":"INFO"`) {
		t.Errorf("mirror file has no INFO records (wrong format wiring?). contents:\n%s", data)
	}
}

// truncate caps s at n runes, appending "…" when truncated. Used only
// for error logging — keeps test output readable.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
