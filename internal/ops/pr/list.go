package pr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

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
)

func (State) Enum() []string {
	return []string{string(StateOpen), string(StateMerged), string(StateDeclined), string(StateSuperseded)}
}

// ParseState reads a state the way the CLI's --state flag does: any case,
// with "closed" meaning declined and "" meaning open.
func ParseState(raw string) (State, error) {
	switch state := strings.ToLower(strings.TrimSpace(raw)); state {
	case "":
		return StateOpen, nil
	case "closed":
		return StateDeclined, nil
	case string(StateOpen), string(StateMerged), string(StateDeclined), string(StateSuperseded):
		return State(state), nil
	default:
		return "", ops.Errorf(ops.Invalid, "invalid PR state %q: expected open, merged, declined, closed, or superseded", raw)
	}
}

func (s State) orDefault() State {
	if s == "" {
		return StateOpen
	}
	return s
}

// ListInput is the input to pr_list.
type ListInput struct {
	ops.RepoRef
	State    State  `json:"state,omitempty" jsonschema:"default open"`
	Author   string `json:"author,omitempty" jsonschema:"author account ID, UUID, or username"`
	Reviewer string `json:"reviewer,omitempty" jsonschema:"reviewer account ID, UUID, or username"`
}

// ListResult is the pull requests pr_list found. It marshals to the array
// `atlas pr list --json` prints.
type ListResult struct {
	PullRequests []bitbucket.PullRequest

	scope ops.Repository
	state State
}

func (r *ListResult) MarshalJSON() ([]byte, error) {
	return json.Marshal(r.PullRequests)
}

var List = &ops.Op[ListInput, *ListResult]{
	Meta: ops.Meta{
		Name:        "pr_list",
		Description: "List Bitbucket pull requests in a repo, or in every repo of a workspace with repo \"workspace/*\" (\"*\" for the default workspace).",
		Surfaces:    ops.Both,
		ReadOnly:    true,
	},
	Run:      list,
	Markdown: (*ListResult).Markdown,
}

func list(ctx context.Context, env ops.Env, in ListInput) (*ListResult, error) {
	scope, err := in.ResolveScope(env)
	if err != nil {
		return nil, err
	}
	client, err := env.Bitbucket(ctx)
	if err != nil {
		return nil, err
	}
	state := in.State.orDefault()
	opts := &bitbucket.PRListOptions{State: strings.ToUpper(string(state)), Author: in.Author, Reviewer: in.Reviewer}
	result := &ListResult{scope: scope, state: state}
	if scope.All() {
		result.PullRequests, err = client.ListAllPullRequests(ctx, scope.Workspace, opts)
	} else {
		result.PullRequests, err = client.ListPullRequests(ctx, scope.Workspace, scope.Repo, opts)
	}
	var partial *bitbucket.RepositoryListError
	if errors.As(err, &partial) {
		return result, ops.Errorf(ops.Partial, "%w", err)
	}
	if err != nil {
		return nil, err
	}
	return result, nil
}

// Markdown renders the pull requests as a table.
func (r *ListResult) Markdown() (string, error) {
	var b strings.Builder
	if len(r.PullRequests) == 0 {
		fmt.Fprintf(&b, "No %s pull requests in %s.\n", r.state, r.scope)
		return b.String(), nil
	}
	noun := "pull requests"
	if len(r.PullRequests) == 1 {
		noun = "pull request"
	}
	fmt.Fprintf(&b, "%d %s %s in %s\n\n", len(r.PullRequests), r.state, noun, r.scope)
	b.WriteString("| PR | Title | Author | Branches | Updated | Reviews | Feedback |\n|---|---|---|---|---|---|---|\n")
	for _, pr := range r.PullRequests {
		id := fmt.Sprintf("#%d", pr.ID)
		if r.scope.All() {
			id = pr.Destination.Repository.FullName + id
		}
		title := pr.Title
		if pr.Draft {
			title = "[draft] " + title
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s → %s | %s | %s | %s |\n",
			id, tableCell(title), tableCell(pr.Author.DisplayName),
			tableCell(pr.Source.Branch.Name), tableCell(pr.Destination.Branch.Name),
			pr.UpdatedOn.UTC().Format(timeLayout), reviewSummary(pr), FeedbackSummary(pr))
	}
	return b.String(), nil
}
