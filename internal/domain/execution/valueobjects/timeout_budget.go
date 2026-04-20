package valueobjects

import (
	"encoding/json"
	"fmt"
	"time"
)

// TimeoutBudget represents a per-request execution time budget with
// millisecond precision. Must be > 0 and ≤ the runtime-configured max
// (A4.1 — default 30 minutes, configurable). Wire representation is
// timeout_budget_ms (int64).
type TimeoutBudget struct {
	d time.Duration
}

// NewTimeoutBudget validates that the budget is > 0 and ≤ max. A max of 0
// is treated as "unlimited" (no upper bound check).
func NewTimeoutBudget(ms int64, max time.Duration) (TimeoutBudget, error) {
	if ms <= 0 {
		return TimeoutBudget{}, fmt.Errorf("timeout_budget_ms must be > 0, got %d", ms)
	}
	d := time.Duration(ms) * time.Millisecond
	if max > 0 && d > max {
		return TimeoutBudget{}, fmt.Errorf("timeout_budget %v exceeds max %v", d, max)
	}
	return TimeoutBudget{d: d}, nil
}

// Duration returns the budget as a time.Duration.
func (b TimeoutBudget) Duration() time.Duration { return b.d }

// Milliseconds returns the budget in milliseconds (wire format).
func (b TimeoutBudget) Milliseconds() int64 { return b.d.Milliseconds() }

// MarshalJSON encodes the budget as an int64 millisecond value, matching
// the wire field name timeout_budget_ms in ExecutionRequest.
func (b TimeoutBudget) MarshalJSON() ([]byte, error) {
	return json.Marshal(b.d.Milliseconds())
}

// UnmarshalJSON expects an int64 millisecond value and validates only that
// it is > 0. Upper-bound validation (MaxTimeoutBudget) is applied by the
// consuming layer that knows the config, not here — a standalone
// UnmarshalJSON cannot access runtime config.
func (b *TimeoutBudget) UnmarshalJSON(raw []byte) error {
	var ms int64
	if err := json.Unmarshal(raw, &ms); err != nil {
		return err
	}
	if ms <= 0 {
		return fmt.Errorf("timeout_budget_ms must be > 0, got %d", ms)
	}
	b.d = time.Duration(ms) * time.Millisecond
	return nil
}
