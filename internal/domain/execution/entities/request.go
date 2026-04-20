package entities

import (
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
)

// ExecutionRequest is the typed input to the runtime. Immutable after
// construction (I14). All validation happens in NewExecutionRequest; no
// setter exists.
//
// Wire format (snake_case per D5.14):
//
//	correlation_id, adapter_id, capability_name, capability_version,
//	payload, timeout_budget_ms, idempotency_key (optional),
//	actor_id / task_id / workflow_run_id (optional), retry_attempt (optional),
//	submitted_at (runtime-set).
type ExecutionRequest struct {
	correlationID     shared.CorrelationID
	adapterID         valueobjects.AdapterID
	capabilityName    string
	capabilityVersion string
	payload           valueobjects.Payload
	timeoutBudget     valueobjects.TimeoutBudget
	idempotencyKey    *shared.IdempotencyKey
	actorID           string
	taskID            string
	workflowRunID     string
	retryAttempt      int
	submittedAt       time.Time
}

// ExecutionRequestInput carries the inputs needed to construct an
// ExecutionRequest. Fields that are required at the domain level are also
// required here. Optional fields are pointers or zero-valued strings.
type ExecutionRequestInput struct {
	CorrelationID     shared.CorrelationID
	AdapterID         valueobjects.AdapterID
	CapabilityName    string
	CapabilityVersion string
	Payload           valueobjects.Payload
	TimeoutBudget     valueobjects.TimeoutBudget

	// Optional:
	IdempotencyKey *shared.IdempotencyKey
	ActorID        string
	TaskID         string
	WorkflowRunID  string
	RetryAttempt   int
}

const metadataIDMaxLen = 128

var (
	capabilityNameRE    = regexp.MustCompile(`^[a-z][a-z0-9_.]{1,63}$`)
	capabilityVersionRE = regexp.MustCompile(`^v[0-9]+$`)
)

// NewExecutionRequest validates the input and returns an immutable
// ExecutionRequest. The clock provides submittedAt; pass shared.RealClock
// in production.
func NewExecutionRequest(in ExecutionRequestInput, clk shared.Clock) (ExecutionRequest, error) {
	// correlation_id, adapter_id, payload, timeout_budget are zero-valued-invalid
	// by construction (their own constructors). Here we only check fields with
	// semantics that aren't captured by their types.
	if in.CorrelationID.String() == "" {
		return ExecutionRequest{}, fmt.Errorf("correlation_id is required")
	}
	if in.AdapterID.String() == "" {
		return ExecutionRequest{}, fmt.Errorf("adapter_id is required")
	}
	if !capabilityNameRE.MatchString(in.CapabilityName) {
		return ExecutionRequest{}, fmt.Errorf("invalid capability_name %q (must match %s)", in.CapabilityName, capabilityNameRE)
	}
	if !capabilityVersionRE.MatchString(in.CapabilityVersion) {
		return ExecutionRequest{}, fmt.Errorf("invalid capability_version %q (must match %s)", in.CapabilityVersion, capabilityVersionRE)
	}
	if in.Payload.SizeBytes() == 0 {
		return ExecutionRequest{}, fmt.Errorf("payload is required")
	}
	if in.TimeoutBudget.Milliseconds() <= 0 {
		return ExecutionRequest{}, fmt.Errorf("timeout_budget must be > 0")
	}
	if err := checkMetadataID("actor_id", in.ActorID); err != nil {
		return ExecutionRequest{}, err
	}
	if err := checkMetadataID("task_id", in.TaskID); err != nil {
		return ExecutionRequest{}, err
	}
	if err := checkMetadataID("workflow_run_id", in.WorkflowRunID); err != nil {
		return ExecutionRequest{}, err
	}
	if in.RetryAttempt < 0 {
		return ExecutionRequest{}, fmt.Errorf("retry_attempt must be ≥ 0, got %d", in.RetryAttempt)
	}

	return ExecutionRequest{
		correlationID:     in.CorrelationID,
		adapterID:         in.AdapterID,
		capabilityName:    in.CapabilityName,
		capabilityVersion: in.CapabilityVersion,
		payload:           in.Payload,
		timeoutBudget:     in.TimeoutBudget,
		idempotencyKey:    in.IdempotencyKey,
		actorID:           in.ActorID,
		taskID:            in.TaskID,
		workflowRunID:     in.WorkflowRunID,
		retryAttempt:      in.RetryAttempt,
		submittedAt:       clk.Now(),
	}, nil
}

func checkMetadataID(field, v string) error {
	if len(v) > metadataIDMaxLen {
		return fmt.Errorf("%s exceeds %d chars (got %d)", field, metadataIDMaxLen, len(v))
	}
	return nil
}

// Getters — no setters, immutable.
func (r ExecutionRequest) CorrelationID() shared.CorrelationID       { return r.correlationID }
func (r ExecutionRequest) AdapterID() valueobjects.AdapterID         { return r.adapterID }
func (r ExecutionRequest) CapabilityName() string                    { return r.capabilityName }
func (r ExecutionRequest) CapabilityVersion() string                 { return r.capabilityVersion }
func (r ExecutionRequest) Payload() valueobjects.Payload             { return r.payload }
func (r ExecutionRequest) TimeoutBudget() valueobjects.TimeoutBudget { return r.timeoutBudget }

// IdempotencyKey returns the optional idempotency key and a boolean indicating
// whether one was set.
func (r ExecutionRequest) IdempotencyKey() (shared.IdempotencyKey, bool) {
	if r.idempotencyKey == nil {
		return shared.IdempotencyKey{}, false
	}
	return *r.idempotencyKey, true
}

func (r ExecutionRequest) ActorID() string        { return r.actorID }
func (r ExecutionRequest) TaskID() string         { return r.taskID }
func (r ExecutionRequest) WorkflowRunID() string  { return r.workflowRunID }
func (r ExecutionRequest) RetryAttempt() int      { return r.retryAttempt }
func (r ExecutionRequest) SubmittedAt() time.Time { return r.submittedAt }

// CapabilityCanonical returns "<adapter_id>.<name>@<version>", matching
// Capability.Canonical() from the valueobjects package. This is the key
// used for normalizer and capability-registry lookups.
func (r ExecutionRequest) CapabilityCanonical() string {
	return r.adapterID.String() + "." + r.capabilityName + "@" + r.capabilityVersion
}

// MarshalJSON emits the wire format (snake_case, UTC ms timestamps).
func (r ExecutionRequest) MarshalJSON() ([]byte, error) {
	type wire struct {
		CorrelationID     shared.CorrelationID   `json:"correlation_id"`
		AdapterID         valueobjects.AdapterID `json:"adapter_id"`
		CapabilityName    string                 `json:"capability_name"`
		CapabilityVersion string                 `json:"capability_version"`
		Payload           json.RawMessage        `json:"payload"`
		TimeoutBudgetMs   int64                  `json:"timeout_budget_ms"`
		IdempotencyKey    *shared.IdempotencyKey `json:"idempotency_key,omitempty"`
		ActorID           string                 `json:"actor_id,omitempty"`
		TaskID            string                 `json:"task_id,omitempty"`
		WorkflowRunID     string                 `json:"workflow_run_id,omitempty"`
		RetryAttempt      int                    `json:"retry_attempt,omitempty"`
		SubmittedAt       string                 `json:"submitted_at"`
	}
	w := wire{
		CorrelationID:     r.correlationID,
		AdapterID:         r.adapterID,
		CapabilityName:    r.capabilityName,
		CapabilityVersion: r.capabilityVersion,
		Payload:           r.payload.Raw(),
		TimeoutBudgetMs:   r.timeoutBudget.Milliseconds(),
		IdempotencyKey:    r.idempotencyKey,
		ActorID:           r.actorID,
		TaskID:            r.taskID,
		WorkflowRunID:     r.workflowRunID,
		RetryAttempt:      r.retryAttempt,
		SubmittedAt:       r.submittedAt.UTC().Format("2006-01-02T15:04:05.000Z07:00"),
	}
	return json.Marshal(w)
}

// UnmarshalJSON parses the wire format and validates via the same rules
// as NewExecutionRequest. DisallowUnknownFields is the caller's
// responsibility (applied at the inbound HTTP boundary — D5.14).
func (r *ExecutionRequest) UnmarshalJSON(b []byte) error {
	type wire struct {
		CorrelationID     string          `json:"correlation_id"`
		AdapterID         string          `json:"adapter_id"`
		CapabilityName    string          `json:"capability_name"`
		CapabilityVersion string          `json:"capability_version"`
		Payload           json.RawMessage `json:"payload"`
		TimeoutBudgetMs   int64           `json:"timeout_budget_ms"`
		IdempotencyKey    *string         `json:"idempotency_key,omitempty"`
		ActorID           string          `json:"actor_id,omitempty"`
		TaskID            string          `json:"task_id,omitempty"`
		WorkflowRunID     string          `json:"workflow_run_id,omitempty"`
		RetryAttempt      int             `json:"retry_attempt,omitempty"`
		SubmittedAt       string          `json:"submitted_at,omitempty"`
	}
	var w wire
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	cid, err := shared.NewCorrelationID(w.CorrelationID)
	if err != nil {
		return err
	}
	aid, err := valueobjects.NewAdapterID(w.AdapterID)
	if err != nil {
		return err
	}
	pl, err := valueobjects.NewPayload(valueobjects.ContentTypeJSON, w.Payload, 0)
	if err != nil {
		return err
	}
	tb, err := valueobjects.NewTimeoutBudget(w.TimeoutBudgetMs, 0)
	if err != nil {
		return err
	}
	var ikPtr *shared.IdempotencyKey
	if w.IdempotencyKey != nil {
		ik, err := shared.NewIdempotencyKey(*w.IdempotencyKey)
		if err != nil {
			return err
		}
		ikPtr = &ik
	}
	in := ExecutionRequestInput{
		CorrelationID: cid, AdapterID: aid,
		CapabilityName: w.CapabilityName, CapabilityVersion: w.CapabilityVersion,
		Payload: pl, TimeoutBudget: tb, IdempotencyKey: ikPtr,
		ActorID: w.ActorID, TaskID: w.TaskID, WorkflowRunID: w.WorkflowRunID,
		RetryAttempt: w.RetryAttempt,
	}
	// Re-use the validation; but we want submittedAt from the wire, not fresh clock.
	clk := frozenClock{}
	if w.SubmittedAt != "" {
		t, err := time.Parse(time.RFC3339Nano, w.SubmittedAt)
		if err != nil {
			return fmt.Errorf("invalid submitted_at %q: %w", w.SubmittedAt, err)
		}
		clk = frozenClock{t: t.UTC()}
	}
	parsed, err := NewExecutionRequest(in, clk)
	if err != nil {
		return err
	}
	*r = parsed
	return nil
}

// frozenClock is an internal Clock implementation that always returns a
// fixed time. Used by UnmarshalJSON to preserve the wire-provided
// submitted_at rather than minting a fresh timestamp.
type frozenClock struct{ t time.Time }

func (f frozenClock) Now() time.Time {
	if f.t.IsZero() {
		return time.Now().UTC()
	}
	return f.t
}
