package shell_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sophia-ecosystem/runtime-adapters/internal/adapters/outbound/shell"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
	"github.com/sophia-ecosystem/runtime-adapters/internal/infrastructure/chaos"
)

func newTestAdapter(t *testing.T) *shell.Adapter {
	t.Helper()
	cfg := shell.Config{
		AllowedCommandsPath: []string{"/usr/bin", "/bin"},
		AllowedWorkingDirs:  []string{"/tmp"},
		InlineStreamLimit:   1024,
	}
	a, err := shell.NewAdapter(cfg)
	require.NoError(t, err)
	return a
}

func mustShellCap(t *testing.T) valueobjects.Capability {
	t.Helper()
	caps, err := valueobjects.NewPhase1Capabilities()
	require.NoError(t, err)
	for _, c := range caps {
		if c.Canonical() == "shell.exec@v1" {
			return c
		}
	}
	t.Fatal("shell.exec@v1 not in catalog")
	return valueobjects.Capability{}
}

func TestShellAdapter_ChaosCapable_Implements(t *testing.T) {
	var _ chaos.ChaosCapable = (*shell.Adapter)(nil)
	a := newTestAdapter(t)
	var iface chaos.ChaosCapable = a
	require.NotNil(t, iface)
}

func TestShellAdapter_SupportedChaosFaults_ShellExec(t *testing.T) {
	a := newTestAdapter(t)
	faults := a.SupportedChaosFaults(mustShellCap(t))
	require.ElementsMatch(t, []chaos.FaultKind{chaos.FaultProcessSignal, chaos.FaultProcessExit}, faults)
}

func TestShellAdapter_SupportedChaosFaults_NonShellCapability_ReturnsNil(t *testing.T) {
	// Construct a capability not owned by shell — pick from Phase 1 catalog.
	caps, err := valueobjects.NewPhase1Capabilities()
	require.NoError(t, err)
	var nonShell valueobjects.Capability
	for _, c := range caps {
		if c.Canonical() != "shell.exec@v1" {
			nonShell = c
			break
		}
	}
	require.NotEqual(t, "shell.exec@v1", nonShell.Canonical())

	a := newTestAdapter(t)
	faults := a.SupportedChaosFaults(nonShell)
	require.Empty(t, faults)
}

func TestShellAdapter_SyntheticOutcome_ProcessSignal_NormalizesToExternalFailure(t *testing.T) {
	a := newTestAdapter(t)
	cap := mustShellCap(t)
	fault := chaos.FaultConfig{Kind: chaos.FaultProcessSignal, Match: map[string]string{"signal": "SIGKILL"}}
	raw, ok := a.SyntheticOutcome(context.Background(), cap, valueobjects.Payload{}, fault)
	require.True(t, ok)
	require.NotNil(t, raw)

	// Round-trip through the real normalizer; assert the resulting
	// ExecutionResult shape is what we'd see for a real signal-kill.
	clk := &shared.FakeClock{}
	result, err := a.Normalize(cap, raw, clk)
	require.NoError(t, err)
	require.Equal(t, valueobjects.StatusFailure, result.Status)
	require.Equal(t, valueobjects.ErrExternalFailure, result.ErrorClass)
}

func TestShellAdapter_SyntheticOutcome_ProcessExit_DefaultExit137(t *testing.T) {
	a := newTestAdapter(t)
	cap := mustShellCap(t)
	fault := chaos.FaultConfig{Kind: chaos.FaultProcessExit}
	raw, ok := a.SyntheticOutcome(context.Background(), cap, valueobjects.Payload{}, fault)
	require.True(t, ok)
	clk := &shared.FakeClock{}
	result, err := a.Normalize(cap, raw, clk)
	require.NoError(t, err)
	require.Equal(t, valueobjects.StatusFailure, result.Status)
	require.Equal(t, valueobjects.ErrExternalFailure, result.ErrorClass)
	// ExitCode pointer should be 137 by default
	require.NotNil(t, result.ExitCode)
	require.Equal(t, 137, *result.ExitCode)
}

func TestShellAdapter_SyntheticOutcome_ProcessExit_CustomExitCode(t *testing.T) {
	a := newTestAdapter(t)
	cap := mustShellCap(t)
	fault := chaos.FaultConfig{Kind: chaos.FaultProcessExit, Match: map[string]string{"exit_code": "42"}}
	raw, ok := a.SyntheticOutcome(context.Background(), cap, valueobjects.Payload{}, fault)
	require.True(t, ok)
	clk := &shared.FakeClock{}
	result, err := a.Normalize(cap, raw, clk)
	require.NoError(t, err)
	require.NotNil(t, result.ExitCode)
	require.Equal(t, 42, *result.ExitCode)
}

func TestShellAdapter_SyntheticOutcome_UnsupportedFault_ReturnsFalse(t *testing.T) {
	a := newTestAdapter(t)
	cap := mustShellCap(t)
	fault := chaos.FaultConfig{Kind: chaos.FaultEIO}
	raw, ok := a.SyntheticOutcome(context.Background(), cap, valueobjects.Payload{}, fault)
	require.False(t, ok)
	require.Nil(t, raw)
}

func TestShellAdapter_SyntheticOutcome_NonShellCapability_ReturnsFalse(t *testing.T) {
	caps, err := valueobjects.NewPhase1Capabilities()
	require.NoError(t, err)
	var nonShell valueobjects.Capability
	for _, c := range caps {
		if c.Canonical() != "shell.exec@v1" {
			nonShell = c
			break
		}
	}
	a := newTestAdapter(t)
	fault := chaos.FaultConfig{Kind: chaos.FaultProcessExit}
	raw, ok := a.SyntheticOutcome(context.Background(), nonShell, valueobjects.Payload{}, fault)
	require.False(t, ok)
	require.Nil(t, raw)
}
