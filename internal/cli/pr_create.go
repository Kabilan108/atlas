package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/git"
	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/ops/pr"
	"github.com/spf13/cobra"
)

func newPRCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a pull request",
	}
	addPRRepoFlag(cmd)
	cmd.Flags().StringP("base", "B", "", "The branch into which you want your code merged")
	cmd.Flags().StringP("head", "H", "", "The branch that contains commits for your pull request")
	cmd.Flags().StringP("title", "t", "", "Title for the pull request")
	cmd.Flags().StringP("body", "b", "", "Body for the pull request")
	cmd.Flags().StringP("body-file", "F", "", "Read body text from file (use - to read from stdin)")
	cmd.Flags().BoolP("editor", "e", false, "Open the editor to write the pull request body")
	cmd.Flags().BoolP("fill", "f", false, "Use commit info for title and body")
	cmd.Flags().StringP("reviewer", "r", "", "Request reviews from people by identifier")
	cmd.Flags().Bool("push", false, "Push the head branch before creating the PR")
	cmd.Flags().Bool("dry-run", false, "Print details instead of creating the PR")
	cmd.Flags().Bool("draft", false, "Create the pull request as draft")
	cmd.Flags().BoolP("web", "w", false, "Open the browser to create a pull request")
	return bind(cmd, binding[pr.CreateInput, *bitbucket.PullRequest]{
		op:        pr.Create,
		input:     prCreateInput,
		intercept: interceptPRCreate,
		human: func(w io.Writer, created *bitbucket.PullRequest) error {
			_, err := fmt.Fprintln(w, created.Links.HTML.Href)
			return err
		},
	})
}

// prCreateInput fills what the op needs from git and the local machine:
// the head from the current branch, the base (fetched first, since --fill
// compares against it), and the body from a file, commits, or the editor.
func prCreateInput(cmd *cobra.Command, _ []string, env *cliEnv) (pr.CreateInput, error) {
	base, _ := cmd.Flags().GetString("base")
	head, _ := cmd.Flags().GetString("head")
	title, _ := cmd.Flags().GetString("title")
	body, _ := cmd.Flags().GetString("body")
	bodyFile, _ := cmd.Flags().GetString("body-file")
	editor, _ := cmd.Flags().GetBool("editor")
	fill, _ := cmd.Flags().GetBool("fill")
	reviewers, _ := cmd.Flags().GetString("reviewer")
	webCreate, _ := cmd.Flags().GetBool("web")
	draft, _ := cmd.Flags().GetBool("draft")
	var in pr.CreateInput
	if webCreate && draft {
		return in, fmt.Errorf("--draft cannot be used with --web; create the draft through the API without --web")
	}
	if body != "" && bodyFile != "" {
		return in, fmt.Errorf("--body and --body-file cannot be used together")
	}
	if editor && (body != "" || bodyFile != "") {
		return in, fmt.Errorf("--editor cannot be used with --body or --body-file")
	}
	repo, err := env.repository(cmd)
	if err != nil {
		return in, err
	}
	in.RepoRef = ops.RepoRef{Repo: repo}
	if base == "" {
		resolved, err := in.Resolve(env)
		if err != nil {
			return in, err
		}
		client, err := env.Bitbucket(cmd.Context())
		if err != nil {
			return in, err
		}
		if base, err = pr.DefaultBase(cmd.Context(), client, resolved); err != nil {
			return in, err
		}
	}
	if head == "" {
		if head, err = git.CurrentBranch(); err != nil {
			return in, err
		}
	}
	if bodyFile != "" {
		bodyBytes, err := readBodyFile(bodyFile)
		if err != nil {
			return in, err
		}
		body = string(bodyBytes)
	}
	if fill {
		filledTitle, filledBody, err := fillPRText(base, head)
		if err != nil {
			return in, err
		}
		if title == "" {
			title = filledTitle
		}
		if body == "" && bodyFile == "" {
			body = filledBody
		}
	}
	if editor {
		if body, err = editTextInEditor(body); err != nil {
			return in, err
		}
	}
	in.Title, in.Head, in.Base, in.Body, in.Draft = title, head, base, body, draft
	in.Reviewers = splitIdentifiers(reviewers)
	return in, nil
}

// interceptPRCreate handles --web, makes sure the head is on origin
// (pushing it with --push), and prints the request instead with --dry-run.
func interceptPRCreate(cmd *cobra.Command, env *cliEnv, in pr.CreateInput) (bool, error) {
	push, _ := cmd.Flags().GetBool("push")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	if web, _ := cmd.Flags().GetBool("web"); web {
		repo, err := in.Resolve(env)
		if err != nil {
			return true, err
		}
		target := fmt.Sprintf("https://bitbucket.org/%s/%s/pull-requests/new?source=%s&dest=%s", repo.Workspace, repo.Repo, url.QueryEscape(in.Head), url.QueryEscape(in.Base))
		return true, openBrowser(target)
	}
	if in.Title == "" {
		return true, fmt.Errorf("--title or --fill is required")
	}
	if !git.RemoteBranchExists("origin", in.Head) {
		if !push {
			return true, fmt.Errorf("head branch %q is not pushed to origin; rerun with --push", in.Head)
		}
		if !dryRun {
			if err := pushHead(in.Head); err != nil {
				return true, err
			}
		}
	}
	if !dryRun {
		return false, nil
	}
	_, payload, err := pr.PrepareCreate(cmd.Context(), env, in)
	if err != nil {
		return true, err
	}
	return true, json.NewEncoder(cmd.OutOrStdout()).Encode(payload)
}

// pushHead pushes head to origin: the current HEAD when head is checked
// out, and the named branch ref otherwise.
func pushHead(head string) error {
	currentBranch, err := git.CurrentBranch()
	if err != nil {
		return err
	}
	if currentBranch == head {
		return git.PushCurrentBranch("origin", head)
	}
	return git.PushBranch("origin", head)
}

func fillPRText(base, head string) (string, string, error) {
	messages, err := git.CommitMessages("origin/"+base, head)
	if err != nil {
		return "", "", err
	}
	if len(messages) == 0 {
		return head, "", nil
	}
	if len(messages) == 1 {
		return messages[0].Subject, messages[0].Body, nil
	}
	var body strings.Builder
	for _, message := range messages {
		fmt.Fprintf(&body, "- %s\n", message.Subject)
	}
	return head, body.String(), nil
}
