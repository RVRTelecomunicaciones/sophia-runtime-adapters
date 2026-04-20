package services_test

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sophia-ecosystem/runtime-adapters/internal/application/services"
)

// ---------------------------------------------------------------------------
// NewConcurrencyLimiter
// ---------------------------------------------------------------------------

func TestNewConcurrencyLimiter_HappyPath(t *testing.T) {
	l := services.NewConcurrencyLimiter(5)
	if l == nil {
		t.Fatal("expected non-nil limiter")
	}
	if l.Max() != 5 {
		t.Errorf("Max() = %d, want 5", l.Max())
	}
	if l.InUse() != 0 {
		t.Errorf("InUse() = %d, want 0", l.InUse())
	}
}

func TestNewConcurrencyLimiter_PanicsOnZero(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for max=0, got none")
		}
	}()
	services.NewConcurrencyLimiter(0)
}

func TestNewConcurrencyLimiter_PanicsOnNegative(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for max=-1, got none")
		}
	}()
	services.NewConcurrencyLimiter(-1)
}

// ---------------------------------------------------------------------------
// TryAcquire + Release cycle
// ---------------------------------------------------------------------------

func TestTryAcquireRelease_Cycle(t *testing.T) {
	l := services.NewConcurrencyLimiter(3)

	if err := l.TryAcquire(); err != nil {
		t.Fatalf("TryAcquire() unexpected error: %v", err)
	}
	if l.InUse() != 1 {
		t.Errorf("after acquire InUse() = %d, want 1", l.InUse())
	}

	l.Release()
	if l.InUse() != 0 {
		t.Errorf("after release InUse() = %d, want 0", l.InUse())
	}
}

// ---------------------------------------------------------------------------
// Fill to capacity
// ---------------------------------------------------------------------------

func TestTryAcquire_ToCapacity(t *testing.T) {
	const max = 4
	l := services.NewConcurrencyLimiter(max)

	for i := 0; i < max; i++ {
		if err := l.TryAcquire(); err != nil {
			t.Fatalf("TryAcquire() slot %d unexpected error: %v", i, err)
		}
	}
	if l.InUse() != int64(max) {
		t.Errorf("InUse() = %d, want %d", l.InUse(), max)
	}

	// cleanup
	for i := 0; i < max; i++ {
		l.Release()
	}
}

// ---------------------------------------------------------------------------
// Beyond capacity → ErrTooManyExecutions
// ---------------------------------------------------------------------------

func TestTryAcquire_BeyondCapacity(t *testing.T) {
	const max = 2
	l := services.NewConcurrencyLimiter(max)

	for i := 0; i < max; i++ {
		if err := l.TryAcquire(); err != nil {
			t.Fatalf("fill slot %d: %v", i, err)
		}
	}

	err := l.TryAcquire()
	if err == nil {
		t.Fatal("expected ErrTooManyExecutions, got nil")
	}
	if !errors.Is(err, services.ErrTooManyExecutions) {
		t.Errorf("errors.Is(ErrTooManyExecutions) = false; err = %v", err)
	}
	msg := err.Error()
	if !strings.Contains(msg, "in_use=") {
		t.Errorf("error message missing in_use field: %q", msg)
	}
	if !strings.Contains(msg, "max=") {
		t.Errorf("error message missing max field: %q", msg)
	}

	// cleanup
	for i := 0; i < max; i++ {
		l.Release()
	}
}

// ---------------------------------------------------------------------------
// Release restores a slot
// ---------------------------------------------------------------------------

func TestRelease_RestoresSlot(t *testing.T) {
	const max = 2
	l := services.NewConcurrencyLimiter(max)

	for i := 0; i < max; i++ {
		if err := l.TryAcquire(); err != nil {
			t.Fatalf("fill slot %d: %v", i, err)
		}
	}

	// full — next acquire must fail
	if err := l.TryAcquire(); !errors.Is(err, services.ErrTooManyExecutions) {
		t.Fatalf("expected rejection when full, got: %v", err)
	}

	l.Release() // free one slot

	if err := l.TryAcquire(); err != nil {
		t.Errorf("TryAcquire after Release should succeed, got: %v", err)
	}

	// cleanup: max slots still held
	for i := 0; i < max; i++ {
		l.Release()
	}
}

// ---------------------------------------------------------------------------
// Concurrent TryAcquire — race safety (test 7)
// ---------------------------------------------------------------------------

func TestConcurrencyLimiter_RaceSafety(t *testing.T) {
	const max = 4
	const workers = 64
	l := services.NewConcurrencyLimiter(max)

	var maxInUse int64
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			if err := l.TryAcquire(); err != nil {
				return // fast-rejected; OK
			}
			defer l.Release()
			cur := l.InUse()
			for {
				prev := atomic.LoadInt64(&maxInUse)
				if cur <= prev || atomic.CompareAndSwapInt64(&maxInUse, prev, cur) {
					break
				}
			}
		}()
	}
	wg.Wait()

	if got := atomic.LoadInt64(&maxInUse); got > int64(max) {
		t.Errorf("max observed in_use = %d, exceeded capacity %d", got, max)
	}
	if l.InUse() != 0 {
		t.Errorf("after all releases, InUse = %d, want 0", l.InUse())
	}
}

// ---------------------------------------------------------------------------
// TryAcquire fast-reject latency (test 8)
// ---------------------------------------------------------------------------

func TestTryAcquire_FastRejectLatency(t *testing.T) {
	const max = 2
	const callers = 100
	const deadline = 500 * time.Millisecond

	l := services.NewConcurrencyLimiter(max)

	// Fill to capacity so all callers below get fast-rejected.
	for i := 0; i < max; i++ {
		if err := l.TryAcquire(); err != nil {
			t.Fatalf("setup fill slot %d: %v", i, err)
		}
	}

	done := make(chan struct{})
	go func() {
		var wg sync.WaitGroup
		wg.Add(callers)
		for i := 0; i < callers; i++ {
			go func() {
				defer wg.Done()
				err := l.TryAcquire()
				if err == nil {
					// Should not happen — limiter is full. Release to avoid
					// leaking the slot and failing the cleanup below.
					l.Release()
					t.Errorf("expected rejection, got nil")
				}
			}()
		}
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// all callers returned within the deadline
	case <-time.After(deadline):
		t.Errorf("%d concurrent TryAcquire calls did not return within %v — possible blocking", callers, deadline)
	}

	// cleanup
	for i := 0; i < max; i++ {
		l.Release()
	}
}
