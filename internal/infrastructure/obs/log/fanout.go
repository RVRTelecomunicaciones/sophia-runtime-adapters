package log

import (
	"fmt"
	"io"
	"sync"
	"time"
)

// mirrorThrottleWindow is the minimum interval between consecutive
// "mirror write failed" diagnostic lines emitted to the error sink.
// Set to 1 minute per spec §3.1: errors logged once via a throttled
// path so a steady-state mirror failure (disk full, permissions
// revoked) doesn't drown the primary writer.
const mirrorThrottleWindow = time.Minute

// SafeFanoutWriter writes to a primary writer (R10 stdout) and, on
// best-effort basis, to a secondary mirror writer. Primary errors
// propagate to the caller. Mirror errors NEVER affect the primary
// write path; they are logged once per mirrorThrottleWindow to an
// error sink (typically stderr in production).
//
// D2C4D.3 / I-D.1: stdout is primary, mirror is best-effort secondary,
// mirror failures must not suppress stdout writes, mirror errors
// throttled.
type SafeFanoutWriter struct {
	primary io.Writer // R10 stdout — errors here propagate
	mirror  io.Writer // JSONL mirror file — best-effort secondary
	sink    io.Writer // throttled error sink for mirror failures (nil → io.Discard)

	mu            sync.Mutex
	lastMirrorErr time.Time
}

// NewSafeFanoutWriter constructs a writer with the documented
// asymmetry. mirror may be nil — in which case the writer behaves
// identically to writing to primary alone. sink may be nil — in which
// case mirror errors are silently discarded.
func NewSafeFanoutWriter(primary, mirror, sink io.Writer) *SafeFanoutWriter {
	if sink == nil {
		sink = io.Discard
	}
	return &SafeFanoutWriter{
		primary: primary,
		mirror:  mirror,
		sink:    sink,
	}
}

// Write copies p to the primary writer, then to the mirror writer if
// non-nil. The return values are taken from the PRIMARY write only —
// mirror state is invisible to the caller.
func (w *SafeFanoutWriter) Write(p []byte) (int, error) {
	n, err := w.primary.Write(p)
	if w.mirror != nil {
		if _, mirrErr := w.mirror.Write(p); mirrErr != nil {
			w.reportMirrorError(mirrErr)
		}
	}
	return n, err
}

// reportMirrorError throttles consecutive failures so a steady-state
// failure (disk full) doesn't drown the sink. At most one diagnostic
// line per mirrorThrottleWindow.
func (w *SafeFanoutWriter) reportMirrorError(err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := time.Now()
	if !w.lastMirrorErr.IsZero() && now.Sub(w.lastMirrorErr) < mirrorThrottleWindow {
		return
	}
	w.lastMirrorErr = now
	_, _ = fmt.Fprintf(w.sink, "log mirror write failed (throttled, 1/min): %v\n", err)
}
