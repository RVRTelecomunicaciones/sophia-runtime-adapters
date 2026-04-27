//go:build integration

package integration

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"

	"github.com/sophia-ecosystem/runtime-adapters/internal/adapters/inbound/sdk"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/entities"
)

// TestPersistInvariant_NoRaceUnderConcurrentPersistFailure verifies the
// persistence-before-return invariant (A4.3) under concurrent load with the
// ci-persist-fail chaos profile active (ChaosReceiptRepository faults every
// Save call).
//
// Four invariants asserted under N=100 concurrent execution requests:
//  1. Every caller received a Go error (no phantom successes).
//  2. Zero receipts persisted in the DB (chaos store rejected every Save).
//  3. runtime_adapters.receipt.persist.failures counter == N.
//  4. No caller observed a non-empty ReceiptID — closes the A4.3 race window
//     (caller must never see a receipt id that the DB doesn't carry).
//
// Refs: spec §10.3, A4.3, plan B4 Task 4.3.
func TestPersistInvariant_NoRaceUnderConcurrentPersistFailure(t *testing.T) {
	app, teardown := ChaosTestApp(t, "ops/chaos/profiles/ci/ci-persist-fail.yaml")
	defer teardown()

	const N = 100

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	results := make([]struct {
		receipt entities.ExecutionReceipt
		err     error
	}, N)

	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			receipt, err := app.SDK.Execute(ctx, sdk.ExecuteInput{
				CorrelationID:     ulid.Make().String(),
				AdapterID:         "filesystem",
				CapabilityName:    "read_file",
				CapabilityVersion: "v1",
				ContentType:       "application/json",
				Payload:           []byte(`{"path":"/tmp/persist-invariant","max_bytes":1024}`),
				TimeoutBudgetMs:   5000,
			})
			results[idx].receipt = receipt
			results[idx].err = err
		}(i)
	}
	wg.Wait()

	// Assertion 1: every caller received a Go error.
	for i, r := range results {
		require.Error(t, r.err, "request %d must have errored under chaos persist failure", i)
	}

	// Assertion 2: zero receipts persisted.
	queryCtx, queryCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer queryCancel()
	count, dbErr := app.Receipts.CountAll(queryCtx)
	require.NoError(t, dbErr, "CountAll must succeed")
	require.Zero(t, count, "no receipt may be persisted under chaos persist failure")

	// Assertion 3: counter incremented exactly N times.
	snap := app.SnapshotMetrics(t)
	require.Equal(t, int64(N), snap.PersistFailures(),
		"receipt.persist.failures counter must equal %d", N)

	// Assertion 4: no caller observed a non-empty ReceiptID
	// (closes A4.3 race: persist failed → caller must NOT see a receipt id).
	for i, r := range results {
		require.Empty(t, r.receipt.ReceiptID().String(),
			"request %d returned a receipt id %q despite chaos persist failure",
			i, r.receipt.ReceiptID().String())
	}
}
