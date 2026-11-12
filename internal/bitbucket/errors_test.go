package bitbucket

import (
	"strings"
	"testing"
	"time"
)

// The client serves the CLI and the MCP server, so its hints must not name
// either one's commands, flags, or headers.
func TestErrorHintsAreNeutral(t *testing.T) {
	for _, err := range []*APIError{
		NewAuthError(401, "invalid credentials"),
		NewAuthError(403, "access denied"),
		NewNotFoundError("pull request", "7"),
		NewRateLimitError(time.Now().Add(time.Minute)),
		NewRateLimitError(time.Now().Add(-time.Minute)),
		NewServerError(502, "bad gateway"),
	} {
		for _, frontEnd := range []string{"atlas", "--", "X-Atlas"} {
			if strings.Contains(err.Hint, frontEnd) {
				t.Errorf("%d hint %q names %q", err.StatusCode, err.Hint, frontEnd)
			}
		}
		if err.HTTPStatus() != err.StatusCode {
			t.Errorf("HTTPStatus() = %d, want %d", err.HTTPStatus(), err.StatusCode)
		}
	}
}
