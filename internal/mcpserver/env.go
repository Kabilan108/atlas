package mcpserver

import (
	"context"
	"net/http"
	"strings"
	"sync"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/jira"
	"github.com/kabilan108/atlas/internal/ops"
)

// Request headers that carry the caller's credentials. Each tool call reads
// them afresh; the server keeps no credentials between calls.
const (
	HeaderEmail          = "X-Atlas-Email"
	HeaderBitbucketToken = "X-Atlas-Bitbucket-Token"
	HeaderJiraToken      = "X-Atlas-Jira-Token"
)

// headerEnv is the ops.Env for one tool call. It builds each client from the
// call's headers the first time an op asks for it, so a Bitbucket tool never
// needs the Jira header, and never uses the disk cache.
type headerEnv struct {
	header           http.Header
	defaultWorkspace string
	bitbucket        func() (*bitbucket.Client, error)
	jira             func() (*jira.Client, error)
}

func newHeaderEnv(header http.Header, opts Options) *headerEnv {
	e := &headerEnv{header: header, defaultWorkspace: opts.DefaultWorkspace}
	e.bitbucket = sync.OnceValues(func() (*bitbucket.Client, error) {
		values, err := e.require(HeaderEmail, HeaderBitbucketToken)
		if err != nil {
			return nil, err
		}
		return bitbucket.NewClientWithCredentials(values[0], values[1], opts.BitbucketOptions...)
	})
	e.jira = sync.OnceValues(func() (*jira.Client, error) {
		values, err := e.require(HeaderEmail, HeaderJiraToken)
		if err != nil {
			return nil, err
		}
		return jira.NewClientWithCredentials(values[0], values[1], opts.JiraCloudID, opts.JiraOptions...)
	})
	return e
}

func (e *headerEnv) DefaultWorkspace() string {
	return e.defaultWorkspace
}

func (e *headerEnv) Bitbucket(context.Context) (*bitbucket.Client, error) {
	return e.bitbucket()
}

func (e *headerEnv) Jira(context.Context) (*jira.Client, error) {
	return e.jira()
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
