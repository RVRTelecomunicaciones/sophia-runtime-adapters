package shared_test

import (
	"testing"
	"time"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
)

func TestRealClockReturnsUTC(t *testing.T) {
	c := shared.RealClock{}
	now := c.Now()
	if now.Location() != time.UTC {
		t.Fatalf("expected UTC, got %v", now.Location())
	}
	if time.Since(now) > time.Second {
		t.Fatalf("RealClock.Now() should be current time")
	}
}

func TestFakeClockAdvances(t *testing.T) {
	start := time.Date(2026, 4, 19, 0, 0, 0, 0, time.UTC)
	fc := &shared.FakeClock{T: start}
	if !fc.Now().Equal(start) {
		t.Fatal("initial time mismatch")
	}
	fc.Advance(5 * time.Second)
	if !fc.Now().Equal(start.Add(5 * time.Second)) {
		t.Fatal("advance failed")
	}
}
