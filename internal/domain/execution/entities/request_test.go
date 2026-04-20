package entities_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/entities"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
)

// helpers ─────────────────────────────────────────────────────────────────────

// validULID is a well-formed Crockford base-32 ULID (26 chars, no I/L/O/U).
const validULID = "01HZXK5JC6QK7XV0YQXA0QJ0YZ"

func mustCorrelationID(t *testing.T, s string) shared.CorrelationID {
	t.Helper()
	id, err := shared.NewCorrelationID(s)
	if err != nil {
		t.Fatalf("mustCorrelationID(%q): %v", s, err)
	}
	return id
}

func mustAdapterID(t *testing.T, s string) valueobjects.AdapterID {
	t.Helper()
	id, err := valueobjects.NewAdapterID(s)
	if err != nil {
		t.Fatalf("mustAdapterID(%q): %v", s, err)
	}
	return id
}

func mustPayload(t *testing.T) valueobjects.Payload {
	t.Helper()
	pl, err := valueobjects.NewPayload(valueobjects.ContentTypeJSON, json.RawMessage(`{"k":1}`), 0)
	if err != nil {
		t.Fatalf("mustPayload: %v", err)
	}
	return pl
}

func mustTimeoutBudget(t *testing.T, ms int64) valueobjects.TimeoutBudget {
	t.Helper()
	tb, err := valueobjects.NewTimeoutBudget(ms, 0)
	if err != nil {
		t.Fatalf("mustTimeoutBudget(%d): %v", ms, err)
	}
	return tb
}

func mustIdempotencyKey(t *testing.T, s string) *shared.IdempotencyKey {
	t.Helper()
	ik, err := shared.NewIdempotencyKey(s)
	if err != nil {
		t.Fatalf("mustIdempotencyKey(%q): %v", s, err)
	}
	return &ik
}

func fakeClock(ts time.Time) *shared.FakeClock {
	return &shared.FakeClock{T: ts}
}

func validInput(t *testing.T) entities.ExecutionRequestInput {
	t.Helper()
	return entities.ExecutionRequestInput{
		CorrelationID:     mustCorrelationID(t, validULID),
		AdapterID:         mustAdapterID(t, "http"),
		CapabilityName:    "re.quest",
		CapabilityVersion: "v1",
		Payload:           mustPayload(t),
		TimeoutBudget:     mustTimeoutBudget(t, 5000),
	}
}

// 1. Construction happy path ───────────────────────────────────────────────────

func TestNewExecutionRequest_HappyPath(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	clk := fakeClock(now)
	in := validInput(t)

	req, err := entities.NewExecutionRequest(in, clk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.CorrelationID().String() != validULID {
		t.Errorf("CorrelationID = %q, want %q", req.CorrelationID().String(), validULID)
	}
	if req.AdapterID().String() != "http" {
		t.Errorf("AdapterID = %q, want %q", req.AdapterID().String(), "http")
	}
	if req.CapabilityName() != "re.quest" {
		t.Errorf("CapabilityName = %q, want %q", req.CapabilityName(), "re.quest")
	}
	if req.CapabilityVersion() != "v1" {
		t.Errorf("CapabilityVersion = %q, want %q", req.CapabilityVersion(), "v1")
	}
	if req.Payload().SizeBytes() == 0 {
		t.Error("Payload.SizeBytes() == 0, expected non-empty")
	}
	if req.TimeoutBudget().Milliseconds() != 5000 {
		t.Errorf("TimeoutBudget = %d ms, want 5000", req.TimeoutBudget().Milliseconds())
	}
	if !req.SubmittedAt().Equal(now) {
		t.Errorf("SubmittedAt = %v, want %v", req.SubmittedAt(), now)
	}
}

// 2. Required field validation ─────────────────────────────────────────────────

func TestNewExecutionRequest_ZeroCorrelationID(t *testing.T) {
	in := validInput(t)
	in.CorrelationID = shared.CorrelationID{} // zero value
	_, err := entities.NewExecutionRequest(in, fakeClock(time.Now()))
	if err == nil {
		t.Error("expected error for zero CorrelationID, got nil")
	}
}

func TestNewExecutionRequest_ZeroAdapterID(t *testing.T) {
	in := validInput(t)
	in.AdapterID = valueobjects.AdapterID{} // zero value
	_, err := entities.NewExecutionRequest(in, fakeClock(time.Now()))
	if err == nil {
		t.Error("expected error for zero AdapterID, got nil")
	}
}

func TestNewExecutionRequest_CapabilityName_Invalid(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"uppercase", "Request"},
		{"leading_digit", "1request"},
		{"too_long", strings.Repeat("a", 65)},
		{"hyphen", "my-request"},
		{"whitespace", "re quest"},
		{"dot_only", ".request"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := validInput(t)
			in.CapabilityName = tc.input
			_, err := entities.NewExecutionRequest(in, fakeClock(time.Now()))
			if err == nil {
				t.Errorf("expected error for capability_name=%q, got nil", tc.input)
			}
		})
	}
}

func TestNewExecutionRequest_CapabilityVersion_Invalid(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"bare_number", "1"},
		{"just_v", "v"},
		{"semver", "v1.0"},
		{"empty", ""},
		{"uppercase_v", "V1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := validInput(t)
			in.CapabilityVersion = tc.input
			_, err := entities.NewExecutionRequest(in, fakeClock(time.Now()))
			if err == nil {
				t.Errorf("expected error for capability_version=%q, got nil", tc.input)
			}
		})
	}
}

func TestNewExecutionRequest_EmptyPayload(t *testing.T) {
	in := validInput(t)
	in.Payload = valueobjects.Payload{} // zero value — SizeBytes()==0
	_, err := entities.NewExecutionRequest(in, fakeClock(time.Now()))
	if err == nil {
		t.Error("expected error for empty payload, got nil")
	}
}

func TestNewExecutionRequest_ZeroTimeoutBudget(t *testing.T) {
	in := validInput(t)
	in.TimeoutBudget = valueobjects.TimeoutBudget{} // zero value — Milliseconds()==0
	_, err := entities.NewExecutionRequest(in, fakeClock(time.Now()))
	if err == nil {
		t.Error("expected error for zero TimeoutBudget, got nil")
	}
}

// 3. Optional fields ──────────────────────────────────────────────────────────

func TestNewExecutionRequest_IdempotencyKey_Absent(t *testing.T) {
	in := validInput(t)
	in.IdempotencyKey = nil
	req, err := entities.NewExecutionRequest(in, fakeClock(time.Now()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, ok := req.IdempotencyKey()
	if ok {
		t.Error("expected IdempotencyKey() == false when not set")
	}
}

func TestNewExecutionRequest_IdempotencyKey_Present(t *testing.T) {
	in := validInput(t)
	in.IdempotencyKey = mustIdempotencyKey(t, validULID)
	req, err := entities.NewExecutionRequest(in, fakeClock(time.Now()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ik, ok := req.IdempotencyKey()
	if !ok {
		t.Fatal("expected IdempotencyKey() == true when set")
	}
	if ik.String() != validULID {
		t.Errorf("IdempotencyKey = %q, want %q", ik.String(), validULID)
	}
}

func TestNewExecutionRequest_MetadataID_TooLong(t *testing.T) {
	longID := strings.Repeat("x", 129)
	cases := []struct {
		name  string
		apply func(in *entities.ExecutionRequestInput)
	}{
		{"actor_id", func(in *entities.ExecutionRequestInput) { in.ActorID = longID }},
		{"task_id", func(in *entities.ExecutionRequestInput) { in.TaskID = longID }},
		{"workflow_run_id", func(in *entities.ExecutionRequestInput) { in.WorkflowRunID = longID }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := validInput(t)
			tc.apply(&in)
			_, err := entities.NewExecutionRequest(in, fakeClock(time.Now()))
			if err == nil {
				t.Errorf("expected error for %s > 128 chars, got nil", tc.name)
			}
		})
	}
}

func TestNewExecutionRequest_MetadataID_AtLimit(t *testing.T) {
	atLimit := strings.Repeat("x", 128)
	in := validInput(t)
	in.ActorID = atLimit
	_, err := entities.NewExecutionRequest(in, fakeClock(time.Now()))
	if err != nil {
		t.Errorf("unexpected error for actor_id at 128 chars: %v", err)
	}
}

func TestNewExecutionRequest_RetryAttempt_Negative(t *testing.T) {
	in := validInput(t)
	in.RetryAttempt = -1
	_, err := entities.NewExecutionRequest(in, fakeClock(time.Now()))
	if err == nil {
		t.Error("expected error for negative retry_attempt, got nil")
	}
}

func TestNewExecutionRequest_RetryAttempt_Zero(t *testing.T) {
	in := validInput(t)
	in.RetryAttempt = 0
	_, err := entities.NewExecutionRequest(in, fakeClock(time.Now()))
	if err != nil {
		t.Errorf("unexpected error for retry_attempt=0: %v", err)
	}
}

func TestNewExecutionRequest_RetryAttempt_Positive(t *testing.T) {
	in := validInput(t)
	in.RetryAttempt = 3
	req, err := entities.NewExecutionRequest(in, fakeClock(time.Now()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.RetryAttempt() != 3 {
		t.Errorf("RetryAttempt() = %d, want 3", req.RetryAttempt())
	}
}

// 4. Getters (compile-time check: no setter exists) ───────────────────────────

func TestNewExecutionRequest_AllGetters(t *testing.T) {
	now := time.Date(2026, 3, 15, 8, 30, 0, 0, time.UTC)
	ik := mustIdempotencyKey(t, validULID)
	in := validInput(t)
	in.IdempotencyKey = ik
	in.ActorID = "actor-1"
	in.TaskID = "task-1"
	in.WorkflowRunID = "wf-1"
	in.RetryAttempt = 2

	req, err := entities.NewExecutionRequest(in, fakeClock(now))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.CorrelationID().String() != validULID {
		t.Errorf("CorrelationID mismatch")
	}
	if req.AdapterID().String() != "http" {
		t.Errorf("AdapterID mismatch")
	}
	if req.CapabilityName() != "re.quest" {
		t.Errorf("CapabilityName mismatch")
	}
	if req.CapabilityVersion() != "v1" {
		t.Errorf("CapabilityVersion mismatch")
	}
	if req.Payload().SizeBytes() == 0 {
		t.Error("Payload is empty")
	}
	if req.TimeoutBudget().Milliseconds() != 5000 {
		t.Errorf("TimeoutBudget mismatch")
	}
	gotIK, ok := req.IdempotencyKey()
	if !ok || gotIK.String() != validULID {
		t.Errorf("IdempotencyKey mismatch: ok=%v val=%q", ok, gotIK.String())
	}
	if req.ActorID() != "actor-1" {
		t.Errorf("ActorID = %q, want actor-1", req.ActorID())
	}
	if req.TaskID() != "task-1" {
		t.Errorf("TaskID = %q, want task-1", req.TaskID())
	}
	if req.WorkflowRunID() != "wf-1" {
		t.Errorf("WorkflowRunID = %q, want wf-1", req.WorkflowRunID())
	}
	if req.RetryAttempt() != 2 {
		t.Errorf("RetryAttempt = %d, want 2", req.RetryAttempt())
	}
	if !req.SubmittedAt().Equal(now) {
		t.Errorf("SubmittedAt = %v, want %v", req.SubmittedAt(), now)
	}
}

// 5. Immutability: mutating the input after construction has no effect ─────────

func TestNewExecutionRequest_Immutability(t *testing.T) {
	in := validInput(t)
	in.ActorID = "original"

	req, err := entities.NewExecutionRequest(in, fakeClock(time.Now()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Mutate input after construction.
	in.ActorID = "mutated"

	if req.ActorID() != "original" {
		t.Errorf("immutability violated: ActorID = %q, want %q", req.ActorID(), "original")
	}
}

// 6. CapabilityCanonical ──────────────────────────────────────────────────────

func TestExecutionRequest_CapabilityCanonical(t *testing.T) {
	in := validInput(t)
	// validInput uses adapter="http", name="re.quest", version="v1"
	req, err := entities.NewExecutionRequest(in, fakeClock(time.Now()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "http.re.quest@v1"
	if got := req.CapabilityCanonical(); got != want {
		t.Errorf("CapabilityCanonical() = %q, want %q", got, want)
	}
}

func TestExecutionRequest_CapabilityCanonical_Simple(t *testing.T) {
	in := validInput(t)
	in.AdapterID = mustAdapterID(t, "http")
	in.CapabilityName = "re.quest"
	in.CapabilityVersion = "v1"

	req, err := entities.NewExecutionRequest(in, fakeClock(time.Now()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := req.CapabilityCanonical(); got != "http.re.quest@v1" {
		t.Errorf("CapabilityCanonical() = %q, want %q", got, "http.re.quest@v1")
	}
}

// 7. JSON round-trip ──────────────────────────────────────────────────────────

func TestExecutionRequest_JSONRoundTrip(t *testing.T) {
	now := time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC)
	in := validInput(t)
	in.IdempotencyKey = mustIdempotencyKey(t, validULID)
	in.ActorID = "svc-governance"
	in.RetryAttempt = 1

	original, err := entities.NewExecutionRequest(in, fakeClock(now))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	b, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded entities.ExecutionRequest
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if decoded.CorrelationID().String() != original.CorrelationID().String() {
		t.Errorf("CorrelationID: got %q, want %q", decoded.CorrelationID(), original.CorrelationID())
	}
	if decoded.AdapterID().String() != original.AdapterID().String() {
		t.Errorf("AdapterID: got %q, want %q", decoded.AdapterID(), original.AdapterID())
	}
	if decoded.CapabilityName() != original.CapabilityName() {
		t.Errorf("CapabilityName: got %q, want %q", decoded.CapabilityName(), original.CapabilityName())
	}
	if decoded.CapabilityVersion() != original.CapabilityVersion() {
		t.Errorf("CapabilityVersion: got %q, want %q", decoded.CapabilityVersion(), original.CapabilityVersion())
	}
	if decoded.TimeoutBudget().Milliseconds() != original.TimeoutBudget().Milliseconds() {
		t.Errorf("TimeoutBudget: got %d, want %d", decoded.TimeoutBudget().Milliseconds(), original.TimeoutBudget().Milliseconds())
	}
	if decoded.ActorID() != original.ActorID() {
		t.Errorf("ActorID: got %q, want %q", decoded.ActorID(), original.ActorID())
	}
	if decoded.RetryAttempt() != original.RetryAttempt() {
		t.Errorf("RetryAttempt: got %d, want %d", decoded.RetryAttempt(), original.RetryAttempt())
	}
	if !decoded.SubmittedAt().Equal(original.SubmittedAt()) {
		t.Errorf("SubmittedAt: got %v, want %v", decoded.SubmittedAt(), original.SubmittedAt())
	}
	gotIK, ok := decoded.IdempotencyKey()
	if !ok {
		t.Error("IdempotencyKey missing after round-trip")
	} else if gotIK.String() != validULID {
		t.Errorf("IdempotencyKey: got %q, want %q", gotIK.String(), validULID)
	}
}

func TestExecutionRequest_JSONRoundTrip_EmbeddedInStruct(t *testing.T) {
	// Regression: embedding in a struct with json tag must not produce empty object.
	type Wrapper struct {
		Request entities.ExecutionRequest `json:"request"`
	}

	now := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	req, err := entities.NewExecutionRequest(validInput(t), fakeClock(now))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	b, err := json.Marshal(Wrapper{Request: req})
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	// Must contain the nested fields — not just `{"request":{}}`
	if !strings.Contains(string(b), "correlation_id") {
		t.Errorf("embedded marshal missing correlation_id: %s", b)
	}

	var w2 Wrapper
	if err := json.Unmarshal(b, &w2); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if w2.Request.CorrelationID().String() != req.CorrelationID().String() {
		t.Errorf("embedded round-trip CorrelationID mismatch")
	}
}

// 8. Wire format correctness ──────────────────────────────────────────────────

func TestExecutionRequest_WireFormat_RequiredKeys(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	req, err := entities.NewExecutionRequest(validInput(t), fakeClock(now))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	b, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	wire := string(b)

	requiredKeys := []string{
		"correlation_id", "adapter_id", "capability_name", "capability_version",
		"payload", "timeout_budget_ms", "submitted_at",
	}
	for _, key := range requiredKeys {
		if !strings.Contains(wire, `"`+key+`"`) {
			t.Errorf("wire format missing key %q: %s", key, wire)
		}
	}

	// Optional fields must be absent when not set.
	absentKeys := []string{"idempotency_key", "actor_id", "task_id", "workflow_run_id", "retry_attempt"}
	for _, key := range absentKeys {
		if strings.Contains(wire, `"`+key+`"`) {
			t.Errorf("wire format should omit %q when absent: %s", key, wire)
		}
	}
}

func TestExecutionRequest_WireFormat_OptionalKeysPresent(t *testing.T) {
	now := time.Now().UTC()
	in := validInput(t)
	in.IdempotencyKey = mustIdempotencyKey(t, validULID)
	in.ActorID = "actor-a"
	in.TaskID = "task-b"
	in.WorkflowRunID = "wf-c"
	in.RetryAttempt = 2

	req, err := entities.NewExecutionRequest(in, fakeClock(now))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	b, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	wire := string(b)

	optKeys := []string{"idempotency_key", "actor_id", "task_id", "workflow_run_id", "retry_attempt"}
	for _, key := range optKeys {
		if !strings.Contains(wire, `"`+key+`"`) {
			t.Errorf("wire format missing optional key %q when set: %s", key, wire)
		}
	}
}

// 9. UnmarshalJSON validation ─────────────────────────────────────────────────

func TestExecutionRequest_UnmarshalJSON_MissingCorrelationID(t *testing.T) {
	raw := `{
		"adapter_id":"http","capability_name":"re.quest","capability_version":"v1",
		"payload":{"k":1},"timeout_budget_ms":5000,"submitted_at":"2026-01-01T00:00:00.000Z"
	}`
	var r entities.ExecutionRequest
	if err := json.Unmarshal([]byte(raw), &r); err == nil {
		t.Error("expected error for missing correlation_id, got nil")
	}
}

func TestExecutionRequest_UnmarshalJSON_InvalidCapabilityName(t *testing.T) {
	raw := `{
		"correlation_id":"` + validULID + `",
		"adapter_id":"http","capability_name":"BAD_NAME","capability_version":"v1",
		"payload":{"k":1},"timeout_budget_ms":5000,"submitted_at":"2026-01-01T00:00:00.000Z"
	}`
	var r entities.ExecutionRequest
	if err := json.Unmarshal([]byte(raw), &r); err == nil {
		t.Error("expected error for invalid capability_name, got nil")
	}
}

func TestExecutionRequest_UnmarshalJSON_MissingPayload(t *testing.T) {
	raw := `{
		"correlation_id":"` + validULID + `",
		"adapter_id":"http","capability_name":"re.quest","capability_version":"v1",
		"timeout_budget_ms":5000,"submitted_at":"2026-01-01T00:00:00.000Z"
	}`
	var r entities.ExecutionRequest
	if err := json.Unmarshal([]byte(raw), &r); err == nil {
		t.Error("expected error for missing payload, got nil")
	}
}

func TestExecutionRequest_UnmarshalJSON_ZeroTimeoutBudget(t *testing.T) {
	raw := `{
		"correlation_id":"` + validULID + `",
		"adapter_id":"http","capability_name":"re.quest","capability_version":"v1",
		"payload":{"k":1},"timeout_budget_ms":0,"submitted_at":"2026-01-01T00:00:00.000Z"
	}`
	var r entities.ExecutionRequest
	if err := json.Unmarshal([]byte(raw), &r); err == nil {
		t.Error("expected error for timeout_budget_ms=0, got nil")
	}
}

func TestExecutionRequest_UnmarshalJSON_InvalidIdempotencyKey(t *testing.T) {
	raw := `{
		"correlation_id":"` + validULID + `",
		"adapter_id":"http","capability_name":"re.quest","capability_version":"v1",
		"payload":{"k":1},"timeout_budget_ms":5000,
		"idempotency_key":"not-a-valid-ulid-or-uuid",
		"submitted_at":"2026-01-01T00:00:00.000Z"
	}`
	var r entities.ExecutionRequest
	if err := json.Unmarshal([]byte(raw), &r); err == nil {
		t.Error("expected error for invalid idempotency_key format, got nil")
	}
}

// 10. UnmarshalJSON preserves submitted_at ─────────────────────────────────────

func TestExecutionRequest_UnmarshalJSON_PreservesSubmittedAt(t *testing.T) {
	wireTime := "2025-06-15T09:30:00.000Z"
	wantTime, _ := time.Parse(time.RFC3339Nano, wireTime)

	raw := `{
		"correlation_id":"` + validULID + `",
		"adapter_id":"http","capability_name":"re.quest","capability_version":"v1",
		"payload":{"k":1},"timeout_budget_ms":5000,
		"submitted_at":"` + wireTime + `"
	}`
	var r entities.ExecutionRequest
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !r.SubmittedAt().Equal(wantTime.UTC()) {
		t.Errorf("SubmittedAt = %v, want %v", r.SubmittedAt(), wantTime.UTC())
	}
}

func TestExecutionRequest_UnmarshalJSON_InvalidSubmittedAt(t *testing.T) {
	raw := `{
		"correlation_id":"` + validULID + `",
		"adapter_id":"http","capability_name":"re.quest","capability_version":"v1",
		"payload":{"k":1},"timeout_budget_ms":5000,
		"submitted_at":"not-a-date"
	}`
	var r entities.ExecutionRequest
	if err := json.Unmarshal([]byte(raw), &r); err == nil {
		t.Error("expected error for invalid submitted_at, got nil")
	}
}

func TestExecutionRequest_UnmarshalJSON_AbsentSubmittedAt_UsesNow(t *testing.T) {
	// When submitted_at is absent from the wire, frozenClock falls back to time.Now().
	// We just check it succeeds and SubmittedAt is non-zero.
	raw := `{
		"correlation_id":"` + validULID + `",
		"adapter_id":"http","capability_name":"re.quest","capability_version":"v1",
		"payload":{"k":1},"timeout_budget_ms":5000
	}`
	var r entities.ExecutionRequest
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.SubmittedAt().IsZero() {
		t.Error("SubmittedAt should not be zero when submitted_at is absent from wire")
	}
}

// 11. Capability name valid edge cases ─────────────────────────────────────────

func TestNewExecutionRequest_CapabilityName_ValidEdgeCases(t *testing.T) {
	cases := []string{
		"re.quest",              // dot allowed after first char
		"ab",                    // min length 2
		"a0",                    // digit after letter
		"a_b",                   // underscore allowed
		strings.Repeat("a", 64), // max length
	}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			in := validInput(t)
			in.CapabilityName = name
			_, err := entities.NewExecutionRequest(in, fakeClock(time.Now()))
			if err != nil {
				t.Errorf("unexpected error for capability_name=%q: %v", name, err)
			}
		})
	}
}

// 12. CapabilityVersion valid ─────────────────────────────────────────────────

func TestNewExecutionRequest_CapabilityVersion_Valid(t *testing.T) {
	cases := []string{"v1", "v2", "v100"}
	for _, v := range cases {
		t.Run(v, func(t *testing.T) {
			in := validInput(t)
			in.CapabilityVersion = v
			_, err := entities.NewExecutionRequest(in, fakeClock(time.Now()))
			if err != nil {
				t.Errorf("unexpected error for capability_version=%q: %v", v, err)
			}
		})
	}
}
