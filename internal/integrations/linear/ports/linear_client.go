// Package ports declares the outbound interfaces the Linear webhook
// adapter depends on. Concrete implementations live in
// internal/integrations/linear/infrastructure/. Per the hexagonal
// boundary established in D2C4AB.5.
package ports

import (
	"context"

	"github.com/sophia-ecosystem/runtime-adapters/internal/integrations/linear/domain"
)

// LinearAPIClient is the minimum surface the lifecycle + handler
// need from Linear. Mocked in tests; concrete impl uses GraphQL
// via net/http (infrastructure/linear_graphql_client.go).
//
// All methods MUST be safe to call concurrently.
type LinearAPIClient interface {
	// FindIssuesByLabel returns every issue currently bearing the
	// given label name, regardless of state. Used to find existing
	// adapter-managed issues for a given alert grouping. Caller
	// filters by state if needed.
	FindIssuesByLabel(ctx context.Context, label string) ([]domain.Issue, error)

	// CreateIssue creates a new issue with the supplied title, body,
	// label NAMES, priority, and team ID. Returns the created issue
	// (with server-assigned ID + CreatedAt populated).
	CreateIssue(ctx context.Context, in CreateIssueInput) (domain.Issue, error)

	// UpdateIssue updates the body of an existing issue (used to
	// refresh "Last update" + "Active alerts in group" on re-firing
	// per D2C4AB.9). Returns the updated issue.
	UpdateIssue(ctx context.Context, id string, body string) (domain.Issue, error)

	// AddComment appends a comment to an issue. Used for re-firing
	// (anti-spam logic in lifecycle.go) and resolution.
	AddComment(ctx context.Context, issueID string, body string) error

	// ArchiveIssue transitions the issue to Cancelled state and
	// archives it (per D2C4AB.8 resolved → Cancelled mapping).
	// Implementations may use Linear's IssueArchive mutation OR
	// IssueUpdate(state: Cancelled) — both are acceptable as long
	// as the issue ceases to surface in default Linear views.
	ArchiveIssue(ctx context.Context, id string) error

	// EnsureLabelByName returns the Linear label ID for the given
	// (teamID, name) tuple. Implementations MUST query first; if
	// no matching label exists, MUST create it via IssueLabelCreate
	// and return the new ID. Idempotent across concurrent calls
	// (race-tolerant: on concurrent create-collision, treat the
	// second create's "label already exists" error as success and
	// re-query for the existing ID).
	//
	// Required because Linear's `issueCreate(input.labelIds: [ID!])`
	// accepts only IDs, not names. CreateIssueInput.Labels carries
	// names; the GraphQL client translates names → IDs via
	// EnsureLabelByName before issuing CreateIssue. Without this
	// translation, no labels attach to the issue and the dedup
	// query (FindIssuesByLabel) silently fails to match — every
	// re-firing event creates a duplicate issue. (Bug surfaced
	// during plan review 2026-05-02.)
	EnsureLabelByName(ctx context.Context, teamID, name string) (string, error)
}

// CreateIssueInput is the POJO carried into CreateIssue.
// Decoupled from domain.Issue (which has CreatedAt + ID populated
// by the server).
type CreateIssueInput struct {
	TeamID   string
	Title    string
	Body     string
	Labels   []string // label NAMES, not IDs
	Priority int
}
