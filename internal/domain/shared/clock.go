package shared

import "time"

// Clock abstracts time so domain and application code can be tested without
// calling time.Now() directly. Production code wires RealClock; tests wire
// FakeClock with deterministic advances.
type Clock interface {
	Now() time.Time
}

// RealClock returns the current UTC time. It is the only acceptable Clock
// implementation in production bootstrap (see R12 in docs/rules.md).
type RealClock struct{}

// Now returns the current time in UTC.
func (RealClock) Now() time.Time { return time.Now().UTC() }

// FakeClock is a deterministic Clock for tests. T is the current time;
// Advance moves it forward.
type FakeClock struct{ T time.Time }

// Now returns the FakeClock's current time.
func (f *FakeClock) Now() time.Time { return f.T }

// Advance moves the FakeClock forward by d.
func (f *FakeClock) Advance(d time.Duration) { f.T = f.T.Add(d) }
