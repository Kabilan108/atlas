package pr

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/ops"
)

// ChecksInput is the input to pr_checks. With Commit alone it reads that
// commit's statuses; with a PR it reads the PR's current head commit.
type ChecksInput struct {
	Ref
	Commit string `json:"commit,omitempty" jsonschema:"commit SHA to read instead of the PR head (pr then optional); with pr, flags a head mismatch"`
}

// Overall check verdicts. None means nothing reported a status, which is not
// the same as passing.
const (
	ChecksNone    = "none"
	ChecksPassed  = "passed"
	ChecksPending = "pending"
	ChecksFailed  = "failed"
)

// Check is one build status on a commit.
type Check struct {
	Key       string    `json:"key"`
	Name      string    `json:"name"`
	State     string    `json:"state"`
	URL       string    `json:"url,omitempty"`
	UpdatedOn time.Time `json:"updated_on"`
}

// ChecksResult is the build statuses on one commit.
type ChecksResult struct {
	Repo    string  `json:"repo"`
	PR      int     `json:"pr,omitempty"`
	PRHead  string  `json:"pr_head,omitempty"`
	Commit  string  `json:"commit"`
	Overall string  `json:"overall"`
	Checks  []Check `json:"checks"`
}

var Checks = &ops.Op[ChecksInput, *ChecksResult]{
	Meta: ops.Meta{
		Name:        "pr_checks",
		Description: "Build statuses on a PR's current head commit, or on a given commit. overall is none (nothing reported, not a pass), pending, failed, or passed. Read this before pr_merge.",
		Surfaces:    ops.Both,
		ReadOnly:    true,
	},
	Run:      checks,
	Markdown: (*ChecksResult).Markdown,
}

func checks(ctx context.Context, env ops.Env, in ChecksInput) (*ChecksResult, error) {
	commit := strings.TrimSpace(in.Commit)
	if commit != "" && strings.ContainsAny(commit, "/?#%") {
		return nil, ops.Errorf(ops.Invalid, "invalid commit %q: expected a commit SHA", in.Commit)
	}
	result := &ChecksResult{Commit: commit}
	var repo ops.Repository
	if strings.TrimSpace(in.PR) == "" && in.CurrentBranch == "" {
		if commit == "" {
			return nil, ops.Errorf(ops.Invalid, "give pr (number, URL, or branch) or commit")
		}
		var err error
		if repo, err = in.RepoRef.Resolve(env); err != nil {
			return nil, err
		}
	} else {
		target, err := in.Resolve(ctx, env)
		if err != nil {
			return nil, err
		}
		repo = target.Repository
		result.PR = target.PR.ID
		result.PRHead = target.PR.Source.Commit.Hash
		if result.PRHead == "" {
			return nil, ops.Errorf(ops.Upstream, "Bitbucket returned no head commit for PR #%d", target.PR.ID)
		}
		if commit == "" {
			result.Commit = result.PRHead
		}
	}
	result.Repo = repo.String()

	client, err := env.Bitbucket(ctx)
	if err != nil {
		return nil, err
	}
	statuses, err := client.ListCommitStatuses(ctx, repo.Workspace, repo.Repo, result.Commit)
	if err != nil {
		return nil, err
	}
	result.Checks = make([]Check, 0, len(statuses))
	for _, status := range statuses {
		result.Checks = append(result.Checks, Check{
			Key:       status.Key,
			Name:      status.Name,
			State:     status.State,
			URL:       status.URL,
			UpdatedOn: status.UpdatedOn,
		})
	}
	result.Overall = overall(result.Checks)
	return result, nil
}

func overall(checks []Check) string {
	if len(checks) == 0 {
		return ChecksNone
	}
	verdict := ChecksPassed
	for _, check := range checks {
		switch check.State {
		case bitbucket.CommitStatusSuccessful:
		case bitbucket.CommitStatusInProgress:
			verdict = ChecksPending
		default:
			return ChecksFailed
		}
	}
	return verdict
}

// Failing returns the checks that did not pass and are not still running.
func (r *ChecksResult) Failing() []Check {
	var failing []Check
	for _, check := range r.Checks {
		if check.State != bitbucket.CommitStatusSuccessful && check.State != bitbucket.CommitStatusInProgress {
			failing = append(failing, check)
		}
	}
	return failing
}

// HeadMismatch reports whether the commit asked for is not the PR's head.
// Bitbucket gives PR heads as short hashes, so a prefix match counts.
func (r *ChecksResult) HeadMismatch() bool {
	if r.PRHead == "" || r.Commit == r.PRHead {
		return false
	}
	return !strings.HasPrefix(r.Commit, r.PRHead) && !strings.HasPrefix(r.PRHead, r.Commit)
}

func (r *ChecksResult) Markdown() (string, error) {
	var b strings.Builder
	subject := "commit " + r.Commit
	if r.PR != 0 {
		subject = fmt.Sprintf("PR #%d at %s", r.PR, r.Commit)
	}
	fmt.Fprintf(&b, "# Checks for %s (%s)\n\n", subject, r.Repo)
	if r.HeadMismatch() {
		fmt.Fprintf(&b, "Warning: PR #%d's head is now %s, not %s. These statuses are not for the code that would merge.\n\n", r.PR, r.PRHead, r.Commit)
	}
	switch r.Overall {
	case ChecksNone:
		b.WriteString("No checks reported on this commit. Nothing has run or posted a status yet, so this is not a pass.\n")
		return b.String(), nil
	case ChecksPassed:
		fmt.Fprintf(&b, "All %d checks passed.\n\n", len(r.Checks))
	case ChecksPending:
		fmt.Fprintf(&b, "Pending: %s. Read again once they finish.\n\n", countStates(r.Checks))
	case ChecksFailed:
		fmt.Fprintf(&b, "Failed: %s.\n\n", countStates(r.Checks))
	}
	b.WriteString("| State | Name | Key | Updated | URL |\n|---|---|---|---|---|\n")
	for _, check := range r.Checks {
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n", check.State, cell(check.Name), cell(check.Key),
			check.UpdatedOn.UTC().Format(time.RFC3339), check.URL)
	}
	return b.String(), nil
}

func countStates(checks []Check) string {
	order := []string{bitbucket.CommitStatusFailed, bitbucket.CommitStatusStopped, bitbucket.CommitStatusInProgress, bitbucket.CommitStatusSuccessful}
	counts := map[string]int{}
	for _, check := range checks {
		counts[check.State]++
	}
	var parts []string
	for _, state := range order {
		if counts[state] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[state], strings.ToLower(state)))
			delete(counts, state)
		}
	}
	for _, state := range slices.Sorted(maps.Keys(counts)) {
		parts = append(parts, fmt.Sprintf("%d %s", counts[state], strings.ToLower(state)))
	}
	return strings.Join(parts, ", ")
}

func cell(s string) string {
	return strings.NewReplacer("|", `\|`, "\n", " ").Replace(s)
}
