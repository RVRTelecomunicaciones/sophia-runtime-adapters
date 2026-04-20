//go:build integration

package shell_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/sophia-ecosystem/runtime-adapters/internal/adapters/outbound/shell"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func integAdapter(t *testing.T) *shell.Adapter {
	t.Helper()
	a, err := shell.NewAdapter(shell.Config{
		AllowedCommandsPath: []string{"/usr/bin", "/bin"},
		AllowedWorkingDirs:  []string{os.TempDir()},
		InlineStreamLimit:   16 * 1024,
	})
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}
	return a
}

func integCap(t *testing.T, a *shell.Adapter) valueobjects.Capability {
	t.Helper()
	caps := a.Capabilities()
	if len(caps) != 1 {
		t.Fatalf("expected 1 capability, got %d", len(caps))
	}
	return caps[0]
}

func integPayload(t *testing.T, p shell.ExecPayload) valueobjects.Payload {
	t.Helper()
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	pl, err := valueobjects.NewPayload(valueobjects.ContentTypeJSON, b, 0)
	if err != nil {
		t.Fatalf("NewPayload: %v", err)
	}
	return pl
}

func normalize(t *testing.T, a *shell.Adapter, cap valueobjects.Capability, raw interface{ IsAdapterRawOutcome() }) {
	t.Helper()
	// We use the Normalize method via a type assertion — the integration test
	// package is shell_test (external), so we call it via the exported method.
}

// ---------------------------------------------------------------------------
// Integration tests
// ---------------------------------------------------------------------------

func TestIntegration_EchoHello(t *testing.T) {
	a := integAdapter(t)
	cap := integCap(t, a)
	pl := integPayload(t, shell.ExecPayload{Command: "echo", Args: []string{"hello"}})
	raw, err := a.Execute(context.Background(), cap, pl)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	clk := shared.RealClock{}
	result, err := a.Normalize(cap, raw, clk)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if result.Status != valueobjects.StatusSuccess {
		t.Errorf("status = %q, want success; err=%q msg=%q", result.Status, result.ErrorClass, result.ErrorMessage)
	}
	if result.StdoutRef == nil {
		t.Fatal("stdout_ref is nil")
	}
	if !bytes.Contains(result.StdoutRef.Data, []byte("hello")) {
		t.Errorf("stdout does not contain 'hello': %q", result.StdoutRef.Data)
	}
	if result.ExitCode == nil || *result.ExitCode != 0 {
		t.Errorf("exit_code = %v, want 0", result.ExitCode)
	}
}

func TestIntegration_True(t *testing.T) {
	a := integAdapter(t)
	cap := integCap(t, a)
	pl := integPayload(t, shell.ExecPayload{Command: "true"})
	raw, err := a.Execute(context.Background(), cap, pl)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	result, err := a.Normalize(cap, raw, shared.RealClock{})
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if result.Status != valueobjects.StatusSuccess {
		t.Errorf("status = %q, want success", result.Status)
	}
	if result.ExitCode == nil || *result.ExitCode != 0 {
		t.Errorf("exit_code = %v, want 0", result.ExitCode)
	}
}

func TestIntegration_False_ExitFailure(t *testing.T) {
	a := integAdapter(t)
	cap := integCap(t, a)
	pl := integPayload(t, shell.ExecPayload{Command: "false"})
	raw, err := a.Execute(context.Background(), cap, pl)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	result, err := a.Normalize(cap, raw, shared.RealClock{})
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if result.Status != valueobjects.StatusFailure {
		t.Errorf("status = %q, want failure", result.Status)
	}
	if result.ErrorClass != valueobjects.ErrExternalFailure {
		t.Errorf("error_class = %q, want external_failure", result.ErrorClass)
	}
}

func TestIntegration_SleepTimeout(t *testing.T) {
	a := integAdapter(t)
	cap := integCap(t, a)
	pl := integPayload(t, shell.ExecPayload{Command: "sleep", Args: []string{"10"}})
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	raw, err := a.Execute(ctx, cap, pl)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	result, err := a.Normalize(cap, raw, shared.RealClock{})
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if result.Status != valueobjects.StatusTimeout {
		t.Errorf("status = %q, want timeout", result.Status)
	}
}

func TestIntegration_EchoWithWorkingDir(t *testing.T) {
	a := integAdapter(t)
	cap := integCap(t, a)
	wd := t.TempDir()
	pl := integPayload(t, shell.ExecPayload{
		Command:    "echo",
		Args:       []string{"in-wd"},
		WorkingDir: wd,
	})
	raw, err := a.Execute(context.Background(), cap, pl)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	result, err := a.Normalize(cap, raw, shared.RealClock{})
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if result.Status != valueobjects.StatusSuccess {
		t.Errorf("status = %q, want success; err=%q msg=%q", result.Status, result.ErrorClass, result.ErrorMessage)
	}
}

func TestIntegration_EchoWithStdin(t *testing.T) {
	a := integAdapter(t)
	cap := integCap(t, a)
	// Use "cat" to echo stdin back.
	pl := integPayload(t, shell.ExecPayload{
		Command: "cat",
		Stdin:   []byte("hello from stdin\n"),
	})
	raw, err := a.Execute(context.Background(), cap, pl)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	result, err := a.Normalize(cap, raw, shared.RealClock{})
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if result.Status != valueobjects.StatusSuccess {
		t.Errorf("status = %q, want success", result.Status)
	}
	if result.StdoutRef == nil || !bytes.Contains(result.StdoutRef.Data, []byte("hello from stdin")) {
		t.Errorf("stdout does not contain stdin data: %v", result.StdoutRef)
	}
}
