package shell

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/services"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/ports/outbound"
)

// AdapterIDString is the spec-mandated external AdapterID for the
// shell adapter (§5.4).
const AdapterIDString = "shell"

// Adapter implements outbound.Adapter for shell.exec@v1.
type Adapter struct {
	id           valueobjects.AdapterID
	capabilities []valueobjects.Capability
	cfg          Config
}

// NewAdapter constructs the adapter. Returns an error if the config is
// invalid (non-positive InlineStreamLimit).
func NewAdapter(cfg Config) (*Adapter, error) {
	if cfg.InlineStreamLimit <= 0 {
		return nil, fmt.Errorf("shell.Config.InlineStreamLimit must be > 0, got %d", cfg.InlineStreamLimit)
	}
	id, err := valueobjects.NewAdapterID(AdapterIDString)
	if err != nil {
		return nil, fmt.Errorf("construct AdapterID: %w", err)
	}
	cap, err := valueobjects.NewCapability(id, "exec", "v1", false, 30*time.Second)
	if err != nil {
		return nil, fmt.Errorf("construct shell.exec@v1 capability: %w", err)
	}
	return &Adapter{
		id:           id,
		capabilities: []valueobjects.Capability{cap},
		cfg:          cfg,
	}, nil
}

// ID returns the adapter's external identifier.
func (a *Adapter) ID() valueobjects.AdapterID { return a.id }

// Capabilities returns the capability set this adapter supports.
func (a *Adapter) Capabilities() []valueobjects.Capability {
	out := make([]valueobjects.Capability, len(a.capabilities))
	copy(out, a.capabilities)
	return out
}

// Execute dispatches shell.exec@v1. Returns (raw, nil) always for
// this adapter — structural Go errors are reserved for panic recovery
// (handled by the caller's safeExecute middleware).
func (a *Adapter) Execute(ctx context.Context, cap valueobjects.Capability, payload valueobjects.Payload) (services.AdapterRawOutcome, error) {
	// Pre-flight: ctx already dead → return empty raw with ctxErr set.
	if err := ctx.Err(); err != nil {
		return &execRaw{ctxErr: err}, nil
	}

	if cap.Canonical() != "shell.exec@v1" {
		return &execRaw{validation: fmt.Sprintf("unknown capability for shell adapter: %s", cap.Canonical())}, nil
	}

	// Decode payload.
	var p ExecPayload
	dec := json.NewDecoder(bytes.NewReader(payload.Raw()))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return &execRaw{validation: fmt.Sprintf("decode payload: %v", err)}, nil
	}

	// Validate + resolve.
	resolvedCmd, err := a.cfg.resolveCommand(p.Command)
	if err != nil {
		return &execRaw{validation: err.Error(), payloadDecoded: true}, nil
	}
	resolvedWD, err := a.cfg.resolveWorkingDir(p.WorkingDir)
	if err != nil {
		return &execRaw{validation: err.Error(), payloadDecoded: true}, nil
	}

	// Build command — no shell interpolation, direct exec.
	cmd := exec.CommandContext(ctx, resolvedCmd, p.Args...) //nolint:gosec // G204: command is allowlist-validated by resolveCommand; args passed directly to exec.CommandContext, no shell interpolation
	cmd.Dir = resolvedWD
	cmd.Env = a.cfg.buildEnv(p.Env)
	if len(p.Stdin) > 0 {
		cmd.Stdin = bytes.NewReader(p.Stdin)
	}
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf

	t0 := time.Now()
	runErr := cmd.Run()
	elapsed := time.Since(t0)

	raw := &execRaw{
		exitCode:       exitCodeFrom(cmd, runErr),
		stdout:         out.Bytes(),
		stderr:         errBuf.Bytes(),
		durationMs:     elapsed.Milliseconds(),
		ctxErr:         ctx.Err(),
		runErr:         runErr,
		exitSuccess:    p.ExitSuccess,
		payloadDecoded: true,
	}
	if len(raw.exitSuccess) == 0 {
		raw.exitSuccess = []int{0}
	}
	return raw, nil
}

func exitCodeFrom(cmd *exec.Cmd, runErr error) int {
	if cmd.ProcessState != nil {
		return cmd.ProcessState.ExitCode()
	}
	if runErr == nil {
		return 0
	}
	// Command never started (e.g. ENOENT) — use -1 sentinel.
	return -1
}

var _ outbound.Adapter = (*Adapter)(nil)
