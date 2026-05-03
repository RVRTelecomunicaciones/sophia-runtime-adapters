package application_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sophia-ecosystem/runtime-adapters/internal/integrations/linear/application"
	"github.com/sophia-ecosystem/runtime-adapters/internal/integrations/linear/domain"
	"github.com/sophia-ecosystem/runtime-adapters/internal/integrations/linear/ports"
)

// mockLinearClient is a hand-rolled mock for ports.LinearAPIClient
// — captures calls, returns canned responses. Concurrency-safe.
type mockLinearClient struct {
	mu sync.Mutex

	findResp      map[string][]domain.Issue
	findErr       error
	createResp    domain.Issue
	createErr     error
	updateResp    domain.Issue
	updateErr     error
	addCommentErr error
	archiveErr    error

	createCalls  []ports.CreateIssueInput
	updateCalls  []struct{ ID, Body string }
	commentCalls []struct{ IssueID, Body string }
	archiveCalls []string
}

func (m *mockLinearClient) FindIssuesByLabel(_ context.Context, label string) ([]domain.Issue, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.findResp[label], m.findErr
}
func (m *mockLinearClient) CreateIssue(_ context.Context, in ports.CreateIssueInput) (domain.Issue, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.createCalls = append(m.createCalls, in)
	return m.createResp, m.createErr
}
func (m *mockLinearClient) UpdateIssue(_ context.Context, id, body string) (domain.Issue, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updateCalls = append(m.updateCalls, struct{ ID, Body string }{id, body})
	return m.updateResp, m.updateErr
}
func (m *mockLinearClient) AddComment(_ context.Context, issueID, body string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.commentCalls = append(m.commentCalls, struct{ IssueID, Body string }{issueID, body})
	return m.addCommentErr
}
func (m *mockLinearClient) ArchiveIssue(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.archiveCalls = append(m.archiveCalls, id)
	return m.archiveErr
}

// EnsureLabelByName satisfies the interface but is unused by the
// lifecycle tests directly — label-name → label-ID translation is
// an implementation concern of the concrete GraphQL client (tested
// in Task 2.8 integration test). The lifecycle treats labels as
// opaque names; the client (real or mock) is the one resolving them.
// Returns a deterministic synthetic ID for any name to keep mock
// behavior simple. Lifecycle tests that exercise CreateIssue can
// still inspect createCalls[i].Labels (which are names) for assertions.
func (m *mockLinearClient) EnsureLabelByName(_ context.Context, _, name string) (string, error) {
	return "label-id-for-" + name, nil
}

func newLifecycle(t *testing.T, mc *mockLinearClient) *application.Lifecycle {
	t.Helper()
	return application.NewLifecycle(application.LifecycleConfig{
		Client:               mc,
		TeamID:               "team-test",
		RecommentMinInterval: 15 * time.Minute,
		Now:                  func() time.Time { return time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC) },
	})
}

func TestLifecycle_FiringNoExistingIssue_CallsCreate(t *testing.T) {
	mc := &mockLinearClient{
		findResp:   map[string][]domain.Issue{},
		createResp: domain.Issue{ID: "issue-1"},
	}
	lc := newLifecycle(t, mc)
	in := application.WebhookEvent{
		Status:       "firing",
		GroupKey:     "gk-1",
		Severity:     domain.SeverityCritical,
		Alertname:    "X",
		Capability:   "shell.exec@v1",
		FirstFiredAt: time.Now().UTC(),
		ActiveCount:  1,
		Summary:      "s",
		ExternalURL:  "u",
	}
	if err := lc.Handle(context.Background(), in); err != nil {
		t.Fatalf("Handle err = %v", err)
	}
	if len(mc.createCalls) != 1 {
		t.Errorf("expected 1 CreateIssue call, got %d", len(mc.createCalls))
	}
	if len(mc.updateCalls) != 0 || len(mc.commentCalls) != 0 || len(mc.archiveCalls) != 0 {
		t.Errorf("expected only Create, got create=%d update=%d comment=%d archive=%d",
			len(mc.createCalls), len(mc.updateCalls), len(mc.commentCalls), len(mc.archiveCalls))
	}
	if mc.createCalls[0].TeamID != "team-test" {
		t.Errorf("CreateIssue.TeamID = %q, want %q", mc.createCalls[0].TeamID, "team-test")
	}
}

func TestLifecycle_FiringExistingIssue_UpdatesBody(t *testing.T) {
	gk := "gk-existing"
	dl := domain.DedupLabel(gk)
	mc := &mockLinearClient{
		findResp: map[string][]domain.Issue{
			dl: {{ID: "issue-99", State: domain.IssueStateOpen, CreatedAt: time.Now()}},
		},
	}
	lc := newLifecycle(t, mc)
	in := application.WebhookEvent{
		Status: "firing", GroupKey: gk, Severity: domain.SeverityWarning, Alertname: "Y",
		FirstFiredAt: time.Now().UTC(), ActiveCount: 2, Summary: "s", ExternalURL: "u",
	}
	if err := lc.Handle(context.Background(), in); err != nil {
		t.Fatalf("Handle err = %v", err)
	}
	if len(mc.updateCalls) != 1 || mc.updateCalls[0].ID != "issue-99" {
		t.Errorf("expected UpdateIssue('issue-99'), got %+v", mc.updateCalls)
	}
	if len(mc.createCalls) != 0 {
		t.Errorf("expected 0 Create on re-firing, got %d", len(mc.createCalls))
	}
}

func TestLifecycle_ResolvedExistingIssue_AddsCommentAndArchives(t *testing.T) {
	gk := "gk-r"
	dl := domain.DedupLabel(gk)
	mc := &mockLinearClient{
		findResp: map[string][]domain.Issue{
			dl: {{ID: "issue-77", State: domain.IssueStateOpen,
				CreatedAt: time.Date(2026, 5, 2, 11, 50, 0, 0, time.UTC)}},
		},
	}
	lc := newLifecycle(t, mc)
	in := application.WebhookEvent{
		Status: "resolved", GroupKey: gk, Severity: domain.SeverityWarning, Alertname: "Y",
		FirstFiredAt: time.Date(2026, 5, 2, 11, 50, 0, 0, time.UTC),
		ActiveCount:  0, Summary: "s", ExternalURL: "u",
	}
	if err := lc.Handle(context.Background(), in); err != nil {
		t.Fatalf("Handle err = %v", err)
	}
	if len(mc.commentCalls) != 1 || mc.commentCalls[0].IssueID != "issue-77" {
		t.Errorf("expected AddComment('issue-77'), got %+v", mc.commentCalls)
	}
	if !strings.Contains(mc.commentCalls[0].Body, "Resolved") {
		t.Errorf("resolution comment must mention 'Resolved', got %q", mc.commentCalls[0].Body)
	}
	if len(mc.archiveCalls) != 1 || mc.archiveCalls[0] != "issue-77" {
		t.Errorf("expected ArchiveIssue('issue-77'), got %+v", mc.archiveCalls)
	}
}

func TestLifecycle_ResolvedNoExistingIssue_NoOp_ReturnsNil(t *testing.T) {
	mc := &mockLinearClient{findResp: map[string][]domain.Issue{}}
	lc := newLifecycle(t, mc)
	in := application.WebhookEvent{
		Status: "resolved", GroupKey: "gk-orphan", Severity: domain.SeverityWarning,
		Alertname: "Z", Summary: "s", ExternalURL: "u",
	}
	// Per A2C4AB.3.5 — semantically non-recoverable, return nil so the
	// handler emits 200 OK and Alertmanager does NOT retry.
	if err := lc.Handle(context.Background(), in); err != nil {
		t.Fatalf("Handle err = %v, want nil (race-condition safe)", err)
	}
	if len(mc.createCalls) != 0 || len(mc.updateCalls) != 0 ||
		len(mc.commentCalls) != 0 || len(mc.archiveCalls) != 0 {
		t.Errorf("expected NO API calls, got create=%d update=%d comment=%d archive=%d",
			len(mc.createCalls), len(mc.updateCalls), len(mc.commentCalls), len(mc.archiveCalls))
	}
}

func TestLifecycle_FiringExisting_AntiSpam_NoCommentBeforeInterval(t *testing.T) {
	gk := "gk-spam"
	dl := domain.DedupLabel(gk)
	mc := &mockLinearClient{
		findResp: map[string][]domain.Issue{
			dl: {{ID: "issue-1", State: domain.IssueStateOpen,
				CreatedAt: time.Date(2026, 5, 2, 11, 55, 0, 0, time.UTC)}}, // 5 min ago
		},
	}
	lc := newLifecycle(t, mc)
	// Re-fire identical group, no severity flip, within 15min interval.
	in := application.WebhookEvent{
		Status: "firing", GroupKey: gk, Severity: domain.SeverityWarning, Alertname: "Y",
		FirstFiredAt: time.Date(2026, 5, 2, 11, 55, 0, 0, time.UTC),
		ActiveCount:  1, Summary: "s", ExternalURL: "u",
	}
	_ = lc.Handle(context.Background(), in)
	if len(mc.commentCalls) != 0 {
		t.Errorf("anti-spam: expected 0 comments within RecommentMinInterval, got %d",
			len(mc.commentCalls))
	}
	// Body must still be refreshed (UpdateIssue called).
	if len(mc.updateCalls) != 1 {
		t.Errorf("expected body refresh via UpdateIssue, got %d calls", len(mc.updateCalls))
	}
}

func TestLifecycle_FiringExisting_LinearAPIError_ReturnsErr(t *testing.T) {
	mc := &mockLinearClient{findErr: errors.New("upstream 502")}
	lc := newLifecycle(t, mc)
	in := application.WebhookEvent{
		Status: "firing", GroupKey: "gk", Severity: domain.SeverityCritical, Alertname: "X",
		Summary: "s", ExternalURL: "u",
	}
	err := lc.Handle(context.Background(), in)
	if err == nil {
		t.Fatal("expected error from FindIssuesByLabel propagated, got nil")
	}
}
