// Package services contains the Phase 1 application services —
// ExecuteService (UC1), QueryServiceImpl (UC2, UC3), and supporting
// helpers (ConcurrencyLimiter, structural error helpers). These
// services orchestrate domain types and call outbound ports; they
// contain no I/O beyond ctx propagation.
package services

import (
	"errors"
	"fmt"
	"sync/atomic"
)

// ErrTooManyExecutions is returned by ConcurrencyLimiter.TryAcquire
// when the maximum number of concurrent executions is already in
// flight. Per A9.1 the limiter fast-rejects without queueing; callers
// (inbound HTTP handlers) map this to HTTP 503.
var ErrTooManyExecutions = errors.New("max concurrent executions reached")

// ConcurrencyLimiter is a fixed-size, non-blocking semaphore used by
// ExecuteService to cap in-flight executions at MaxConcurrentExecutions
// (A9.1). The zero value is NOT usable — construct via
// NewConcurrencyLimiter.
type ConcurrencyLimiter struct {
	max   int
	slots chan struct{}
	inUse atomic.Int64 // exposed via InUse() for observability
}

// NewConcurrencyLimiter returns a limiter with capacity max. Panics if
// max <= 0 — a non-positive cap is always a bootstrap programming
// error (R14 requires the limit be configured).
func NewConcurrencyLimiter(max int) *ConcurrencyLimiter {
	if max <= 0 {
		panic(fmt.Sprintf("ConcurrencyLimiter: max must be > 0, got %d", max))
	}
	return &ConcurrencyLimiter{
		max:   max,
		slots: make(chan struct{}, max),
	}
}

// TryAcquire attempts to reserve a slot without blocking. Returns nil
// on success (caller MUST pair with Release via defer), or
// ErrTooManyExecutions wrapped when all slots are full.
func (l *ConcurrencyLimiter) TryAcquire() error {
	select {
	case l.slots <- struct{}{}:
		l.inUse.Add(1)
		return nil
	default:
		return fmt.Errorf("%w: in_use=%d max=%d", ErrTooManyExecutions, l.InUse(), l.max)
	}
}

// Release returns a reserved slot to the pool. Callers MUST pair each
// successful TryAcquire with exactly one Release (typically via
// defer). Calling Release without a prior successful TryAcquire
// corrupts the counter and may cause subsequent TryAcquire to block —
// the implementation intentionally does NOT guard against this because
// the callsite pattern (TryAcquire + defer Release) makes it a
// programming error when violated.
func (l *ConcurrencyLimiter) Release() {
	<-l.slots
	l.inUse.Add(-1)
}

// Max returns the configured capacity.
func (l *ConcurrencyLimiter) Max() int { return l.max }

// InUse returns the current number of acquired slots. For
// observability only — the value may change between reading and
// acting on it.
func (l *ConcurrencyLimiter) InUse() int64 { return l.inUse.Load() }
