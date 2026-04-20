package shell

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newTestAdapter(t *testing.T) *Adapter {
	t.Helper()
	a, err := NewAdapter(Config{
		AllowedCommandsPath: []string{"/usr/bin", "/bin"},
		AllowedWorkingDirs:  []string{os.TempDir()},
		InlineStreamLimit:   16 * 1024,
	})
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}
	return a
}

func execCap(t *testing.T, a *Adapter) valueobjects.Capability {
	t.Helper()
	caps := a.Capabilities()
	if len(caps) != 1 {
		t.Fatalf("expected 1 capability, got %d", len(caps))
	}
	return caps[0]
}

func mustPayload(t *testing.T, raw json.RawMessage) valueobjects.Payload {
	t.Helper()
	p, err := valueobjects.NewPayload(valueobjects.ContentTypeJSON, raw, 0)
	if err != nil {
		t.Fatalf("NewPayload: %v", err)
	}
	return p
}

// ---------------------------------------------------------------------------
// Constructor
// ---------------------------------------------------------------------------

func TestNewAdapter_RejectsZeroInlineStreamLimit(t *testing.T) {
	_, err := NewAdapter(Config{
		AllowedCommandsPath: []string{"/bin"},
		AllowedWorkingDirs:  []string{os.TempDir()},
		InlineStreamLimit:   0,
	})
	if err == nil {
		t.Fatal("expected error for InlineStreamLimit=0, got nil")
	}
}

func TestNewAdapter_RejectsNegativeInlineStreamLimit(t *testing.T) {
	_, err := NewAdapter(Config{
		AllowedCommandsPath: []string{"/bin"},
		AllowedWorkingDirs:  []string{os.TempDir()},
		InlineStreamLimit:   -1,
	})
	if err == nil {
		t.Fatal("expected error for InlineStreamLimit=-1, got nil")
	}
}

// ---------------------------------------------------------------------------
// ID and Capabilities
// ---------------------------------------------------------------------------

func TestAdapter_IDReturnsShell(t *testing.T) {
	a := newTestAdapter(t)
	if got := a.ID().String(); got != "shell" {
		t.Errorf("ID() = %q, want %q", got, "shell")
	}
}

func TestAdapter_CapabilitiesReturns1WithCorrectCanonical(t *testing.T) {
	a := newTestAdapter(t)
	caps := a.Capabilities()
	if len(caps) != 1 {
		t.Fatalf("len(Capabilities()) = %d, want 1", len(caps))
	}
	c := caps[0]
	if got := c.Canonical(); got != "shell.exec@v1" {
		t.Errorf("Canonical() = %q, want %q", got, "shell.exec@v1")
	}
	if c.AllowsPartial() {
		t.Error("AllowsPartial() = true, want false")
	}
	if got := c.DefaultTimeout(); got != 30*time.Second {
		t.Errorf("DefaultTimeout() = %v, want 30s", got)
	}
}

func TestAdapter_CapabilitiesIsStable(t *testing.T) {
	a := newTestAdapter(t)
	first := a.Capabilities()
	second := a.Capabilities()
	if len(first) != len(second) {
		t.Fatalf("Capabilities() lengths differ: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i].Canonical() != second[i].Canonical() {
			t.Errorf("[%d] canonical mismatch: %q vs %q", i, first[i].Canonical(), second[i].Canonical())
		}
	}
}

// ---------------------------------------------------------------------------
// Execute — validation paths (no real subprocess)
// ---------------------------------------------------------------------------

func TestExecute_MalformedJSONProducesValidationRaw(t *testing.T) {
	a := newTestAdapter(t)
	cap := execCap(t, a)
	p := mustPayload(t, json.RawMessage(`{"unknown_field_xyz": 42}`))
	raw, err := a.Execute(context.Background(), cap, p)
	if err != nil {
		t.Fatalf("Execute returned Go error: %v", err)
	}
	r, ok := raw.(*execRaw)
	if !ok {
		t.Fatalf("raw type = %T, want *execRaw", raw)
	}
	if r.validation == "" {
		t.Error("validation field should be non-empty for unknown field")
	}
}

func TestExecute_MissingCommandProducesValidationRaw(t *testing.T) {
	a := newTestAdapter(t)
	cap := execCap(t, a)
	// command is empty string — resolveCommand rejects it.
	p := mustPayload(t, json.RawMessage(`{"command":""}`))
	raw, err := a.Execute(context.Background(), cap, p)
	if err != nil {
		t.Fatalf("Execute returned Go error: %v", err)
	}
	r, ok := raw.(*execRaw)
	if !ok {
		t.Fatalf("raw type = %T, want *execRaw", raw)
	}
	if r.validation == "" {
		t.Error("validation should be non-empty for empty command")
	}
}

func TestExecute_DisallowedCommandProducesValidationRaw(t *testing.T) {
	a, err := NewAdapter(Config{
		AllowedCommandsPath: []string{"/nonexistent_dir_xyzzy"},
		AllowedWorkingDirs:  []string{os.TempDir()},
		InlineStreamLimit:   16 * 1024,
	})
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}
	cap := execCap(t, a)
	p := mustPayload(t, json.RawMessage(`{"command":"echo"}`))
	raw, err := a.Execute(context.Background(), cap, p)
	if err != nil {
		t.Fatalf("Execute returned Go error: %v", err)
	}
	r := raw.(*execRaw)
	if r.validation == "" {
		t.Error("expected validation failure for disallowed command")
	}
}

func TestExecute_DisallowedWorkingDirProducesValidationRaw(t *testing.T) {
	a, err := NewAdapter(Config{
		AllowedCommandsPath: []string{"/usr/bin", "/bin"},
		AllowedWorkingDirs:  []string{"/tmp/only_this_dir_allowed"},
		InlineStreamLimit:   16 * 1024,
	})
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}
	cap := execCap(t, a)
	// Use /tmp directly (not the specific subdir).
	p := mustPayload(t, json.RawMessage(`{"command":"echo","args":["hi"],"working_dir":"/tmp"}`))
	raw, err := a.Execute(context.Background(), cap, p)
	if err != nil {
		t.Fatalf("Execute returned Go error: %v", err)
	}
	r := raw.(*execRaw)
	if r.validation == "" {
		t.Error("expected validation failure for disallowed working_dir")
	}
}

func TestExecute_UnknownCapabilityProducesValidationRaw(t *testing.T) {
	a := newTestAdapter(t)
	// Construct a different capability (same adapter ID but different name).
	id, _ := valueobjects.NewAdapterID("shell")
	c, _ := valueobjects.NewCapability(id, "nonexistent", "v1", false, 30*time.Second)
	p := mustPayload(t, json.RawMessage(`{"command":"echo"}`))
	raw, err := a.Execute(context.Background(), c, p)
	if err != nil {
		t.Fatalf("Execute returned Go error: %v", err)
	}
	r := raw.(*execRaw)
	if r.validation == "" {
		t.Error("expected validation failure for unknown capability")
	}
}

func TestExecute_PreCancelledCtxReturnsRaw(t *testing.T) {
	a := newTestAdapter(t)
	cap := execCap(t, a)
	p := mustPayload(t, json.RawMessage(`{"command":"echo","args":["hi"]}`))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	raw, err := a.Execute(ctx, cap, p)
	if err != nil {
		t.Fatalf("Execute returned Go error: %v", err)
	}
	if raw == nil {
		t.Fatal("raw is nil for cancelled ctx")
	}
	r := raw.(*execRaw)
	if r.ctxErr == nil {
		t.Error("ctxErr should be set for pre-cancelled ctx")
	}
}

// ---------------------------------------------------------------------------
// resolveCommand
// ---------------------------------------------------------------------------

func TestResolveCommand_AbsoluteExisting(t *testing.T) {
	// /bin/sh exists on all supported platforms.
	// If not, find something that does.
	target := "/bin/sh"
	if _, err := os.Stat(target); os.IsNotExist(err) {
		t.Skip("/bin/sh not present on this platform")
	}
	c := Config{AllowedCommandsPath: nil}
	got, err := c.resolveCommand(target)
	if err != nil {
		t.Fatalf("resolveCommand(%q): %v", target, err)
	}
	if got != target {
		t.Errorf("got %q, want %q", got, target)
	}
}

func TestResolveCommand_AbsoluteMissing(t *testing.T) {
	c := Config{}
	_, err := c.resolveCommand("/nonexistent_path_xyz/cmd")
	if err == nil {
		t.Error("expected error for missing absolute command")
	}
}

func TestResolveCommand_RelativeFoundInAllowlist(t *testing.T) {
	// Create a temp dir with a fake executable.
	dir := t.TempDir()
	fake := filepath.Join(dir, "myfakecmd")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write fake cmd: %v", err)
	}
	c := Config{AllowedCommandsPath: []string{dir}}
	got, err := c.resolveCommand("myfakecmd")
	if err != nil {
		t.Fatalf("resolveCommand: %v", err)
	}
	if got != fake {
		t.Errorf("got %q, want %q", got, fake)
	}
}

func TestResolveCommand_RelativeNotFound(t *testing.T) {
	c := Config{AllowedCommandsPath: []string{"/nonexistent_dir_xyzzy"}}
	_, err := c.resolveCommand("cmd")
	if err == nil {
		t.Error("expected error when command not found in allowlist")
	}
}

func TestResolveCommand_EmptyCommand(t *testing.T) {
	c := Config{}
	_, err := c.resolveCommand("")
	if err == nil {
		t.Error("expected error for empty command")
	}
}

// ---------------------------------------------------------------------------
// resolveWorkingDir
// ---------------------------------------------------------------------------

func TestResolveWorkingDir_EmptyAccepted(t *testing.T) {
	c := Config{AllowedWorkingDirs: nil}
	got, err := c.resolveWorkingDir("")
	if err != nil {
		t.Fatalf("resolveWorkingDir(''): %v", err)
	}
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

func TestResolveWorkingDir_RelativeRejected(t *testing.T) {
	c := Config{AllowedWorkingDirs: []string{"/tmp"}}
	_, err := c.resolveWorkingDir("relative/path")
	if err == nil {
		t.Error("expected error for relative working_dir")
	}
}

func TestResolveWorkingDir_AbsoluteWithinAllow(t *testing.T) {
	tmp := os.TempDir()
	sub := filepath.Join(tmp, "subdir_test_shell")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	c := Config{AllowedWorkingDirs: []string{tmp}}
	got, err := c.resolveWorkingDir(sub)
	if err != nil {
		t.Fatalf("resolveWorkingDir(%q): %v", sub, err)
	}
	if got != sub {
		t.Errorf("got %q, want %q", got, sub)
	}
}

func TestResolveWorkingDir_AbsoluteOutsideAllow(t *testing.T) {
	c := Config{AllowedWorkingDirs: []string{"/tmp/allowed_only"}}
	_, err := c.resolveWorkingDir("/etc")
	if err == nil {
		t.Error("expected error for /etc outside allowed dirs")
	}
}

func TestResolveWorkingDir_ExactRootAccepted(t *testing.T) {
	tmp := os.TempDir()
	// filepath.Abs normalizes away trailing slashes; the returned value
	// must equal filepath.Abs(tmp), not the raw os.TempDir() string.
	want, err := filepath.Abs(tmp)
	if err != nil {
		t.Fatalf("filepath.Abs: %v", err)
	}
	c := Config{AllowedWorkingDirs: []string{tmp}}
	got, err := c.resolveWorkingDir(tmp)
	if err != nil {
		t.Fatalf("resolveWorkingDir(%q): %v", tmp, err)
	}
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// buildEnv
// ---------------------------------------------------------------------------

func TestBuildEnv_ContainsBaseEnvKeys(t *testing.T) {
	// Set a guaranteed value so the check is not platform-dependent.
	t.Setenv("PATH", "/usr/bin:/bin")
	c := Config{}
	env := c.buildEnv(nil)
	found := false
	for _, e := range env {
		if e == "PATH=/usr/bin:/bin" {
			found = true
		}
	}
	if !found {
		t.Errorf("PATH not found in buildEnv output: %v", env)
	}
}

func TestBuildEnv_CallerOverrideTakesPrecedence(t *testing.T) {
	t.Setenv("PATH", "/original")
	c := Config{}
	env := c.buildEnv(map[string]string{"PATH": "/overridden"})
	found := false
	for _, e := range env {
		if e == "PATH=/overridden" {
			found = true
		}
		if e == "PATH=/original" {
			t.Error("original PATH should be overridden")
		}
	}
	if !found {
		t.Errorf("overridden PATH not found in env: %v", env)
	}
}

func TestBuildEnv_DoesNotContainSystemWideKeys(t *testing.T) {
	// Env keys NOT in baseEnvKeys should not appear.
	t.Setenv("MY_SECRET_KEY", "secret")
	c := Config{}
	env := c.buildEnv(nil)
	for _, e := range env {
		if len(e) >= 13 && e[:13] == "MY_SECRET_KEY" {
			t.Errorf("unexpected MY_SECRET_KEY in env: %v", env)
		}
	}
}

// ---------------------------------------------------------------------------
// Normalize
// ---------------------------------------------------------------------------

func TestNormalize_CtxCancelled(t *testing.T) {
	a := newTestAdapter(t)
	cap := execCap(t, a)
	clk := &shared.FakeClock{T: time.Now()}
	raw := &execRaw{ctxErr: context.Canceled, exitCode: -1}
	result, err := a.Normalize(cap, raw, clk)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if result.Status != valueobjects.StatusCancelled {
		t.Errorf("status = %q, want cancelled", result.Status)
	}
	if result.ErrorClass != valueobjects.ErrCancelled {
		t.Errorf("error_class = %q, want cancelled", result.ErrorClass)
	}
}

func TestNormalize_CtxDeadlineExceeded(t *testing.T) {
	a := newTestAdapter(t)
	cap := execCap(t, a)
	clk := &shared.FakeClock{T: time.Now()}
	raw := &execRaw{ctxErr: context.DeadlineExceeded, exitCode: -1}
	result, err := a.Normalize(cap, raw, clk)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if result.Status != valueobjects.StatusTimeout {
		t.Errorf("status = %q, want timeout", result.Status)
	}
	if result.ErrorClass != valueobjects.ErrTimeout {
		t.Errorf("error_class = %q, want timeout", result.ErrorClass)
	}
}

func TestNormalize_ValidationFailure(t *testing.T) {
	a := newTestAdapter(t)
	cap := execCap(t, a)
	clk := &shared.FakeClock{T: time.Now()}
	raw := &execRaw{validation: "command not allowed"}
	result, err := a.Normalize(cap, raw, clk)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if result.Status != valueobjects.StatusFailure {
		t.Errorf("status = %q, want failure", result.Status)
	}
	if result.ErrorClass != valueobjects.ErrValidationFailure {
		t.Errorf("error_class = %q, want validation_failure", result.ErrorClass)
	}
}

func TestNormalize_ExitZeroInExitSuccess(t *testing.T) {
	a := newTestAdapter(t)
	cap := execCap(t, a)
	clk := &shared.FakeClock{T: time.Now()}
	raw := &execRaw{exitCode: 0, exitSuccess: []int{0}, payloadDecoded: true}
	result, err := a.Normalize(cap, raw, clk)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if result.Status != valueobjects.StatusSuccess {
		t.Errorf("status = %q, want success", result.Status)
	}
	if result.ErrorClass != "" {
		t.Errorf("error_class = %q, want empty for success", result.ErrorClass)
	}
}

func TestNormalize_ExitOneNotInExitSuccess(t *testing.T) {
	a := newTestAdapter(t)
	cap := execCap(t, a)
	clk := &shared.FakeClock{T: time.Now()}
	raw := &execRaw{exitCode: 1, exitSuccess: []int{0}, payloadDecoded: true}
	result, err := a.Normalize(cap, raw, clk)
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

func TestNormalize_EmptyExitSuccessDefaultsToZero(t *testing.T) {
	a := newTestAdapter(t)
	cap := execCap(t, a)
	clk := &shared.FakeClock{T: time.Now()}
	// exitSuccess = nil (empty), exitCode = 0 — must be treated as success.
	raw := &execRaw{exitCode: 0, exitSuccess: []int{0}, payloadDecoded: true}
	result, err := a.Normalize(cap, raw, clk)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if result.Status != valueobjects.StatusSuccess {
		t.Errorf("status = %q, want success", result.Status)
	}
}

func TestNormalize_RejectsForeignRawType(t *testing.T) {
	a := newTestAdapter(t)
	cap := execCap(t, a)
	clk := &shared.FakeClock{T: time.Now()}
	_, err := a.Normalize(cap, &foreignRaw{}, clk)
	if err == nil {
		t.Error("expected error for foreign raw type, got nil")
	}
}

// foreignRaw is a test-only type to exercise the type-assertion guard.
type foreignRaw struct{}

func (*foreignRaw) IsAdapterRawOutcome() {}

func TestNormalize_CustomExitSuccessListAcceptsNonZero(t *testing.T) {
	a := newTestAdapter(t)
	cap := execCap(t, a)
	clk := &shared.FakeClock{T: time.Now()}
	// exit_success=[1,2], exitCode=2 → success
	raw := &execRaw{exitCode: 2, exitSuccess: []int{1, 2}, payloadDecoded: true}
	result, err := a.Normalize(cap, raw, clk)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if result.Status != valueobjects.StatusSuccess {
		t.Errorf("status = %q, want success", result.Status)
	}
}
