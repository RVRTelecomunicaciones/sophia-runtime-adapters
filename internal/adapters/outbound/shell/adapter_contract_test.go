package shell_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/sophia-ecosystem/runtime-adapters/internal/adapters/outbound/shell"
	"github.com/sophia-ecosystem/runtime-adapters/test/contract"
)

func TestShellAdapter_ContractSuite(t *testing.T) {
	a, err := shell.NewAdapter(shell.Config{
		AllowedCommandsPath: []string{"/usr/bin", "/bin"},
		AllowedWorkingDirs:  []string{os.TempDir()},
		InlineStreamLimit:   16 * 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	contract.RunContractSuite(t, a, map[string]contract.SamplePayload{
		"shell.exec@v1": {
			Valid: json.RawMessage(`{"command":"echo","args":["hi"]}`),
			Invalid: []json.RawMessage{
				json.RawMessage(`{"command":""}`),                                        // empty command
				json.RawMessage(`{"command":"disallowed-xyz"}`),                          // not in allowlist
				json.RawMessage(`{"command":"/bin/echo","working_dir":"/root"}`),         // disallowed WD
				json.RawMessage(`{"command":"echo","unknown_field":"nope"}`),             // DisallowUnknownFields
				json.RawMessage(`not json`),                                              // invalid JSON (will be skipped at envelope level)
			},
		},
	})
}
