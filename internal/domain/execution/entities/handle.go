package entities

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
)

// IDGenerator produces new identifier strings. Production wiring uses
// ULIDGen which emits Crockford base-32 ULIDs suitable for ReceiptID and
// HandleID (I1, §4.2). Tests may inject a deterministic generator to make
// assertions on IDs.
type IDGenerator interface {
	New() string
}

// ULIDGen is the production IDGenerator. Each call returns a new ULID
// with millisecond timestamp derived from time.Now() and entropy from
// crypto/rand. The returned string is the 26-char Crockford base-32
// canonical form expected by shared.ReceiptID and shared.HandleID.
type ULIDGen struct {
	// Entropy is the source of ULID randomness. Defaults to crypto/rand.Reader
	// when nil. Exposed for tests that need reproducible output.
	Entropy io.Reader
}

// New returns a new ULID string.
func (g ULIDGen) New() string {
	src := g.Entropy
	if src == nil {
		src = rand.Reader
	}
	// ulid.Now() returns the current Unix ms as uint64 — matches the
	// spec requirement "UTC with millisecond precision".
	id := ulid.MustNew(ulid.Now(), src)
	return id.String()
}

// FakeIDGen returns a deterministic sequence of pre-supplied ULID strings.
// Test-only; panics if New() is called past the end of the sequence.
type FakeIDGen struct {
	IDs []string
	n   int
}

// New returns the next ID in the sequence. Panics if exhausted.
func (g *FakeIDGen) New() string {
	if g.n >= len(g.IDs) {
		panic(fmt.Sprintf("FakeIDGen exhausted after %d calls", g.n))
	}
	s := g.IDs[g.n]
	g.n++
	return s
}

// ExecutionHandle is a lightweight reference to a running or completed
// execution (§4.2). Not persisted independently in Phase 1 — it is
// embedded in the ExecutionReceipt. Fields are unexported; access via
// getters.
type ExecutionHandle struct {
	handleID      shared.HandleID
	correlationID shared.CorrelationID
	adapterID     valueobjects.AdapterID
	capability    valueobjects.Capability
	startedAt     time.Time
}

// NewExecutionHandle generates a new ULID handle_id from the injected
// IDGenerator and stamps started_at from the clock.
func NewExecutionHandle(gen IDGenerator, cid shared.CorrelationID, cap valueobjects.Capability, clk shared.Clock) (ExecutionHandle, error) {
	if gen == nil {
		return ExecutionHandle{}, fmt.Errorf("IDGenerator is required")
	}
	if clk == nil {
		return ExecutionHandle{}, fmt.Errorf("Clock is required")
	}
	hid, err := shared.NewHandleID(gen.New())
	if err != nil {
		return ExecutionHandle{}, fmt.Errorf("generate handle_id: %w", err)
	}
	if cid.String() == "" {
		return ExecutionHandle{}, fmt.Errorf("correlation_id is required")
	}
	if cap.AdapterID().String() == "" {
		return ExecutionHandle{}, fmt.Errorf("capability is required (zero value)")
	}
	return ExecutionHandle{
		handleID:      hid,
		correlationID: cid,
		adapterID:     cap.AdapterID(),
		capability:    cap,
		startedAt:     clk.Now(),
	}, nil
}

// HandleID returns the handle's unique identifier.
func (h ExecutionHandle) HandleID() shared.HandleID { return h.handleID }

// CorrelationID returns the cross-repository correlation identifier.
func (h ExecutionHandle) CorrelationID() shared.CorrelationID { return h.correlationID }

// AdapterID returns the owning adapter's identifier.
func (h ExecutionHandle) AdapterID() valueobjects.AdapterID { return h.adapterID }

// Capability returns the versioned capability that was invoked.
func (h ExecutionHandle) Capability() valueobjects.Capability { return h.capability }

// StartedAt returns the UTC timestamp when the handle was created.
func (h ExecutionHandle) StartedAt() time.Time { return h.startedAt }

// MarshalJSON emits snake_case wire format per D5.14. The capability is
// serialized as its canonical string (e.g. "shell.exec@v1").
func (h ExecutionHandle) MarshalJSON() ([]byte, error) {
	type wire struct {
		HandleID      shared.HandleID        `json:"handle_id"`
		CorrelationID shared.CorrelationID   `json:"correlation_id"`
		AdapterID     valueobjects.AdapterID `json:"adapter_id"`
		CapabilityCan string                 `json:"capability"`
		StartedAt     string                 `json:"started_at"`
	}
	return json.Marshal(wire{
		HandleID:      h.handleID,
		CorrelationID: h.correlationID,
		AdapterID:     h.adapterID,
		CapabilityCan: h.capability.Canonical(),
		StartedAt:     h.startedAt.UTC().Format("2006-01-02T15:04:05.000Z07:00"),
	})
}

// UnmarshalJSON reconstructs an ExecutionHandle from its MarshalJSON output.
// The capability field on the wire is a canonical string; this decoder
// parses the canonical back into an AdapterID + name + version + defaults
// (AllowsPartial=false, DefaultTimeout=time.Second — the persisted receipt
// is trusted, and capability timeouts live in the registry, not the receipt).
//
// This is used by persistence adapters (internal/adapters/outbound/pg/)
// when rehydrating receipts from the database. It is NOT intended for
// untrusted wire inputs — those should go through a registry lookup.
func (h *ExecutionHandle) UnmarshalJSON(b []byte) error {
	type wire struct {
		HandleID      string `json:"handle_id"`
		CorrelationID string `json:"correlation_id"`
		AdapterID     string `json:"adapter_id"`
		CapabilityCan string `json:"capability"`
		StartedAt     string `json:"started_at"`
	}
	var w wire
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	hid, err := shared.NewHandleID(w.HandleID)
	if err != nil {
		return fmt.Errorf("handle_id: %w", err)
	}
	cid, err := shared.NewCorrelationID(w.CorrelationID)
	if err != nil {
		return fmt.Errorf("correlation_id: %w", err)
	}
	aid, err := valueobjects.NewAdapterID(w.AdapterID)
	if err != nil {
		return fmt.Errorf("adapter_id: %w", err)
	}
	// Parse capability canonical "<adapter>.<name>@<version>" to reconstruct.
	canon := w.CapabilityCan
	atIdx := strings.LastIndex(canon, "@")
	if atIdx < 0 {
		return fmt.Errorf("capability missing @version: %q", canon)
	}
	version := canon[atIdx+1:]
	nameWithAdapter := canon[:atIdx]
	dotIdx := strings.Index(nameWithAdapter, ".")
	if dotIdx < 0 {
		return fmt.Errorf("capability missing adapter.name: %q", canon)
	}
	name := nameWithAdapter[dotIdx+1:]
	cap, err := valueobjects.NewCapability(aid, name, version, false, time.Second)
	if err != nil {
		return fmt.Errorf("capability: %w", err)
	}
	t, err := time.Parse(time.RFC3339Nano, w.StartedAt)
	if err != nil {
		return fmt.Errorf("started_at: %w", err)
	}
	h.handleID = hid
	h.correlationID = cid
	h.adapterID = aid
	h.capability = cap
	h.startedAt = t.UTC()
	return nil
}
