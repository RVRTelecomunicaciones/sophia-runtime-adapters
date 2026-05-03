package infrastructure_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sophia-ecosystem/runtime-adapters/internal/integrations/linear/application"
	"github.com/sophia-ecosystem/runtime-adapters/internal/integrations/linear/domain"
	"github.com/sophia-ecosystem/runtime-adapters/internal/integrations/linear/infrastructure"
	"github.com/sophia-ecosystem/runtime-adapters/internal/integrations/linear/ports"
)

func newTestServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *infrastructure.LinearGraphQLClient) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := infrastructure.NewLinearGraphQLClient(infrastructure.Config{
		APIURL:   srv.URL,
		APIToken: "test-token",
	})
	return srv, c
}

func TestGraphQL_FindIssuesByLabel_ParsesNodes(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"label":"alert:abcd"`) {
			t.Errorf("expected label in vars, got %s", body)
		}
		_, _ = io.WriteString(w, `{
		  "data": {
		    "issues": {
		      "nodes": [
		        {"id":"i1","title":"T","state":{"name":"In Progress"},"createdAt":"2026-05-02T12:00:00Z","labels":{"nodes":[{"name":"alert:abcd"}]}}
		      ]
		    }
		  }
		}`)
	})
	got, err := c.FindIssuesByLabel(context.Background(), "alert:abcd")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(got) != 1 || got[0].ID != "i1" {
		t.Errorf("got %+v, want 1 issue with ID=i1", got)
	}
	if got[0].State != domain.IssueStateOpen {
		t.Errorf("non-Cancelled state must map to Open, got %q", got[0].State)
	}
}

func TestGraphQL_FindIssuesByLabel_MapsCancelled(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{
		  "data":{"issues":{"nodes":[
		    {"id":"i9","title":"T","state":{"name":"Cancelled"},"createdAt":"2026-05-02T12:00:00Z","labels":{"nodes":[]}}
		  ]}}}`)
	})
	got, _ := c.FindIssuesByLabel(context.Background(), "x")
	if got[0].State != domain.IssueStateCancelled {
		t.Errorf("state Cancelled must map to IssueStateCancelled, got %q", got[0].State)
	}
}

func TestGraphQL_CreateIssue_PassesAuthHeader(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "test-token" {
			t.Errorf("Authorization header = %q, want %q", got, "test-token")
		}
		_, _ = io.WriteString(w, `{"data":{"issueCreate":{"success":true,"issue":{"id":"new1","title":"T","createdAt":"2026-05-02T12:00:00Z","state":{"name":"Backlog"},"labels":{"nodes":[]}}}}}`)
	})
	got, err := c.CreateIssue(context.Background(), ports.CreateIssueInput{TeamID: "t", Title: "T", Body: "B", Priority: 1})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got.ID != "new1" {
		t.Errorf("got ID=%q, want new1", got.ID)
	}
}

func TestGraphQL_5xx_ReturnsErrLinearClient5xx(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusBadGateway)
	})
	_, err := c.CreateIssue(context.Background(), ports.CreateIssueInput{})
	if !errors.Is(err, application.ErrLinearClient5xx) {
		t.Errorf("err = %v, want ErrLinearClient5xx", err)
	}
}

func TestGraphQL_4xx_ReturnsErrLinearClient4xx(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "auth fail", http.StatusUnauthorized)
	})
	_, err := c.CreateIssue(context.Background(), ports.CreateIssueInput{})
	if !errors.Is(err, application.ErrLinearClient4xx) {
		t.Errorf("err = %v, want ErrLinearClient4xx", err)
	}
}

func TestGraphQL_GraphQLErrorsField_ReturnsErrLinearClient4xx(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"errors":[{"message":"bad input"}]}`)
	})
	_, err := c.CreateIssue(context.Background(), ports.CreateIssueInput{})
	if !errors.Is(err, application.ErrLinearClient4xx) {
		t.Errorf("err = %v, want ErrLinearClient4xx (GraphQL errors field is a 4xx-class semantic)", err)
	}
}

func TestGraphQL_AddCommentAndArchive_ReturnNilOnSuccess(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query string `json:"query"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		// Both mutations return success:true.
		_, _ = io.WriteString(w, `{"data":{"commentCreate":{"success":true},"issueArchive":{"success":true},"issueUpdate":{"success":true,"issue":{"id":"i","title":"T","createdAt":"2026-05-02T12:00:00Z","state":{"name":"In Progress"},"labels":{"nodes":[]}}}}}`)
	})
	if err := c.AddComment(context.Background(), "i", "hello"); err != nil {
		t.Errorf("AddComment err = %v", err)
	}
	if err := c.ArchiveIssue(context.Background(), "i"); err != nil {
		t.Errorf("ArchiveIssue err = %v", err)
	}
}
