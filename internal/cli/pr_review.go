package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/kabilan108/atlas/internal/ops/pr"
	"github.com/spf13/cobra"
)

func newPRReviewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "review [number|url|branch]",
		Short: "Post review comments and finish a pull request review",
		Args:  cobra.MaximumNArgs(1),
	}
	addPRRepoFlag(cmd)
	addCommentBodyFlags(cmd)
	cmd.Flags().Bool("approve", false, "Finish by approving")
	cmd.Flags().Bool("request-changes", false, "Finish by requesting changes")
	cmd.Flags().Bool("comment", false, "Finish with comments only")
	cmd.Flags().String("comment-spec", "", "JSON file of comments to post before the final review action")
	return bind(cmd, binding[pr.ReviewInput, *pr.ReviewResult]{
		op:    pr.Review,
		input: prReviewInput,
		human: printPRReview,
	})
}

func prReviewInput(cmd *cobra.Command, args []string, env *cliEnv) (pr.ReviewInput, error) {
	action, err := reviewActionFromFlags(cmd)
	if err != nil {
		return pr.ReviewInput{}, err
	}
	body, hasSummary, err := readCommentBodyFromFlags(cmd, false)
	if err != nil {
		return pr.ReviewInput{}, err
	}
	specPath, _ := cmd.Flags().GetString("comment-spec")
	specs, err := readCommentSpecs(specPath)
	if err != nil {
		return pr.ReviewInput{}, err
	}
	if action == pr.ReviewComment && len(specs) == 0 && !hasSummary {
		return pr.ReviewInput{}, fmt.Errorf("comment-only review requires --body, --body-file, --editor, or --comment-spec")
	}
	ref, err := env.prRef(cmd, optionalArg(args))
	return pr.ReviewInput{Ref: ref, Action: action, Body: body, Comments: specs}, err
}

func printPRReview(w io.Writer, result *pr.ReviewResult) error {
	if !result.Complete() {
		return nil
	}
	var err error
	switch result.Action {
	case pr.ReviewApprove:
		_, err = fmt.Fprintf(w, "Posted %d comments and approved pull request #%d\n", len(result.Comments), result.PullRequest)
	case pr.ReviewRequestChanges:
		_, err = fmt.Fprintf(w, "Posted %d comments and requested changes on pull request #%d\n", len(result.Comments), result.PullRequest)
	default:
		_, err = fmt.Fprintf(w, "Posted %d comments on pull request #%d\n", len(result.Comments), result.PullRequest)
	}
	return err
}

// readCommentSpecs reads a --comment-spec file. Sides are matched without
// regard to case, as they always were.
func readCommentSpecs(path string) ([]pr.CommentSpec, error) {
	if path == "" {
		return nil, nil
	}
	data, err := readBodyFile(path)
	if err != nil {
		return nil, err
	}
	var specs []pr.CommentSpec
	if err := json.Unmarshal(data, &specs); err != nil {
		return nil, fmt.Errorf("failed to parse comment spec: %w", err)
	}
	for i := range specs {
		specs[i].Side = pr.Side(strings.ToLower(strings.TrimSpace(string(specs[i].Side))))
	}
	return specs, nil
}

func reviewActionFromFlags(cmd *cobra.Command) (pr.ReviewAction, error) {
	var actions []pr.ReviewAction
	for _, action := range []pr.ReviewAction{pr.ReviewApprove, pr.ReviewRequestChanges, pr.ReviewComment} {
		if set, _ := cmd.Flags().GetBool(string(action)); set {
			actions = append(actions, action)
		}
	}
	if len(actions) != 1 {
		return "", fmt.Errorf("exactly one of --approve, --request-changes, or --comment is required")
	}
	return actions[0], nil
}
