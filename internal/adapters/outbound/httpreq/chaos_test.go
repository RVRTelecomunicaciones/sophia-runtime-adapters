package httpreq_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sophia-ecosystem/runtime-adapters/internal/adapters/outbound/httpreq"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
	"github.com/sophia-ecosystem/runtime-adapters/internal/infrastructure/chaos"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func newChaosHTTPAdapter(t *testing.T) *httpreq.Adapter {
	t.Helper()
	a, err := httpreq.NewAdapter(httpreq.Config{
		AllowPrivateNetworks: true,
		InlineStreamLimit:    16 * 1024,
	}, &shared.FakeClock{T: time.Now()})
	require.NoError(t, err)
	return a
}

func httpCap(t *testing.T) valueobjects.Capability {
	t.Helper()
	a := newChaosHTTPAdapter(t)
	caps := a.Capabilities()
	require.Len(t, caps, 1)
	return caps[0]
}

func mustHTTPPhase1Cap(t *testing.T, canonical string) valueobjects.Capability {
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

func mustHTTPPayload(t *testing.T, v interface{}) valueobjects.Payload {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	p, err := valueobjects.NewPayload(valueobjects.ContentTypeJSON, b, 0)
	require.NoError(t, err)
	return p
}

// ---------------------------------------------------------------------------
// 1. Compile-time + runtime interface assertion
// ---------------------------------------------------------------------------

func TestHTTPReqAdapter_ChaosCapable_Implements(t *testing.T) {
	var _ chaos.ChaosCapable = (*httpreq.Adapter)(nil)
	a := newChaosHTTPAdapter(t)
	var iface chaos.ChaosCapable = a
	require.NotNil(t, iface)
}

// ---------------------------------------------------------------------------
// 2. SupportedChaosFaults — http.request@v1
// ---------------------------------------------------------------------------

func TestHTTPReqAdapter_SupportedChaosFaults_HTTPRequest(t *testing.T) {
	a := newChaosHTTPAdapter(t)
	cap := httpCap(t)
	faults := a.SupportedChaosFaults(cap)
	require.ElementsMatch(t, []chaos.FaultKind{
		chaos.FaultConnectionReset,
		chaos.FaultSlowBody,
		chaos.FaultRedirectLoop,
		chaos.FaultTLSHandshakeFail,
		chaos.FaultAuthFailure,
		chaos.FaultRemoteUnreachable,
	}, faults)
}

// ---------------------------------------------------------------------------
// 3. SupportedChaosFaults — non-HTTP capability → nil
// ---------------------------------------------------------------------------

func TestHTTPReqAdapter_SupportedChaosFaults_NonHTTPCapability_ReturnsNil(t *testing.T) {
	a := newChaosHTTPAdapter(t)
	cap := mustHTTPPhase1Cap(t, "shell.exec@v1")
	faults := a.SupportedChaosFaults(cap)
	require.Empty(t, faults)
}

// ---------------------------------------------------------------------------
// 4. SyntheticOutcome — connection_reset → failure / ExternalFailure
// ---------------------------------------------------------------------------

func TestHTTPReqAdapter_SyntheticOutcome_ConnectionReset_NormalizesToExternalFailure(t *testing.T) {
	a := newChaosHTTPAdapter(t)
	cap := httpCap(t)
	fault := chaos.FaultConfig{Kind: chaos.FaultConnectionReset}
	raw, ok := a.SyntheticOutcome(context.Background(), cap, valueobjects.Payload{}, fault)
	require.True(t, ok)
	require.NotNil(t, raw)

	clk := &shared.FakeClock{T: time.Now()}
	result, err := a.Normalize(cap, raw, clk)
	require.NoError(t, err)
	require.Equal(t, valueobjects.StatusFailure, result.Status)
	require.Equal(t, valueobjects.ErrExternalFailure, result.ErrorClass)
	require.Contains(t, result.ErrorMessage, "connection reset")
}

// ---------------------------------------------------------------------------
// 5. SyntheticOutcome — slow_body → failure / ExternalFailure
// ---------------------------------------------------------------------------

func TestHTTPReqAdapter_SyntheticOutcome_SlowBody_NormalizesToExternalFailure(t *testing.T) {
	a := newChaosHTTPAdapter(t)
	cap := httpCap(t)
	fault := chaos.FaultConfig{Kind: chaos.FaultSlowBody}
	raw, ok := a.SyntheticOutcome(context.Background(), cap, valueobjects.Payload{}, fault)
	require.True(t, ok)
	require.NotNil(t, raw)

	clk := &shared.FakeClock{T: time.Now()}
	result, err := a.Normalize(cap, raw, clk)
	require.NoError(t, err)
	require.Equal(t, valueobjects.StatusFailure, result.Status)
	require.Equal(t, valueobjects.ErrExternalFailure, result.ErrorClass)
	require.Contains(t, result.ErrorMessage, "slow body")
}

// ---------------------------------------------------------------------------
// 6. SyntheticOutcome — redirect_loop → failure / ExternalFailure
// ---------------------------------------------------------------------------

func TestHTTPReqAdapter_SyntheticOutcome_RedirectLoop_NormalizesToExternalFailure(t *testing.T) {
	a := newChaosHTTPAdapter(t)
	cap := httpCap(t)
	fault := chaos.FaultConfig{Kind: chaos.FaultRedirectLoop}
	raw, ok := a.SyntheticOutcome(context.Background(), cap, valueobjects.Payload{}, fault)
	require.True(t, ok)
	require.NotNil(t, raw)

	clk := &shared.FakeClock{T: time.Now()}
	result, err := a.Normalize(cap, raw, clk)
	require.NoError(t, err)
	require.Equal(t, valueobjects.StatusFailure, result.Status)
	require.Equal(t, valueobjects.ErrExternalFailure, result.ErrorClass)
	require.Contains(t, result.ErrorMessage, "redirects")
}

// ---------------------------------------------------------------------------
// 7. SyntheticOutcome — tls_handshake_fail → failure / ExternalFailure
// ---------------------------------------------------------------------------

func TestHTTPReqAdapter_SyntheticOutcome_TLSHandshakeFail_NormalizesToExternalFailure(t *testing.T) {
	a := newChaosHTTPAdapter(t)
	cap := httpCap(t)
	fault := chaos.FaultConfig{Kind: chaos.FaultTLSHandshakeFail}
	raw, ok := a.SyntheticOutcome(context.Background(), cap, valueobjects.Payload{}, fault)
	require.True(t, ok)
	require.NotNil(t, raw)

	clk := &shared.FakeClock{T: time.Now()}
	result, err := a.Normalize(cap, raw, clk)
	require.NoError(t, err)
	require.Equal(t, valueobjects.StatusFailure, result.Status)
	require.Equal(t, valueobjects.ErrExternalFailure, result.ErrorClass)
	require.Contains(t, result.ErrorMessage, "tls")
}

// ---------------------------------------------------------------------------
// 8. SyntheticOutcome — auth_failure → failure / ExternalFailure (status 401)
// ---------------------------------------------------------------------------

func TestHTTPReqAdapter_SyntheticOutcome_AuthFailure_NormalizesToExternalFailure(t *testing.T) {
	a := newChaosHTTPAdapter(t)
	cap := httpCap(t)
	fault := chaos.FaultConfig{Kind: chaos.FaultAuthFailure}
	raw, ok := a.SyntheticOutcome(context.Background(), cap, valueobjects.Payload{}, fault)
	require.True(t, ok)
	require.NotNil(t, raw)

	clk := &shared.FakeClock{T: time.Now()}
	result, err := a.Normalize(cap, raw, clk)
	require.NoError(t, err)
	require.Equal(t, valueobjects.StatusFailure, result.Status)
	require.Equal(t, valueobjects.ErrExternalFailure, result.ErrorClass)
	// Normalizer status-mismatch message includes the status code.
	require.Contains(t, result.ErrorMessage, "401")
}

// ---------------------------------------------------------------------------
// 9. SyntheticOutcome — auth_failure respects payload ExpectedStatus
// ---------------------------------------------------------------------------

func TestHTTPReqAdapter_SyntheticOutcome_AuthFailure_RespectsPayloadExpectedStatus(t *testing.T) {
	a := newChaosHTTPAdapter(t)
	cap := httpCap(t)

	// Payload declares expected_status: [201, 204] — 401 must still be a mismatch.
	payload := mustHTTPPayload(t, map[string]any{
		"method":          "POST",
		"url":             "https://example.invalid/api",
		"expected_status": []int{201, 204},
	})

	fault := chaos.FaultConfig{Kind: chaos.FaultAuthFailure}
	raw, ok := a.SyntheticOutcome(context.Background(), cap, payload, fault)
	require.True(t, ok)
	require.NotNil(t, raw)

	clk := &shared.FakeClock{T: time.Now()}
	result, err := a.Normalize(cap, raw, clk)
	require.NoError(t, err)
	require.Equal(t, valueobjects.StatusFailure, result.Status)
	require.Equal(t, valueobjects.ErrExternalFailure, result.ErrorClass)
	// The mismatch message should reference the caller's expected list.
	require.Contains(t, result.ErrorMessage, "401")
	require.Contains(t, result.ErrorMessage, "201")
}

// ---------------------------------------------------------------------------
// 10. SyntheticOutcome — remote_unreachable → failure / ExternalFailure
// ---------------------------------------------------------------------------

func TestHTTPReqAdapter_SyntheticOutcome_RemoteUnreachable_NormalizesToExternalFailure(t *testing.T) {
	a := newChaosHTTPAdapter(t)
	cap := httpCap(t)
	fault := chaos.FaultConfig{Kind: chaos.FaultRemoteUnreachable}
	raw, ok := a.SyntheticOutcome(context.Background(), cap, valueobjects.Payload{}, fault)
	require.True(t, ok)
	require.NotNil(t, raw)

	clk := &shared.FakeClock{T: time.Now()}
	result, err := a.Normalize(cap, raw, clk)
	require.NoError(t, err)
	require.Equal(t, valueobjects.StatusFailure, result.Status)
	require.Equal(t, valueobjects.ErrExternalFailure, result.ErrorClass)
	require.Contains(t, result.ErrorMessage, "no route to host")
}

// ---------------------------------------------------------------------------
// 11. SyntheticOutcome — payload URL appears in runErr message
// ---------------------------------------------------------------------------

func TestHTTPReqAdapter_SyntheticOutcome_PayloadEncodesURLInRunErr(t *testing.T) {
	const targetURL = "https://example.invalid/foo"
	a := newChaosHTTPAdapter(t)
	cap := httpCap(t)

	payload := mustHTTPPayload(t, map[string]any{
		"method": "GET",
		"url":    targetURL,
	})
	fault := chaos.FaultConfig{Kind: chaos.FaultConnectionReset}

	raw, ok := a.SyntheticOutcome(context.Background(), cap, payload, fault)
	require.True(t, ok)
	require.NotNil(t, raw)

	clk := &shared.FakeClock{T: time.Now()}
	result, err := a.Normalize(cap, raw, clk)
	require.NoError(t, err)
	require.Equal(t, valueobjects.StatusFailure, result.Status)
	require.Contains(t, result.ErrorMessage, targetURL,
		"error message should embed the URL for observability per I24")
}

// ---------------------------------------------------------------------------
// 12. SyntheticOutcome — empty payload falls back to <unknown>
// ---------------------------------------------------------------------------

func TestHTTPReqAdapter_SyntheticOutcome_NoPayload_FallbackToUnknown(t *testing.T) {
	a := newChaosHTTPAdapter(t)
	cap := httpCap(t)
	fault := chaos.FaultConfig{Kind: chaos.FaultConnectionReset}

	raw, ok := a.SyntheticOutcome(context.Background(), cap, valueobjects.Payload{}, fault)
	require.True(t, ok)
	require.NotNil(t, raw)

	clk := &shared.FakeClock{T: time.Now()}
	result, err := a.Normalize(cap, raw, clk)
	require.NoError(t, err)
	require.Equal(t, valueobjects.StatusFailure, result.Status)
	require.Contains(t, result.ErrorMessage, "<unknown>")
}

// ---------------------------------------------------------------------------
// 13. SyntheticOutcome — unsupported fault kind → (nil, false)
// ---------------------------------------------------------------------------

func TestHTTPReqAdapter_SyntheticOutcome_UnsupportedFault_ReturnsFalse(t *testing.T) {
	a := newChaosHTTPAdapter(t)
	cap := httpCap(t)
	// FaultEIO is a filesystem fault — unsupported for httpreq.
	fault := chaos.FaultConfig{Kind: chaos.FaultEIO}
	raw, ok := a.SyntheticOutcome(context.Background(), cap, valueobjects.Payload{}, fault)
	require.False(t, ok)
	require.Nil(t, raw)
}

// ---------------------------------------------------------------------------
// 14. SyntheticOutcome — non-HTTP capability → (nil, false)
// ---------------------------------------------------------------------------

func TestHTTPReqAdapter_SyntheticOutcome_NonHTTPCapability_ReturnsFalse(t *testing.T) {
	a := newChaosHTTPAdapter(t)
	cap := mustHTTPPhase1Cap(t, "shell.exec@v1")
	fault := chaos.FaultConfig{Kind: chaos.FaultConnectionReset}
	raw, ok := a.SyntheticOutcome(context.Background(), cap, valueobjects.Payload{}, fault)
	require.False(t, ok)
	require.Nil(t, raw)
}
