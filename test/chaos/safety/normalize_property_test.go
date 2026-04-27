//go:build integration

// Package safety — Task 4.2: ResultNormalizer determinism + invariant property
// tests (spec §10.2, Bundle 4).
//
// Five test functions exercise each Phase 1 adapter normalizer:
//
//  1. TestNormalize_Determinism      — same (cap,raw,clock) → same result.
//  2. TestNormalize_StatusInClosedEnum — status ∈ {success,failure,timeout,cancelled,partial} (R15).
//  3. TestNormalize_FailureHasErrorClass — status=failure → error_class non-empty.
//  4. TestNormalize_RetryHintFollowsDefaultMapping — retry_hint==DefaultRetryHint(error_class).
//  5. TestNormalize_Status_NotPartial_ForNonPartialCapabilities — partial only from git.clone@v1 (D4.6).
//
// Raw outcome strategy: pre-cancelled / expired-deadline contexts are passed
// to Execute to obtain cancel/timeout raw shapes without real I/O.
// SyntheticOutcome provides adapter-synthetic chaos fault shapes.
// Safe no-side-effect executions (read /dev/null, run /bin/true) cover success
// paths where available.
//
// quick.Config{MaxCount: 1000} per test.
// Build tag: integration (mirrors Task 4.1 / B3 chaos integration tests).
package safety

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"testing/quick"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sophia-ecosystem/runtime-adapters/internal/adapters/outbound/filesystem"
	"github.com/sophia-ecosystem/runtime-adapters/internal/adapters/outbound/git"
	"github.com/sophia-ecosystem/runtime-adapters/internal/adapters/outbound/httpreq"
	"github.com/sophia-ecosystem/runtime-adapters/internal/adapters/outbound/shell"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/entities"
	domainservices "github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/services"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
	"github.com/sophia-ecosystem/runtime-adapters/internal/infrastructure/chaos"
)

// ---------------------------------------------------------------------------
// propResult — thin result carrier for property assertions.
//
// Holds the three fields checked by the invariant tests, plus the full
// entities.ExecutionResult for deep-equality determinism checks.
//
// reflect.DeepEqual is used (not fmt.Sprintf("%+v")) because ExecutionResult
// contains pointer fields (StdoutRef, StderrRef, ExitCode) whose addresses
// differ between calls even when the pointed-to values are identical.
// DeepEqual compares pointer contents, not addresses.
// ---------------------------------------------------------------------------

type propResult struct {
	status    valueobjects.ExecutionStatus
	errClass  valueobjects.ErrorClass
	retryHint valueobjects.RetryHint
	full      entities.ExecutionResult // used by determinism test via reflect.DeepEqual
}

// ---------------------------------------------------------------------------
// normFn is a typed per-scenario normalizer closure.
// It calls the adapter's Normalize method and wraps the result as propResult.
// ---------------------------------------------------------------------------

type normFn func(
	cap valueobjects.Capability,
	raw domainservices.AdapterRawOutcome,
	clk shared.Clock,
) (propResult, error)

// wrapShell wraps shell.Adapter.Normalize.
func wrapShell(a *shell.Adapter) normFn {
	return func(cap valueobjects.Capability, raw domainservices.AdapterRawOutcome, clk shared.Clock) (propResult, error) {
		r, err := a.Normalize(cap, raw, clk)
		if err != nil {
			return propResult{}, err
		}
		return toPropResult(r), nil
	}
}

// wrapGitStatus wraps git.Adapter.NormalizeStatus.
func wrapGitStatus(a *git.Adapter) normFn {
	return func(cap valueobjects.Capability, raw domainservices.AdapterRawOutcome, clk shared.Clock) (propResult, error) {
		r, err := a.NormalizeStatus(cap, raw, clk)
		if err != nil {
			return propResult{}, err
		}
		return toPropResult(r), nil
	}
}

// wrapGitClone wraps git.Adapter.NormalizeClone.
func wrapGitClone(a *git.Adapter) normFn {
	return func(cap valueobjects.Capability, raw domainservices.AdapterRawOutcome, clk shared.Clock) (propResult, error) {
		r, err := a.NormalizeClone(cap, raw, clk)
		if err != nil {
			return propResult{}, err
		}
		return toPropResult(r), nil
	}
}

// wrapGitDiff wraps git.Adapter.NormalizeDiff.
func wrapGitDiff(a *git.Adapter) normFn {
	return func(cap valueobjects.Capability, raw domainservices.AdapterRawOutcome, clk shared.Clock) (propResult, error) {
		r, err := a.NormalizeDiff(cap, raw, clk)
		if err != nil {
			return propResult{}, err
		}
		return toPropResult(r), nil
	}
}

// wrapGitCommit wraps git.Adapter.NormalizeCommit.
func wrapGitCommit(a *git.Adapter) normFn {
	return func(cap valueobjects.Capability, raw domainservices.AdapterRawOutcome, clk shared.Clock) (propResult, error) {
		r, err := a.NormalizeCommit(cap, raw, clk)
		if err != nil {
			return propResult{}, err
		}
		return toPropResult(r), nil
	}
}

// wrapFSRead wraps filesystem.Adapter.NormalizeRead.
func wrapFSRead(a *filesystem.Adapter) normFn {
	return func(cap valueobjects.Capability, raw domainservices.AdapterRawOutcome, clk shared.Clock) (propResult, error) {
		r, err := a.NormalizeRead(cap, raw, clk)
		if err != nil {
			return propResult{}, err
		}
		return toPropResult(r), nil
	}
}

// wrapFSWrite wraps filesystem.Adapter.NormalizeWrite.
func wrapFSWrite(a *filesystem.Adapter) normFn {
	return func(cap valueobjects.Capability, raw domainservices.AdapterRawOutcome, clk shared.Clock) (propResult, error) {
		r, err := a.NormalizeWrite(cap, raw, clk)
		if err != nil {
			return propResult{}, err
		}
		return toPropResult(r), nil
	}
}

// wrapHTTP wraps httpreq.Adapter.Normalize.
func wrapHTTP(a *httpreq.Adapter) normFn {
	return func(cap valueobjects.Capability, raw domainservices.AdapterRawOutcome, clk shared.Clock) (propResult, error) {
		r, err := a.Normalize(cap, raw, clk)
		if err != nil {
			return propResult{}, err
		}
		return toPropResult(r), nil
	}
}

// toPropResult extracts the invariant-relevant fields from a full result.
func toPropResult(r entities.ExecutionResult) propResult {
	return propResult{
		status:    r.Status,
		errClass:  r.ErrorClass,
		retryHint: r.Retryable,
		full:      r,
	}
}

// ---------------------------------------------------------------------------
// propScenario — one (cap, raw, normFn, label) entry.
// ---------------------------------------------------------------------------

type propScenario struct {
	label string
	cap   valueobjects.Capability
	raw   domainservices.AdapterRawOutcome
	norm  normFn
}

// ---------------------------------------------------------------------------
// Adapter constructors
// ---------------------------------------------------------------------------

const propInlineStreamLimit = 4096

var propFixedClock = &shared.FakeClock{T: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}

func newShellAdapter(t *testing.T) *shell.Adapter {
	t.Helper()
	a, err := shell.NewAdapter(shell.Config{
		AllowedCommandsPath: []string{"/bin", "/usr/bin"},
		AllowedWorkingDirs:  []string{os.TempDir()},
		InlineStreamLimit:   propInlineStreamLimit,
	})
	require.NoError(t, err)
	return a
}

func newGitAdapter(t *testing.T) *git.Adapter {
	t.Helper()
	a, err := git.NewAdapter(git.Config{
		AllowedWorkingDirs: []string{os.TempDir(), "/"},
		SSHAgent:           false,
		InlineStreamLimit:  propInlineStreamLimit,
	}, propFixedClock)
	require.NoError(t, err)
	return a
}

func newGitAdapterWithRoot(t *testing.T, root string) *git.Adapter {
	t.Helper()
	a, err := git.NewAdapter(git.Config{
		AllowedWorkingDirs: []string{root, os.TempDir()},
		SSHAgent:           false,
		InlineStreamLimit:  propInlineStreamLimit,
	}, propFixedClock)
	require.NoError(t, err)
	return a
}

func newFSAdapter(t *testing.T) *filesystem.Adapter {
	t.Helper()
	a, err := filesystem.NewAdapter(filesystem.Config{
		AllowedFilesystemRoots: []string{os.TempDir(), "/dev"},
		InlineStreamLimit:      propInlineStreamLimit,
	}, propFixedClock)
	require.NoError(t, err)
	return a
}

func newFSAdapterWithRoot(t *testing.T, root string) *filesystem.Adapter {
	t.Helper()
	a, err := filesystem.NewAdapter(filesystem.Config{
		AllowedFilesystemRoots: []string{root},
		InlineStreamLimit:      propInlineStreamLimit,
	}, propFixedClock)
	require.NoError(t, err)
	return a
}

func newHTTPAdapter(t *testing.T) *httpreq.Adapter {
	t.Helper()
	a, err := httpreq.NewAdapter(httpreq.Config{
		MaxBodyBytes:      1024,
		MaxRedirects:      5,
		InlineStreamLimit: propInlineStreamLimit,
	}, propFixedClock)
	require.NoError(t, err)
	return a
}

// ---------------------------------------------------------------------------
// Capability lookup
// ---------------------------------------------------------------------------

func phase1Cap(t *testing.T, canonical string) valueobjects.Capability {
	t.Helper()
	caps, err := valueobjects.NewPhase1Capabilities()
	require.NoError(t, err)
	for _, c := range caps {
		if c.Canonical() == canonical {
			return c
		}
	}
	t.Fatalf("capability %q not in Phase 1 catalog", canonical)
	return valueobjects.Capability{}
}

// ---------------------------------------------------------------------------
// Payload helpers
// ---------------------------------------------------------------------------

func jsonPayload(t *testing.T, body string) valueobjects.Payload {
	t.Helper()
	p, err := valueobjects.NewPayload(valueobjects.ContentTypeJSON, []byte(body), 0)
	require.NoError(t, err)
	return p
}

// emptyObjectPayload returns a `{}` payload — valid JSON with no fields.
// When passed to any Phase 1 adapter it triggers the adapter's own semantic
// validation (missing required fields such as command/path/url), producing
// a validation-failure raw outcome. Cannot use truly-invalid JSON because
// valueobjects.NewPayload validates JSON at the envelope level.
func emptyObjectPayload(t *testing.T) valueobjects.Payload {
	t.Helper()
	return jsonPayload(t, `{}`)
}

// cancelCtx returns a context that is already cancelled.
func cancelCtx() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

// timeoutCtx returns a context whose deadline has already expired.
func timeoutCtx() context.Context {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	_ = cancel
	return ctx
}

// ---------------------------------------------------------------------------
// buildScenarios — the full scenario matrix for all 4 adapters × ~15 buckets.
// ---------------------------------------------------------------------------

// buildScenarios constructs all (cap, raw, normFn) triples. Raw outcomes are
// obtained exclusively through Execute (with pre-cancelled / expired contexts)
// or SyntheticOutcome — never by constructing unexported raw types directly.
func buildScenarios(t *testing.T) []propScenario {
	t.Helper()

	shellA := newShellAdapter(t)
	gitA := newGitAdapter(t)
	fsA := newFSAdapter(t)
	httpA := newHTTPAdapter(t)

	var ss []propScenario

	add := func(label string, cap valueobjects.Capability, raw domainservices.AdapterRawOutcome, fn normFn) {
		ss = append(ss, propScenario{label: label, cap: cap, raw: raw, norm: fn})
	}

	// ---- shell.exec@v1 ----
	{
		cap := phase1Cap(t, "shell.exec@v1")
		fn := wrapShell(shellA)
		pl := jsonPayload(t, `{"command":"/bin/true"}`)

		// cancel / timeout — no real subprocess spawned (pre-flight check fires).
		raw, err := shellA.Execute(cancelCtx(), cap, pl)
		require.NoError(t, err)
		add("shell/cancel", cap, raw, fn)

		raw, err = shellA.Execute(timeoutCtx(), cap, pl)
		require.NoError(t, err)
		add("shell/timeout", cap, raw, fn)

		// validation: empty command field → adapter's own semantic validation fires.
		// Use `{}` (valid JSON, missing required "command" field) because
		// valueobjects.NewPayload rejects truly-invalid JSON at the envelope level.
		raw, err = shellA.Execute(context.Background(), cap, emptyObjectPayload(t))
		require.NoError(t, err)
		add("shell/validation-empty-cmd", cap, raw, fn)

		// validation: command not in allowlist
		raw, err = shellA.Execute(context.Background(), cap, jsonPayload(t, `{"command":"/not/allowed/cmd"}`))
		require.NoError(t, err)
		add("shell/validation-disallowed", cap, raw, fn)

		// chaos: process_signal (exit=-1, runErr set — no real subprocess)
		if synth, ok := shellA.SyntheticOutcome(context.Background(), cap, pl, chaos.FaultConfig{Kind: chaos.FaultProcessSignal}); ok {
			add("shell/chaos-signal", cap, synth, fn)
		}

		// chaos: process_exit (non-zero exit, not in exitSuccess)
		if synth, ok := shellA.SyntheticOutcome(context.Background(), cap, pl, chaos.FaultConfig{Kind: chaos.FaultProcessExit}); ok {
			add("shell/chaos-exit", cap, synth, fn)
		}

		// success: run /bin/true or /usr/bin/true if available — exits 0, no output.
		for _, cmd := range []string{"/bin/true", "/usr/bin/true"} {
			if _, statErr := os.Stat(cmd); statErr == nil {
				raw, err = shellA.Execute(context.Background(), cap, jsonPayload(t, fmt.Sprintf(`{"command":%q}`, cmd)))
				require.NoError(t, err)
				add("shell/success", cap, raw, fn)
				break
			}
		}
	}

	// ---- git.status@v1 ----
	{
		cap := phase1Cap(t, "git.status@v1")
		fn := wrapGitStatus(gitA)
		pl := jsonPayload(t, `{"repo_path":"/tmp"}`)

		raw, err := gitA.Execute(cancelCtx(), cap, pl)
		require.NoError(t, err)
		add("git.status/cancel", cap, raw, fn)

		raw, err = gitA.Execute(timeoutCtx(), cap, pl)
		require.NoError(t, err)
		add("git.status/timeout", cap, raw, fn)

		// validation: {} → repo_path="" → resolvePath("") returns error
		raw, err = gitA.Execute(context.Background(), cap, emptyObjectPayload(t))
		require.NoError(t, err)
		add("git.status/validation", cap, raw, fn)

		// external failure: non-git path → go-git returns error
		raw, err = gitA.Execute(context.Background(), cap, jsonPayload(t, `{"repo_path":"/tmp/not-a-repo-xyzzy99"}`))
		require.NoError(t, err)
		add("git.status/external-failure", cap, raw, fn)

		// success: run status on the module repo root if accessible.
		if root := moduleRoot(); root != "" {
			gitAForRoot := newGitAdapterWithRoot(t, root)
			fnForRoot := wrapGitStatus(gitAForRoot)
			raw2, err2 := gitAForRoot.Execute(context.Background(), cap, jsonPayload(t, fmt.Sprintf(`{"repo_path":%q}`, root)))
			if err2 == nil {
				add("git.status/success", cap, raw2, fnForRoot)
			}
		}
	}

	// ---- git.clone@v1 ----
	{
		cap := phase1Cap(t, "git.clone@v1")
		fn := wrapGitClone(gitA)
		pl := jsonPayload(t, `{"repo_url":"https://github.com/x/y","destination_path":"/tmp/noop-prop-test"}`)

		raw, err := gitA.Execute(cancelCtx(), cap, pl)
		require.NoError(t, err)
		add("git.clone/cancel", cap, raw, fn)

		raw, err = gitA.Execute(timeoutCtx(), cap, pl)
		require.NoError(t, err)
		add("git.clone/timeout", cap, raw, fn)

		// validation: {} → repo_url="" and destination_path="" → validation error
		raw, err = gitA.Execute(context.Background(), cap, emptyObjectPayload(t))
		require.NoError(t, err)
		add("git.clone/validation", cap, raw, fn)

		if synth, ok := gitA.SyntheticOutcome(context.Background(), cap,
			jsonPayload(t, `{"repo_url":"https://github.com/x/y"}`),
			chaos.FaultConfig{Kind: chaos.FaultRemoteUnreachable}); ok {
			add("git.clone/chaos-unreachable", cap, synth, fn)
		}
		if synth, ok := gitA.SyntheticOutcome(context.Background(), cap,
			jsonPayload(t, `{"repo_url":"https://github.com/x/y"}`),
			chaos.FaultConfig{Kind: chaos.FaultAuthFailure}); ok {
			add("git.clone/chaos-auth", cap, synth, fn)
		}
		// Note: partial (D4.6) scenario requires a real partial clone (fetchOK=true,
		// checkoutOK=false). Triggering this deterministically without a git server
		// or network is not feasible in a no-side-effect property test.
		// D4.6 partial classification logic is exercised by
		// TestPartialClassification_Classify_TruthTable (entities/partial_rule_test.go)
		// and by the git adapter integration tests (git/clone_test.go).
	}

	// ---- git.diff@v1 ----
	{
		cap := phase1Cap(t, "git.diff@v1")
		fn := wrapGitDiff(gitA)
		pl := jsonPayload(t, `{"repo_path":"/tmp"}`)

		raw, err := gitA.Execute(cancelCtx(), cap, pl)
		require.NoError(t, err)
		add("git.diff/cancel", cap, raw, fn)

		raw, err = gitA.Execute(timeoutCtx(), cap, pl)
		require.NoError(t, err)
		add("git.diff/timeout", cap, raw, fn)

		// validation: {} → repo_path="" → resolvePath("") validation error
		raw, err = gitA.Execute(context.Background(), cap, emptyObjectPayload(t))
		require.NoError(t, err)
		add("git.diff/validation", cap, raw, fn)

		raw, err = gitA.Execute(context.Background(), cap, jsonPayload(t, `{"repo_path":"/tmp/not-a-repo-xyzzy99"}`))
		require.NoError(t, err)
		add("git.diff/external-failure", cap, raw, fn)
	}

	// ---- git.commit@v1 ----
	{
		cap := phase1Cap(t, "git.commit@v1")
		fn := wrapGitCommit(gitA)
		pl := jsonPayload(t, `{"repo_path":"/tmp","message":"m","author_name":"a","author_email":"a@b.com"}`)

		raw, err := gitA.Execute(cancelCtx(), cap, pl)
		require.NoError(t, err)
		add("git.commit/cancel", cap, raw, fn)

		raw, err = gitA.Execute(timeoutCtx(), cap, pl)
		require.NoError(t, err)
		add("git.commit/timeout", cap, raw, fn)

		// validation: {} → repo_path="" → resolvePath("") validation error
		raw, err = gitA.Execute(context.Background(), cap, emptyObjectPayload(t))
		require.NoError(t, err)
		add("git.commit/validation", cap, raw, fn)

		raw, err = gitA.Execute(context.Background(), cap, jsonPayload(t, `{"repo_path":"/tmp/not-a-repo-xyzzy99","message":"m","author_name":"a","author_email":"a@b.com"}`))
		require.NoError(t, err)
		add("git.commit/external-failure", cap, raw, fn)
	}

	// ---- filesystem.read_file@v1 ----
	{
		cap := phase1Cap(t, "filesystem.read_file@v1")
		fn := wrapFSRead(fsA)
		pl := jsonPayload(t, `{"path":"/dev/null"}`)

		raw, err := fsA.Execute(cancelCtx(), cap, pl)
		require.NoError(t, err)
		add("fs.read/cancel", cap, raw, fn)

		raw, err = fsA.Execute(timeoutCtx(), cap, pl)
		require.NoError(t, err)
		add("fs.read/timeout", cap, raw, fn)

		// validation: {} → path="" → adapter's path validation fires
		raw, err = fsA.Execute(context.Background(), cap, emptyObjectPayload(t))
		require.NoError(t, err)
		add("fs.read/validation", cap, raw, fn)

		if synth, ok := fsA.SyntheticOutcome(context.Background(), cap, pl,
			chaos.FaultConfig{Kind: chaos.FaultEIO}); ok {
			add("fs.read/chaos-eio", cap, synth, fn)
		}
		if synth, ok := fsA.SyntheticOutcome(context.Background(), cap, pl,
			chaos.FaultConfig{Kind: chaos.FaultPermissionDenied}); ok {
			add("fs.read/chaos-permdeny", cap, synth, fn)
		}
		// success: read /dev/null — always present, 0 bytes, no side effects.
		raw, err = fsA.Execute(context.Background(), cap, pl)
		require.NoError(t, err)
		add("fs.read/success", cap, raw, fn)
	}

	// ---- filesystem.write_file@v1 ----
	{
		cap := phase1Cap(t, "filesystem.write_file@v1")
		tmpDir := t.TempDir()
		fsAWrite := newFSAdapterWithRoot(t, tmpDir)
		fn := wrapFSWrite(fsAWrite)
		writeTarget := fmt.Sprintf("%s/prop_write.bin", tmpDir)
		pl := jsonPayload(t, fmt.Sprintf(`{"path":%q,"data":"aGVsbG8="}`, writeTarget))
		plOverwrite := jsonPayload(t, fmt.Sprintf(`{"path":%q,"data":"aGVsbG8=","overwrite":true}`, writeTarget))

		raw, err := fsAWrite.Execute(cancelCtx(), cap, pl)
		require.NoError(t, err)
		add("fs.write/cancel", cap, raw, fn)

		raw, err = fsAWrite.Execute(timeoutCtx(), cap, pl)
		require.NoError(t, err)
		add("fs.write/timeout", cap, raw, fn)

		// validation: {} → path="" → adapter's path validation fires
		raw, err = fsAWrite.Execute(context.Background(), cap, emptyObjectPayload(t))
		require.NoError(t, err)
		add("fs.write/validation", cap, raw, fn)

		if synth, ok := fsAWrite.SyntheticOutcome(context.Background(), cap, pl,
			chaos.FaultConfig{Kind: chaos.FaultEIO}); ok {
			add("fs.write/chaos-eio", cap, synth, fn)
		}
		if synth, ok := fsAWrite.SyntheticOutcome(context.Background(), cap, pl,
			chaos.FaultConfig{Kind: chaos.FaultENOSPC}); ok {
			add("fs.write/chaos-enospc", cap, synth, fn)
		}
		if synth, ok := fsAWrite.SyntheticOutcome(context.Background(), cap, pl,
			chaos.FaultConfig{Kind: chaos.FaultPermissionDenied}); ok {
			add("fs.write/chaos-permdeny", cap, synth, fn)
		}
		// success: write to the temp dir — atomic tmp+fsync+rename.
		raw, err = fsAWrite.Execute(context.Background(), cap, plOverwrite)
		require.NoError(t, err)
		add("fs.write/success", cap, raw, fn)
	}

	// ---- http.request@v1 ----
	{
		cap := phase1Cap(t, "http.request@v1")
		fn := wrapHTTP(httpA)
		pl := jsonPayload(t, `{"method":"GET","url":"http://localhost:0"}`)

		raw, err := httpA.Execute(cancelCtx(), cap, pl)
		require.NoError(t, err)
		add("http/cancel", cap, raw, fn)

		raw, err = httpA.Execute(timeoutCtx(), cap, pl)
		require.NoError(t, err)
		add("http/timeout", cap, raw, fn)

		// validation: {} → method="" or url="" → adapter's validation fires
		raw, err = httpA.Execute(context.Background(), cap, emptyObjectPayload(t))
		require.NoError(t, err)
		add("http/validation", cap, raw, fn)

		httpChaosFaults := []struct {
			label string
			kind  chaos.FaultKind
		}{
			{"http/chaos-connection-reset", chaos.FaultConnectionReset},
			{"http/chaos-slow-body", chaos.FaultSlowBody},
			{"http/chaos-redirect-loop", chaos.FaultRedirectLoop},
			{"http/chaos-tls-fail", chaos.FaultTLSHandshakeFail},
			{"http/chaos-auth-failure", chaos.FaultAuthFailure},
			{"http/chaos-unreachable", chaos.FaultRemoteUnreachable},
		}
		httpPL := jsonPayload(t, `{"method":"GET","url":"http://example.com","expected_status":[200]}`)
		for _, f := range httpChaosFaults {
			if synth, ok := httpA.SyntheticOutcome(context.Background(), cap, httpPL,
				chaos.FaultConfig{Kind: f.kind}); ok {
				add(f.label, cap, synth, fn)
			}
		}
	}

	require.NotEmpty(t, ss, "buildScenarios: no scenarios built — check adapter constructors")
	return ss
}

// moduleRoot returns the absolute path of the module root for this repo.
// Used to obtain a real git repo for the git.status@v1 success scenario.
//
// Walks up from the current working directory until it finds go.mod
// (mirrors the findModuleRoot pattern used by chaos.LoadProfile).
// Portable across developer machines and CI runners. Returns "" if
// go.mod isn't found (the success-scenario consumer treats this as
// a soft skip; other scenarios for the same capability still populate).
func moduleRoot() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	dir := cwd
	for i := 0; i < 20; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// ---------------------------------------------------------------------------
// TestNormalize_Determinism (§10.2 property 1)
//
// Same (cap, raw, clock) ⇒ same ExecutionResult across two identical calls.
// This catches any non-determinism introduced by wall-clock reads, random
// IDs, or mutable adapter state.
// ---------------------------------------------------------------------------

func TestNormalize_Determinism(t *testing.T) {
	scenarios := buildScenarios(t)
	clk := &shared.FakeClock{T: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}

	cfg := &quick.Config{MaxCount: 1000}
	err := quick.Check(func(seed uint16) bool {
		s := scenarios[int(seed)%len(scenarios)]

		r1, e1 := s.norm(s.cap, s.raw, clk)
		r2, e2 := s.norm(s.cap, s.raw, clk)

		if e1 != nil || e2 != nil {
			t.Errorf("BLOCKED (fuzzer bug): scenario %q: Normalize returned structural error: e1=%v e2=%v",
				s.label, e1, e2)
			return false
		}
		// Use reflect.DeepEqual to compare pointer contents rather than pointer
		// addresses. fmt.Sprintf("%+v") would include addresses and fail falsely
		// for pointer fields (StdoutRef, StderrRef, ExitCode) that are
		// value-equal but allocated separately on each call.
		if !reflect.DeepEqual(r1.full, r2.full) {
			t.Errorf("INVARIANT VIOLATION (determinism): scenario %q\n  call1 = %+v\n  call2 = %+v",
				s.label, r1.full, r2.full)
			return false
		}
		return true
	}, cfg)
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// TestNormalize_StatusInClosedEnum (§10.2 property 2 / R15)
//
// result.Status ∈ {success, failure, timeout, cancelled, partial}.
// No "unknown" or other string is permitted.
// ---------------------------------------------------------------------------

func TestNormalize_StatusInClosedEnum(t *testing.T) {
	scenarios := buildScenarios(t)
	clk := &shared.FakeClock{T: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}

	validStatuses := map[valueobjects.ExecutionStatus]struct{}{
		valueobjects.StatusSuccess:   {},
		valueobjects.StatusFailure:   {},
		valueobjects.StatusTimeout:   {},
		valueobjects.StatusCancelled: {},
		valueobjects.StatusPartial:   {},
	}

	cfg := &quick.Config{MaxCount: 1000}
	err := quick.Check(func(seed uint16) bool {
		s := scenarios[int(seed)%len(scenarios)]
		r, nErr := s.norm(s.cap, s.raw, clk)
		if nErr != nil {
			t.Errorf("BLOCKED (fuzzer bug): scenario %q: Normalize error: %v", s.label, nErr)
			return false
		}
		if _, ok := validStatuses[r.status]; !ok {
			t.Errorf("INVARIANT VIOLATION (R15): scenario %q produced status=%q (not in closed enum)",
				s.label, r.status)
			return false
		}
		return true
	}, cfg)
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// TestNormalize_FailureHasErrorClass (§10.2 property 3)
//
// status=failure ⇒ error_class non-empty.
// status=partial ⇒ error_class non-empty (D4.6 partial carries ExternalFailure).
// status=success ⇒ error_class empty (consistency invariant §5.9).
// ---------------------------------------------------------------------------

func TestNormalize_FailureHasErrorClass(t *testing.T) {
	scenarios := buildScenarios(t)
	clk := &shared.FakeClock{T: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}

	cfg := &quick.Config{MaxCount: 1000}
	err := quick.Check(func(seed uint16) bool {
		s := scenarios[int(seed)%len(scenarios)]
		r, nErr := s.norm(s.cap, s.raw, clk)
		if nErr != nil {
			t.Errorf("BLOCKED (fuzzer bug): scenario %q: Normalize error: %v", s.label, nErr)
			return false
		}

		switch r.status {
		case valueobjects.StatusFailure, valueobjects.StatusPartial:
			if r.errClass == "" {
				t.Errorf("INVARIANT VIOLATION: scenario %q status=%v but error_class is empty",
					s.label, r.status)
				return false
			}
		case valueobjects.StatusSuccess:
			if r.errClass != "" {
				t.Errorf("INVARIANT VIOLATION: scenario %q status=success but error_class=%q (must be empty)",
					s.label, r.errClass)
				return false
			}
		}
		return true
	}, cfg)
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// TestNormalize_RetryHintFollowsDefaultMapping (§10.2 property 4)
//
// For all outcomes with non-empty error_class, retry_hint must equal
// DefaultRetryHint(error_class) OR be an intentionally documented override.
//
// KNOWN OVERRIDE: httpreq normalizer emits HintRetryable for HTTP 5xx
// responses (error_class=external_failure). DefaultRetryHint(ErrExternalFailure)
// = HintUnknown. This is an intentional adapter-level override per spec §8.5
// ("Adapters may override with more specific information in the raw outcome").
// The test skips the DefaultRetryHint check for all http scenarios and instead
// only verifies the hint is a valid enum member (which is always true).
//
// All non-HTTP adapters must satisfy the default mapping exactly.
// ---------------------------------------------------------------------------

func TestNormalize_RetryHintFollowsDefaultMapping(t *testing.T) {
	scenarios := buildScenarios(t)
	clk := &shared.FakeClock{T: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}

	validHints := map[valueobjects.RetryHint]struct{}{
		valueobjects.HintRetryable:    {},
		valueobjects.HintNonRetryable: {},
		valueobjects.HintUnknown:      {},
	}

	cfg := &quick.Config{MaxCount: 1000}
	err := quick.Check(func(seed uint16) bool {
		s := scenarios[int(seed)%len(scenarios)]
		r, nErr := s.norm(s.cap, s.raw, clk)
		if nErr != nil {
			t.Errorf("BLOCKED (fuzzer bug): scenario %q: Normalize error: %v", s.label, nErr)
			return false
		}

		// Invariant A: retry_hint must be a valid closed-enum member.
		if _, ok := validHints[r.retryHint]; !ok {
			t.Errorf("INVARIANT VIOLATION: scenario %q produced invalid retry_hint=%q",
				s.label, r.retryHint)
			return false
		}

		// Invariant B: when error_class is set, check DefaultRetryHint mapping.
		if r.errClass == "" {
			return true
		}

		expected := r.errClass.DefaultRetryHint()

		// HTTP adapter: skip default-mapping check for http scenarios.
		// The httpreq normalizer intentionally emits HintRetryable for HTTP 5xx
		// (error_class=external_failure). This is a documented adapter override.
		if s.cap.Canonical() == "http.request@v1" {
			// Only assert the hint is valid (already checked above).
			return true
		}

		if r.retryHint != expected {
			t.Errorf(
				"INVARIANT VIOLATION: scenario %q\n"+
					"  capability    = %s\n"+
					"  error_class   = %s\n"+
					"  retry_hint    = %s\n"+
					"  expected hint = %s (DefaultRetryHint)\n"+
					"  If this is an intentional adapter override, exclude this capability\n"+
					"  from the DefaultRetryHint check above and document the rationale.",
				s.label, s.cap.Canonical(), r.errClass, r.retryHint, expected,
			)
			return false
		}
		return true
	}, cfg)
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// TestNormalize_Status_NotPartial_ForNonPartialCapabilities (§10.2 property 5 / D4.6)
//
// StatusPartial is only permitted from git.clone@v1 (AllowsPartial=true).
// Every other capability must never produce partial — the partial rule logic
// (entities.PartialClassification.Classify) enforces this via AllowsPartial.
// ---------------------------------------------------------------------------

func TestNormalize_Status_NotPartial_ForNonPartialCapabilities(t *testing.T) {
	scenarios := buildScenarios(t)
	clk := &shared.FakeClock{T: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}

	cfg := &quick.Config{MaxCount: 1000}
	err := quick.Check(func(seed uint16) bool {
		s := scenarios[int(seed)%len(scenarios)]
		r, nErr := s.norm(s.cap, s.raw, clk)
		if nErr != nil {
			t.Errorf("BLOCKED (fuzzer bug): scenario %q: Normalize error: %v", s.label, nErr)
			return false
		}
		if r.status == valueobjects.StatusPartial && s.cap.Canonical() != "git.clone@v1" {
			t.Errorf(
				"INVARIANT VIOLATION (D4.6): scenario %q capability=%q produced status=partial; "+
					"only git.clone@v1 (AllowsPartial=true) may emit partial",
				s.label, s.cap.Canonical(),
			)
			return false
		}
		return true
	}, cfg)
	require.NoError(t, err)
}
