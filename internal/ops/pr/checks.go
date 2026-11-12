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
// commit's statuses; with a PR it reads the PR's current head commit. It
// spells out Ref's fields rather than embedding Ref, whose pr is required.
type ChecksInput struct {
	ops.RepoRef
	PR          string `json:"pr,omitempty" jsonschema:"PR number, source branch name, or Bitbucket PR URL; this or commit is required"`
	Commit      string `json:"commit,omitempty" jsonschema:"commit SHA to read instead of the PR head; with pr, flags a head mismatch"`
	WaitSeconds int    `json:"wait_seconds,omitempty" jsonschema:"wait until no check is pending, max 600; default 0"`

	// CurrentBranch is used when PR and Commit are empty. Only the CLI sets
	// it, from git.
	CurrentBranch string `json:"-"`
}

func (ChecksInput) PropertyDescriptions() map[string]string {
	return map[string]string{"repo": prRepoDescription}
}

func (in ChecksInput) ref() Ref {
	return Ref{RepoRef: in.RepoRef, PR: in.PR, CurrentBranch: in.CurrentBranch}
}

// Overall check verdicts. None means nothing reported a status, which is not
// the same as passing.
const (
	ChecksNone    = "none"
	ChecksPassed  = "passed"
	ChecksPending = "pending"
	ChecksFailed  = "failed"
)

// Check is one build status on a commit. Build is the Pipelines build
// number when the status links to a Pipelines run.
type Check struct {
	Key       string    `json:"key"`
	Name      string    `json:"name"`
	State     string    `json:"state"`
	Build     int       `json:"build,omitempty"`
	URL       string    `json:"url,omitempty"`
	UpdatedOn time.Time `json:"updated_on"`
}

// ChecksResult is the build statuses on one commit. Waited is how long the
// call waited for pending checks.
type ChecksResult struct {
	Repo    string        `json:"repo"`
	PR      int           `json:"pr,omitempty"`
	PRHead  string        `json:"pr_head,omitempty"`
	Commit  string        `json:"commit"`
	Overall string        `json:"overall"`
	Checks  []Check       `json:"checks"`
	Waited  time.Duration `json:"-"`
}

// maxChecksWait caps ChecksInput.WaitSeconds.
const maxChecksWait = 600 * time.Second

// Poll intervals while waiting for pending checks; tests shorten them.
var (
	checksPollInitial = 5 * time.Second
	checksPollMax     = 20 * time.Second
)

var Checks = &ops.Op[ChecksInput, *ChecksResult]{
	Meta: ops.Meta{
		Name:        "pr_checks",
		Description: "Build statuses on a PR's current head commit, or on a given commit. overall is none (nothing reported, not a pass), pending, failed, or passed. Read this before pr_merge; for a failed Pipelines check pass its build to pipeline_log.",
		Surfaces:    ops.Both,
		ReadOnly:    true,
	},
	Run:      checks,
	Markdown: (*ChecksResult).Markdown,
}

func checks(ctx context.Context, env ops.Env, in ChecksInput) (*ChecksResult, error) {
	switch {
	case in.WaitSeconds < 0:
		return nil, ops.Errorf(ops.Invalid, "wait_seconds must not be negative")
	case time.Duration(in.WaitSeconds)*time.Second > maxChecksWait:
		return nil, ops.Errorf(ops.Invalid, "wait_seconds %d is over the %d-second maximum", in.WaitSeconds, int(maxChecksWait.Seconds()))
	}
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
		target, err := in.ref().ResolveCurrent(ctx, env)
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
	if err := result.read(ctx, client, repo); err != nil {
		return nil, err
	}
	if in.WaitSeconds > 0 && result.Overall == ChecksPending {
		if err := result.waitForPending(ctx, client, repo, time.Duration(in.WaitSeconds)*time.Second); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (r *ChecksResult) read(ctx context.Context, client *bitbucket.Client, repo ops.Repository) error {
	statuses, err := client.ListCommitStatuses(ctx, repo.Workspace, repo.Repo, r.Commit)
	if err != nil {
		return err
	}
	r.Checks = make([]Check, 0, len(statuses))
	for _, status := range statuses {
		check := Check{
			Key:       status.Key,
			Name:      status.Name,
			State:     status.State,
			URL:       status.URL,
			UpdatedOn: status.UpdatedOn,
		}
		if run, ok := bitbucket.ParsePipelineResultURL(status.URL); ok {
			check.Build = run.BuildNumber
		}
		r.Checks = append(r.Checks, check)
	}
	r.Overall = overall(r.Checks)
	return nil
}

// waitForPending reads the statuses again, backing off, until none is
// pending or wait passes. Running out of time is not an error: the result
// still says pending.
func (r *ChecksResult) waitForPending(ctx context.Context, client *bitbucket.Client, repo ops.Repository, wait time.Duration) error {
	started := time.Now()
	deadline := started.Add(wait)
	delay := checksPollInitial
	for r.Overall == ChecksPending {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		timer := time.NewTimer(min(delay, remaining))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		delay = min(delay*2, checksPollMax)
		if err := r.read(ctx, client, repo); err != nil {
			return err
		}
	}
	r.Waited = time.Since(started).Round(time.Second)
	return nil
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
		if r.Waited > 0 {
			fmt.Fprintf(&b, "Still pending after waiting %s: %s. Read again later.\n\n", r.Waited, countStates(r.Checks))
		} else {
			fmt.Fprintf(&b, "Pending: %s. Read again once they finish, or pass wait_seconds.\n\n", countStates(r.Checks))
		}
	case ChecksFailed:
		fmt.Fprintf(&b, "Failed: %s.\n\n", countStates(r.Checks))
	}
	b.WriteString("| State | Name | Key | Build | Updated | URL |\n|---|---|---|---|---|---|\n")
	for _, check := range r.Checks {
		build := ""
		if check.Build > 0 {
			build = fmt.Sprintf("%d", check.Build)
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s |\n", check.State, cell(check.Name), cell(check.Key), build,
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
