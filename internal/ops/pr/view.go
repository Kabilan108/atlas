package pr

import (
	"bytes"
	"context"
	"fmt"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/output"
)

// MaxViewComments and MaxViewTasks are the most comments and tasks pr_view
// reads; the markdown says when a pull request has more.
const (
	MaxViewComments = 1000
	MaxViewTasks    = 1000
)

// ViewInput is the input to pr_view.
type ViewInput struct {
	Ref
	Comments        bool `json:"comments,omitempty" jsonschema:"include comment threads (with diff context) and tasks"`
	IncludeResolved bool `json:"include_resolved,omitempty" jsonschema:"with comments, also show resolved threads"`
	Activity        bool `json:"activity,omitempty" jsonschema:"add the activity feed: approvals, pushes, and updates with times"`

	// JSON fetches only what `atlas pr view --json` prints: the pull request
	// and all of its comments. Only the CLI sets it.
	JSON bool `json:"-"`
}

// ViewResult is a pull request with everything its markdown needs. It
// marshals to the JSON that `atlas pr view --json` prints.
type ViewResult struct {
	*bitbucket.PullRequest
	Comments []bitbucket.Comment             `json:"comments,omitempty"`
	Activity []bitbucket.PullRequestActivity `json:"activity,omitempty"`
	// CommentsTruncated is set when the pull request has more than
	// MaxViewComments comments.
	CommentsTruncated bool `json:"comments_truncated,omitempty"`

	workspace       string
	repo            string
	showComments    bool
	includeResolved bool
	tasks           []bitbucket.Task
	tasksTruncated  bool
	diff            []byte
	users           map[string]bitbucket.User
}

var View = &ops.Op[ViewInput, *ViewResult]{
	Meta: ops.Meta{
		Name:        "pr_view",
		Description: "Show a Bitbucket pull request as markdown: state, branches, reviewers, and description. Set comments to add review threads and tasks.",
		Surfaces:    ops.Both,
		ReadOnly:    true,
	},
	Run:      view,
	Markdown: (*ViewResult).Markdown,
}

func view(ctx context.Context, env ops.Env, in ViewInput) (*ViewResult, error) {
	target, err := in.Resolve(ctx, env)
	if err != nil {
		return nil, err
	}
	client, err := env.Bitbucket(ctx)
	if err != nil {
		return nil, err
	}
	result := &ViewResult{
		PullRequest:     target.PR,
		workspace:       target.Workspace,
		repo:            target.Repo,
		showComments:    in.Comments && !in.JSON,
		includeResolved: in.IncludeResolved,
	}
	id := target.PR.ID
	if in.Comments || in.JSON {
		var scan bitbucket.Scan
		result.Comments, scan, err = client.ListPullRequestComments(ctx, target.Workspace, target.Repo, id, MaxViewComments)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch comments: %w", err)
		}
		result.CommentsTruncated = scan.More
	}
	if in.Activity {
		result.Activity, err = client.ListPullRequestActivity(ctx, target.Workspace, target.Repo, id, ActivityLimit)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch activity: %w", err)
		}
	}
	if in.JSON {
		return result, nil
	}
	if result.showComments {
		var scan bitbucket.Scan
		result.tasks, scan, err = client.ListPullRequestTasks(ctx, target.Workspace, target.Repo, id, MaxViewTasks)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch tasks: %w", err)
		}
		result.tasksTruncated = scan.More
		// The diff only adds code context to inline threads; without it the
		// threads still render. It is read with pr_diff's download cap, so a
		// huge diff only leaves threads on its later files without context.
		if diff, err := client.GetPullRequestDiffLimit(ctx, target.Workspace, target.Repo, id, MaxDiffDownload); err == nil {
			result.diff = diff.Data
		}
	}
	if err := result.resolveMentions(ctx, client); err != nil {
		return nil, err
	}
	return result, nil
}

// Markdown renders the pull request without calling Bitbucket.
func (r *ViewResult) Markdown() (string, error) {
	return r.render(func(accountID string) (bitbucket.User, bool) {
		user, ok := r.users[accountID]
		return user, ok
	})
}

// resolveMentions looks up the users that @{account-id} mentions refer to.
// Rendering once with a live resolver asks for exactly the mentions the
// markdown shows, skipping those inside code.
func (r *ViewResult) resolveMentions(ctx context.Context, client *bitbucket.Client) error {
	r.users = make(map[string]bitbucket.User)
	for _, user := range r.knownUsers() {
		if user.AccountID != "" {
			r.users[user.AccountID] = user
		}
	}
	unknown := make(map[string]bool)
	_, err := r.render(func(accountID string) (bitbucket.User, bool) {
		if user, ok := r.users[accountID]; ok {
			return user, true
		}
		if unknown[accountID] {
			return bitbucket.User{}, false
		}
		user, err := client.GetUser(ctx, accountID)
		if err != nil {
			unknown[accountID] = true
			return bitbucket.User{}, false
		}
		r.users[accountID] = *user
		return *user, true
	})
	return err
}

func (r *ViewResult) knownUsers() []bitbucket.User {
	users := append([]bitbucket.User{r.Author}, r.Reviewers...)
	for _, participant := range r.Participants {
		users = append(users, participant.User)
	}
	for _, comment := range r.Comments {
		users = append(users, comment.User)
	}
	for _, task := range r.tasks {
		users = append(users, task.Comment.User)
	}
	return users
}

func (r *ViewResult) render(resolve func(string) (bitbucket.User, bool)) (string, error) {
	var buf bytes.Buffer
	prWriter := output.NewPRMarkdownWriter(&buf)
	prWriter.SetUserResolver(resolve)
	prWriter.SetContext(r.workspace, r.repo, r.Comments, r.showComments, r.tasks, r.showComments)
	if err := prWriter.WritePR(r.PullRequest); err != nil {
		return "", err
	}
	if !r.showComments {
		writeActivity(&buf, r.Activity)
		return buf.String(), nil
	}

	fmt.Fprintln(&buf)
	commentWriter := output.NewCommentWriter(&buf, r.Author)
	commentWriter.SetUserResolver(resolve)
	if len(r.diff) > 0 {
		commentWriter.SetDiff(r.diff)
	}
	if err := commentWriter.WriteComments(r.Comments, r.includeResolved); err != nil {
		return "", err
	}
	if r.CommentsTruncated {
		fmt.Fprintf(&buf, "\n[Showing the first %d comments; open the pull request on bitbucket.org for the rest.]\n", MaxViewComments)
	}
	if len(r.tasks) > 0 {
		fmt.Fprintln(&buf)
		taskWriter := output.NewTaskWriter(&buf)
		taskWriter.SetUserResolver(resolve)
		if err := taskWriter.WriteTasks(r.tasks); err != nil {
			return "", err
		}
		if r.tasksTruncated {
			fmt.Fprintf(&buf, "\n[Showing the first %d tasks; open the pull request on bitbucket.org for the rest.]\n", MaxViewTasks)
		}
	}
	writeActivity(&buf, r.Activity)
	return buf.String(), nil
}
