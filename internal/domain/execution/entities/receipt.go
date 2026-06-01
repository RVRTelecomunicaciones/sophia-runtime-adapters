package entities

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
)

// UnmarshalJSON reconstructs an ExecutionReceipt from its MarshalJSON
// output. Intended for persistence adapters (pg/) that rehydrate
// trusted receipts from storage. NOT for untrusted wire inputs.
func (r *ExecutionReceipt) UnmarshalJSON(b []byte) error {
	type wire struct {
		ReceiptID     string           `json:"receipt_id"`
		SchemaVersion string           `json:"schema_version"`
		Request       ExecutionRequest `json:"request"`
		Handle        ExecutionHandle  `json:"handle"`
		Result        ExecutionResult  `json:"result"`
		Provenance    Provenance       `json:"provenance"`
		Timings       Timings          `json:"timings"`
		CreatedAt     string           `json:"created_at"`
		PersistedAt   *string          `json:"persisted_at,omitempty"`
	}
	var w wire
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	rid, err := shared.NewReceiptID(w.ReceiptID)
	if err != nil {
		return fmt.Errorf("receipt_id: %w", err)
	}
	if w.SchemaVersion != SchemaVersionV1 {
		return fmt.Errorf("schema_version mismatch: %q (want %q)", w.SchemaVersion, SchemaVersionV1)
	}
	createdAt, err := time.Parse(time.RFC3339Nano, w.CreatedAt)
	if err != nil {
		return fmt.Errorf("created_at: %w", err)
	}
	var persistedAt *time.Time
	if w.PersistedAt != nil {
		t, err := time.Parse(time.RFC3339Nano, *w.PersistedAt)
		if err != nil {
			return fmt.Errorf("persisted_at: %w", err)
		}
		tUTC := t.UTC()
		persistedAt = &tUTC
	}
	r.receiptID = rid
	r.schemaVersion = w.SchemaVersion
	r.request = w.Request
	r.handle = w.Handle
	r.result = w.Result
	r.provenance = w.Provenance
	r.timings = w.Timings
	r.createdAt = createdAt.UTC()
	r.persistedAt = persistedAt
	return nil
}

// SchemaVersionV1 is the Phase 1 schema version. Bumping requires ADR +
// migration plan (R15 / I20).
const SchemaVersionV1 = "v1"

// ExecutionReceipt is the central auditable artifact of the runtime (D1.4,
// D3.2). It is the single aggregate root of the execution bounded
// context. Identity is ReceiptID. Once persisted the receipt is
// append-only (R2) — corrections happen via new receipts, never in-place
// mutations.
//
// Wire format (snake_case per D5.14, schema_version stamped "v1"):
//
//	receipt_id, schema_version, request, handle, result, provenance,
//	timings, created_at, persisted_at (nullable).
type ExecutionReceipt struct {
	receiptID     shared.ReceiptID
	schemaVersion string
	request       ExecutionRequest
	handle        ExecutionHandle
	result        ExecutionResult
	provenance    Provenance
	timings       Timings
	createdAt     time.Time
	persistedAt   *time.Time
}

// NewExecutionReceipt assembles a receipt. Always stamps schema_version
// = "v1". gen supplies a fresh ReceiptID; clk supplies created_at.
func NewExecutionReceipt(
	gen IDGenerator,
	req ExecutionRequest,
	h ExecutionHandle,
	res ExecutionResult,
	prov Provenance,
	timings Timings,
	clk shared.Clock,
) (ExecutionReceipt, error) {
	if gen == nil {
		return ExecutionReceipt{}, fmt.Errorf("IDGenerator is required")
	}
	if clk == nil {
		return ExecutionReceipt{}, fmt.Errorf("clock is required")
	}
	rid, err := shared.NewReceiptID(gen.New())
	if err != nil {
		return ExecutionReceipt{}, fmt.Errorf("generate receipt_id: %w", err)
	}
	if req.CorrelationID().String() == "" {
		return ExecutionReceipt{}, fmt.Errorf("request.correlation_id is required (zero request)")
	}
	if h.HandleID().String() == "" {
		return ExecutionReceipt{}, fmt.Errorf("handle.handle_id is required (zero handle)")
	}
	if prov.Source == "" {
		return ExecutionReceipt{}, fmt.Errorf("provenance.source is required (zero provenance)")
	}
	if timings.SubmittedAt.IsZero() {
		return ExecutionReceipt{}, fmt.Errorf("timings.submitted_at is required (zero timings)")
	}
	return ExecutionReceipt{
		receiptID:     rid,
		schemaVersion: SchemaVersionV1,
		request:       req,
		handle:        h,
		result:        res,
		provenance:    prov,
		timings:       timings,
		createdAt:     clk.Now().UTC(),
	}, nil
}

// Getters — no setters, aggregate root is immutable.
func (r ExecutionReceipt) ReceiptID() shared.ReceiptID { return r.receiptID }
func (r ExecutionReceipt) SchemaVersion() string       { return r.schemaVersion }
func (r ExecutionReceipt) Request() ExecutionRequest   { return r.request }
func (r ExecutionReceipt) Handle() ExecutionHandle     { return r.handle }
func (r ExecutionReceipt) Result() ExecutionResult     { return r.result }
func (r ExecutionReceipt) Provenance() Provenance      { return r.provenance }
func (r ExecutionReceipt) Timings() Timings            { return r.timings }
func (r ExecutionReceipt) CreatedAt() time.Time        { return r.createdAt }

// PersistedAt returns (t, true) when set, else zero time and false.
func (r ExecutionReceipt) PersistedAt() (time.Time, bool) {
	if r.persistedAt == nil {
		return time.Time{}, false
	}
	return *r.persistedAt, true
}

// WithPersistedAt returns a COPY of the receipt with PersistedAt set.
// The original is unchanged — aggregate root immutability (R2) is
// preserved. Used by persistence adapters when recording the DB-assigned
// persistence timestamp.
func (r ExecutionReceipt) WithPersistedAt(t time.Time) ExecutionReceipt {
	t = t.UTC()
	r.persistedAt = &t
	return r
}

// StripStreams returns a copy with the inner ExecutionResult's streams
// nil'd (UC3 include_streams=false per §6.4). Original unchanged.
func (r ExecutionReceipt) StripStreams() ExecutionReceipt {
	r.result = r.result.StripStreams()
	return r
}

// MarshalJSON emits the wire format with snake_case keys.
func (r ExecutionReceipt) MarshalJSON() ([]byte, error) {
	type wire struct {
		ReceiptID     shared.ReceiptID `json:"receipt_id"`
		SchemaVersion string           `json:"schema_version"`
		Request       ExecutionRequest `json:"request"`
		Handle        ExecutionHandle  `json:"handle"`
		Result        ExecutionResult  `json:"result"`
		Provenance    Provenance       `json:"provenance"`
		Timings       Timings          `json:"timings"`
		CreatedAt     string           `json:"created_at"`
		PersistedAt   *string          `json:"persisted_at,omitempty"`
	}
	w := wire{
		ReceiptID:     r.receiptID,
		SchemaVersion: r.schemaVersion,
		Request:       r.request,
		Handle:        r.handle,
		Result:        r.result,
		Provenance:    r.provenance,
		Timings:       r.timings,
		CreatedAt:     r.createdAt.UTC().Format("2006-01-02T15:04:05.000Z07:00"),
	}
	if r.persistedAt != nil {
		s := r.persistedAt.UTC().Format("2006-01-02T15:04:05.000Z07:00")
		w.PersistedAt = &s
	}
	return json.Marshal(w)
}
