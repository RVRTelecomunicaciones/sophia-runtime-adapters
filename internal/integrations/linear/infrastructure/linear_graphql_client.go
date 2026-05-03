// Package infrastructure holds the concrete adapters the Linear
// webhook adapter binds at composition time. linear_graphql_client.go
// is the LinearAPIClient implementation against
// https://api.linear.app/graphql per spec §7.
package infrastructure

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/sophia-ecosystem/runtime-adapters/internal/integrations/linear/application"
	"github.com/sophia-ecosystem/runtime-adapters/internal/integrations/linear/domain"
	"github.com/sophia-ecosystem/runtime-adapters/internal/integrations/linear/ports"
)

// Config carries the GraphQL client configuration.
type Config struct {
	APIURL   string        // defaults to https://api.linear.app/graphql
	APIToken string        // raw token; sent as Authorization: <token>
	Timeout  time.Duration // per-request timeout; defaults to 10s
}

// LinearGraphQLClient implements ports.LinearAPIClient via Linear's
// GraphQL HTTP endpoint.
type LinearGraphQLClient struct {
	cfg  Config
	http *http.Client
}

// NewLinearGraphQLClient returns a client. Defaults APIURL to
// https://api.linear.app/graphql and Timeout to 10s if unset.
func NewLinearGraphQLClient(cfg Config) *LinearGraphQLClient {
	if cfg.APIURL == "" {
		cfg.APIURL = "https://api.linear.app/graphql"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 10 * time.Second
	}
	return &LinearGraphQLClient{
		cfg:  cfg,
		http: &http.Client{Timeout: cfg.Timeout},
	}
}

// gqlRequest is the GraphQL POST envelope.
type gqlRequest struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables,omitempty"`
}

// gqlResponse mirrors GraphQL spec — Data is opaque, Errors is
// non-nil when the server reported semantic errors.
type gqlResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// do performs a GraphQL POST. Returns ErrLinearClient5xx on
// transport errors / 5xx, ErrLinearClient4xx on 4xx or non-empty
// errors[]. Successful response with empty errors → nil.
func (c *LinearGraphQLClient) do(ctx context.Context, req gqlRequest, out interface{}) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.APIURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", c.cfg.APIToken)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return fmt.Errorf("%w: transport: %v", application.ErrLinearClient5xx, err)
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()

	if resp.StatusCode >= 500 {
		return fmt.Errorf("%w: http %d", application.ErrLinearClient5xx, resp.StatusCode)
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("%w: http %d", application.ErrLinearClient4xx, resp.StatusCode)
	}

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("%w: read body: %v", application.ErrLinearClient5xx, err)
	}
	var env gqlResponse
	if err := json.Unmarshal(respBytes, &env); err != nil {
		return fmt.Errorf("%w: decode envelope: %v", application.ErrLinearClient4xx, err)
	}
	if len(env.Errors) > 0 {
		return fmt.Errorf("%w: graphql errors: %s", application.ErrLinearClient4xx, env.Errors[0].Message)
	}
	if out != nil && len(env.Data) > 0 {
		if err := json.Unmarshal(env.Data, out); err != nil {
			return fmt.Errorf("%w: decode data: %v", application.ErrLinearClient4xx, err)
		}
	}
	return nil
}

// FindIssuesByLabel implements ports.LinearAPIClient.
func (c *LinearGraphQLClient) FindIssuesByLabel(ctx context.Context, label string) ([]domain.Issue, error) {
	const q = `query Find($label: String!) {
	  issues(filter: { labels: { name: { eq: $label } } }) {
	    nodes { id title createdAt state { name } labels { nodes { name } } }
	  }
	}`
	var raw struct {
		Issues struct {
			Nodes []struct {
				ID        string    `json:"id"`
				Title     string    `json:"title"`
				CreatedAt time.Time `json:"createdAt"`
				State     struct {
					Name string `json:"name"`
				} `json:"state"`
				Labels struct {
					Nodes []struct {
						Name string `json:"name"`
					} `json:"nodes"`
				} `json:"labels"`
			} `json:"nodes"`
		} `json:"issues"`
	}
	if err := c.do(ctx, gqlRequest{Query: q, Variables: map[string]interface{}{"label": label}}, &raw); err != nil {
		return nil, err
	}
	out := make([]domain.Issue, 0, len(raw.Issues.Nodes))
	for _, n := range raw.Issues.Nodes {
		state := domain.IssueStateOpen
		if n.State.Name == "Cancelled" {
			state = domain.IssueStateCancelled
		}
		labels := make([]string, 0, len(n.Labels.Nodes))
		for _, l := range n.Labels.Nodes {
			labels = append(labels, l.Name)
		}
		out = append(out, domain.Issue{
			ID: n.ID, Title: n.Title, State: state, CreatedAt: n.CreatedAt, Labels: labels,
		})
	}
	return out, nil
}

// CreateIssue implements ports.LinearAPIClient.
//
// Translates label NAMES (CreateIssueInput.Labels) into label IDs
// via EnsureLabelByName, because Linear's issueCreate mutation
// accepts only labelIds, not names. Without this translation, no
// labels attach to the issue and the dedup query
// (FindIssuesByLabel) fails to match on subsequent firings —
// duplicate issues per re-firing. (Bug fix from plan review
// 2026-05-02.)
func (c *LinearGraphQLClient) CreateIssue(ctx context.Context, in ports.CreateIssueInput) (domain.Issue, error) {
	// Translate label names → label IDs (query-or-create per name).
	labelIDs := make([]string, 0, len(in.Labels))
	for _, name := range in.Labels {
		id, err := c.EnsureLabelByName(ctx, in.TeamID, name)
		if err != nil {
			return domain.Issue{}, fmt.Errorf("ensure label %q: %w", name, err)
		}
		labelIDs = append(labelIDs, id)
	}

	const q = `mutation Create($input: IssueCreateInput!) {
	  issueCreate(input: $input) {
	    success
	    issue { id title createdAt state { name } labels { nodes { name } } }
	  }
	}`
	vars := map[string]interface{}{
		"input": map[string]interface{}{
			"teamId":     in.TeamID,
			"title":      in.Title,
			"description": in.Body,
			"priority":   in.Priority,
			"labelIds":   labelIDs,
		},
	}
	var raw struct {
		IssueCreate struct {
			Success bool `json:"success"`
			Issue   struct {
				ID        string    `json:"id"`
				Title     string    `json:"title"`
				CreatedAt time.Time `json:"createdAt"`
				State     struct {
					Name string `json:"name"`
				} `json:"state"`
			} `json:"issue"`
		} `json:"issueCreate"`
	}
	if err := c.do(ctx, gqlRequest{Query: q, Variables: vars}, &raw); err != nil {
		return domain.Issue{}, err
	}
	if !raw.IssueCreate.Success {
		return domain.Issue{}, fmt.Errorf("%w: issueCreate.success=false", application.ErrLinearClient4xx)
	}
	state := domain.IssueStateOpen
	if raw.IssueCreate.Issue.State.Name == "Cancelled" {
		state = domain.IssueStateCancelled
	}
	return domain.Issue{
		ID: raw.IssueCreate.Issue.ID, Title: raw.IssueCreate.Issue.Title,
		State: state, CreatedAt: raw.IssueCreate.Issue.CreatedAt,
	}, nil
}

// UpdateIssue implements ports.LinearAPIClient.
func (c *LinearGraphQLClient) UpdateIssue(ctx context.Context, id, body string) (domain.Issue, error) {
	const q = `mutation Update($id: String!, $input: IssueUpdateInput!) {
	  issueUpdate(id: $id, input: $input) {
	    success
	    issue { id title createdAt state { name } labels { nodes { name } } }
	  }
	}`
	vars := map[string]interface{}{
		"id":    id,
		"input": map[string]interface{}{"description": body},
	}
	var raw struct {
		IssueUpdate struct {
			Success bool `json:"success"`
			Issue   struct {
				ID        string    `json:"id"`
				Title     string    `json:"title"`
				CreatedAt time.Time `json:"createdAt"`
				State     struct {
					Name string `json:"name"`
				} `json:"state"`
			} `json:"issue"`
		} `json:"issueUpdate"`
	}
	if err := c.do(ctx, gqlRequest{Query: q, Variables: vars}, &raw); err != nil {
		return domain.Issue{}, err
	}
	if !raw.IssueUpdate.Success {
		return domain.Issue{}, fmt.Errorf("%w: issueUpdate.success=false", application.ErrLinearClient4xx)
	}
	state := domain.IssueStateOpen
	if raw.IssueUpdate.Issue.State.Name == "Cancelled" {
		state = domain.IssueStateCancelled
	}
	return domain.Issue{
		ID: raw.IssueUpdate.Issue.ID, Title: raw.IssueUpdate.Issue.Title,
		State: state, CreatedAt: raw.IssueUpdate.Issue.CreatedAt,
	}, nil
}

// AddComment implements ports.LinearAPIClient.
func (c *LinearGraphQLClient) AddComment(ctx context.Context, issueID, body string) error {
	const q = `mutation AddComment($input: CommentCreateInput!) {
	  commentCreate(input: $input) { success }
	}`
	vars := map[string]interface{}{
		"input": map[string]interface{}{"issueId": issueID, "body": body},
	}
	var raw struct {
		CommentCreate struct {
			Success bool `json:"success"`
		} `json:"commentCreate"`
	}
	if err := c.do(ctx, gqlRequest{Query: q, Variables: vars}, &raw); err != nil {
		return err
	}
	if !raw.CommentCreate.Success {
		return fmt.Errorf("%w: commentCreate.success=false", application.ErrLinearClient4xx)
	}
	return nil
}

// ArchiveIssue implements ports.LinearAPIClient via the
// issueArchive mutation. Linear archives the issue and transitions
// its state to Cancelled in one mutation when invoked on a
// non-archived issue.
func (c *LinearGraphQLClient) ArchiveIssue(ctx context.Context, id string) error {
	const q = `mutation Archive($id: String!) {
	  issueArchive(id: $id) { success }
	}`
	var raw struct {
		IssueArchive struct {
			Success bool `json:"success"`
		} `json:"issueArchive"`
	}
	if err := c.do(ctx, gqlRequest{Query: q, Variables: map[string]interface{}{"id": id}}, &raw); err != nil {
		return err
	}
	if !raw.IssueArchive.Success {
		return fmt.Errorf("%w: issueArchive.success=false", application.ErrLinearClient4xx)
	}
	return nil
}

// EnsureLabelByName implements ports.LinearAPIClient. Query-or-create
// translation from label name to label ID. Required because Linear's
// issueCreate mutation accepts only labelIds, not names. CreateIssue
// uses this to resolve every name in CreateIssueInput.Labels into the
// IDs it passes to issueCreate.
//
// No in-process cache: the spec's I-AB.4 (stateless) and I-AB.7
// (no persistence) imply behavior cannot depend on prior calls. The
// cardinality is bounded (~250 labels per spec §6) and Linear's label
// query is cheap; per-issue label resolution is acceptable. Add a
// cache only if profiling later shows it materially affects p99
// (out of v0.8.0 scope).
//
// Race tolerance: if two goroutines concurrently call EnsureLabelByName
// for the same missing name, the second IssueLabelCreate may fail with
// "label already exists" or similar. The implementation re-queries on
// such failures and returns the existing ID — never duplicates. (Linear
// enforces label name uniqueness per team.)
func (c *LinearGraphQLClient) EnsureLabelByName(ctx context.Context, teamID, name string) (string, error) {
	// 1. Query: does a label with this (team, name) tuple exist?
	const findQ = `query FindLabel($name: String!, $teamId: ID!) {
	  issueLabels(filter: { name: { eq: $name }, team: { id: { eq: $teamId } } }) {
	    nodes { id name }
	  }
	}`
	var findRaw struct {
		IssueLabels struct {
			Nodes []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"nodes"`
		} `json:"issueLabels"`
	}
	if err := c.do(ctx, gqlRequest{Query: findQ, Variables: map[string]interface{}{
		"name": name, "teamId": teamID,
	}}, &findRaw); err != nil {
		return "", fmt.Errorf("query label by name %q: %w", name, err)
	}
	if len(findRaw.IssueLabels.Nodes) > 0 {
		return findRaw.IssueLabels.Nodes[0].ID, nil
	}

	// 2. Create: label does not exist; issueLabelCreate.
	const createQ = `mutation CreateLabel($input: IssueLabelCreateInput!) {
	  issueLabelCreate(input: $input) {
	    success
	    issueLabel { id name }
	  }
	}`
	var createRaw struct {
		IssueLabelCreate struct {
			Success    bool `json:"success"`
			IssueLabel struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"issueLabel"`
		} `json:"issueLabelCreate"`
	}
	if err := c.do(ctx, gqlRequest{Query: createQ, Variables: map[string]interface{}{
		"input": map[string]interface{}{
			"name":   name,
			"teamId": teamID,
		},
	}}, &createRaw); err != nil {
		// Race: a concurrent caller may have just created the same
		// label. Re-query once before returning the error. Linear
		// enforces team-scoped name uniqueness so the second create
		// returns a 4xx-shaped GraphQL error.
		if err2 := c.do(ctx, gqlRequest{Query: findQ, Variables: map[string]interface{}{
			"name": name, "teamId": teamID,
		}}, &findRaw); err2 == nil && len(findRaw.IssueLabels.Nodes) > 0 {
			return findRaw.IssueLabels.Nodes[0].ID, nil
		}
		return "", fmt.Errorf("create label %q: %w", name, err)
	}
	if !createRaw.IssueLabelCreate.Success {
		return "", fmt.Errorf("issueLabelCreate.success=false for %q", name)
	}
	return createRaw.IssueLabelCreate.IssueLabel.ID, nil
}
