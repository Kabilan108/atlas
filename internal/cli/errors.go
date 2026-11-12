package cli

import (
	"errors"
	"fmt"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/jira"
)

const (
	credentialsHint     = "Run 'atlas config verify' to check your credentials, or 'atlas login' to set them up"
	scopesHint          = "Check that the API token has the scopes this command needs; 'atlas login' lists them"
	jiraCredentialsHint = "Check your Atlassian email ('atlas config get username') and Jira API token (ATLAS_JIRA_TOKEN or jira_token)"
	jiraScopesHint      = "Check that the Jira API token has the scopes this command needs and that your account can see the project"
)

// withCLIHint replaces the API clients' neutral auth hints with ones that
// name CLI commands and settings. Other hints are already neutral and stay.
func withCLIHint(err error) error {
	var bitbucketErr *bitbucket.APIError
	if errors.As(err, &bitbucketErr) {
		switch bitbucketErr.StatusCode {
		case 401:
			bitbucketErr.Hint = credentialsHint
		case 403:
			bitbucketErr.Hint = scopesHint
		}
		return err
	}
	var jiraErr *jira.APIError
	if errors.As(err, &jiraErr) {
		switch jiraErr.StatusCode {
		case 401:
			return fmt.Errorf("%w\nHint: %s", err, jiraCredentialsHint)
		case 403:
			return fmt.Errorf("%w\nHint: %s", err, jiraScopesHint)
		}
	}
	return err
}
