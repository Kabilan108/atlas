package pr

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/ops"
)

// MergeStrategy is a Bitbucket merge strategy. Empty means the destination
// branch's default.
type MergeStrategy string

func (MergeStrategy) Enum() []string {
	return bitbucket.MergeStrategies
}

const (
	defaultMergeWait = 120 * time.Second
	maxMergeWait     = 600 * time.Second
)

// MergeInput is the input to pr_merge.
type MergeInput struct {
	Ref
	Strategy          MergeStrategy `json:"strategy,omitempty" jsonschema:"default: the destination branch's"`
	Message           string        `json:"message,omitempty" jsonschema:"merge commit message"`
	CloseSourceBranch *bool         `json:"close_source_branch,omitempty" jsonschema:"default: the PR's setting"`
	Head              string        `json:"head,omitempty" jsonschema:"merge only if the PR head is still this commit (from pr_checks); refuses if it moved"`
	WaitSeconds       int           `json:"wait_seconds,omitempty" jsonschema:"wait for a queued merge, default 120, max 600"`
}

// MergeReceipt reports a finished merge.
type MergeReceipt struct {
	Repo        string `json:"repo"`
	PR          int    `json:"pr"`
	State       string `json:"state"`
	Destination string `json:"destination"`
	MergeCommit string `json:"merge_commit,omitempty"`
	// TaskURL is Bitbucket's merge task when State is MergePending.
	TaskURL string `json:"task_url,omitempty"`
	URL     string `json:"url"`
}

// MergePending is a receipt's State when the merge was accepted but had not
// finished by the deadline.
const MergePending = "PENDING"

var Merge = &ops.Op[MergeInput, *MergeReceipt]{
	Meta: ops.Meta{
		Name:        "pr_merge",
		Description: "Merge an open PR and return the merge commit. A refusal carries Bitbucket's reason; check pr_checks and approvals. PENDING means accepted but unfinished: do not retry. Pass head so a push after the checks cannot merge unchecked code.",
		Surfaces:    ops.Both,
	},
	Run:      merge,
	Markdown: (*MergeReceipt).Markdown,
}

func merge(ctx context.Context, env ops.Env, in MergeInput) (*MergeReceipt, error) {
	wait, err := waitDuration(in.WaitSeconds)
	if err != nil {
		return nil, err
	}
	head := strings.TrimSpace(in.Head)
	if head != "" && !commitPattern.MatchString(head) {
		return nil, ops.Errorf(ops.Invalid, "invalid head %q: expected a commit SHA", in.Head)
	}
	target, err := in.ResolveCurrent(ctx, env)
	if err != nil {
		return nil, err
	}
	id := target.PR.ID
	if target.PR.State != "OPEN" {
		return nil, ops.Errorf(ops.Conflict, "PR #%d is %s, not OPEN; nothing to merge", id, target.PR.State)
	}
	if current := target.PR.Source.Commit.Hash; head != "" && !sameCommit(head, current) {
		return nil, ops.Errorf(ops.Conflict, "PR #%d's head is now %s, not %s: someone pushed since; read pr_checks for the new head before merging", id, current, head)
	}
	client, err := env.Bitbucket(ctx)
	if err != nil {
		return nil, err
	}
	client = client.Uncached()

	started := time.Now()
	result, err := client.MergePullRequest(ctx, target.Workspace, target.Repo, id, bitbucket.PullRequestMergeOptions{
		Strategy:          string(in.Strategy),
		Message:           in.Message,
		CloseSourceBranch: in.CloseSourceBranch,
	})
	if err != nil {
		return nil, mergeRefused(id, err)
	}
	receipt := &MergeReceipt{
		Repo:        target.Repository.String(),
		PR:          id,
		State:       "MERGED",
		Destination: target.PR.Destination.Branch.Name,
		URL:         target.PR.Links.HTML.Href,
	}
	merged := result.PullRequest
	if result.Pending() {
		merged, err = client.WaitForMerge(ctx, result.TaskLocation, started.Add(wait))
		switch {
		case errors.Is(err, bitbucket.ErrMergePending):
			receipt.State, receipt.TaskURL = MergePending, result.TaskLocation
			return receipt, nil
		case err != nil:
			receipt.State, receipt.TaskURL = MergePending, result.TaskLocation
			return receipt, ops.Errorf(ops.Partial, "Bitbucket accepted PR #%d's merge, but it did not finish cleanly: %w. Check pr_view for its state before retrying", id, err)
		}
	}
	if merged == nil || merged.MergeCommit == nil {
		if merged, err = client.GetPullRequest(ctx, target.Workspace, target.Repo, id); err != nil {
			return receipt, ops.Errorf(ops.Partial, "PR #%d merged, but reading back its merge commit failed: %w", id, err)
		}
	}
	if merged.State != "" {
		receipt.State = merged.State
	}
	if merged.MergeCommit != nil {
		receipt.MergeCommit = merged.MergeCommit.Hash
	}
	return receipt, nil
}

var commitPattern = regexp.MustCompile(`^[0-9a-fA-F]{7,40}$`)

// sameCommit compares commit hashes of any length, since Bitbucket gives PR
// heads as short hashes.
func sameCommit(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	a, b = strings.ToLower(a), strings.ToLower(b)
	return strings.HasPrefix(a, b) || strings.HasPrefix(b, a)
}

func waitDuration(seconds int) (time.Duration, error) {
	switch {
	case seconds < 0:
		return 0, ops.Errorf(ops.Invalid, "wait_seconds must not be negative")
	case seconds == 0:
		return defaultMergeWait, nil
	}
	wait := time.Duration(seconds) * time.Second
	if wait > maxMergeWait {
		return 0, ops.Errorf(ops.Invalid, "wait_seconds %d is over the %d-second maximum", seconds, int(maxMergeWait.Seconds()))
	}
	return wait, nil
}

// mergeRefused keeps the API error's kind (Conflict for 409, Invalid for a
// failed merge check) and says what to look at before retrying.
func mergeRefused(id int, err error) error {
	kind := ops.KindOf(err)
	switch kind {
	case ops.Conflict, ops.Invalid:
		return ops.Errorf(kind, "Bitbucket refused to merge PR #%d: %w. Check pr_checks, approvals and open tasks (pr_view comments), fix them, then retry", id, err)
	default:
		return ops.Errorf(kind, "merge PR #%d: %w", id, err)
	}
}

func (r *MergeReceipt) Markdown() (string, error) {
	var b strings.Builder
	if r.State == MergePending {
		fmt.Fprintf(&b, "Bitbucket accepted PR #%d's merge into %s (%s), but it had not finished when atlas stopped waiting. Do not merge again: check pr_view for state MERGED, or the task at %s.\n%s\n",
			r.PR, r.Destination, r.Repo, r.TaskURL, r.URL)
		return b.String(), nil
	}
	fmt.Fprintf(&b, "Merged PR #%d into %s (%s)", r.PR, r.Destination, r.Repo)
	if r.MergeCommit != "" {
		fmt.Fprintf(&b, " as %s", r.MergeCommit)
	}
	b.WriteString(".")
	if r.State != "MERGED" {
		fmt.Fprintf(&b, " Bitbucket now reports the PR as %s.", r.State)
	}
	fmt.Fprintf(&b, "\n%s\n", r.URL)
	return b.String(), nil
}
