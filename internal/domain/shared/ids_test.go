package shared_test

import (
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
