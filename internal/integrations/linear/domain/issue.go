package domain

import "time"

// IssueState mirrors the Linear workflow-state names the adapter
// cares about. Linear has many workflow states; the adapter only
// distinguishes two semantic categories: open (any non-archived
// non-cancelled state) and Cancelled (the resolved-by-itself
// terminal state per D2C4AB.8).
type IssueState string

const (
	// IssueStateOpen — any in-progress, triaged, or backlog state.
	IssueStateOpen IssueState = "Open"
	// IssueStateCancelled — terminal state for auto-resolved alerts
	// (D2C4AB.8). Reads in Linear UI as "the alert resolved on its
	// own", distinct from "Done" which implies human work was done.
	IssueStateCancelled IssueState = "Cancelled"
)

// Issue is the projection of a Linear issue the adapter cares
// about. NOT the full GraphQL Issue type — only the fields the
// adapter reads or writes.
type Issue struct {
	// ID is Linear's globally unique issue ID (UUID-shaped string).
	ID string
	// Title is the rendered title (see application/renderer.go).
	Title string
	// Body is the rendered Markdown body.
	Body string
	// State is the workflow state the issue currently sits in.
	State IssueState
	// Priority is the Linear priority (1=P1 Urgent, 3=P3 Medium).
	Priority int
	// Labels is the full set of label NAMES applied to the issue
	// (NOT label IDs — the adapter uses names for dedup queries
	// per D2C4AB.7).
	Labels []string
	// CreatedAt is the Linear server timestamp of issue creation.
	// Used to pick the most-recent issue when multiple match the
	// dedup label.
	CreatedAt time.Time
}
