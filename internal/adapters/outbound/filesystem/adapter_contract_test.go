package filesystem_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sophia-ecosystem/runtime-adapters/internal/adapters/outbound/filesystem"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
	"github.com/sophia-ecosystem/runtime-adapters/test/contract"
)

func TestFilesystemAdapter_ContractSuite(t *testing.T) {
	root := t.TempDir()
	// Create a real file for read sample.
	srcFile := filepath.Join(root, "sample.txt")
	if err := os.WriteFile(srcFile, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	dstFile := filepath.Join(root, "out.txt")

	a, err := filesystem.NewAdapter(filesystem.Config{
		AllowedFilesystemRoots: []string{root},
		InlineStreamLimit:      16 * 1024,
	}, &shared.FakeClock{T: time.Now()})
	if err != nil {
		t.Fatal(err)
	}

	contract.RunContractSuite(t, a, map[string]contract.SamplePayload{
		"filesystem.read_file@v1": {
			Valid: jsonMust(t, map[string]any{"path": srcFile}),
			Invalid: []json.RawMessage{
				jsonMust(t, map[string]any{}),                           // missing path
				jsonMust(t, map[string]any{"path": "/etc/passwd"}),      // outside allowlist
				jsonMust(t, map[string]any{"path": srcFile, "junk": 1}), // unknown field
				json.RawMessage(`not json`),
			},
		},
		"filesystem.write_file@v1": {
			Valid: jsonMust(t, map[string]any{
				"path": dstFile,
				"data": []byte("payload"),
			}),
			Invalid: []json.RawMessage{
				jsonMust(t, map[string]any{}),                                // missing path
				jsonMust(t, map[string]any{"path": "/etc/foo", "data": "x"}), // outside allowlist
				jsonMust(t, map[string]any{"path": srcFile, "data": "x"}),    // exists, overwrite=false
				json.RawMessage(`not json`),
			},
		},
	})
}

func jsonMust(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
