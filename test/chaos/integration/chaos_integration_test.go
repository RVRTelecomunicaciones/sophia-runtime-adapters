//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"

	"github.com/sophia-ecosystem/runtime-adapters/internal/adapters/inbound/sdk"
)

// TestChaos_ClassificationContract exercises each of the 6 CI fault profiles
// (from Bundle 2) end-to-end and asserts the receipt classification, receipt
// persistence, and metric increment contracts.
//
// Each sub-test:
//   - Builds a dedicated runtime app with the profile's chaos config loaded.
//   - Invokes the targeted capability via the SDK peer.
//   - Asserts receipt.{Status, ErrorClass, RetryHint}.
//   - Asserts the receipt is (or is not) persisted in the DB.
//   - Asserts runtime_adapters.execution.total (or receipt.persist.failures) counter.
//
// The ci-persist-fail profile is the inverse case: expects an error to the
// caller, zero receipts in the DB, and receipt.persist.failures >= 1.
//
// Refs: spec §9, Q6=c, plan B3 Tasks 3.1-3.4.
func TestChaos_ClassificationContract(t *testing.T) {
	cases := []struct {
		name              string
		profileRel        string // relative to repo root; must be in chaos loader allowlist
		adapterID         string
		capabilityName    string
		capabilityVersion string
		payloadJSON       string
		timeoutBudgetMs   int64
		// useCancel forces the test to use context.WithCancel + a goroutine
		// that cancels after a short delay, instead of the default
		// context.WithTimeout. Required by the hang_until_cancel scenario
		// to produce ctx.Err() == context.Canceled (which the per-adapter
		// normalizer maps to status=cancelled / error_class=cancelled /
		// retry=non_retryable). Without this, ctx.WithTimeout's
		// DeadlineExceeded would map to timeout instead.
		useCancel      bool
		cancelAfterMs  int64
		expectStatus   string
		expectErrClass string
		expectRetry    string
		expectPersist  bool // false = ci-persist-fail: assert error+zero receipts+counter
	}{
		{
			// shell.exec@v1 hangs; caller cancels via context.Cancel.
			// chaos.FaultHangUntilCancel blocks on <-ctx.Done(), returning
			// ctx.Err() == context.Canceled. The shell normalizer's
			// ctxErr branch with non-deadline error maps to:
			// status=cancelled / error_class=cancelled / retry=non_retryable.
			name:              "shell-hang-cancel",
			profileRel:        "ops/chaos/profiles/ci/ci-shell-hang-cancel.yaml",
			adapterID:         "shell",
			capabilityName:    "exec",
			capabilityVersion: "v1",
			payloadJSON:       `{"command":"true","args":[],"working_dir":"","env":null,"stdin":null}`,
			timeoutBudgetMs:   5000,
			useCancel:         true,
			cancelAfterMs:     50,
			expectStatus:      "cancelled",
			expectErrClass:    "cancelled",
			expectRetry:       "non_retryable",
			expectPersist:     true,
		},
		{
			// git.clone@v1 with remote_unreachable synthetic fault.
			// SyntheticOutcome in git/chaos.go returns a cloneRaw{runErr: ...}.
			// NormalizeClone: runErr path → failure / external_failure / unknown.
			// (ExternalFailure default hint is HintUnknown per DefaultRetryHint.)
			name:              "git-remote-unreachable",
			profileRel:        "ops/chaos/profiles/ci/ci-git-remote-unreachable.yaml",
			adapterID:         "git",
			capabilityName:    "clone",
			capabilityVersion: "v1",
			payloadJSON:       `{"repo_url":"https://example.invalid/repo.git","destination_path":"/tmp/chaos-git-clone","ref":"main","auth_mode":"none"}`,
			timeoutBudgetMs:   5000,
			expectStatus:      "failure",
			expectErrClass:    "external_failure",
			expectRetry:       "unknown",
			expectPersist:     true,
		},
		{
			// filesystem.read_file@v1 with eio (hardware I/O error) synthetic fault.
			// SyntheticOutcome in filesystem/chaos.go returns a readRaw{runErr: ...}.
			// NormalizeRead: runErr path → failure / external_failure / unknown.
			name:              "fs-readfile-eio",
			profileRel:        "ops/chaos/profiles/ci/ci-fs-readfile-eio.yaml",
			adapterID:         "filesystem",
			capabilityName:    "read_file",
			capabilityVersion: "v1",
			payloadJSON:       `{"path":"/tmp/chaos-fs-read","max_bytes":1024}`,
			timeoutBudgetMs:   5000,
			expectStatus:      "failure",
			expectErrClass:    "external_failure",
			expectRetry:       "unknown",
			expectPersist:     true,
		},
		{
			// http.request@v1 with connection_reset synthetic fault.
			// SyntheticOutcome in httpreq/chaos.go returns a httpRaw{runErr: ...}.
			// Normalize: runErr path → failure / external_failure / unknown.
			name:              "http-connection-reset",
			profileRel:        "ops/chaos/profiles/ci/ci-http-connection-reset.yaml",
			adapterID:         "http",
			capabilityName:    "request",
			capabilityVersion: "v1",
			payloadJSON:       `{"method":"GET","url":"https://example.invalid/","headers":{},"body":null,"expected_status":[200]}`,
			timeoutBudgetMs:   5000,
			expectStatus:      "failure",
			expectErrClass:    "external_failure",
			expectRetry:       "unknown",
			expectPersist:     true,
		},
		{
			// shell.exec@v1 with inject_panic fault.
			// ChaosAdapter.dispatchFault panics; SafeExecute recovers it into
			// a panicRaw → execute_service builds:
			// failure / adapter_internal_error / non_retryable.
			name:              "shell-panic",
			profileRel:        "ops/chaos/profiles/ci/ci-shell-panic.yaml",
			adapterID:         "shell",
			capabilityName:    "exec",
			capabilityVersion: "v1",
			payloadJSON:       `{"command":"true","args":[],"working_dir":"","env":null,"stdin":null}`,
			timeoutBudgetMs:   5000,
			expectStatus:      "failure",
			expectErrClass:    "adapter_internal_error",
			expectRetry:       "non_retryable",
			expectPersist:     true,
		},
		{
			// filesystem.read_file@v1 with persist_failure fault on the receipt store.
			// The adapter itself runs normally; ChaosReceiptRepository.Save returns
			// ErrChaosPersistFailure. execute_service propagates that as a Go error.
			// Caller receives an error (no receipt), zero rows in DB,
			// receipt.persist.failures counter >= 1 (A4.3, B8 wired the increment).
			name:              "persist-fail",
			profileRel:        "ops/chaos/profiles/ci/ci-persist-fail.yaml",
			adapterID:         "filesystem",
			capabilityName:    "read_file",
			capabilityVersion: "v1",
			payloadJSON:       `{"path":"/tmp/chaos-persist-fail","max_bytes":1024}`,
			timeoutBudgetMs:   5000,
			expectPersist:     false, // assert error + zero receipts + persist counter
		},
	}

	for _, tc := range cases {
		tc := tc // capture for t.Parallel if added later
		t.Run(tc.name, func(t *testing.T) {
			app, teardown := ChaosTestApp(t, tc.profileRel)
			defer teardown()

			// Two ctx patterns: WithTimeout (default; deadline produces
			// timeout classification) or WithCancel + delayed cancel
			// (produces cancelled classification — required for the
			// hang-cancel scenario).
			var ctx context.Context
			var cancel context.CancelFunc
			if tc.useCancel {
				ctx, cancel = context.WithCancel(context.Background())
				go func() {
					time.Sleep(time.Duration(tc.cancelAfterMs) * time.Millisecond)
					cancel()
				}()
				// Defer is a no-op for already-cancelled ctx; safe.
				defer cancel()
			} else {
				ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
			}

			receipt, err := app.SDK.Execute(ctx, sdk.ExecuteInput{
				CorrelationID:     ulid.Make().String(),
				AdapterID:         tc.adapterID,
				CapabilityName:    tc.capabilityName,
				CapabilityVersion: tc.capabilityVersion,
				ContentType:       "application/json",
				Payload:           []byte(tc.payloadJSON),
				TimeoutBudgetMs:   tc.timeoutBudgetMs,
			})

			if !tc.expectPersist {
				// ci-persist-fail: caller must receive an error (A4.3 —
				// persistence-before-return means the error propagates up).
				require.Error(t, err, "persist failure must surface to caller as Go error")

				// Zero receipts: the chaos receipt store never delegated to
				// the real pg store, so no row was inserted.
				countCtx, countCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer countCancel()
				count, dbErr := app.Receipts.CountAll(countCtx)
				require.NoError(t, dbErr, "CountAll must not fail")
				require.Zero(t, count, "no receipt may be persisted under chaos persist failure")

				// Metric: runtime_adapters.receipt.persist.failures >= 1
				// (B8 wired the increment at the persist-failure site).
				snap := app.SnapshotMetrics(t)
				require.GreaterOrEqual(t, snap.PersistFailures(), int64(1),
					"receipt.persist.failures counter must increment on persist fault")
				return
			}

			// Normal cases: execution succeeds or produces a classified failure;
			// the receipt is persisted and the execution.total counter is incremented.
			require.NoError(t, err, "execution classification failures travel in the receipt, not as Go errors")

			require.Equal(t, tc.expectStatus, string(receipt.Result().Status),
				"receipt status mismatch")
			require.Equal(t, tc.expectErrClass, string(receipt.Result().ErrorClass),
				"receipt error_class mismatch")
			require.Equal(t, tc.expectRetry, string(receipt.Result().Retryable),
				"receipt retry_hint mismatch")

			// Verify the receipt was persisted in the DB (A4.3). Use a
			// fresh ctx in case the original was already cancelled by the
			// useCancel branch above.
			countCtx, countCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer countCancel()
			count, dbErr := app.Receipts.CountAll(countCtx)
			require.NoError(t, dbErr, "CountAll must not fail")
			require.GreaterOrEqual(t, count, int64(1),
				"receipt must be persisted after a classified execution")

			// Verify execution.total metric was incremented.
			snap := app.SnapshotMetrics(t)
			capCanon := tc.adapterID + "." + tc.capabilityName + "@" + tc.capabilityVersion
			require.GreaterOrEqual(t,
				snap.ExecutionTotal(capCanon, tc.expectStatus),
				int64(1),
				"runtime_adapters.execution.total{capability=%q, status=%q} must be >= 1",
				capCanon, tc.expectStatus,
			)
		})
	}
}
