// Package opstest provides a fake ops.Env for op tests, backed by in-process
// HTTP servers.
package opstest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/jira"
	"github.com/kabilan108/atlas/internal/ops"
)

// The credentials every client from this package sends.
const (
	Email = "agent@example.org"
	Token = "opstest-token"
)

// Env is an ops.Env for tests. A client that was never configured fails with
// an Auth error, as a call without credentials would.
type Env struct {
	Workspace       string
	BitbucketClient *bitbucket.Client
	JiraClient      *jira.Client
}

// New returns an Env whose bare repo names resolve in workspace.
func New(workspace string) *Env {
	return &Env{Workspace: workspace}
}

// WithBitbucket serves the Bitbucket client from handler, which sees request
// paths as api.bitbucket.org does (under /2.0).
func (e *Env) WithBitbucket(t testing.TB, handler http.Handler) *Env {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := bitbucket.NewClientWithCredentials(Email, Token,
		bitbucket.WithBaseURL(server.URL+"/2.0"), bitbucket.WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	e.BitbucketClient = client
	return e
}

// WithJira serves the Jira client from handler, which sees REST paths such
// as /rest/api/3/myself.
func (e *Env) WithJira(t testing.TB, handler http.Handler) *Env {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := jira.NewClientWithCredentials(Email, Token, "",
		jira.WithBaseURL(server.URL), jira.WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	e.JiraClient = client
	return e
}

func (e *Env) Bitbucket(context.Context) (*bitbucket.Client, error) {
	if e.BitbucketClient == nil {
		return nil, ops.Errorf(ops.Auth, "opstest: no Bitbucket client configured")
	}
	return e.BitbucketClient, nil
}

func (e *Env) Jira(context.Context) (*jira.Client, error) {
	if e.JiraClient == nil {
		return nil, ops.Errorf(ops.Auth, "opstest: no Jira client configured")
	}
	return e.JiraClient, nil
}

func (e *Env) DefaultWorkspace() string {
	return e.Workspace
}

var _ ops.Env = (*Env)(nil)
