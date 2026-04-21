package entities_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/entities"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
)

// ─── receipt test helpers ─────────────────────────────────────────────────────

// secondValidULID is a distinct ULID used wherever a second unique ID is needed.
const secondValidULID = "01HZXK5JC6QK7XV0YQXA0QJ0YA"

func mustRequest(t *testing.T) entities.ExecutionRequest {
	t.Helper()
	cid := mustCorrelationID(t, validULID)
	aid := mustAdapterID(t, "http")
	pl := mustPayload(t)
	tb := mustTimeoutBudget(t, 5000)
	clk := fakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	req, err := entities.NewExecutionRequest(entities.ExecutionRequestInput{
		CorrelationID:     cid,
		AdapterID:         aid,
		CapabilityName:    "re.quest",
		CapabilityVersion: "v1",
		Payload:           pl,
		TimeoutBudget:     tb,
	}, clk)
	if err != nil {
		t.Fatalf("mustRequest: %v", err)
	}
	return req
}

func mustHandle(t *testing.T) entities.ExecutionHandle {
	t.Helper()
	cid := mustCorrelationID(t, validULID)
	aid, err := valueobjects.NewAdapterID("shell")
	if err != nil {
		t.Fatalf("mustHandle NewAdapterID: %v", err)
	}
	cap, err := valueobjects.NewCapability(aid, "exec", "v1", false, 5*time.Second)
	if err != nil {
		t.Fatalf("mustHandle NewCapability: %v", err)
	}
	gen := &entities.FakeIDGen{IDs: []string{secondValidULID}}
	clk := fakeClock(time.Date(2026, 1, 1, 0, 0, 1, 0, time.UTC))
	h, err := entities.NewExecutionHandle(gen, cid, cap, clk)
	if err != nil {
		t.Fatalf("mustHandle: %v", err)
	}
	return h
}

func mustSuccessResult(t *testing.T) entities.ExecutionResult {
	t.Helper()
	res, err := entities.NewExecutionResult(
		valueobjects.StatusSuccess,
		valueobjects.HintNonRetryable,
		"", "",
		nil, nil, nil,
		nil, nil,
		0, 0,
		100*time.Millisecond,
		time.Date(2026, 1, 1, 0, 0, 2, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("mustSuccessResult: %v", err)
	}
	return res
}

func mustResultWithStatus(t *testing.T, status valueobjects.ExecutionStatus) entities.ExecutionResult {
	t.Helper()
	var errClass valueobjects.ErrorClass
	var errMsg string
	switch status {
	case valueobjects.StatusSuccess:
		// nothing
	case valueobjects.StatusTimeout:
		errClass = valueobjects.ErrTimeout
	case valueobjects.StatusCancelled:
		errClass = valueobjects.ErrCancelled
	case valueobjects.StatusFailure, valueobjects.StatusPartial:
		errClass = valueobjects.ErrAdapterInternalError
		errMsg = "something went wrong"
	}
	res, err := entities.NewExecutionResult(
		status,
		valueobjects.HintRetryable,
		errClass, errMsg,
		nil, nil, nil,
		nil, nil,
		0, 0,
		50*time.Millisecond,
		time.Date(2026, 1, 1, 0, 0, 2, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("mustResultWithStatus(%s): %v", status, err)
	}
	return res
}

func mustProvenance(t *testing.T) entities.Provenance {
	t.Helper()
	prov, err := entities.NewProvenance(entities.ProvTest, "1.0.0", "host-a", "runtime-1.2.3", "")
	if err != nil {
		t.Fatalf("mustProvenance: %v", err)
	}
	return prov
}

func mustTimings(t *testing.T) entities.Timings {
	t.Helper()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := &shared.FakeClock{T: base}
	bld := entities.NewTimingsBuilder(clk).MarkSubmitted()
	clk.Advance(10 * time.Millisecond)
	bld.MarkStarted()
	clk.Advance(100 * time.Millisecond)
	bld.MarkCompleted()
	tim, err := bld.Build()
	if err != nil {
		t.Fatalf("mustTimings: %v", err)
	}
	return tim
}

func mustReceipt(t *testing.T) entities.ExecutionReceipt {
	t.Helper()
	gen := &entities.FakeIDGen{IDs: []string{validULID}}
	clk := fakeClock(time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC))
	r, err := entities.NewExecutionReceipt(
		gen,
		mustRequest(t),
		mustHandle(t),
		mustSuccessResult(t),
		mustProvenance(t),
		mustTimings(t),
		clk,
	)
	if err != nil {
		t.Fatalf("mustReceipt: %v", err)
	}
	return r
}

// ─── 1. Construction happy path ───────────────────────────────────────────────

func TestNewExecutionReceipt_HappyPath(t *testing.T) {
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	gen := &entities.FakeIDGen{IDs: []string{validULID}}
	clk := fakeClock(now)

	req := mustRequest(t)
	h := mustHandle(t)
	res := mustSuccessResult(t)
	prov := mustProvenance(t)
	tim := mustTimings(t)

	r, err := entities.NewExecutionReceipt(gen, req, h, res, prov, tim, clk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if r.SchemaVersion() != entities.SchemaVersionV1 {
		t.Errorf("SchemaVersion = %q, want %q", r.SchemaVersion(), entities.SchemaVersionV1)
	}
	if r.ReceiptID().String() != validULID {
		t.Errorf("ReceiptID = %q, want %q", r.ReceiptID().String(), validULID)
	}
	if _, err := shared.NewReceiptID(r.ReceiptID().String()); err != nil {
		t.Errorf("ReceiptID is not a valid ULID: %v", err)
	}
	if !r.CreatedAt().Equal(now) {
		t.Errorf("CreatedAt = %v, want %v", r.CreatedAt(), now)
	}
}

// ─── 2. Nil generator, nil clock → error ─────────────────────────────────────

func TestNewExecutionReceipt_NilGeneratorRejected(t *testing.T) {
	clk := fakeClock(time.Now())
	_, err := entities.NewExecutionReceipt(nil, mustRequest(t), mustHandle(t), mustSuccessResult(t), mustProvenance(t), mustTimings(t), clk)
	if err == nil {
		t.Fatal("expected error for nil generator, got nil")
	}
	if !strings.Contains(err.Error(), "IDGenerator") {
		t.Errorf("error %q should mention IDGenerator", err.Error())
	}
}

func TestNewExecutionReceipt_NilClockRejected(t *testing.T) {
	gen := &entities.FakeIDGen{IDs: []string{validULID}}
	_, err := entities.NewExecutionReceipt(gen, mustRequest(t), mustHandle(t), mustSuccessResult(t), mustProvenance(t), mustTimings(t), nil)
	if err == nil {
		t.Fatal("expected error for nil clock, got nil")
	}
	if !strings.Contains(err.Error(), "Clock") {
		t.Errorf("error %q should mention Clock", err.Error())
	}
}

// ─── 3. Invalid generator output → error via NewReceiptID ────────────────────

func TestNewExecutionReceipt_InvalidGeneratorOutputRejected(t *testing.T) {
	gen := &entities.FakeIDGen{IDs: []string{"not-a-ulid"}}
	clk := fakeClock(time.Now())
	_, err := entities.NewExecutionReceipt(gen, mustRequest(t), mustHandle(t), mustSuccessResult(t), mustProvenance(t), mustTimings(t), clk)
	if err == nil {
		t.Fatal("expected error for invalid generator output, got nil")
	}
	if !strings.Contains(err.Error(), "generate receipt_id") {
		t.Errorf("error %q should mention generate receipt_id", err.Error())
	}
}

// ─── 4. Zero request → error ──────────────────────────────────────────────────

func TestNewExecutionReceipt_ZeroRequestRejected(t *testing.T) {
	gen := &entities.FakeIDGen{IDs: []string{validULID}}
	clk := fakeClock(time.Now())
	_, err := entities.NewExecutionReceipt(gen, entities.ExecutionRequest{}, mustHandle(t), mustSuccessResult(t), mustProvenance(t), mustTimings(t), clk)
	if err == nil {
		t.Fatal("expected error for zero request, got nil")
	}
	if !strings.Contains(err.Error(), "correlation_id") {
		t.Errorf("error %q should mention correlation_id", err.Error())
	}
}

// ─── 5. Zero handle → error ───────────────────────────────────────────────────

func TestNewExecutionReceipt_ZeroHandleRejected(t *testing.T) {
	gen := &entities.FakeIDGen{IDs: []string{validULID}}
	clk := fakeClock(time.Now())
	_, err := entities.NewExecutionReceipt(gen, mustRequest(t), entities.ExecutionHandle{}, mustSuccessResult(t), mustProvenance(t), mustTimings(t), clk)
	if err == nil {
		t.Fatal("expected error for zero handle, got nil")
	}
	if !strings.Contains(err.Error(), "handle_id") {
		t.Errorf("error %q should mention handle_id", err.Error())
	}
}

// ─── 6. Zero provenance → error ───────────────────────────────────────────────

func TestNewExecutionReceipt_ZeroProvenanceRejected(t *testing.T) {
	gen := &entities.FakeIDGen{IDs: []string{validULID}}
	clk := fakeClock(time.Now())
	_, err := entities.NewExecutionReceipt(gen, mustRequest(t), mustHandle(t), mustSuccessResult(t), entities.Provenance{}, mustTimings(t), clk)
	if err == nil {
		t.Fatal("expected error for zero provenance, got nil")
	}
	if !strings.Contains(err.Error(), "provenance.source") {
		t.Errorf("error %q should mention provenance.source", err.Error())
	}
}

// ─── 7. Zero timings → error ─────────────────────────────────────────────────

func TestNewExecutionReceipt_ZeroTimingsRejected(t *testing.T) {
	gen := &entities.FakeIDGen{IDs: []string{validULID}}
	clk := fakeClock(time.Now())
	_, err := entities.NewExecutionReceipt(gen, mustRequest(t), mustHandle(t), mustSuccessResult(t), mustProvenance(t), entities.Timings{}, clk)
	if err == nil {
		t.Fatal("expected error for zero timings, got nil")
	}
	if !strings.Contains(err.Error(), "timings.submitted_at") {
		t.Errorf("error %q should mention timings.submitted_at", err.Error())
	}
}

// ─── 8. Result can be any status ─────────────────────────────────────────────

func TestNewExecutionReceipt_AllResultStatuses(t *testing.T) {
	statuses := []valueobjects.ExecutionStatus{
		valueobjects.StatusSuccess,
		valueobjects.StatusFailure,
		valueobjects.StatusTimeout,
		valueobjects.StatusCancelled,
		valueobjects.StatusPartial,
	}
	for _, status := range statuses {
		t.Run(string(status), func(t *testing.T) {
			gen := &entities.FakeIDGen{IDs: []string{validULID}}
			clk := fakeClock(time.Now())
			res := mustResultWithStatus(t, status)
			_, err := entities.NewExecutionReceipt(gen, mustRequest(t), mustHandle(t), res, mustProvenance(t), mustTimings(t), clk)
			if err != nil {
				t.Errorf("status %s: unexpected error: %v", status, err)
			}
		})
	}
}

// ─── 9. Getters ───────────────────────────────────────────────────────────────

func TestExecutionReceipt_Getters(t *testing.T) {
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	gen := &entities.FakeIDGen{IDs: []string{validULID}}
	clk := fakeClock(now)

	req := mustRequest(t)
	h := mustHandle(t)
	res := mustSuccessResult(t)
	prov := mustProvenance(t)
	tim := mustTimings(t)

	r, err := entities.NewExecutionReceipt(gen, req, h, res, prov, tim, clk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if r.ReceiptID().String() != validULID {
		t.Errorf("ReceiptID = %q, want %q", r.ReceiptID().String(), validULID)
	}
	if r.SchemaVersion() != entities.SchemaVersionV1 {
		t.Errorf("SchemaVersion = %q, want %q", r.SchemaVersion(), entities.SchemaVersionV1)
	}
	if r.Request().CorrelationID().String() != req.CorrelationID().String() {
		t.Errorf("Request.CorrelationID = %q, want %q", r.Request().CorrelationID(), req.CorrelationID())
	}
	if r.Handle().HandleID().String() != h.HandleID().String() {
		t.Errorf("Handle.HandleID = %q, want %q", r.Handle().HandleID(), h.HandleID())
	}
	if r.Result().Status != res.Status {
		t.Errorf("Result.Status = %q, want %q", r.Result().Status, res.Status)
	}
	if r.Provenance().Source != prov.Source {
		t.Errorf("Provenance.Source = %q, want %q", r.Provenance().Source, prov.Source)
	}
	if !r.Timings().SubmittedAt.Equal(tim.SubmittedAt) {
		t.Errorf("Timings.SubmittedAt = %v, want %v", r.Timings().SubmittedAt, tim.SubmittedAt)
	}
	if !r.CreatedAt().Equal(now) {
		t.Errorf("CreatedAt = %v, want %v", r.CreatedAt(), now)
	}
}

// ─── 10. PersistedAt unset → (_, false) ──────────────────────────────────────

func TestExecutionReceipt_PersistedAt_Unset(t *testing.T) {
	r := mustReceipt(t)
	_, ok := r.PersistedAt()
	if ok {
		t.Error("PersistedAt should return false when unset")
	}
}

// ─── 11. WithPersistedAt returns copy; original unchanged ────────────────────

func TestExecutionReceipt_WithPersistedAt_ReturnsCopy(t *testing.T) {
	original := mustReceipt(t)
	pt := time.Date(2026, 4, 19, 12, 30, 0, 0, time.UTC)

	updated := original.WithPersistedAt(pt)

	// Updated has the persisted timestamp.
	got, ok := updated.PersistedAt()
	if !ok {
		t.Fatal("updated receipt: PersistedAt should return true")
	}
	if !got.Equal(pt) {
		t.Errorf("updated.PersistedAt = %v, want %v", got, pt)
	}

	// Original is unchanged.
	_, okOrig := original.PersistedAt()
	if okOrig {
		t.Error("original receipt should not have PersistedAt set after WithPersistedAt")
	}
}

func TestExecutionReceipt_WithPersistedAt_NormalizesToUTC(t *testing.T) {
	r := mustReceipt(t)
	loc, _ := time.LoadLocation("America/New_York")
	localTime := time.Date(2026, 4, 19, 8, 0, 0, 0, loc) // -4h from UTC
	updated := r.WithPersistedAt(localTime)
	got, _ := updated.PersistedAt()
	if got.Location() != time.UTC {
		t.Errorf("PersistedAt should be UTC, got location %v", got.Location())
	}
	wantUTC := localTime.UTC()
	if !got.Equal(wantUTC) {
		t.Errorf("PersistedAt = %v, want %v", got, wantUTC)
	}
}

// ─── 12. StripStreams returns copy; original unchanged ────────────────────────

func TestExecutionReceipt_StripStreams(t *testing.T) {
	// Build a result with stream refs.
	data := []byte("hello stdout")
	streamRef, err := entities.NewStreamRef(data, int64(len(data)), 16*1024)
	if err != nil {
		t.Fatalf("NewStreamRef: %v", err)
	}
	res, err := entities.NewExecutionResult(
		valueobjects.StatusSuccess,
		valueobjects.HintNonRetryable,
		"", "",
		&streamRef, &streamRef, nil,
		nil, nil,
		0, 0,
		100*time.Millisecond,
		time.Date(2026, 1, 1, 0, 0, 2, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("NewExecutionResult: %v", err)
	}

	gen := &entities.FakeIDGen{IDs: []string{validULID}}
	clk := fakeClock(time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC))
	original, err := entities.NewExecutionReceipt(gen, mustRequest(t), mustHandle(t), res, mustProvenance(t), mustTimings(t), clk)
	if err != nil {
		t.Fatalf("NewExecutionReceipt: %v", err)
	}

	stripped := original.StripStreams()

	// Stripped has no stream refs.
	if stripped.Result().StdoutRef != nil {
		t.Error("stripped receipt should have nil StdoutRef")
	}
	if stripped.Result().StderrRef != nil {
		t.Error("stripped receipt should have nil StderrRef")
	}

	// Original is unchanged.
	if original.Result().StdoutRef == nil {
		t.Error("original receipt StdoutRef should not be nil after StripStreams")
	}
	if original.Result().StderrRef == nil {
		t.Error("original receipt StderrRef should not be nil after StripStreams")
	}
}

// ─── 13. MarshalJSON wire format ─────────────────────────────────────────────

func TestExecutionReceipt_MarshalJSON_SnakeCaseKeys(t *testing.T) {
	now := time.Date(2026, 4, 19, 12, 30, 45, 123000000, time.UTC)
	gen := &entities.FakeIDGen{IDs: []string{validULID}}
	clk := fakeClock(now)

	r, err := entities.NewExecutionReceipt(gen, mustRequest(t), mustHandle(t), mustSuccessResult(t), mustProvenance(t), mustTimings(t), clk)
	if err != nil {
		t.Fatalf("NewExecutionReceipt: %v", err)
	}

	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}

	var got map[string]interface{}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal for inspection: %v", err)
	}

	for _, key := range []string{
		"receipt_id", "schema_version", "request", "handle", "result",
		"provenance", "timings", "created_at",
	} {
		if _, ok := got[key]; !ok {
			t.Errorf("missing key %q in JSON: %s", key, string(b))
		}
	}

	// schema_version must always be "v1".
	if got["schema_version"] != entities.SchemaVersionV1 {
		t.Errorf("schema_version = %q, want %q", got["schema_version"], entities.SchemaVersionV1)
	}

	// receipt_id matches injected ULID.
	if got["receipt_id"] != validULID {
		t.Errorf("receipt_id = %v, want %q", got["receipt_id"], validULID)
	}

	// persisted_at omitted when unset.
	if _, hasPersisted := got["persisted_at"]; hasPersisted {
		t.Error("persisted_at should be omitted when unset")
	}

	// created_at is ms-precision UTC ISO.
	wantCreatedAt := "2026-04-19T12:30:45.123Z"
	if got["created_at"] != wantCreatedAt {
		t.Errorf("created_at = %v, want %q", got["created_at"], wantCreatedAt)
	}
}

func TestExecutionReceipt_MarshalJSON_PersistedAtPresent(t *testing.T) {
	r := mustReceipt(t)
	pt := time.Date(2026, 4, 19, 12, 30, 0, 0, time.UTC)
	rWithPersist := r.WithPersistedAt(pt)

	b, err := json.Marshal(rWithPersist)
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}

	var got map[string]interface{}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if _, ok := got["persisted_at"]; !ok {
		t.Errorf("persisted_at should be present when set: %s", string(b))
	}
	wantPersistedAt := "2026-04-19T12:30:00.000Z"
	if got["persisted_at"] != wantPersistedAt {
		t.Errorf("persisted_at = %v, want %q", got["persisted_at"], wantPersistedAt)
	}
}

// ─── 14. Embed-in-struct marshals correctly ───────────────────────────────────

func TestExecutionReceipt_EmbedInStruct_MarshalJSON(t *testing.T) {
	type wrapper struct {
		Receipt entities.ExecutionReceipt `json:"receipt"`
	}
	r := mustReceipt(t)
	w := wrapper{Receipt: r}

	b, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("MarshalJSON wrapper: %v", err)
	}

	var got map[string]interface{}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal wrapper: %v", err)
	}

	receiptVal, ok := got["receipt"].(map[string]interface{})
	if !ok {
		t.Fatalf("receipt field is not an object: %s", string(b))
	}

	for _, key := range []string{"receipt_id", "schema_version", "created_at"} {
		if _, ok := receiptVal[key]; !ok {
			t.Errorf("embedded receipt missing key %q: %s", key, string(b))
		}
	}
}

// ─── 15. MarshalJSON includes nested receipt fields ───────────────────────────

func TestExecutionReceipt_MarshalJSON_NestedFields(t *testing.T) {
	r := mustReceipt(t)

	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}

	var got map[string]interface{}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// request is a nested object.
	reqObj, ok := got["request"].(map[string]interface{})
	if !ok {
		t.Fatalf("request field is not an object: %s", string(b))
	}
	if _, hasCorr := reqObj["correlation_id"]; !hasCorr {
		t.Error("request.correlation_id missing")
	}

	// handle is a nested object.
	handleObj, ok := got["handle"].(map[string]interface{})
	if !ok {
		t.Fatalf("handle field is not an object: %s", string(b))
	}
	if _, hasHandle := handleObj["handle_id"]; !hasHandle {
		t.Error("handle.handle_id missing")
	}

	// result is a nested object.
	if _, ok := got["result"].(map[string]interface{}); !ok {
		t.Fatalf("result field is not an object: %s", string(b))
	}

	// provenance is a nested object.
	provObj, ok := got["provenance"].(map[string]interface{})
	if !ok {
		t.Fatalf("provenance field is not an object: %s", string(b))
	}
	if _, hasSource := provObj["source"]; !hasSource {
		t.Error("provenance.source missing")
	}

	// timings is a nested object.
	timObj, ok := got["timings"].(map[string]interface{})
	if !ok {
		t.Fatalf("timings field is not an object: %s", string(b))
	}
	if _, hasSubmitted := timObj["submitted_at"]; !hasSubmitted {
		t.Error("timings.submitted_at missing")
	}
}

// ─── JSON round-trip (Added in Bundle 5 T48 for persistence round-trip support) ─

// TestExecutionReceipt_JSONRoundTrip verifies Marshal → Unmarshal yields
// a receipt with equal public state.
func TestExecutionReceipt_JSONRoundTrip(t *testing.T) {
	original := mustReceipt(t)

	b, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}

	var restored entities.ExecutionReceipt
	if err := json.Unmarshal(b, &restored); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}

	if restored.ReceiptID().String() != original.ReceiptID().String() {
		t.Errorf("ReceiptID: got %q, want %q", restored.ReceiptID(), original.ReceiptID())
	}
	if restored.SchemaVersion() != original.SchemaVersion() {
		t.Errorf("SchemaVersion: got %q, want %q", restored.SchemaVersion(), original.SchemaVersion())
	}
	if restored.Request().CorrelationID().String() != original.Request().CorrelationID().String() {
		t.Errorf("Request.CorrelationID: got %q, want %q", restored.Request().CorrelationID(), original.Request().CorrelationID())
	}
	if restored.Handle().HandleID().String() != original.Handle().HandleID().String() {
		t.Errorf("Handle.HandleID: got %q, want %q", restored.Handle().HandleID(), original.Handle().HandleID())
	}
	if restored.Result().Status != original.Result().Status {
		t.Errorf("Result.Status: got %q, want %q", restored.Result().Status, original.Result().Status)
	}
	if restored.Provenance().Source != original.Provenance().Source {
		t.Errorf("Provenance.Source: got %q, want %q", restored.Provenance().Source, original.Provenance().Source)
	}
	if !restored.Timings().SubmittedAt.Equal(original.Timings().SubmittedAt) {
		t.Errorf("Timings.SubmittedAt: got %v, want %v", restored.Timings().SubmittedAt, original.Timings().SubmittedAt)
	}
	if !restored.CreatedAt().Equal(original.CreatedAt()) {
		t.Errorf("CreatedAt: got %v, want %v", restored.CreatedAt(), original.CreatedAt())
	}
	_, ok := restored.PersistedAt()
	if ok {
		t.Error("PersistedAt should be absent after round-trip of unset receipt")
	}
}

// ─── No exported fields ───────────────────────────────────────────────────────

func TestExecutionReceipt_NoExportedFields(t *testing.T) {
	rType := reflect.TypeOf(entities.ExecutionReceipt{})
	for i := range rType.NumField() {
		f := rType.Field(i)
		if f.IsExported() {
			t.Errorf("ExecutionReceipt field %q must not be exported", f.Name)
		}
	}
}

// ─── SchemaVersionV1 constant ─────────────────────────────────────────────────

func TestSchemaVersionV1_IsV1(t *testing.T) {
	if entities.SchemaVersionV1 != "v1" {
		t.Errorf("SchemaVersionV1 = %q, want %q", entities.SchemaVersionV1, "v1")
	}
}
