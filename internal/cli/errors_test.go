package cli

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"
)

type statusTransport int

func (s statusTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: int(s),
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"type":"error","error":{"message":"nope"}}`)),
		Request:    r,
	}, nil
}

// The Bitbucket client's hints are neutral; the CLI names its own commands.
func TestAuthErrorsCarryCLIHints(t *testing.T) {
	tests := []struct {
		status int
		hint   string
	}{
		{401, credentialsHint},
		{403, scopesHint},
	}
	for _, tc := range tests {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			setupCLIEnv(t)
			root := NewRootCmd("test", WithHTTPClient(&http.Client{Transport: statusTransport(tc.status)}))
			root.PersistentPreRun = nil
			root.SilenceUsage = true
			root.SilenceErrors = true
			root.SetOut(&bytes.Buffer{})
			root.SetArgs([]string{"--no-cache", "pr", "view", "42", "-R", "acme/widgets", "--raw"})
			err := root.Execute()
			if err == nil || !strings.HasSuffix(err.Error(), "\nHint: "+tc.hint) {
				t.Fatalf("error = %v, want hint %q", err, tc.hint)
			}
		})
	}
}
