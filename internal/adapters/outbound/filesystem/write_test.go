package filesystem

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
)

// ---------------------------------------------------------------------------
// writeFile — happy path
// ---------------------------------------------------------------------------

func TestWriteFile_HappyPath_NewFile(t *testing.T) {
	root, a := newTestFSAdapter(t)
	target := filepath.Join(root, "output.txt")
	data := []byte("hello filesystem")
	cap := capFSByName(t, a, "write_file")
	payload := mustFSPayload(t, map[string]any{"path": target, "data": data})
	raw, err := a.writeFile(context.Background(), payload)
	if err != nil {
		t.Fatalf("writeFile: %v", err)
	}
	r := raw.(*writeRaw)
	if r.validation != "" {
		t.Fatalf("validation: %s", r.validation)
	}
	if r.runErr != nil {
		t.Fatalf("runErr: %v", r.runErr)
	}
	if r.bytesWritten != int64(len(data)) {
		t.Errorf("bytesWritten = %d, want %d", r.bytesWritten, len(data))
	}
	sum := sha256.Sum256(data)
	wantChecksum := hex.EncodeToString(sum[:])
	if r.checksumHex != wantChecksum {
		t.Errorf("checksumHex = %q, want %q", r.checksumHex, wantChecksum)
	}
	// File exists with correct content.
	got, err := os.ReadFile(r.resolvedPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("file content = %q, want %q", got, data)
	}
	// Normalize to success.
	clk := &shared.FakeClock{T: time.Now()}
	result, normErr := a.NormalizeWrite(cap, r, clk)
	if normErr != nil {
		t.Fatalf("NormalizeWrite: %v", normErr)
	}
	if result.Status != valueobjects.StatusSuccess {
		t.Errorf("status = %q, want success", result.Status)
	}
	if len(result.Artifacts) != 1 {
		t.Errorf("len(Artifacts) = %d, want 1", len(result.Artifacts))
	}
	art := result.Artifacts[0]
	if art.URI != "file://"+r.resolvedPath {
		t.Errorf("artifact URI = %q, want %q", art.URI, "file://"+r.resolvedPath)
	}
	if art.Checksum != wantChecksum {
		t.Errorf("artifact Checksum = %q, want %q", art.Checksum, wantChecksum)
	}
	if art.Attributes["mode"] != fmt.Sprintf("%o", r.mode) {
		t.Errorf("artifact mode attr = %q, want %q", art.Attributes["mode"], fmt.Sprintf("%o", r.mode))
	}
}

// ---------------------------------------------------------------------------
// writeFile — overwrite
// ---------------------------------------------------------------------------

func TestWriteFile_OverwriteFalse_ExistingFile_ReturnsValidation(t *testing.T) {
	root, a := newTestFSAdapter(t)
	target := filepath.Join(root, "existing.txt")
	if err := os.WriteFile(target, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	payload := mustFSPayload(t, map[string]any{"path": target, "data": []byte("new"), "overwrite": false})
	raw, err := a.writeFile(context.Background(), payload)
	if err != nil {
		t.Fatalf("writeFile: %v", err)
	}
	r := raw.(*writeRaw)
	if r.validation == "" {
		t.Error("expected validation error for overwrite=false on existing file, got none")
	}
	// Verify old content is untouched.
	got, _ := os.ReadFile(target)
	if string(got) != "old" {
		t.Errorf("file was modified unexpectedly: %q", got)
	}
}

func TestWriteFile_OverwriteTrue_ExistingFile_Succeeds(t *testing.T) {
	root, a := newTestFSAdapter(t)
	target := filepath.Join(root, "overwrite.txt")
	if err := os.WriteFile(target, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	newData := []byte("new content")
	payload := mustFSPayload(t, map[string]any{"path": target, "data": newData, "overwrite": true})
	raw, err := a.writeFile(context.Background(), payload)
	if err != nil {
		t.Fatalf("writeFile: %v", err)
	}
	r := raw.(*writeRaw)
	if r.validation != "" {
		t.Fatalf("validation: %s", r.validation)
	}
	if r.runErr != nil {
		t.Fatalf("runErr: %v", r.runErr)
	}
	got, _ := os.ReadFile(r.resolvedPath)
	if string(got) != string(newData) {
		t.Errorf("file content = %q, want %q", got, newData)
	}
}

// ---------------------------------------------------------------------------
// writeFile — create_dirs
// ---------------------------------------------------------------------------

func TestWriteFile_CreateDirsTrue_NonExistentParent_Succeeds(t *testing.T) {
	root, a := newTestFSAdapter(t)
	target := filepath.Join(root, "a", "b", "c", "file.txt")
	data := []byte("nested")
	payload := mustFSPayload(t, map[string]any{"path": target, "data": data, "create_dirs": true})
	raw, err := a.writeFile(context.Background(), payload)
	if err != nil {
		t.Fatalf("writeFile: %v", err)
	}
	r := raw.(*writeRaw)
	if r.runErr != nil {
		t.Fatalf("runErr: %v", r.runErr)
	}
	if r.validation != "" {
		t.Fatalf("validation: %s", r.validation)
	}
	got, readErr := os.ReadFile(r.resolvedPath)
	if readErr != nil {
		t.Fatalf("ReadFile: %v", readErr)
	}
	if string(got) != string(data) {
		t.Errorf("file content = %q, want %q", got, data)
	}
}

func TestWriteFile_CreateDirsFalse_NonExistentParent_ReturnsError(t *testing.T) {
	root, a := newTestFSAdapter(t)
	// Use a path whose parent does not exist.
	target := filepath.Join(root, "nonexistent_dir", "file.txt")
	payload := mustFSPayload(t, map[string]any{"path": target, "data": []byte("x"), "create_dirs": false})
	raw, err := a.writeFile(context.Background(), payload)
	if err != nil {
		t.Fatalf("writeFile: %v", err)
	}
	r := raw.(*writeRaw)
	// resolveTargetPath fails because parent doesn't exist AND is not in allowlist after EvalSymlinks failure.
	// OR the tmp create fails. Either way, must be an error.
	if r.validation == "" && r.runErr == nil {
		t.Error("expected validation or runErr for non-existent parent without create_dirs, got neither")
	}
}

// ---------------------------------------------------------------------------
// writeFile — validation
// ---------------------------------------------------------------------------

func TestWriteFile_EmptyPath_ReturnsValidation(t *testing.T) {
	_, a := newTestFSAdapter(t)
	payload := mustFSPayload(t, map[string]any{"data": []byte("x")})
	raw, err := a.writeFile(context.Background(), payload)
	if err != nil {
		t.Fatalf("writeFile: %v", err)
	}
	r := raw.(*writeRaw)
	if r.validation == "" {
		t.Error("expected validation error for empty path, got none")
	}
}

func TestWriteFile_RelativePath_ReturnsValidation(t *testing.T) {
	_, a := newTestFSAdapter(t)
	payload := mustFSPayload(t, map[string]any{"path": "relative/path.txt", "data": []byte("x")})
	raw, err := a.writeFile(context.Background(), payload)
	if err != nil {
		t.Fatalf("writeFile: %v", err)
	}
	r := raw.(*writeRaw)
	if r.validation == "" {
		t.Error("expected validation error for relative path, got none")
	}
}

func TestWriteFile_OutsideAllowlist_ReturnsValidation(t *testing.T) {
	_, a := newTestFSAdapter(t)
	payload := mustFSPayload(t, map[string]any{"path": "/etc/malicious.txt", "data": []byte("x")})
	raw, err := a.writeFile(context.Background(), payload)
	if err != nil {
		t.Fatalf("writeFile: %v", err)
	}
	r := raw.(*writeRaw)
	if r.validation == "" {
		t.Error("expected validation error for outside-allowlist path, got none")
	}
}

func TestWriteFile_UnknownField_ReturnsValidation(t *testing.T) {
	_, a := newTestFSAdapter(t)
	payload := mustFSPayloadRaw(t, []byte(`{"path":"/tmp/x","data":"","junk":1}`))
	raw, err := a.writeFile(context.Background(), payload)
	if err != nil {
		t.Fatalf("writeFile: %v", err)
	}
	r := raw.(*writeRaw)
	if r.validation == "" {
		t.Error("expected validation error for unknown field, got none")
	}
}

// ---------------------------------------------------------------------------
// writeFile — mode default
// ---------------------------------------------------------------------------

func TestWriteFile_DefaultMode_Is0600(t *testing.T) {
	root, a := newTestFSAdapter(t)
	target := filepath.Join(root, "mode_test.txt")
	payload := mustFSPayload(t, map[string]any{"path": target, "data": []byte("x")})
	raw, err := a.writeFile(context.Background(), payload)
	if err != nil {
		t.Fatalf("writeFile: %v", err)
	}
	r := raw.(*writeRaw)
	if r.mode != 0o600 {
		t.Errorf("mode = %o, want 600", r.mode)
	}
}

// ---------------------------------------------------------------------------
// writeFile — atomic check (no tmp file left on success)
// ---------------------------------------------------------------------------

func TestWriteFile_Atomic_NoTmpFileAfterSuccess(t *testing.T) {
	root, a := newTestFSAdapter(t)
	target := filepath.Join(root, "atomic.txt")
	data := []byte("atomic write")
	payload := mustFSPayload(t, map[string]any{"path": target, "data": data})
	raw, err := a.writeFile(context.Background(), payload)
	if err != nil {
		t.Fatalf("writeFile: %v", err)
	}
	r := raw.(*writeRaw)
	if r.runErr != nil {
		t.Fatalf("runErr: %v", r.runErr)
	}
	// Verify no .tmp. files remain in root.
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		if e.Name() != "atomic.txt" {
			t.Errorf("unexpected file remains: %q", e.Name())
		}
	}
}

// ---------------------------------------------------------------------------
// writeFile — cancelled ctx
// ---------------------------------------------------------------------------

func TestWriteFile_CancelledCtx_ReturnsCtxErrOrValidation(t *testing.T) {
	root, a := newTestFSAdapter(t)
	target := filepath.Join(root, "cancel_write.txt")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	payload := mustFSPayload(t, map[string]any{"path": target, "data": []byte("x")})
	raw, err := a.writeFile(ctx, payload)
	if err != nil {
		t.Fatalf("writeFile: %v", err)
	}
	r := raw.(*writeRaw)
	if r.ctxErr == nil && r.validation == "" && r.runErr == nil {
		t.Error("expected ctxErr, validation, or runErr for cancelled ctx, got none")
	}
}

// ---------------------------------------------------------------------------
// NormalizeWrite — branches
// ---------------------------------------------------------------------------

func TestNormalizeWrite_CtxCancelled(t *testing.T) {
	_, a := newTestFSAdapter(t)
	clk := &shared.FakeClock{T: time.Now()}
	cap := capFSByName(t, a, "write_file")
	result, err := a.NormalizeWrite(cap, &writeRaw{ctxErr: context.Canceled}, clk)
	if err != nil {
		t.Fatalf("NormalizeWrite: %v", err)
	}
	if result.Status != valueobjects.StatusCancelled {
		t.Errorf("status = %q, want cancelled", result.Status)
	}
}

func TestNormalizeWrite_CtxDeadlineExceeded(t *testing.T) {
	_, a := newTestFSAdapter(t)
	clk := &shared.FakeClock{T: time.Now()}
	cap := capFSByName(t, a, "write_file")
	result, err := a.NormalizeWrite(cap, &writeRaw{ctxErr: context.DeadlineExceeded}, clk)
	if err != nil {
		t.Fatalf("NormalizeWrite: %v", err)
	}
	if result.Status != valueobjects.StatusTimeout {
		t.Errorf("status = %q, want timeout", result.Status)
	}
}

func TestNormalizeWrite_ValidationFailure(t *testing.T) {
	_, a := newTestFSAdapter(t)
	clk := &shared.FakeClock{T: time.Now()}
	cap := capFSByName(t, a, "write_file")
	result, err := a.NormalizeWrite(cap, &writeRaw{validation: "path is required"}, clk)
	if err != nil {
		t.Fatalf("NormalizeWrite: %v", err)
	}
	if result.Status != valueobjects.StatusFailure {
		t.Errorf("status = %q, want failure", result.Status)
	}
	if result.ErrorClass != valueobjects.ErrValidationFailure {
		t.Errorf("error_class = %q, want validation_failure", result.ErrorClass)
	}
}

func TestNormalizeWrite_RunErr(t *testing.T) {
	_, a := newTestFSAdapter(t)
	clk := &shared.FakeClock{T: time.Now()}
	cap := capFSByName(t, a, "write_file")
	result, err := a.NormalizeWrite(cap, &writeRaw{runErr: os.ErrPermission}, clk)
	if err != nil {
		t.Fatalf("NormalizeWrite: %v", err)
	}
	if result.Status != valueobjects.StatusFailure {
		t.Errorf("status = %q, want failure", result.Status)
	}
	if result.ErrorClass != valueobjects.ErrExternalFailure {
		t.Errorf("error_class = %q, want external_failure", result.ErrorClass)
	}
}

func TestNormalizeWrite_ForeignRawType_ReturnsError(t *testing.T) {
	_, a := newTestFSAdapter(t)
	clk := &shared.FakeClock{T: time.Now()}
	cap := capFSByName(t, a, "write_file")
	_, err := a.NormalizeWrite(cap, &foreignFSRaw{}, clk)
	if err == nil {
		t.Error("expected error for foreign raw type, got nil")
	}
}
