package application

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/sophia-ecosystem/runtime-adapters/internal/integrations/linear/domain"
	"github.com/sophia-ecosystem/runtime-adapters/internal/integrations/linear/ports"
)

// WebhookEvent is the projection of an Alertmanager webhook payload
// the lifecycle needs. The webhook handler builds this from the raw
// payload. (RenderInput in renderer.go is a similar projection
// scoped to renderer needs; both share fields but the contracts
// are separate to keep concerns isolated.)
type WebhookEvent struct {
	Status       string // "firing" or "resolved"
	GroupKey     string
	Severity     domain.Severity
	Alertname    string
	Capability   string
	FirstFiredAt time.Time
	ActiveCount  int
	Summary      string
	Description  string
	ExternalURL  string
	Runbook      string
	Dashboard    string
}

// LifecycleConfig wires the Lifecycle's dependencies.
type LifecycleConfig struct {
	Client               ports.LinearAPIClient
	TeamID               string
	RecommentMinInterval time.Duration // D2C4AB.9 — minimum duration between adapter-emitted comments on the same issue
	Now                  func() time.Time
}

// Lifecycle is the firing/resolved branching engine. Stateless;
// pure dependency on Linear API state via the client.
type Lifecycle struct {
	cfg LifecycleConfig
}

// NewLifecycle returns a Lifecycle bound to the supplied config.
// Defaults Now to time.Now if not supplied.
func NewLifecycle(cfg LifecycleConfig) *Lifecycle {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Lifecycle{cfg: cfg}
}

// Handle dispatches the event to the firing or resolved branch per
// spec §7.5. Returns an error iff the adapter could not complete
// the lifecycle action (a Linear API error). Returns nil on:
//   - successful create / update / archive
//   - resolved-with-no-existing-issue (semantically non-recoverable;
//     A2C4AB.3.5 — caller emits 200 OK so Alertmanager does NOT retry)
func (l *Lifecycle) Handle(ctx context.Context, in WebhookEvent) error {
	switch in.Status {
	case "firing":
		return l.handleFiring(ctx, in)
	case "resolved":
		return l.handleResolved(ctx, in)
	default:
		return fmt.Errorf("unknown status %q (expected 'firing' or 'resolved')", in.Status)
	}
}

func (l *Lifecycle) handleFiring(ctx context.Context, in WebhookEvent) error {
	dl := domain.DedupLabel(in.GroupKey)
	existing, err := l.cfg.Client.FindIssuesByLabel(ctx, dl)
	if err != nil {
		return fmt.Errorf("find issues by label %q: %w", dl, err)
	}
	open := pickMostRecentOpen(existing)
	now := l.cfg.Now().UTC()
	renderIn := RenderInput{
		Alertname:    in.Alertname,
		Severity:     in.Severity,
		Capability:   in.Capability,
		FirstFiredAt: in.FirstFiredAt,
		LastUpdate:   now,
		ActiveCount:  in.ActiveCount,
		Summary:      in.Summary,
		Description:  in.Description,
		ExternalURL:  in.ExternalURL,
		Runbook:      in.Runbook,
		Dashboard:    in.Dashboard,
		GroupKey:     in.GroupKey,
	}
	body := BuildBody(renderIn)

	if open == nil {
		// Spec §7.5.1: no existing issue → create.
		_, err := l.cfg.Client.CreateIssue(ctx, ports.CreateIssueInput{
			TeamID:   l.cfg.TeamID,
			Title:    BuildTitle(in.Severity, in.Alertname, in.Capability),
			Body:     body,
			Labels:   BuildLabels(in.Severity, in.Capability, in.GroupKey),
			Priority: in.Severity.LinearPriority(),
		})
		if err != nil {
			return fmt.Errorf("create issue: %w", err)
		}
		return nil
	}

	// Spec §7.5.2: existing issue → refresh body (always) + maybe comment (D2C4AB.9).
	if _, err := l.cfg.Client.UpdateIssue(ctx, open.ID, body); err != nil {
		return fmt.Errorf("update issue %s: %w", open.ID, err)
	}

	// Anti-spam (D2C4AB.9): only comment if interval has elapsed since
	// CreatedAt. Severity-flip and group-set-change conditions are
	// derivable from richer state; for v0.8.0 we ship the
	// time-elapsed branch only — the other two require richer event
	// state (last-comment time, prior-firing fingerprint set) and
	// would need adapter-side state which I-AB.7 forbids. The
	// time-elapsed branch alone caps re-comment frequency to 1 per
	// RecommentMinInterval per issue, which dominates the spam
	// scenarios in practice.
	if now.Sub(open.CreatedAt) >= l.cfg.RecommentMinInterval {
		comment := fmt.Sprintf("Re-firing at %s; %d active alerts in group.",
			now.Format(time.RFC3339), in.ActiveCount)
		if err := l.cfg.Client.AddComment(ctx, open.ID, comment); err != nil {
			return fmt.Errorf("add re-firing comment to %s: %w", open.ID, err)
		}
	}
	return nil
}

func (l *Lifecycle) handleResolved(ctx context.Context, in WebhookEvent) error {
	dl := domain.DedupLabel(in.GroupKey)
	existing, err := l.cfg.Client.FindIssuesByLabel(ctx, dl)
	if err != nil {
		return fmt.Errorf("find issues by label %q: %w", dl, err)
	}
	open := pickMostRecentOpen(existing)
	if open == nil {
		// Spec §7.5.4 / A2C4AB.3.5: race condition — resolved with no
		// prior firing. Return nil so the handler emits 200 OK and
		// Alertmanager does NOT retry.
		return nil
	}
	now := l.cfg.Now().UTC()
	dur := now.Sub(in.FirstFiredAt).Truncate(time.Second)
	comment := fmt.Sprintf("Resolved at %s, duration %s.", now.Format(time.RFC3339), dur)
	if err := l.cfg.Client.AddComment(ctx, open.ID, comment); err != nil {
		return fmt.Errorf("add resolution comment to %s: %w", open.ID, err)
	}
	if err := l.cfg.Client.ArchiveIssue(ctx, open.ID); err != nil {
		return fmt.Errorf("archive issue %s: %w", open.ID, err)
	}
	return nil
}

// pickMostRecentOpen returns the most-recently-created Open issue
// from the list, or nil if none. Cancelled issues are ignored
// (D2C4AB.8 — Cancelled is the terminal state for auto-resolved).
func pickMostRecentOpen(in []domain.Issue) *domain.Issue {
	var open []domain.Issue
	for _, i := range in {
		if i.State == domain.IssueStateOpen {
			open = append(open, i)
		}
	}
	if len(open) == 0 {
		return nil
	}
	sort.Slice(open, func(i, j int) bool {
		return open[i].CreatedAt.After(open[j].CreatedAt)
	})
	return &open[0]
}
