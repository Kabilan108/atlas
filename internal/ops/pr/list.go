package pr

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/ops"
)

// State is a pull request state to list.
type State string

const (
	StateOpen       State = "open"
	StateMerged     State = "merged"
	StateDeclined   State = "declined"
	StateSuperseded State = "superseded"
	StateAll        State = "all"
)

func (State) Enum() []string {
	return []string{string(StateOpen), string(StateMerged), string(StateDeclined), string(StateSuperseded), string(StateAll)}
}

// ParseState reads a state the way the CLI's --state flag does: any case,
// with "closed" meaning declined and "" meaning open.
func ParseState(raw string) (State, error) {
	switch state := strings.ToLower(strings.TrimSpace(raw)); state {
	case "":
		return StateOpen, nil
	case "closed":
		return StateDeclined, nil
	case string(StateOpen), string(StateMerged), string(StateDeclined), string(StateSuperseded), string(StateAll):
		return State(state), nil
	default:
		return "", ops.Errorf(ops.Invalid, "invalid PR state %q: expected open, merged, declined, closed, superseded, or all", raw)
	}
}

func (s State) orDefault() State {
	if s == "" {
		return StateOpen
	}
	return s
}

// apiStates are the Bitbucket states s lists.
func (s State) apiStates() []string {
	if s == StateAll {
		return []string{"OPEN", "MERGED", "DECLINED", "SUPERSEDED"}
	}
	return []string{strings.ToUpper(string(s))}
}

const (
	// DefaultListLimit is how many pull requests per repository pr_list
	// shows when ListInput.Limit is zero.
	DefaultListLimit = 50
	// MaxListRepos caps ListInput.Repos.
	MaxListRepos = 50
)

// ListInput is the input to pr_list.
type ListInput struct {
	ops.RepoRef
	Repos        []string `json:"repos,omitempty" jsonschema:"instead of repo: several repos (workspace/repo or bare), up to 50"`
	State        State    `json:"state,omitempty" jsonschema:"default open"`
	Author       string   `json:"author,omitempty" jsonschema:"author account ID, UUID, or username"`
	Reviewer     string   `json:"reviewer,omitempty" jsonschema:"reviewer account ID, UUID, or username"`
	UpdatedSince string   `json:"updated_since,omitempty" jsonschema:"only PRs updated on or after this, YYYY-MM-DD or ISO 8601"`
	Limit        int      `json:"limit,omitempty" jsonschema:"max PRs per repo, newest updated first; default 50, max 500"`
}

// ListResult is the pull requests pr_list found, newest updated first. It
// marshals to the array `atlas pr list --json` prints.
type ListResult struct {
	PullRequests []bitbucket.PullRequest

	scope string
	multi bool
	state State
	since time.Time
	limit int
	// more names the repositories that had more pull requests than limit.
	more []string
}

func (r *ListResult) MarshalJSON() ([]byte, error) {
	return json.Marshal(r.PullRequests)
}

var List = &ops.Op[ListInput, *ListResult]{
	Meta: ops.Meta{
		Name:        "pr_list",
		Description: "List Bitbucket pull requests, newest updated first: one repo, several (repos), or a whole workspace (repo \"workspace/*\", \"*\" for the default). Filter by state, author, reviewer, updated_since.",
		Surfaces:    ops.Both,
		ReadOnly:    true,
	},
	Run:      list,
	Markdown: (*ListResult).Markdown,
}

func list(ctx context.Context, env ops.Env, in ListInput) (*ListResult, error) {
	limit := in.Limit
	switch {
	case limit == 0:
		limit = DefaultListLimit
	case limit < 0 || limit > bitbucket.MaxPRListLimit:
		return nil, ops.Errorf(ops.Invalid, "limit must be between 1 and %d", bitbucket.MaxPRListLimit)
	}
	since, err := ParseUpdatedSince(in.UpdatedSince)
	if err != nil {
		return nil, err
	}
	state := in.State.orDefault()
	result := &ListResult{state: state, since: since, limit: limit}
	var repos []ops.Repository
	listAll := false
	switch {
	case len(in.Repos) > 0 && strings.TrimSpace(in.Repo) != "":
		return nil, ops.Errorf(ops.Invalid, "give repo or repos, not both")
	case len(in.Repos) > 0:
		if repos, err = resolveRepos(env, in.Repos); err != nil {
			return nil, err
		}
		result.multi = true
		result.scope = fmt.Sprintf("%d repos", len(repos))
		if len(repos) == 1 {
			result.scope = repos[0].String()
		}
	default:
		scope, err := in.ResolveScope(env)
		if err != nil {
			return nil, err
		}
		result.scope = scope.String()
		listAll, result.multi = scope.All(), scope.All()
		repos = []ops.Repository{scope}
	}
	client, err := env.Bitbucket(ctx)
	if err != nil {
		return nil, err
	}
	if listAll {
		workspace := repos[0].Workspace
		listed, err := client.ListRepositories(ctx, workspace)
		if err != nil {
			return nil, err
		}
		repos = repos[:0]
		for _, repo := range listed {
			repos = append(repos, ops.Repository{Workspace: workspace, Repo: repoSlug(repo)})
		}
	}
	opts := &bitbucket.PRListOptions{
		States:       state.apiStates(),
		Author:       in.Author,
		Reviewer:     in.Reviewer,
		UpdatedSince: since,
		Limit:        limit,
	}
	listErr := &bitbucket.RepositoryListError{Repositories: len(repos)}
	for _, repo := range repos {
		listed, err := client.ListPullRequests(ctx, repo.Workspace, repo.Repo, opts)
		if err != nil {
			if !result.multi {
				return nil, err
			}
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			name := repo.Repo
			if len(in.Repos) > 0 {
				name = repo.String()
			}
			listErr.Failed = append(listErr.Failed, bitbucket.RepositoryError{Repository: name, Err: err})
			continue
		}
		result.PullRequests = append(result.PullRequests, listed.PullRequests...)
		if listed.More {
			result.more = append(result.more, repo.String())
		}
	}
	slices.SortStableFunc(result.PullRequests, func(a, b bitbucket.PullRequest) int {
		return b.UpdatedOn.Compare(a.UpdatedOn)
	})
	if len(listErr.Failed) > 0 {
		return result, ops.Errorf(ops.Partial, "%w", error(listErr))
	}
	return result, nil
}

func resolveRepos(env ops.Env, raw []string) ([]ops.Repository, error) {
	if len(raw) > MaxListRepos {
		return nil, ops.Errorf(ops.Invalid, "repos holds %d repos; the limit is %d", len(raw), MaxListRepos)
	}
	seen := map[ops.Repository]bool{}
	var repos []ops.Repository
	for i, name := range raw {
		repo, err := ops.RepoRef{Repo: name}.Resolve(env)
		if err != nil {
			return nil, ops.Errorf(ops.Invalid, "repos[%d]: %w", i, err)
		}
		if !seen[repo] {
			seen[repo] = true
			repos = append(repos, repo)
		}
	}
	return repos, nil
}

// repoSlug is the path segment for repo: the part of full_name after the
// workspace, since a repository's display name may differ from its slug.
func repoSlug(repo bitbucket.Repository) string {
	if _, slug, ok := strings.Cut(repo.FullName, "/"); ok && slug != "" {
		return slug
	}
	return repo.Name
}

// ParseUpdatedSince reads a date (YYYY-MM-DD, midnight UTC) or an ISO 8601
// time; one without an offset is UTC. Empty is the zero time.
func ParseUpdatedSince(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, nil
	}
	for _, layout := range []string{time.DateOnly, time.RFC3339Nano, "2006-01-02T15:04Z07:00", "2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02T15:04:05.000-0700", "2006-01-02T15:04:05-0700"} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t, nil
		}
	}
	return time.Time{}, ops.Errorf(ops.Invalid, "invalid updated_since %q: expected YYYY-MM-DD or an ISO 8601 time such as 2025-03-01T09:00:00Z", raw)
}

// Markdown renders the pull requests as a table.
func (r *ListResult) Markdown() (string, error) {
	var b strings.Builder
	what := string(r.state)
	if r.state == StateAll {
		what = "(all states)"
	}
	since := ""
	if !r.since.IsZero() {
		since = " updated since " + r.since.UTC().Format(timeLayout)
	}
	if len(r.PullRequests) == 0 {
		if r.state == StateAll {
			fmt.Fprintf(&b, "No pull requests in %s%s.\n", r.scope, since)
		} else {
			fmt.Fprintf(&b, "No %s pull requests in %s%s.\n", r.state, r.scope, since)
		}
		return b.String(), nil
	}
	noun := "pull requests"
	if len(r.PullRequests) == 1 {
		noun = "pull request"
	}
	if r.state == StateAll {
		fmt.Fprintf(&b, "%d %s %s in %s%s\n\n", len(r.PullRequests), noun, what, r.scope, since)
	} else {
		fmt.Fprintf(&b, "%d %s %s in %s%s\n\n", len(r.PullRequests), what, noun, r.scope, since)
	}
	header := []string{"PR", "Title", "Author"}
	if r.state == StateAll {
		header = append(header, "State")
	}
	header = append(header, "Branches", "Created", "Updated", "Reviews", "Feedback", "URL")
	b.WriteString("| " + strings.Join(header, " | ") + " |\n|" + strings.Repeat("---|", len(header)) + "\n")
	for _, pr := range r.PullRequests {
		id := fmt.Sprintf("#%d", pr.ID)
		if r.multi {
			id = pr.Destination.Repository.FullName + id
		}
		title := pr.Title
		if pr.Draft {
			title = "[draft] " + title
		}
		row := []string{id, tableCell(title), tableCell(pr.Author.DisplayName)}
		if r.state == StateAll {
			row = append(row, pr.State)
		}
		row = append(row,
			tableCell(pr.Source.Branch.Name)+" → "+tableCell(pr.Destination.Branch.Name),
			formatTime(pr.CreatedOn), formatTime(pr.UpdatedOn),
			reviewSummary(pr), FeedbackSummary(pr), pr.Links.HTML.Href)
		b.WriteString("| " + strings.Join(row, " | ") + " |\n")
	}
	if len(r.more) > 0 {
		verb := "have"
		if len(r.more) == 1 {
			verb = "has"
		}
		fmt.Fprintf(&b, "\nShowing the newest %d per repo; %s %s more. Raise limit (max %d) or set updated_since.\n",
			r.limit, strings.Join(r.more, ", "), verb, bitbucket.MaxPRListLimit)
	}
	return b.String(), nil
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(timeLayout)
}
