package filesystem_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sophia-ecosystem/runtime-adapters/internal/adapters/outbound/filesystem"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
	"github.com/sophia-ecosystem/runtime-adapters/internal/infrastructure/chaos"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func newChaosFSAdapter(t *testing.T) *filesystem.Adapter {
	t.Helper()
	a, err := filesystem.NewAdapter(filesystem.Config{
		AllowedFilesystemRoots: []string{t.TempDir()},
		InlineStreamLimit:      16 * 1024,
	}, &shared.FakeClock{T: time.Now()})
	require.NoError(t, err)
	return a
}

func mustFSCap(t *testing.T, canonical string) valueobjects.Capability {
	t.Helper()
	caps, err := valueobjects.NewPhase1Capabilities()
	require.NoError(t, err)
	for _, c := range caps {
		if c.Canonical() == canonical {
			return c
		}
	}
	t.Fatalf("%s not in Phase 1 catalog", canonical)
	return valueobjects.Capability{}
}

func mustFSPayloadWithPath(t *testing.T, path string) valueobjects.Payload {
	t.Helper()
	type pathPayload struct {
		Path string `json:"path"`
	}
	b, err := json.Marshal(pathPayload{Path: path})
	require.NoError(t, err)
	p, err := valueobjects.NewPayload(valueobjects.ContentTypeJSON, b, 0)
	require.NoError(t, err)
	return p
}

// ---------------------------------------------------------------------------
// 1. Compile-time + runtime interface assertion
// ---------------------------------------------------------------------------

func TestFilesystemAdapter_ChaosCapable_Implements(t *testing.T) {
	var _ chaos.ChaosCapable = (*filesystem.Adapter)(nil)
	a := newChaosFSAdapter(t)
	var iface chaos.ChaosCapable = a
	require.NotNil(t, iface)
}

// ---------------------------------------------------------------------------
// 2. SupportedChaosFaults — read_file
// ---------------------------------------------------------------------------

func TestFilesystemAdapter_SupportedChaosFaults_ReadFile(t *testing.T) {
	a := newChaosFSAdapter(t)
	cap := mustFSCap(t, "filesystem.read_file@v1")
	faults := a.SupportedChaosFaults(cap)
	require.ElementsMatch(t, []chaos.FaultKind{
		chaos.FaultEIO,
		chaos.FaultPermissionDenied,
	}, faults)
}

// ---------------------------------------------------------------------------
// 3. SupportedChaosFaults — write_file
// ---------------------------------------------------------------------------

func TestFilesystemAdapter_SupportedChaosFaults_WriteFile(t *testing.T) {
	a := newChaosFSAdapter(t)
	cap := mustFSCap(t, "filesystem.write_file@v1")
	faults := a.SupportedChaosFaults(cap)
	require.ElementsMatch(t, []chaos.FaultKind{
		chaos.FaultEIO,
		chaos.FaultENOSPC,
		chaos.FaultPermissionDenied,
	}, faults)
}

// ---------------------------------------------------------------------------
// 4. SupportedChaosFaults — non-filesystem capability → nil
// ---------------------------------------------------------------------------

func TestFilesystemAdapter_SupportedChaosFaults_NonFSCapability_ReturnsNil(t *testing.T) {
	a := newChaosFSAdapter(t)
	cap := mustFSCap(t, "shell.exec@v1")
	faults := a.SupportedChaosFaults(cap)
	require.Empty(t, faults)
}

// ---------------------------------------------------------------------------
// 5. SyntheticOutcome — read_file + EIO
// ---------------------------------------------------------------------------

func TestFilesystemAdapter_SyntheticOutcome_ReadFile_EIO_NormalizesToExternalFailure(t *testing.T) {
	a := newChaosFSAdapter(t)
	cap := mustFSCap(t, "filesystem.read_file@v1")
	fault := chaos.FaultConfig{Kind: chaos.FaultEIO}
	raw, ok := a.SyntheticOutcome(context.Background(), cap, valueobjects.Payload{}, fault)
	require.True(t, ok)
	require.NotNil(t, raw)

	clk := &shared.FakeClock{T: time.Now()}
	result, err := a.NormalizeRead(cap, raw, clk)
	require.NoError(t, err)
	require.Equal(t, valueobjects.StatusFailure, result.Status)
	require.Equal(t, valueobjects.ErrExternalFailure, result.ErrorClass)
	require.Contains(t, result.ErrorMessage, "input/output error")
}

// ---------------------------------------------------------------------------
// 6. SyntheticOutcome — read_file + permission_denied
// ---------------------------------------------------------------------------

func TestFilesystemAdapter_SyntheticOutcome_ReadFile_PermissionDenied_NormalizesToExternalFailure(t *testing.T) {
	a := newChaosFSAdapter(t)
	cap := mustFSCap(t, "filesystem.read_file@v1")
	fault := chaos.FaultConfig{Kind: chaos.FaultPermissionDenied}
	raw, ok := a.SyntheticOutcome(context.Background(), cap, valueobjects.Payload{}, fault)
	require.True(t, ok)
	require.NotNil(t, raw)

	clk := &shared.FakeClock{T: time.Now()}
	result, err := a.NormalizeRead(cap, raw, clk)
	require.NoError(t, err)
	require.Equal(t, valueobjects.StatusFailure, result.Status)
	require.Equal(t, valueobjects.ErrExternalFailure, result.ErrorClass)
	require.Contains(t, result.ErrorMessage, "permission denied")
}

// ---------------------------------------------------------------------------
// 7. SyntheticOutcome — write_file + EIO
// ---------------------------------------------------------------------------

func TestFilesystemAdapter_SyntheticOutcome_WriteFile_EIO_NormalizesToExternalFailure(t *testing.T) {
	a := newChaosFSAdapter(t)
	cap := mustFSCap(t, "filesystem.write_file@v1")
	fault := chaos.FaultConfig{Kind: chaos.FaultEIO}
	raw, ok := a.SyntheticOutcome(context.Background(), cap, valueobjects.Payload{}, fault)
	require.True(t, ok)
	require.NotNil(t, raw)

	clk := &shared.FakeClock{T: time.Now()}
	result, err := a.NormalizeWrite(cap, raw, clk)
	require.NoError(t, err)
	require.Equal(t, valueobjects.StatusFailure, result.Status)
	require.Equal(t, valueobjects.ErrExternalFailure, result.ErrorClass)
	require.Contains(t, result.ErrorMessage, "input/output error")
}

// ---------------------------------------------------------------------------
// 8. SyntheticOutcome — write_file + ENOSPC
// ---------------------------------------------------------------------------

func TestFilesystemAdapter_SyntheticOutcome_WriteFile_ENOSPC_NormalizesToExternalFailure(t *testing.T) {
	a := newChaosFSAdapter(t)
	cap := mustFSCap(t, "filesystem.write_file@v1")
	fault := chaos.FaultConfig{Kind: chaos.FaultENOSPC}
	raw, ok := a.SyntheticOutcome(context.Background(), cap, valueobjects.Payload{}, fault)
	require.True(t, ok)
	require.NotNil(t, raw)

	clk := &shared.FakeClock{T: time.Now()}
	result, err := a.NormalizeWrite(cap, raw, clk)
	require.NoError(t, err)
	require.Equal(t, valueobjects.StatusFailure, result.Status)
	require.Equal(t, valueobjects.ErrExternalFailure, result.ErrorClass)
	require.Contains(t, result.ErrorMessage, "no space left")
}

// ---------------------------------------------------------------------------
// 9. SyntheticOutcome — write_file + permission_denied
// ---------------------------------------------------------------------------

func TestFilesystemAdapter_SyntheticOutcome_WriteFile_PermissionDenied_NormalizesToExternalFailure(t *testing.T) {
	a := newChaosFSAdapter(t)
	cap := mustFSCap(t, "filesystem.write_file@v1")
	fault := chaos.FaultConfig{Kind: chaos.FaultPermissionDenied}
	raw, ok := a.SyntheticOutcome(context.Background(), cap, valueobjects.Payload{}, fault)
	require.True(t, ok)
	require.NotNil(t, raw)

	clk := &shared.FakeClock{T: time.Now()}
	result, err := a.NormalizeWrite(cap, raw, clk)
	require.NoError(t, err)
	require.Equal(t, valueobjects.StatusFailure, result.Status)
	require.Equal(t, valueobjects.ErrExternalFailure, result.ErrorClass)
	require.Contains(t, result.ErrorMessage, "permission denied")
}

// ---------------------------------------------------------------------------
// 10. SyntheticOutcome: path from payload appears in error message
// ---------------------------------------------------------------------------

func TestFilesystemAdapter_SyntheticOutcome_PayloadEncodesPathInRunErr(t *testing.T) {
	const filePath = "/some/file"
	a := newChaosFSAdapter(t)
	cap := mustFSCap(t, "filesystem.read_file@v1")
	payload := mustFSPayloadWithPath(t, filePath)
	fault := chaos.FaultConfig{Kind: chaos.FaultEIO}

	raw, ok := a.SyntheticOutcome(context.Background(), cap, payload, fault)
	require.True(t, ok)
	require.NotNil(t, raw)

	clk := &shared.FakeClock{T: time.Now()}
	result, err := a.NormalizeRead(cap, raw, clk)
	require.NoError(t, err)
	require.Equal(t, valueobjects.StatusFailure, result.Status)
	require.Contains(t, result.ErrorMessage, filePath,
		"error message should contain the file path for observability per I24")
}

// ---------------------------------------------------------------------------
// 11. SyntheticOutcome: unsupported fault on read_file → (nil, false)
// ---------------------------------------------------------------------------

func TestFilesystemAdapter_SyntheticOutcome_UnsupportedFault_ReturnsFalse(t *testing.T) {
	a := newChaosFSAdapter(t)
	cap := mustFSCap(t, "filesystem.read_file@v1")
	fault := chaos.FaultConfig{Kind: chaos.FaultConnectionReset}
	raw, ok := a.SyntheticOutcome(context.Background(), cap, valueobjects.Payload{}, fault)
	require.False(t, ok)
	require.Nil(t, raw)
}

// ---------------------------------------------------------------------------
// 12. SyntheticOutcome: non-filesystem capability → (nil, false)
// ---------------------------------------------------------------------------

func TestFilesystemAdapter_SyntheticOutcome_NonFSCapability_ReturnsFalse(t *testing.T) {
	a := newChaosFSAdapter(t)
	cap := mustFSCap(t, "shell.exec@v1")
	fault := chaos.FaultConfig{Kind: chaos.FaultEIO}
	raw, ok := a.SyntheticOutcome(context.Background(), cap, valueobjects.Payload{}, fault)
	require.False(t, ok)
	require.Nil(t, raw)
}
