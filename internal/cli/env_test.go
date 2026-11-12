package cli

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/kabilan108/atlas/internal/ops"
	"github.com/spf13/cobra"
)

func commandWithHTTP(transport http.RoundTripper) *cobra.Command {
	cmd := &cobra.Command{}
	cmd.SetContext(context.WithValue(context.Background(), rootOptionsKey{}, &rootOptions{httpClient: &http.Client{Transport: transport}}))
	return cmd
}

func TestCLIEnvBuildsJiraOnlyWhenAskedAndFromConfig(t *testing.T) {
	setupCLIEnv(t)
	var requests []string
	transport := prTestTransport(func(r *http.Request) (*http.Response, error) {
		user, password, _ := r.BasicAuth()
		requests = append(requests, r.URL.Host+r.URL.Path+" as "+user+":"+password)
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{"accountId":"a1","account_id":"a1","displayName":"Ada"}`)), Request: r}, nil
	})
	ctx := context.Background()

	env, err := newCLIEnv(commandWithHTTP(transport))
	if err != nil {
		t.Fatal(err)
	}
	client, err := env.Bitbucket(ctx)
	if err != nil {
		t.Fatalf("Bitbucket without Jira config: %v", err)
	}
	if again, _ := env.Bitbucket(ctx); again != client {
		t.Fatal("Bitbucket client rebuilt within one run")
	}
	if _, err := env.Jira(ctx); ops.KindOf(err) != ops.Auth || !strings.Contains(err.Error(), "ATLAS_JIRA_TOKEN") {
		t.Fatalf("Jira without a token: %v", err)
	}

	t.Setenv("ATLAS_JIRA_TOKEN", "jt")
	env, _ = newCLIEnv(commandWithHTTP(transport))
	if _, err := env.Jira(ctx); ops.KindOf(err) != ops.Invalid || !strings.Contains(err.Error(), "jira_cloud_id") {
		t.Fatalf("Jira without a cloud ID: %v", err)
	}

	t.Setenv("ATLAS_JIRA_CLOUD_ID", "cloud-9")
	env, _ = newCLIEnv(commandWithHTTP(transport))
	jiraClient, err := env.Jira(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if again, _ := env.Jira(ctx); again != jiraClient {
		t.Fatal("Jira client rebuilt within one run")
	}
	if _, err := jiraClient.Myself(ctx); err != nil {
		t.Fatal(err)
	}
	if got := requests[len(requests)-1]; got != "api.atlassian.com/ex/jira/cloud-9/rest/api/3/myself as cli-user:jt" {
		t.Fatalf("Jira request = %s", got)
	}
}
