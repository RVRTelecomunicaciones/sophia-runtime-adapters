// Package shell implements the Phase 1 shell adapter (shell.exec@v1 —
// §8.2). It spawns external commands with no shell interpolation, env
// isolation (minimal base), and allowlist-based command + working-dir
// resolution. NO artifacts are ever produced (D4.8 / I8).
//
// External contract: AdapterID = "shell".
package shell

import (
	"encoding/json"
	"time"
)

// Config bundles the bootstrap-loaded safety settings. Zero value of
// the struct is NOT usable — bootstrap must populate AllowedCommandsPath
// and AllowedWorkingDirs to sane defaults (e.g. {"/usr/bin", "/bin"}
// and {os.Getenv("HOME")}).
type Config struct {
	// AllowedCommandsPath lists absolute directories in which a
	// non-absolute command may be resolved. An empty list rejects all
	// non-absolute commands.
	AllowedCommandsPath []string

	// AllowedWorkingDirs lists absolute directories within which
	// ExecPayload.WorkingDir may resolve. Empty string means the
	// process's current working dir; an explicit non-empty value must
	// be within one of these roots.
	AllowedWorkingDirs []string

	// InlineStreamLimit is the per-stream byte cap when constructing
	// entities.StreamRef for stdout/stderr. Default populated from the
	// runtime's MaxStreamBytes config.
	InlineStreamLimit int
}

// ExecPayload is the wire payload for shell.exec@v1.
type ExecPayload struct {
	Command     string            `json:"command"`                // absolute path OR a name resolvable via AllowedCommandsPath
	Args        []string          `json:"args,omitempty"`
	Env         map[string]string `json:"env,omitempty"`          // additive to the minimal base env
	WorkingDir  string            `json:"working_dir,omitempty"`  // empty = process cwd
	Stdin       []byte            `json:"stdin,omitempty"`        // base64 on wire
	ExitSuccess []int             `json:"exit_success,omitempty"` // default [0]
}

// execRaw is the adapter's raw outcome — never crosses into the domain
// untyped; only the registered shell normalizer (normalize.go) reads it.
type execRaw struct {
	exitCode       int
	stdout         []byte
	stderr         []byte
	durationMs     int64
	ctxErr         error
	runErr         error
	exitSuccess    []int
	validation     string // non-empty when validation failed (e.g. disallowed command)
	payloadDecoded bool
}

// IsAdapterRawOutcome satisfies services.AdapterRawOutcome.
func (*execRaw) IsAdapterRawOutcome() {}

// baseEnvKeys are the env entries inherited by every spawned process.
// Callers may override these via ExecPayload.Env.
var baseEnvKeys = []string{"PATH", "HOME", "LANG", "LC_ALL", "TZ"}

// DefaultConfigTimeout mirrors the capability default timeout declared
// in vo.NewPhase1Capabilities; duplicated here for the integration test
// default. Authoritative value lives in the catalog.
const DefaultConfigTimeout = 30 * time.Second

var _ = json.Valid // import guard
