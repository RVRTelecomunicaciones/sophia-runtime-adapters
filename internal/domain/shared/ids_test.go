package shared_test

import (
	"encoding/json"
	"testing"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
)

func TestNewReceiptID(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{"valid ULID", "01HZXK5JC6QK7XV0YQXA0QJ0YZ", false},
		{"empty", "", true},
		{"not ULID", "not-a-ulid", true},
		{"uuid (not valid for ReceiptID)", "7e2b3d2e-3f8c-4c9f-8d4f-7a0d0a4e4b7e", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id, err := shared.NewReceiptID(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q, got id %q", tc.in, id.String())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if id.String() != tc.in {
				t.Fatalf("want %s, got %s", tc.in, id.String())
			}
		})
	}
}

func TestNewHandleID(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{"valid ULID", "01HZXK5JC6QK7XV0YQXA0QJ0YZ", false},
		{"empty", "", true},
		{"not ULID", "not-a-ulid", true},
		{"uuid (not valid for HandleID)", "7e2b3d2e-3f8c-4c9f-8d4f-7a0d0a4e4b7e", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id, err := shared.NewHandleID(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q, got id %q", tc.in, id.String())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if id.String() != tc.in {
				t.Fatalf("want %s, got %s", tc.in, id.String())
			}
		})
	}
}

func TestNewCorrelationID(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{"valid ULID", "01HZXK5JC6QK7XV0YQXA0QJ0YZ", false},
		{"empty", "", true},
		{"not ULID", "not-a-ulid", true},
		{"uuid (not valid for CorrelationID)", "7e2b3d2e-3f8c-4c9f-8d4f-7a0d0a4e4b7e", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id, err := shared.NewCorrelationID(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q, got id %q", tc.in, id.String())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if id.String() != tc.in {
				t.Fatalf("want %s, got %s", tc.in, id.String())
			}
		})
	}
}

func TestNewIdempotencyKey(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{"valid ULID", "01HZXK5JC6QK7XV0YQXA0QJ0YZ", false},
		{"valid UUID lowercase", "7e2b3d2e-3f8c-4c9f-8d4f-7a0d0a4e4b7e", false},
		{"valid UUID uppercase", "7E2B3D2E-3F8C-4C9F-8D4F-7A0D0A4E4B7E", false},
		{"empty", "", true},
		{"garbage", "not-valid", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			k, err := shared.NewIdempotencyKey(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tc.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if k.String() != tc.in {
				t.Fatalf("want %s, got %s", tc.in, k.String())
			}
		})
	}
}

// --- JSON round-trip tests ---

func TestReceiptID_JSONRoundTrip(t *testing.T) {
	r, err := shared.NewReceiptID("01HZXK5JC6QK7XV0YQXA0QJ0YZ")
	if err != nil {
		t.Fatal(err)
	}

	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `"01HZXK5JC6QK7XV0YQXA0QJ0YZ"` {
		t.Fatalf("marshal: got %s", string(b))
	}

	var back shared.ReceiptID
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.String() != r.String() {
		t.Fatalf("round-trip: got %s want %s", back.String(), r.String())
	}
}

func TestReceiptID_UnmarshalJSON_Invalid(t *testing.T) {
	var r shared.ReceiptID
	cases := []string{
		`"not-a-ulid"`,
		`""`,
		`"7e2b3d2e-3f8c-4c9f-8d4f-7a0d0a4e4b7e"`, // UUID rejected for ReceiptID
		`123`,
		`{}`,
		`null`,
	}
	for _, tc := range cases {
		t.Run(tc, func(t *testing.T) {
			if err := json.Unmarshal([]byte(tc), &r); err == nil {
				t.Fatalf("expected error for %s", tc)
			}
		})
	}
}

func TestReceiptID_EmbeddedInStruct(t *testing.T) {
	type envelope struct {
		ID shared.ReceiptID `json:"id"`
	}
	r, _ := shared.NewReceiptID("01HZXK5JC6QK7XV0YQXA0QJ0YZ")
	b, err := json.Marshal(envelope{ID: r})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"id":"01HZXK5JC6QK7XV0YQXA0QJ0YZ"}`
	if string(b) != want {
		t.Fatalf("got %s want %s", string(b), want)
	}

	var back envelope
	if err := json.Unmarshal([]byte(want), &back); err != nil {
		t.Fatal(err)
	}
	if back.ID.String() != r.String() {
		t.Fatalf("unmarshal: got %s want %s", back.ID.String(), r.String())
	}
}

func TestHandleID_JSONRoundTrip(t *testing.T) {
	h, err := shared.NewHandleID("01HZXK5JC6QK7XV0YQXA0QJ0YZ")
	if err != nil {
		t.Fatal(err)
	}

	b, err := json.Marshal(h)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `"01HZXK5JC6QK7XV0YQXA0QJ0YZ"` {
		t.Fatalf("marshal: got %s", string(b))
	}

	var back shared.HandleID
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.String() != h.String() {
		t.Fatalf("round-trip: got %s want %s", back.String(), h.String())
	}
}

func TestHandleID_UnmarshalJSON_Invalid(t *testing.T) {
	var h shared.HandleID
	cases := []string{
		`"not-a-ulid"`,
		`""`,
		`"7e2b3d2e-3f8c-4c9f-8d4f-7a0d0a4e4b7e"`, // UUID rejected for HandleID
		`123`,
		`{}`,
		`null`,
	}
	for _, tc := range cases {
		t.Run(tc, func(t *testing.T) {
			if err := json.Unmarshal([]byte(tc), &h); err == nil {
				t.Fatalf("expected error for %s", tc)
			}
		})
	}
}

func TestHandleID_EmbeddedInStruct(t *testing.T) {
	type envelope struct {
		ID shared.HandleID `json:"id"`
	}
	h, _ := shared.NewHandleID("01HZXK5JC6QK7XV0YQXA0QJ0YZ")
	b, err := json.Marshal(envelope{ID: h})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"id":"01HZXK5JC6QK7XV0YQXA0QJ0YZ"}`
	if string(b) != want {
		t.Fatalf("got %s want %s", string(b), want)
	}

	var back envelope
	if err := json.Unmarshal([]byte(want), &back); err != nil {
		t.Fatal(err)
	}
	if back.ID.String() != h.String() {
		t.Fatalf("unmarshal: got %s want %s", back.ID.String(), h.String())
	}
}

func TestCorrelationID_JSONRoundTrip(t *testing.T) {
	c, err := shared.NewCorrelationID("01HZXK5JC6QK7XV0YQXA0QJ0YZ")
	if err != nil {
		t.Fatal(err)
	}

	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `"01HZXK5JC6QK7XV0YQXA0QJ0YZ"` {
		t.Fatalf("marshal: got %s", string(b))
	}

	var back shared.CorrelationID
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.String() != c.String() {
		t.Fatalf("round-trip: got %s want %s", back.String(), c.String())
	}
}

func TestCorrelationID_UnmarshalJSON_Invalid(t *testing.T) {
	var c shared.CorrelationID
	cases := []string{
		`"not-a-ulid"`,
		`""`,
		`"7e2b3d2e-3f8c-4c9f-8d4f-7a0d0a4e4b7e"`, // UUID rejected for CorrelationID
		`123`,
		`{}`,
		`null`,
	}
	for _, tc := range cases {
		t.Run(tc, func(t *testing.T) {
			if err := json.Unmarshal([]byte(tc), &c); err == nil {
				t.Fatalf("expected error for %s", tc)
			}
		})
	}
}

func TestCorrelationID_EmbeddedInStruct(t *testing.T) {
	type envelope struct {
		ID shared.CorrelationID `json:"correlation_id"`
	}
	c, _ := shared.NewCorrelationID("01HZXK5JC6QK7XV0YQXA0QJ0YZ")
	b, err := json.Marshal(envelope{ID: c})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"correlation_id":"01HZXK5JC6QK7XV0YQXA0QJ0YZ"}`
	if string(b) != want {
		t.Fatalf("got %s want %s", string(b), want)
	}

	var back envelope
	if err := json.Unmarshal([]byte(want), &back); err != nil {
		t.Fatal(err)
	}
	if back.ID.String() != c.String() {
		t.Fatalf("unmarshal: got %s want %s", back.ID.String(), c.String())
	}
}

func TestIdempotencyKey_JSONRoundTrip_ULID(t *testing.T) {
	k, err := shared.NewIdempotencyKey("01HZXK5JC6QK7XV0YQXA0QJ0YZ")
	if err != nil {
		t.Fatal(err)
	}

	b, err := json.Marshal(k)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `"01HZXK5JC6QK7XV0YQXA0QJ0YZ"` {
		t.Fatalf("marshal: got %s", string(b))
	}

	var back shared.IdempotencyKey
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.String() != k.String() {
		t.Fatalf("round-trip: got %s want %s", back.String(), k.String())
	}
}

func TestIdempotencyKey_JSONRoundTrip_UUID(t *testing.T) {
	k, err := shared.NewIdempotencyKey("7e2b3d2e-3f8c-4c9f-8d4f-7a0d0a4e4b7e")
	if err != nil {
		t.Fatal(err)
	}

	b, err := json.Marshal(k)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `"7e2b3d2e-3f8c-4c9f-8d4f-7a0d0a4e4b7e"` {
		t.Fatalf("marshal: got %s", string(b))
	}

	var back shared.IdempotencyKey
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.String() != k.String() {
		t.Fatalf("round-trip: got %s want %s", back.String(), k.String())
	}
}

func TestIdempotencyKey_UnmarshalJSON_Invalid(t *testing.T) {
	var k shared.IdempotencyKey
	cases := []string{
		`"not-valid"`,
		`""`,
		`123`,
		`{}`,
		`null`,
	}
	for _, tc := range cases {
		t.Run(tc, func(t *testing.T) {
			if err := json.Unmarshal([]byte(tc), &k); err == nil {
				t.Fatalf("expected error for %s", tc)
			}
		})
	}
}

func TestIdempotencyKey_EmbeddedInStruct(t *testing.T) {
	type envelope struct {
		Key shared.IdempotencyKey `json:"idempotency_key"`
	}
	k, _ := shared.NewIdempotencyKey("01HZXK5JC6QK7XV0YQXA0QJ0YZ")
	b, err := json.Marshal(envelope{Key: k})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"idempotency_key":"01HZXK5JC6QK7XV0YQXA0QJ0YZ"}`
	if string(b) != want {
		t.Fatalf("got %s want %s", string(b), want)
	}

	var back envelope
	if err := json.Unmarshal([]byte(want), &back); err != nil {
		t.Fatal(err)
	}
	if back.Key.String() != k.String() {
		t.Fatalf("unmarshal: got %s want %s", back.Key.String(), k.String())
	}
}
