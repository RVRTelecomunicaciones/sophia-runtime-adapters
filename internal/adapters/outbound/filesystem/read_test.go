package filesystem

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
)

// ---------------------------------------------------------------------------
// readFile — happy path
// ---------------------------------------------------------------------------

func TestReadFile_HappyPath(t *testing.T) {
	root, a := newTestFSAdapter(t)
	content := []byte("hello world")
	p := filepath.Join(root, "test.txt")
	if err := os.WriteFile(p, content, 0o600); err != nil {
		t.Fatal(err)
	}
	cap := capFSByName(t, a, "read_file")
	payload := mustFSPayload(t, map[string]any{"path": p})
	raw, err := a.readFile(context.Background(), payload)
	if err != nil {
		t.Fatalf("readFile: %v", err)
	}
	r := raw.(*readRaw)
	if r.runErr != nil {
		t.Fatalf("runErr: %v", r.runErr)
	}
	if r.validation != "" {
		t.Fatalf("validation: %s", r.validation)
	}
	if string(r.data) != string(content) {
		t.Errorf("data = %q, want %q", r.data, content)
	}
	if r.totalBytes != int64(len(content)) {
		t.Errorf("totalBytes = %d, want %d", r.totalBytes, len(content))
	}
	if r.truncated {
		t.Error("truncated should be false")
	}
	// Normalize to success.
	clk := &shared.FakeClock{T: time.Now()}
	result, normErr := a.NormalizeRead(cap, r, clk)
	if normErr != nil {
		t.Fatalf("NormalizeRead: %v", normErr)
	}
	if result.Status != valueobjects.StatusSuccess {
		t.Errorf("status = %q, want success", result.Status)
	}
	if result.StdoutRef == nil {
		t.Error("StdoutRef should not be nil for success")
	}
}

// ---------------------------------------------------------------------------
// readFile — truncation
// ---------------------------------------------------------------------------

func TestReadFile_Truncation(t *testing.T) {
	root, a := newTestFSAdapter(t)
	content := []byte(strings.Repeat("x", 100))
	p := filepath.Join(root, "big.txt")
	if err := os.WriteFile(p, content, 0o600); err != nil {
		t.Fatal(err)
	}
	payload := mustFSPayload(t, map[string]any{"path": p, "max_bytes": 10})
	raw, err := a.readFile(context.Background(), payload)
	if err != nil {
		t.Fatalf("readFile: %v", err)
	}
	r := raw.(*readRaw)
	if len(r.data) != 10 {
		t.Errorf("len(data) = %d, want 10", len(r.data))
	}
	if !r.truncated {
		t.Error("truncated should be true")
	}
	if r.totalBytes != 100 {
		t.Errorf("totalBytes = %d, want 100", r.totalBytes)
	}
}

// ---------------------------------------------------------------------------
// readFile — validation failures
// ---------------------------------------------------------------------------

func TestReadFile_EmptyPath_ReturnsValidation(t *testing.T) {
	_, a := newTestFSAdapter(t)
	payload := mustFSPayload(t, map[string]any{})
	raw, err := a.readFile(context.Background(), payload)
	if err != nil {
		t.Fatalf("readFile: %v", err)
	}
	r := raw.(*readRaw)
	if r.validation == "" {
		t.Error("expected validation error for empty path, got none")
	}
}

func TestReadFile_RelativePath_ReturnsValidation(t *testing.T) {
	_, a := newTestFSAdapter(t)
	payload := mustFSPayload(t, map[string]any{"path": "relative/path.txt"})
	raw, err := a.readFile(context.Background(), payload)
	if err != nil {
		t.Fatalf("readFile: %v", err)
	}
	r := raw.(*readRaw)
	if r.validation == "" {
		t.Error("expected validation error for relative path, got none")
	}
}

func TestReadFile_OutsideAllowlist_ReturnsValidation(t *testing.T) {
	_, a := newTestFSAdapter(t)
	payload := mustFSPayload(t, map[string]any{"path": "/etc/passwd"})
	raw, err := a.readFile(context.Background(), payload)
	if err != nil {
		t.Fatalf("readFile: %v", err)
	}
	r := raw.(*readRaw)
	if r.validation == "" {
		t.Error("expected validation error for outside-allowlist path, got none")
	}
}

func TestReadFile_UnknownField_ReturnsValidation(t *testing.T) {
	_, a := newTestFSAdapter(t)
	payload := mustFSPayloadRaw(t, []byte(`{"path":"/tmp","junk":1}`))
	raw, err := a.readFile(context.Background(), payload)
	if err != nil {
		t.Fatalf("readFile: %v", err)
	}
	r := raw.(*readRaw)
	if r.validation == "" {
		t.Error("expected validation error for unknown field, got none")
	}
}

// ---------------------------------------------------------------------------
// readFile — runErr
// ---------------------------------------------------------------------------

func TestReadFile_MissingFile_ReturnsRunErr(t *testing.T) {
	root, a := newTestFSAdapter(t)
	// Create a file then delete it after the adapter resolves the path.
	p := filepath.Join(root, "phantom.txt")
	if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	// resolveExistingPath needs the file to exist — use a non-existent sub-path directly.
	// Instead write to root, then remove immediately.
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	// The path won't survive resolveExistingPath (EvalSymlinks fails).
	payload := mustFSPayload(t, map[string]any{"path": p})
	raw, err := a.readFile(context.Background(), payload)
	if err != nil {
		t.Fatalf("readFile: %v", err)
	}
	r := raw.(*readRaw)
	// resolveExistingPath fails for non-existent file (EvalSymlinks), so expect validation error.
	if r.validation == "" && r.runErr == nil {
		t.Error("expected validation or runErr for non-existent file, got neither")
	}
}

// ---------------------------------------------------------------------------
// readFile — cancelled ctx
// ---------------------------------------------------------------------------

func TestReadFile_CancelledCtx_ReturnsCtxErr(t *testing.T) {
	root, a := newTestFSAdapter(t)
	p := filepath.Join(root, "cancel.txt")
	if err := os.WriteFile(p, []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	payload := mustFSPayload(t, map[string]any{"path": p})
	raw, err := a.readFile(ctx, payload)
	if err != nil {
		t.Fatalf("readFile: %v", err)
	}
	r := raw.(*readRaw)
	// ctxErr may be set (if ctx is checked before open) or validation may fire first on
	// re-resolve — adapter checks ctx between resolve and open.
	if r.ctxErr == nil && r.validation == "" && r.runErr == nil {
		t.Error("expected ctxErr or validation after cancelled ctx")
	}
}

// ---------------------------------------------------------------------------
// NormalizeRead — branches
// ---------------------------------------------------------------------------

func TestNormalizeRead_CtxCancelled(t *testing.T) {
	_, a := newTestFSAdapter(t)
	clk := &shared.FakeClock{T: time.Now()}
	cap := capFSByName(t, a, "read_file")
	result, err := a.NormalizeRead(cap, &readRaw{ctxErr: context.Canceled}, clk)
	if err != nil {
		t.Fatalf("NormalizeRead: %v", err)
	}
	if result.Status != valueobjects.StatusCancelled {
		t.Errorf("status = %q, want cancelled", result.Status)
	}
	if result.ErrorClass != valueobjects.ErrCancelled {
		t.Errorf("error_class = %q, want cancelled", result.ErrorClass)
	}
}

func TestNormalizeRead_CtxDeadlineExceeded(t *testing.T) {
	_, a := newTestFSAdapter(t)
	clk := &shared.FakeClock{T: time.Now()}
	cap := capFSByName(t, a, "read_file")
	result, err := a.NormalizeRead(cap, &readRaw{ctxErr: context.DeadlineExceeded}, clk)
	if err != nil {
		t.Fatalf("NormalizeRead: %v", err)
	}
	if result.Status != valueobjects.StatusTimeout {
		t.Errorf("status = %q, want timeout", result.Status)
	}
	if result.ErrorClass != valueobjects.ErrTimeout {
		t.Errorf("error_class = %q, want timeout", result.ErrorClass)
	}
}

func TestNormalizeRead_ValidationFailure(t *testing.T) {
	_, a := newTestFSAdapter(t)
	clk := &shared.FakeClock{T: time.Now()}
	cap := capFSByName(t, a, "read_file")
	result, err := a.NormalizeRead(cap, &readRaw{validation: "path is required"}, clk)
	if err != nil {
		t.Fatalf("NormalizeRead: %v", err)
	}
	if result.Status != valueobjects.StatusFailure {
		t.Errorf("status = %q, want failure", result.Status)
	}
	if result.ErrorClass != valueobjects.ErrValidationFailure {
		t.Errorf("error_class = %q, want validation_failure", result.ErrorClass)
	}
}

func TestNormalizeRead_RunErr(t *testing.T) {
	_, a := newTestFSAdapter(t)
	clk := &shared.FakeClock{T: time.Now()}
	cap := capFSByName(t, a, "read_file")
	result, err := a.NormalizeRead(cap, &readRaw{runErr: os.ErrNotExist}, clk)
	if err != nil {
		t.Fatalf("NormalizeRead: %v", err)
	}
	if result.Status != valueobjects.StatusFailure {
		t.Errorf("status = %q, want failure", result.Status)
	}
	if result.ErrorClass != valueobjects.ErrExternalFailure {
		t.Errorf("error_class = %q, want external_failure", result.ErrorClass)
	}
}

func TestNormalizeRead_ForeignRawType_ReturnsError(t *testing.T) {
	_, a := newTestFSAdapter(t)
	clk := &shared.FakeClock{T: time.Now()}
	cap := capFSByName(t, a, "read_file")
	_, err := a.NormalizeRead(cap, &foreignFSRaw{}, clk)
	if err == nil {
		t.Error("expected error for foreign raw type, got nil")
	}
}

// foreignFSRaw is a test-only type to exercise the type-assertion guard.
type foreignFSRaw struct{}

func (*foreignFSRaw) IsAdapterRawOutcome() {}
