package chaos

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
)

// TestProfile_ParseValidYAML_FromTestdata parses the minimal-enabled fixture
// and asserts every populated field is mapped correctly.
func TestProfile_ParseValidYAML_FromTestdata(t *testing.T) {
	cat := testCatalog()
	p, err := parseProfile("testdata/minimal-enabled.yaml", cat)
	require.NoError(t, err)

	assert.Equal(t, 1, p.Version)
	assert.Equal(t, "minimal-enabled", p.Name)

	fc, ok := p.Adapters["shell.exec@v1"]
	require.True(t, ok, "shell.exec@v1 must be present in Adapters")
	assert.Equal(t, FaultHangUntilCancel, fc.Kind)
	assert.Equal(t, TimingPostDispatch, fc.Timing)

	assert.Equal(t, "cancelled", p.Expected.Status)
	assert.Equal(t, "Interrupted", p.Expected.ErrorClass)
	assert.Equal(t, "non_retryable", p.Expected.RetryHint)

	assert.Nil(t, p.Persist)
}

// TestProfile_RejectsUnknownVersion asserts that version != 1 is rejected
// with an error mentioning "version".
func TestProfile_RejectsUnknownVersion(t *testing.T) {
	cat := testCatalog()
	_, err := parseProfile("testdata/invalid-version.yaml", cat)
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "version"),
		"error %q should mention version", err.Error())
}

// TestProfile_RejectsUnknownFaultKind asserts that an unknown fault kind is
// rejected with an error mentioning "fault kind" and the bad value.
func TestProfile_RejectsUnknownFaultKind(t *testing.T) {
	cat := testCatalog()
	_, err := parseProfile("testdata/invalid-fault.yaml", cat)
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "fault kind"),
		"error %q should mention fault kind", err.Error())
	assert.True(t, strings.Contains(err.Error(), "not_a_real_fault"),
		"error %q should mention the bad value", err.Error())
}

// TestProfile_RejectsCapabilityNotInCatalog asserts that an unknown capability
// is rejected with an error mentioning "not in catalog" and the capability key.
func TestProfile_RejectsCapabilityNotInCatalog(t *testing.T) {
	cat := testCatalog()
	_, err := parseProfile("testdata/invalid-capability.yaml", cat)
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "not in catalog"),
		"error %q should mention not in catalog", err.Error())
	assert.True(t, strings.Contains(err.Error(), "not.a.capability@v1"),
		"error %q should mention the bad capability", err.Error())
}

// TestProfile_RejectsMissingExpectedOutcome asserts that a profile without an
// expected_outcome block is rejected with an error mentioning "expected_outcome".
func TestProfile_RejectsMissingExpectedOutcome(t *testing.T) {
	cat := testCatalog()
	_, err := parseProfile("testdata/missing-expected-outcome.yaml", cat)
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "expected_outcome"),
		"error %q should mention expected_outcome", err.Error())
}

// TestProfile_RejectsEmptyName asserts that a profile with an empty name is
// rejected with an error mentioning "name is required".
func TestProfile_RejectsEmptyName(t *testing.T) {
	cat := testCatalog()
	yaml := []byte(`
version: 1
name: ""
expected_outcome:
  status: success
  error_class: ""
  retry_hint: unknown
`)
	_, err := parseProfileBytes(yaml, cat)
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "name is required"),
		"error %q should mention name is required", err.Error())
}

// TestProfile_RejectsInvalidTiming asserts that an unrecognised timing value
// is rejected with an error mentioning "timing".
func TestProfile_RejectsInvalidTiming(t *testing.T) {
	cat := testCatalog()
	yaml := []byte(`
version: 1
name: bad-timing
adapters:
  shell.exec@v1:
    fault: latency
    timing: middle_of_dispatch
expected_outcome:
  status: success
  error_class: ""
  retry_hint: unknown
`)
	_, err := parseProfileBytes(yaml, cat)
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "timing"),
		"error %q should mention timing", err.Error())
}

// TestProfile_RejectsInvalidPersistFaultKind asserts that an unknown
// persistence.fault value is rejected with an error mentioning "persist".
func TestProfile_RejectsInvalidPersistFaultKind(t *testing.T) {
	cat := testCatalog()
	yaml := []byte(`
version: 1
name: bad-persist
adapters: {}
persistence:
  fault: not_a_real
expected_outcome:
  status: success
  error_class: ""
  retry_hint: unknown
`)
	_, err := parseProfileBytes(yaml, cat)
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "persist"),
		"error %q should mention persist", err.Error())
}

// TestProfile_PersistOnly_ParsesOK asserts that a persist-only profile
// (no adapter faults) parses successfully with Persist populated and
// Adapters empty.
func TestProfile_PersistOnly_ParsesOK(t *testing.T) {
	cat := testCatalog()
	p, err := parseProfile("testdata/persist-only.yaml", cat)
	require.NoError(t, err)

	assert.NotNil(t, p.Persist)
	assert.Equal(t, FaultPersistFailure, p.Persist.Kind)
	assert.Empty(t, p.Adapters)
}

// TestProfile_TimingDefaults_ToEmpty asserts that omitting the timing key
// leaves FaultConfig.Timing as the zero value (empty string).
func TestProfile_TimingDefaults_ToEmpty(t *testing.T) {
	cat := testCatalog()
	yaml := []byte(`
version: 1
name: no-timing
adapters:
  shell.exec@v1:
    fault: latency
expected_outcome:
  status: failure
  error_class: ExternalFailure
  retry_hint: retryable
`)
	p, err := parseProfileBytes(yaml, cat)
	require.NoError(t, err)

	fc, ok := p.Adapters["shell.exec@v1"]
	require.True(t, ok)
	assert.Equal(t, Timing(""), fc.Timing, "timing should default to empty string")
}

// TestNewCatalogFromCapabilities_HasAllPhase1 builds a Catalog from the real
// Phase 1 capabilities and verifies a known canonical is present and an
// unknown one is absent.
func TestNewCatalogFromCapabilities_HasAllPhase1(t *testing.T) {
	caps, err := valueobjects.NewPhase1Capabilities()
	require.NoError(t, err)

	cat := NewCatalogFromCapabilities(caps)

	assert.True(t, cat.Has("shell.exec@v1"), "shell.exec@v1 must be in Phase 1 catalog")
	assert.False(t, cat.Has("nonexistent"), "nonexistent must not be in catalog")
}

// TestParseProfileBytes_DecoderError asserts that malformed YAML returns an
// error mentioning "decode".
func TestParseProfileBytes_DecoderError(t *testing.T) {
	cat := testCatalog()
	_, err := parseProfileBytes([]byte("not yaml: [[["), cat)
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "decode"),
		"error %q should mention decode", err.Error())
}

// TestProfile_AllCIProfilesParse walks ops/chaos/profiles/ci/ and asserts every
// YAML file parses cleanly against the test catalog (all 8 Phase 1 canonicals).
// This catches malformed profiles and schema v1 drift before integration tests
// (Bundle 3) run.
func TestProfile_AllCIProfilesParse(t *testing.T) {
	// Walk ops/chaos/profiles/ci/ from the repo root.
	repoRoot, err := os.Getwd()
	require.NoError(t, err)
	// chaos package's CWD during go test is internal/infrastructure/chaos/.
	// Walk up to the module root (containing go.mod) — match the pattern
	// already used in loader.go's findModuleRoot helper if accessible,
	// otherwise walk up manually until go.mod is found.
	for repoRoot != "/" {
		if _, err := os.Stat(filepath.Join(repoRoot, "go.mod")); err == nil {
			break
		}
		repoRoot = filepath.Dir(repoRoot)
	}
	require.NotEqual(t, "/", repoRoot, "go.mod not found walking up from CWD")

	dir := filepath.Join(repoRoot, "ops/chaos/profiles/ci")
	entries, err := os.ReadDir(dir)
	require.NoError(t, err, "directory %s must exist with profiles", dir)
	require.NotEmpty(t, entries)

	cat := testCatalog() // from chaos_test_helpers_test.go (extends to all 8 Phase 1 capabilities now)
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
			continue
		}
		t.Run(e.Name(), func(t *testing.T) {
			path := filepath.Join(dir, e.Name())
			p, err := parseProfile(path, cat)
			require.NoError(t, err, "profile %s must parse against the test catalog", e.Name())
			require.NotEmpty(t, p.Name, "profile %s name must be non-empty", e.Name())
			require.NotEmpty(t, p.Expected.Status, "profile %s expected_outcome.status must be non-empty", e.Name())
		})
	}
}

// validLocalStatuses enumerates the only ExecutionStatus values permitted by
// R15 ("Only 5 statuses"). The local dir-walk asserts each profile's
// expected_outcome.status is in this set; the CI walker does not, because
// CI profiles are exercised end-to-end and any drift would already trip the
// integration suite. Local profiles are not always run end-to-end (operator
// triage), so this static guard catches drift earlier on the filesystem.
var validLocalStatuses = map[string]struct{}{
	"success":   {},
	"failure":   {},
	"timeout":   {},
	"cancelled": {},
	"partial":   {},
}

// TestProfile_AllLocalProfilesParse walks ops/chaos/profiles/local/ and
// asserts every YAML file parses cleanly against the test catalog (all 8
// Phase 1 canonicals). Sibling guard to TestProfile_AllCIProfilesParse —
// the two are independent: CI profiles ride the production pipeline, local
// profiles are operator-driven triage scenarios that ship in the repo for
// hands-on use against ops/local/compose.yaml.
//
// In addition to the CI walker's parse + non-empty-name + non-empty-status
// checks, this walker validates expected_outcome.status against the closed
// 5-value enum from R15, catching drift in profiles that are not always
// exercised end-to-end.
func TestProfile_AllLocalProfilesParse(t *testing.T) {
	// Walk ops/chaos/profiles/local/ from the repo root. Same module-root
	// resolution pattern as TestProfile_AllCIProfilesParse.
	repoRoot, err := os.Getwd()
	require.NoError(t, err)
	for repoRoot != "/" {
		if _, err := os.Stat(filepath.Join(repoRoot, "go.mod")); err == nil {
			break
		}
		repoRoot = filepath.Dir(repoRoot)
	}
	require.NotEqual(t, "/", repoRoot, "go.mod not found walking up from CWD")

	dir := filepath.Join(repoRoot, "ops/chaos/profiles/local")
	entries, err := os.ReadDir(dir)
	require.NoError(t, err, "directory %s must exist with profiles", dir)
	require.NotEmpty(t, entries, "ops/chaos/profiles/local must contain at least one profile")

	cat := testCatalog()
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
			continue
		}
		t.Run(e.Name(), func(t *testing.T) {
			path := filepath.Join(dir, e.Name())
			p, err := parseProfile(path, cat)
			require.NoError(t, err, "profile %s must parse against the test catalog", e.Name())
			require.NotEmpty(t, p.Name, "profile %s name must be non-empty", e.Name())
			require.NotEmpty(t, p.Expected.Status, "profile %s expected_outcome.status must be non-empty", e.Name())
			_, ok := validLocalStatuses[p.Expected.Status]
			require.True(t, ok,
				"profile %s expected_outcome.status %q must be one of success|failure|timeout|cancelled|partial (R15)",
				e.Name(), p.Expected.Status)
		})
	}
}
