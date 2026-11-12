package mcpserver

import (
	"context"
	"net/http"
	"strings"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/ops"
)

// Request headers that carry the caller's credentials. Each tool call reads
// them afresh; the server keeps no credentials between calls.
const (
	HeaderEmail          = "X-Atlas-Email"
	HeaderBitbucketToken = "X-Atlas-Bitbucket-Token"
	// HeaderJiraToken is reserved for the Jira ops; nothing reads it yet.
	HeaderJiraToken = "X-Atlas-Jira-Token"
)

// headerEnv is the ops.Env for one tool call. It builds clients from the
// call's headers and never uses the disk cache.
type headerEnv struct {
	header           http.Header
	defaultWorkspace string
	clientOptions    []bitbucket.ClientOption
	bitbucket        *bitbucket.Client
}

func (e *headerEnv) DefaultWorkspace() string {
	return e.defaultWorkspace
}

func (e *headerEnv) Bitbucket(context.Context) (*bitbucket.Client, error) {
	if e.bitbucket != nil {
		return e.bitbucket, nil
	}
	values, err := e.require(HeaderEmail, HeaderBitbucketToken)
	if err != nil {
		return nil, err
	}
	client, err := bitbucket.NewClientWithCredentials(values[0], values[1], e.clientOptions...)
	if err != nil {
		return nil, err
	}
	e.bitbucket = client
	return client, nil
}

// require returns the named header values, or an Auth error naming every
// header that is missing.
func (e *headerEnv) require(names ...string) ([]string, error) {
	values := make([]string, len(names))
	var missing []string
	for i, name := range names {
		values[i] = strings.TrimSpace(e.header.Get(name))
		if values[i] == "" {
			missing = append(missing, name)
		}
	}
	switch len(missing) {
	case 0:
		return values, nil
	case 1:
		return nil, ops.Errorf(ops.Auth, "missing required header %s", missing[0])
	default:
		return nil, ops.Errorf(ops.Auth, "missing required headers %s", strings.Join(missing, ", "))
	}
}
