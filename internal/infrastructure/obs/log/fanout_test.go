package log_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	loglocal "github.com/sophia-ecosystem/runtime-adapters/internal/infrastructure/obs/log"
)

type errWriter struct{ err error }

func (w errWriter) Write(p []byte) (int, error) { return 0, w.err }

func TestSafeFanout_PrimaryAndMirrorBothReceiveBytes(t *testing.T) {
	var primary, mirror bytes.Buffer
	w := loglocal.NewSafeFanoutWriter(&primary, &mirror, nil)

	n, err := w.Write([]byte("hello\n"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != 6 {
		t.Errorf("n: got %d, want 6", n)
	}
	if primary.String() != "hello\n" {
		t.Errorf("primary: got %q, want %q", primary.String(), "hello\n")
	}
	if mirror.String() != "hello\n" {
		t.Errorf("mirror: got %q, want %q", mirror.String(), "hello\n")
	}
}

func TestSafeFanout_PrimaryFailure_PropagatesError(t *testing.T) {
	primary := errWriter{err: errors.New("broken pipe")}
	var mirror bytes.Buffer
	w := loglocal.NewSafeFanoutWriter(primary, &mirror, nil)

	_, err := w.Write([]byte("hello"))
	if err == nil {
		t.Fatal("Write: expected error from primary failure, got nil")
	}
	if !strings.Contains(err.Error(), "broken pipe") {
		t.Errorf("err: got %v, want contains 'broken pipe'", err)
	}
}

func TestSafeFanout_MirrorFailure_DoesNotAffectPrimaryReturn(t *testing.T) {
	var primary bytes.Buffer
	mirror := errWriter{err: errors.New("disk full")}
	var sinkBuf bytes.Buffer
	w := loglocal.NewSafeFanoutWriter(&primary, mirror, &sinkBuf)

	n, err := w.Write([]byte("hello\n"))
	if err != nil {
		t.Fatalf("Write: mirror failure must not propagate, got %v", err)
	}
	if n != 6 {
		t.Errorf("n: got %d, want 6 (primary len)", n)
	}
	if primary.String() != "hello\n" {
		t.Errorf("primary: got %q, want %q", primary.String(), "hello\n")
	}
}

func TestSafeFanout_MirrorErrorsAreThrottled(t *testing.T) {
	var primary bytes.Buffer
	mirror := errWriter{err: errors.New("disk full")}
	var sinkBuf bytes.Buffer
	w := loglocal.NewSafeFanoutWriter(&primary, mirror, &sinkBuf)

	for i := 0; i < 100; i++ {
		_, _ = w.Write([]byte("x"))
	}
	count := strings.Count(sinkBuf.String(), "log mirror write failed")
	if count > 1 {
		t.Errorf("mirror error logs in sink: got %d, want <= 1 (throttle window 1m)", count)
	}
}
