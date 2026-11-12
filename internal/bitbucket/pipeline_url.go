package bitbucket

import (
	"net/url"
	"strconv"
	"strings"
)

// PipelineResult is a Pipelines run named by its web URL.
type PipelineResult struct {
	Workspace   string
	Repo        string
	BuildNumber int
}

// ParsePipelineResultURL reads a Pipelines result page URL, such as
// https://bitbucket.org/acme/app/pipelines/results/2090 (optionally with
// /steps/...) or the older .../addon/pipelines/home#!/results/2090 that
// Pipelines puts on commit statuses.
func ParsePipelineResultURL(raw string) (PipelineResult, bool) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host != "bitbucket.org" {
		return PipelineResult{}, false
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) < 3 {
		return PipelineResult{}, false
	}
	var build string
	switch {
	case len(parts) >= 5 && parts[2] == "pipelines" && parts[3] == "results":
		build = parts[4]
	case len(parts) >= 5 && parts[2] == "addon" && parts[3] == "pipelines" && parts[4] == "home":
		fragment := strings.Split(strings.Trim(strings.TrimPrefix(parsed.Fragment, "!"), "/"), "/")
		if len(fragment) < 2 || fragment[0] != "results" {
			return PipelineResult{}, false
		}
		build = fragment[1]
	default:
		return PipelineResult{}, false
	}
	number, err := strconv.Atoi(build)
	if err != nil || number <= 0 || parts[0] == "" || parts[1] == "" {
		return PipelineResult{}, false
	}
	return PipelineResult{Workspace: parts[0], Repo: parts[1], BuildNumber: number}, true
}
