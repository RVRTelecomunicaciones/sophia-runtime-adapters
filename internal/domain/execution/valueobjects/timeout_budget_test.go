package valueobjects

import (
	"encoding/json"
	"testing"
	"time"
)

func TestNewTimeoutBudget(t *testing.T) {
	const maxMs = int64(30 * 60 * 1000) // 30 minutes in ms
	max30m := 30 * time.Minute

	tests := []struct {
		name    string
		ms      int64
		max     time.Duration
		wantErr bool
		wantDur time.Duration
	}{
		{
			name:    "valid 1000ms",
			ms:      1000,
			max:     max30m,
			wantErr: false,
			wantDur: time.Second,
		},
		{
			name:    "boundary exactly 30m",
			ms:      maxMs,
			max:     max30m,
			wantErr: false,
			wantDur: 30 * time.Minute,
		},
		{
			name:    "reject zero",
			ms:      0,
			max:     max30m,
			wantErr: true,
		},
		{
			name:    "reject negative",
			ms:      -5,
			max:     max30m,
			wantErr: true,
		},
		{
			name:    "reject over max",
			ms:      31 * 60 * 1000,
			max:     max30m,
			wantErr: true,
		},
		{
			name:    "max zero means unlimited",
			ms:      1_000_000_000,
			max:     0,
			wantErr: false,
			wantDur: time.Duration(1_000_000_000) * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := NewTimeoutBudget(tt.ms, tt.max)
			if (err != nil) != tt.wantErr {
				t.Fatalf("NewTimeoutBudget(%d, %v) error = %v, wantErr %v", tt.ms, tt.max, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if b.Duration() != tt.wantDur {
				t.Errorf("Duration() = %v, want %v", b.Duration(), tt.wantDur)
			}
		})
	}
}

func TestTimeoutBudget_Duration(t *testing.T) {
	b, err := NewTimeoutBudget(5000, 30*time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := b.Duration()
	if got != 5*time.Second {
		t.Errorf("Duration() = %v, want %v", got, 5*time.Second)
	}
}

func TestTimeoutBudget_Milliseconds(t *testing.T) {
	tests := []struct {
		name string
		ms   int64
	}{
		{"1000ms", 1000},
		{"250ms", 250},
		{"maxMs", int64(30 * 60 * 1000)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := NewTimeoutBudget(tt.ms, 30*time.Minute)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := b.Milliseconds(); got != tt.ms {
				t.Errorf("Milliseconds() = %d, want %d", got, tt.ms)
			}
		})
	}
}

func TestTimeoutBudget_JSONRoundTrip(t *testing.T) {
	original, err := NewTimeoutBudget(7500, 30*time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Marshal produces a raw int64 number, not an object.
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("MarshalJSON error: %v", err)
	}

	// Confirm it's a bare number, not wrapped in an object.
	var raw int64
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("marshaled value is not a bare int64: %v", err)
	}
	if raw != 7500 {
		t.Errorf("marshaled value = %d, want 7500", raw)
	}

	// Unmarshal back and verify round-trip equality.
	var restored TimeoutBudget
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("UnmarshalJSON error: %v", err)
	}
	if restored.Duration() != original.Duration() {
		t.Errorf("round-trip Duration() = %v, want %v", restored.Duration(), original.Duration())
	}
	if restored.Milliseconds() != original.Milliseconds() {
		t.Errorf("round-trip Milliseconds() = %d, want %d", restored.Milliseconds(), original.Milliseconds())
	}
}

func TestTimeoutBudget_UnmarshalJSON_Rejects(t *testing.T) {
	tests := []struct {
		name string
		json string
	}{
		{"zero", `0`},
		{"negative", `-1`},
		{"string", `"1000"`},
		{"object", `{"ms":1000}`},
		{"null", `null`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var b TimeoutBudget
			err := json.Unmarshal([]byte(tt.json), &b)
			if err == nil {
				t.Errorf("expected error for input %s, got nil", tt.json)
			}
		})
	}
}
