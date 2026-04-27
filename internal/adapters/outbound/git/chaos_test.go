package git_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sophia-ecosystem/runtime-adapters/internal/adapters/outbound/git"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
	"github.com/sophia-ecosystem/runtime-adapters/internal/infrastructure/chaos"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func newChaosTestAdapter(t *testing.T) *git.Adapter {
	t.Helper()
	a, err := git.NewAdapter(git.Config{
		AllowedWorkingDirs: []string{os.TempDir()},
		SSHAgent:           false,
		InlineStreamLimit:  16 * 1024,
	}, &shared.FakeClock{T: time.Now()})
	require.NoError(t, err)
	return a
}

func mustGitCap(t *testing.T, canonical string) valueobjects.Capability {
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

func mustClonePayload(t *testing.T, repoURL string) valueobjects.Payload {
	t.Helper()
	type clonePayloadJSON struct {
		RepoURL string `json:"repo_url"`
	}
	b, err := json.Marshal(clonePayloadJSON{RepoURL: repoURL})
	require.NoError(t, err)
	p, err := valueobjects.NewPayload(valueobjects.ContentTypeJSON, b, 0)
	require.NoError(t, err)
	return p
}

// ---------------------------------------------------------------------------
// 1. Compile-time + runtime interface assertion
// ---------------------------------------------------------------------------

func TestGitAdapter_ChaosCapable_Implements(t *testing.T) {
	var _ chaos.ChaosCapable = (*git.Adapter)(nil)
	a := newChaosTestAdapter(t)
	var iface chaos.ChaosCapable = a
	require.NotNil(t, iface)
}

// ---------------------------------------------------------------------------
// 2–5. SupportedChaosFaults per capability
// ---------------------------------------------------------------------------

func TestGitAdapter_SupportedChaosFaults_Clone(t *testing.T) {
	a := newChaosTestAdapter(t)
	cap := mustGitCap(t, "git.clone@v1")
	faults := a.SupportedChaosFaults(cap)
	require.ElementsMatch(t, []chaos.FaultKind{
		chaos.FaultRemoteUnreachable,
		chaos.FaultAuthFailure,
	}, faults)
}

func TestGitAdapter_SupportedChaosFaults_Status_ReturnsNil(t *testing.T) {
	a := newChaosTestAdapter(t)
	cap := mustGitCap(t, "git.status@v1")
	faults := a.SupportedChaosFaults(cap)
	require.Empty(t, faults)
}

func TestGitAdapter_SupportedChaosFaults_Diff_ReturnsNil(t *testing.T) {
	a := newChaosTestAdapter(t)
	cap := mustGitCap(t, "git.diff@v1")
	faults := a.SupportedChaosFaults(cap)
	require.Empty(t, faults)
}

func TestGitAdapter_SupportedChaosFaults_Commit_ReturnsNil(t *testing.T) {
	a := newChaosTestAdapter(t)
	cap := mustGitCap(t, "git.commit@v1")
	faults := a.SupportedChaosFaults(cap)
	require.Empty(t, faults)
}

// ---------------------------------------------------------------------------
// 6. SyntheticOutcome round-trip: FaultRemoteUnreachable
// ---------------------------------------------------------------------------

func TestGitAdapter_SyntheticOutcome_RemoteUnreachable_NormalizesToExternalFailure(t *testing.T) {
	a := newChaosTestAdapter(t)
	cap := mustGitCap(t, "git.clone@v1")
	fault := chaos.FaultConfig{Kind: chaos.FaultRemoteUnreachable}
	raw, ok := a.SyntheticOutcome(context.Background(), cap, valueobjects.Payload{}, fault)
	require.True(t, ok)
	require.NotNil(t, raw)

	clk := &shared.FakeClock{T: time.Now()}
	result, err := a.NormalizeClone(cap, raw, clk)
	require.NoError(t, err)
	require.Equal(t, valueobjects.StatusFailure, result.Status)
	require.Equal(t, valueobjects.ErrExternalFailure, result.ErrorClass)
}

// ---------------------------------------------------------------------------
// 7. SyntheticOutcome round-trip: FaultAuthFailure
// ---------------------------------------------------------------------------

func TestGitAdapter_SyntheticOutcome_AuthFailure_NormalizesToExternalFailure(t *testing.T) {
	a := newChaosTestAdapter(t)
	cap := mustGitCap(t, "git.clone@v1")
	fault := chaos.FaultConfig{Kind: chaos.FaultAuthFailure}
	raw, ok := a.SyntheticOutcome(context.Background(), cap, valueobjects.Payload{}, fault)
	require.True(t, ok)
	require.NotNil(t, raw)

	clk := &shared.FakeClock{T: time.Now()}
	result, err := a.NormalizeClone(cap, raw, clk)
	require.NoError(t, err)
	require.Equal(t, valueobjects.StatusFailure, result.Status)
	require.Equal(t, valueobjects.ErrExternalFailure, result.ErrorClass)
}

// ---------------------------------------------------------------------------
// 8. SyntheticOutcome: unsupported fault kind on clone cap → (nil, false)
// ---------------------------------------------------------------------------

func TestGitAdapter_SyntheticOutcome_UnsupportedFault_ReturnsFalse(t *testing.T) {
	a := newChaosTestAdapter(t)
	cap := mustGitCap(t, "git.clone@v1")
	fault := chaos.FaultConfig{Kind: chaos.FaultEIO}
	raw, ok := a.SyntheticOutcome(context.Background(), cap, valueobjects.Payload{}, fault)
	require.False(t, ok)
	require.Nil(t, raw)
}

// ---------------------------------------------------------------------------
// 9. SyntheticOutcome: non-clone capability → (nil, false)
// ---------------------------------------------------------------------------

func TestGitAdapter_SyntheticOutcome_NonCloneCapability_ReturnsFalse(t *testing.T) {
	a := newChaosTestAdapter(t)
	cap := mustGitCap(t, "git.status@v1")
	fault := chaos.FaultConfig{Kind: chaos.FaultRemoteUnreachable}
	raw, ok := a.SyntheticOutcome(context.Background(), cap, valueobjects.Payload{}, fault)
	require.False(t, ok)
	require.Nil(t, raw)
}

// ---------------------------------------------------------------------------
// 10. SyntheticOutcome: RepoURL from payload appears in normalized message
// ---------------------------------------------------------------------------

func TestGitAdapter_SyntheticOutcome_PayloadEncodesURLInRunErr(t *testing.T) {
	const repoURL = "git@example.com:org/repo.git"
	a := newChaosTestAdapter(t)
	cap := mustGitCap(t, "git.clone@v1")
	payload := mustClonePayload(t, repoURL)
	fault := chaos.FaultConfig{Kind: chaos.FaultRemoteUnreachable}

	raw, ok := a.SyntheticOutcome(context.Background(), cap, payload, fault)
	require.True(t, ok)
	require.NotNil(t, raw)

	clk := &shared.FakeClock{T: time.Now()}
	result, err := a.NormalizeClone(cap, raw, clk)
	require.NoError(t, err)
	require.Equal(t, valueobjects.StatusFailure, result.Status)
	require.Contains(t, result.ErrorMessage, repoURL,
		"error message should contain the repo URL for observability per I24")

	// Sanity-check that the raw payload bytes also encode cleanly.
	p := payload.Raw()
	var decoded struct {
		RepoURL string `json:"repo_url"`
	}
	require.NoError(t, json.NewDecoder(bytes.NewReader(p)).Decode(&decoded))
	require.Equal(t, repoURL, decoded.RepoURL)
}
