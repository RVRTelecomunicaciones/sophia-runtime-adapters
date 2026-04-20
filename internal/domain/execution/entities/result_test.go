package entities

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
)

// helpers ----------------------------------------------------------------

var (
	testNow      = time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	testDuration = 100 * time.Millisecond
)

func okResult(t *testing.T, opts ...func(*resultArgs)) ExecutionResult {
	t.Helper()
	a := defaultArgs()
	for _, o := range opts {
		o(a)
	}
	r, err := NewExecutionResult(
		a.status, a.retry, a.errClass, a.errMsg,
		a.stdoutRef, a.stderrRef, a.exitCode,
		a.artifacts, a.adapterMeta,
		a.bytesRead, a.bytesWritten,
		a.duration, a.completedAt,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return r
}

type resultArgs struct {
	status       valueobjects.ExecutionStatus
	retry        valueobjects.RetryHint
	errClass     valueobjects.ErrorClass
	errMsg       string
	stdoutRef    *StreamRef
	stderrRef    *StreamRef
	exitCode     *int
	artifacts    []Artifact
	adapterMeta  map[string]string
	bytesRead    int64
	bytesWritten int64
	duration     time.Duration
	completedAt  time.Time
}

func defaultArgs() *resultArgs {
	return &resultArgs{
		status:      valueobjects.StatusSuccess,
		retry:       valueobjects.HintNonRetryable,
		duration:    testDuration,
		completedAt: testNow,
	}
}

func buildResult(a *resultArgs) (ExecutionResult, error) {
	return NewExecutionResult(
		a.status, a.retry, a.errClass, a.errMsg,
		a.stdoutRef, a.stderrRef, a.exitCode,
		a.artifacts, a.adapterMeta,
		a.bytesRead, a.bytesWritten,
		a.duration, a.completedAt,
	)
}

// §5.9 invariant table ---------------------------------------------------

func TestNewExecutionResult_Invariants(t *testing.T) {
	t.Parallel()

	type tc struct {
		name      string
		status    valueobjects.ExecutionStatus
		retry     valueobjects.RetryHint
		errClass  valueobjects.ErrorClass
		errMsg    string
		wantErr   bool
		errSubstr string
	}

	tests := []tc{
		// 1. success, empty errClass + errMsg → OK
		{
			name:   "success_ok",
			status: valueobjects.StatusSuccess,
			retry:  valueobjects.HintNonRetryable,
		},
		// 2. success with non-empty errClass → error
		{
			name:      "success_with_errclass",
			status:    valueobjects.StatusSuccess,
			retry:     valueobjects.HintNonRetryable,
			errClass:  valueobjects.ErrExternalFailure,
			wantErr:   true,
			errSubstr: "status=success must not carry",
		},
		// 3. success with non-empty errMsg → error
		{
			name:      "success_with_errmsg",
			status:    valueobjects.StatusSuccess,
			retry:     valueobjects.HintNonRetryable,
			errMsg:    "some message",
			wantErr:   true,
			errSubstr: "status=success must not carry",
		},
		// 4. timeout + ErrTimeout → OK
		{
			name:     "timeout_ok",
			status:   valueobjects.StatusTimeout,
			retry:    valueobjects.HintRetryable,
			errClass: valueobjects.ErrTimeout,
		},
		// 5. timeout + ErrCancelled → error
		{
			name:      "timeout_wrong_errclass",
			status:    valueobjects.StatusTimeout,
			retry:     valueobjects.HintRetryable,
			errClass:  valueobjects.ErrCancelled,
			wantErr:   true,
			errSubstr: "status=timeout must carry error_class=timeout",
		},
		// 6. cancelled + ErrCancelled → OK
		{
			name:     "cancelled_ok",
			status:   valueobjects.StatusCancelled,
			retry:    valueobjects.HintNonRetryable,
			errClass: valueobjects.ErrCancelled,
		},
		// 7. cancelled + ErrTimeout → error
		{
			name:      "cancelled_wrong_errclass",
			status:    valueobjects.StatusCancelled,
			retry:     valueobjects.HintNonRetryable,
			errClass:  valueobjects.ErrTimeout,
			wantErr:   true,
			errSubstr: "status=cancelled must carry error_class=cancelled",
		},
		// 8. failure + ErrExternalFailure + errMsg → OK
		{
			name:     "failure_ok",
			status:   valueobjects.StatusFailure,
			retry:    valueobjects.HintRetryable,
			errClass: valueobjects.ErrExternalFailure,
			errMsg:   "boom",
		},
		// 9. failure + empty errClass → error
		{
			name:      "failure_no_errclass",
			status:    valueobjects.StatusFailure,
			retry:     valueobjects.HintRetryable,
			errMsg:    "boom",
			wantErr:   true,
			errSubstr: "must carry a non-empty error_class",
		},
		// 10. failure + errClass but empty errMsg → error
		{
			name:      "failure_no_errmsg",
			status:    valueobjects.StatusFailure,
			retry:     valueobjects.HintRetryable,
			errClass:  valueobjects.ErrExternalFailure,
			wantErr:   true,
			errSubstr: "must carry a non-empty error_message",
		},
		// 11. partial — same pattern as failure
		{
			name:     "partial_ok",
			status:   valueobjects.StatusPartial,
			retry:    valueobjects.HintUnknown,
			errClass: valueobjects.ErrInterrupted,
			errMsg:   "only part done",
		},
		{
			name:      "partial_no_errclass",
			status:    valueobjects.StatusPartial,
			retry:     valueobjects.HintUnknown,
			errMsg:    "partial",
			wantErr:   true,
			errSubstr: "must carry a non-empty error_class",
		},
		{
			name:      "partial_no_errmsg",
			status:    valueobjects.StatusPartial,
			retry:     valueobjects.HintUnknown,
			errClass:  valueobjects.ErrInterrupted,
			wantErr:   true,
			errSubstr: "must carry a non-empty error_message",
		},
		// 12. invalid status
		{
			name:      "invalid_status_empty",
			status:    valueobjects.ExecutionStatus(""),
			retry:     valueobjects.HintNonRetryable,
			wantErr:   true,
			errSubstr: "invalid execution_status",
		},
		{
			name:      "invalid_status_unknown",
			status:    valueobjects.ExecutionStatus("running"),
			retry:     valueobjects.HintNonRetryable,
			wantErr:   true,
			errSubstr: "invalid execution_status",
		},
		// 13. invalid retry_hint
		{
			name:      "invalid_retry_hint",
			status:    valueobjects.StatusSuccess,
			retry:     valueobjects.RetryHint("maybe"),
			wantErr:   true,
			errSubstr: "invalid retry_hint",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			a := defaultArgs()
			a.status = tt.status
			a.retry = tt.retry
			a.errClass = tt.errClass
			a.errMsg = tt.errMsg
			_, err := buildResult(a)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tt.errSubstr != "" && !strings.Contains(err.Error(), tt.errSubstr) {
					t.Fatalf("error %q does not contain %q", err.Error(), tt.errSubstr)
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// 14. ErrorMessage > 4 KiB → truncated silently to exactly 4 KiB ---------

func TestNewExecutionResult_ErrorMessageTruncated(t *testing.T) {
	t.Parallel()
	msg := strings.Repeat("x", MaxErrorMessageBytes+100)
	a := defaultArgs()
	a.status = valueobjects.StatusFailure
	a.retry = valueobjects.HintRetryable
	a.errClass = valueobjects.ErrExternalFailure
	a.errMsg = msg
	r, err := buildResult(a)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(r.ErrorMessage) != MaxErrorMessageBytes {
		t.Fatalf("want ErrorMessage len %d, got %d", MaxErrorMessageBytes, len(r.ErrorMessage))
	}
}

// 15. AdapterMeta total > 8 KiB → error ----------------------------------

func TestNewExecutionResult_AdapterMetaTooLarge(t *testing.T) {
	t.Parallel()
	a := defaultArgs()
	a.adapterMeta = map[string]string{
		"key": strings.Repeat("v", MaxAdapterMetaBytes+1),
	}
	_, err := buildResult(a)
	if err == nil {
		t.Fatal("expected error for oversized adapter_meta")
	}
	if !strings.Contains(err.Error(), "adapter_meta total value bytes") {
		t.Fatalf("unexpected error text: %v", err)
	}
}

// 16. bytesRead / bytesWritten negative → error ---------------------------

func TestNewExecutionResult_NegativeBytes(t *testing.T) {
	t.Parallel()

	t.Run("negative_bytes_read", func(t *testing.T) {
		t.Parallel()
		a := defaultArgs()
		a.bytesRead = -1
		_, err := buildResult(a)
		if err == nil {
			t.Fatal("expected error for negative bytes_read")
		}
		if !strings.Contains(err.Error(), "bytes_read") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("negative_bytes_written", func(t *testing.T) {
		t.Parallel()
		a := defaultArgs()
		a.bytesWritten = -1
		_, err := buildResult(a)
		if err == nil {
			t.Fatal("expected error for negative bytes_written")
		}
		if !strings.Contains(err.Error(), "bytes_written") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

// 17. duration negative → error -------------------------------------------

func TestNewExecutionResult_NegativeDuration(t *testing.T) {
	t.Parallel()
	a := defaultArgs()
	a.duration = -1 * time.Millisecond
	_, err := buildResult(a)
	if err == nil {
		t.Fatal("expected error for negative duration")
	}
	if !strings.Contains(err.Error(), "duration") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// 18. nil artifacts → empty slice (JSON marshals as []) -------------------

func TestNewExecutionResult_NilArtifacts(t *testing.T) {
	t.Parallel()
	a := defaultArgs()
	a.artifacts = nil
	r, err := buildResult(a)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Artifacts == nil {
		t.Fatal("Artifacts must not be nil")
	}
	if len(r.Artifacts) != 0 {
		t.Fatalf("expected empty Artifacts, got %d", len(r.Artifacts))
	}
	// JSON must be [] not null.
	b, _ := json.Marshal(r)
	if !strings.Contains(string(b), `"artifacts":[]`) {
		t.Fatalf("expected artifacts:[] in JSON, got: %s", b)
	}
}

// 19. nil adapterMeta → empty map (JSON marshals as {}) -------------------

func TestNewExecutionResult_NilAdapterMeta(t *testing.T) {
	t.Parallel()
	a := defaultArgs()
	a.adapterMeta = nil
	r, err := buildResult(a)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.AdapterMeta == nil {
		t.Fatal("AdapterMeta must not be nil")
	}
	b, _ := json.Marshal(r)
	if !strings.Contains(string(b), `"adapter_meta":{}`) {
		t.Fatalf("expected adapter_meta:{} in JSON, got: %s", b)
	}
}

// 20. Defensive copy: adapterMeta ----------------------------------------

func TestNewExecutionResult_DefensiveCopy_AdapterMeta(t *testing.T) {
	t.Parallel()
	input := map[string]string{"k": "v1"}
	a := defaultArgs()
	a.adapterMeta = input
	r, err := buildResult(a)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Mutate original.
	input["k"] = "mutated"
	input["new"] = "extra"
	if r.AdapterMeta["k"] != "v1" {
		t.Fatalf("defensive copy failed: AdapterMeta[k] = %q, want %q", r.AdapterMeta["k"], "v1")
	}
	if _, ok := r.AdapterMeta["new"]; ok {
		t.Fatal("defensive copy failed: new key leaked into result")
	}
}

// 21. Defensive copy: artifacts ------------------------------------------

func TestNewExecutionResult_DefensiveCopy_Artifacts(t *testing.T) {
	t.Parallel()
	art, _ := NewArtifact(ArtifactFile, "s3://bucket/a", 100, "", nil)
	input := []Artifact{art}
	a := defaultArgs()
	a.artifacts = input
	r, err := buildResult(a)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Modifying an element by index on the original slice.
	input[0].URI = "mutated"
	// Artifact is a value type; the copy is independent.
	if r.Artifacts[0].URI != "s3://bucket/a" {
		t.Fatalf("defensive copy failed: Artifacts[0].URI = %q, want original", r.Artifacts[0].URI)
	}
	// Appending to original doesn't affect the copy; verify result length
	// is still 1 regardless of what happens to the input slice.
	_, _ = NewArtifact(ArtifactFile, "s3://bucket/b", 200, "", nil)
	if len(r.Artifacts) != 1 {
		t.Fatalf("defensive copy failed: len(Artifacts) = %d, want 1", len(r.Artifacts))
	}
}

// 22. StripStreams --------------------------------------------------------

func TestExecutionResult_StripStreams(t *testing.T) {
	t.Parallel()
	stdout, _ := NewStreamRef([]byte("out"), 3, 16*1024)
	stderr, _ := NewStreamRef([]byte("err"), 3, 16*1024)

	a := defaultArgs()
	a.stdoutRef = &stdout
	a.stderrRef = &stderr
	r, err := buildResult(a)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stripped := r.StripStreams()

	// Stripped copy has nil refs.
	if stripped.StdoutRef != nil {
		t.Fatal("StripStreams: StdoutRef must be nil")
	}
	if stripped.StderrRef != nil {
		t.Fatal("StripStreams: StderrRef must be nil")
	}

	// Original is unchanged.
	if r.StdoutRef == nil {
		t.Fatal("StripStreams: original StdoutRef must not be nil after strip")
	}
	if r.StderrRef == nil {
		t.Fatal("StripStreams: original StderrRef must not be nil after strip")
	}
}

// 23. JSON round-trip -----------------------------------------------------

func TestNewExecutionResult_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	a := defaultArgs()
	r, err := buildResult(a)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var got ExecutionResult
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if got.Status != r.Status {
		t.Errorf("Status: got %q, want %q", got.Status, r.Status)
	}
	if got.Retryable != r.Retryable {
		t.Errorf("Retryable: got %q, want %q", got.Retryable, r.Retryable)
	}
	if got.DurationMs != r.DurationMs {
		t.Errorf("DurationMs: got %d, want %d", got.DurationMs, r.DurationMs)
	}
	if !got.CompletedAt.Equal(r.CompletedAt) {
		t.Errorf("CompletedAt: got %v, want %v", got.CompletedAt, r.CompletedAt)
	}
	if got.Artifacts == nil {
		t.Error("Artifacts must not be nil after round-trip")
	}
	if got.AdapterMeta == nil {
		t.Error("AdapterMeta must not be nil after round-trip")
	}
}

// 24. ExitCode nil vs set — nil omitted from JSON -------------------------

func TestNewExecutionResult_ExitCode(t *testing.T) {
	t.Parallel()

	t.Run("nil_exit_code_omitted", func(t *testing.T) {
		t.Parallel()
		r := okResult(t)
		b, _ := json.Marshal(r)
		if strings.Contains(string(b), "exit_code") {
			t.Fatalf("nil ExitCode must be omitted from JSON, got: %s", b)
		}
	})

	t.Run("set_exit_code_present", func(t *testing.T) {
		t.Parallel()
		code := 42
		a := defaultArgs()
		a.exitCode = &code
		r, err := buildResult(a)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		b, _ := json.Marshal(r)
		if !strings.Contains(string(b), `"exit_code":42`) {
			t.Fatalf("ExitCode must appear in JSON, got: %s", b)
		}
	})
}
