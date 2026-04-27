package chaos

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/entities"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
)

// testRawOutcome is a minimal AdapterRawOutcome used across chaos package
// white-box tests. Shared here so that chaos_adapter_test.go and
// chaos_receipt_repository_test.go (both package chaos) can use it without
// duplicate-type collisions.
type testRawOutcome struct{ tag string }

func (testRawOutcome) IsAdapterRawOutcome() {}

// mustCapability returns the Phase 1 capability with the given canonical or
// fails the test. Used by both ChaosAdapter and ChaosReceiptRepository tests.
func mustCapability(t *testing.T, canonical string) valueobjects.Capability {
	t.Helper()
	caps, err := valueobjects.NewPhase1Capabilities()
	require.NoError(t, err)
	for _, c := range caps {
		if c.Canonical() == canonical {
			return c
		}
	}
	t.Fatalf("capability %q not in Phase 1 catalog", canonical)
	return valueobjects.Capability{}
}

// mustReceipt builds a minimal valid ExecutionReceipt using the given ULID
// as its ReceiptID. Used by ChaosReceiptRepository tests.
func mustReceipt(t *testing.T, receiptULID string) entities.ExecutionReceipt {
	t.Helper()

	// Two distinct ULIDs are needed: one for the receipt, one for the handle.
	handleULID := "01HZXK5JC6QK7XV0YQXA0QJ0YA"
	if receiptULID == handleULID {
		handleULID = "01HZXK5JC6QK7XV0YQXA0QJ0YB"
	}
	gen := &entities.FakeIDGen{IDs: []string{receiptULID}}

	baseTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := &shared.FakeClock{T: baseTime}

	cid, err := shared.NewCorrelationID("01HZXK5JC6QK7XV0YQXA0QJ0YZ")
	require.NoError(t, err, "mustReceipt NewCorrelationID")

	aid, err := valueobjects.NewAdapterID("shell")
	require.NoError(t, err, "mustReceipt NewAdapterID")

	cap, err := valueobjects.NewCapability(aid, "exec", "v1", false, 5*time.Second)
	require.NoError(t, err, "mustReceipt NewCapability")

	pl, err := valueobjects.NewPayload(valueobjects.ContentTypeJSON, json.RawMessage(`{"cmd":"ls"}`), 0)
	require.NoError(t, err, "mustReceipt NewPayload")

	tb, err := valueobjects.NewTimeoutBudget(5000, 0)
	require.NoError(t, err, "mustReceipt NewTimeoutBudget")

	req, err := entities.NewExecutionRequest(entities.ExecutionRequestInput{
		CorrelationID:     cid,
		AdapterID:         aid,
		CapabilityName:    cap.Name(),
		CapabilityVersion: cap.Version(),
		Payload:           pl,
		TimeoutBudget:     tb,
	}, clk)
	require.NoError(t, err, "mustReceipt NewExecutionRequest")

	handleGen := &entities.FakeIDGen{IDs: []string{handleULID}}
	clk.Advance(time.Millisecond)
	h, err := entities.NewExecutionHandle(handleGen, cid, cap, clk)
	require.NoError(t, err, "mustReceipt NewExecutionHandle")

	clk.Advance(100 * time.Millisecond)
	res, err := entities.NewExecutionResult(
		valueobjects.StatusSuccess,
		valueobjects.HintNonRetryable,
		"", "",
		nil, nil, nil,
		nil, nil,
		0, 0,
		100*time.Millisecond,
		clk.Now(),
	)
	require.NoError(t, err, "mustReceipt NewExecutionResult")

	prov, err := entities.NewProvenance(entities.ProvTest, "1.0.0", "host-a", "runtime-1.2.3", "")
	require.NoError(t, err, "mustReceipt NewProvenance")

	timBase := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	timClk := &shared.FakeClock{T: timBase}
	bld := entities.NewTimingsBuilder(timClk).MarkSubmitted()
	timClk.Advance(10 * time.Millisecond)
	bld.MarkStarted()
	timClk.Advance(100 * time.Millisecond)
	bld.MarkCompleted()
	timings, err := bld.Build()
	require.NoError(t, err, "mustReceipt Build timings")

	clk.Advance(10 * time.Millisecond)
	receipt, err := entities.NewExecutionReceipt(gen, req, h, res, prov, timings, clk)
	require.NoError(t, err, "mustReceipt NewExecutionReceipt")
	return receipt
}
