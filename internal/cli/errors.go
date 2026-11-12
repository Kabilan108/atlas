package cli

import (
	"errors"

	"github.com/kabilan108/atlas/internal/bitbucket"
)

const (
	credentialsHint = "Run 'atlas config verify' to check your credentials, or 'atlas login' to set them up"
	scopesHint      = "Check that the API token has the scopes this command needs; 'atlas login' lists them"
)

// withCLIHint replaces the Bitbucket client's neutral auth hints with ones
// that name CLI commands. Other hints are already neutral and stay.
func withCLIHint(err error) error {
	var apiErr *bitbucket.APIError
	if !errors.As(err, &apiErr) {
		return err
	}
	switch apiErr.StatusCode {
	case 401:
		apiErr.Hint = credentialsHint
	case 403:
		apiErr.Hint = scopesHint
	}
	return err
}
