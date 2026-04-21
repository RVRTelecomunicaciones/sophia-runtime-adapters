package pg

import (
	"context"
	"testing"

	"github.com/sophia-ecosystem/runtime-adapters/internal/adapters/outbound/pg/migrations"
)

// TestEmbedFS_ListsAllMigrations verifies the embedded FS contains the
// 4 expected .sql files (2 versions × up+down). Guards against future
// additions being silently ignored.
func TestEmbedFS_ListsAllMigrations(t *testing.T) {
	entries, err := migrations.FS.ReadDir(".")
	if err != nil {
		t.Fatalf("read embedded FS: %v", err)
	}
	got := map[string]bool{}
	for _, e := range entries {
		got[e.Name()] = true
	}
	want := []string{
		"0001_receipts.up.sql",
		"0001_receipts.down.sql",
		"0002_idempotency.up.sql",
		"0002_idempotency.down.sql",
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("missing migration file %q", w)
		}
	}
	if len(entries) != len(want) {
		t.Errorf("expected %d entries, got %d: %v", len(want), len(entries), got)
	}
}

// TestMigrate_RejectsNilPool verifies the public API guards against
// nil pool — the full integration test (real PG) lives in T50.
func TestMigrate_RejectsNilPool(t *testing.T) {
	if err := Migrate(context.Background(), nil); err == nil {
		t.Error("expected error for nil pool")
	}
}

// TestStripScheme covers the helper's branches.
func TestStripScheme(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"postgres://user:pass@host/db", "user:pass@host/db"},
		{"postgresql://user:pass@host/db?sslmode=disable", "user:pass@host/db?sslmode=disable"},
		{"host/db", "host/db"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := stripScheme(tc.in); got != tc.want {
			t.Errorf("stripScheme(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
