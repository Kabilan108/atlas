package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/kabilan108/atlas/internal/ops/pr"
	"github.com/kabilan108/atlas/internal/output"
	"github.com/spf13/cobra"
)

func newPRCommentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "comment [number|url|branch]",
		Short: "Comment on a pull request",
		Args:  cobra.MaximumNArgs(1),
	}
	addPRRepoFlag(cmd)
	addCommentBodyFlags(cmd)
	cmd.Flags().Int("reply-to", 0, "Reply to an existing comment")
	cmd.Flags().String("path", "", "Attach the comment to a file path")
	cmd.Flags().Int("line", 0, "Attach the comment to a line in --path")
	cmd.Flags().String("side", "new", "Line side for inline comments: new or old")
	cmd.Flags().Int("edit", 0, "Replace the body of the comment with this ID")
	cmd.Flags().Int("delete", 0, "Delete the comment with this ID")
	cmd.MarkFlagsMutuallyExclusive("edit", "delete")
	return bind(cmd, binding[pr.CommentInput, *pr.CommentResult]{
		op:        pr.Comment,
		input:     prCommentInput,
		intercept: deletePRComment,
		human: func(w io.Writer, result *pr.CommentResult) error {
			verb := "Created"
			if result.Edited() {
				verb = "Updated"
			}
			_, err := fmt.Fprintf(w, "%s comment #%d on pull request #%d\n", verb, result.ID, result.PullRequest())
			return err
		},
	})
}

func prCommentInput(cmd *cobra.Command, args []string, env *cliEnv) (pr.CommentInput, error) {
	var in pr.CommentInput
	deleteID, _ := cmd.Flags().GetInt("delete")
	if cmd.Flags().Changed("delete") {
		for _, flag := range []string{"body", "body-file", "editor", "reply-to", "path", "line", "side"} {
			if cmd.Flags().Changed(flag) {
				return in, fmt.Errorf("--delete cannot be combined with --%s", flag)
			}
		}
		if deleteID <= 0 {
			return in, fmt.Errorf("--delete needs a comment ID")
		}
	} else {
		body, _, err := readCommentBodyFromFlags(cmd, true)
		if err != nil {
			return in, err
		}
		replyTo, _ := cmd.Flags().GetInt("reply-to")
		path, _ := cmd.Flags().GetString("path")
		line, _ := cmd.Flags().GetInt("line")
		side, _ := cmd.Flags().GetString("side")
		if !cmd.Flags().Changed("side") && line == 0 {
			side = ""
		}
		edit, _ := cmd.Flags().GetInt("edit")
		if cmd.Flags().Changed("edit") && edit <= 0 {
			return in, fmt.Errorf("--edit needs a comment ID")
		}
		in.CommentSpec = pr.CommentSpec{Body: body, ReplyTo: replyTo, Path: path, Line: line, Side: pr.Side(strings.ToLower(strings.TrimSpace(side)))}
		in.Edit = edit
	}
	ref, err := env.prRef(cmd, optionalArg(args))
	in.Ref = ref
	return in, err
}

// deletePRComment runs --delete through pr_comment_delete, the MCP tool for
// it, since one command is bound to one op.
func deletePRComment(cmd *cobra.Command, env *cliEnv, in pr.CommentInput) (bool, error) {
	if !cmd.Flags().Changed("delete") {
		return false, nil
	}
	id, _ := cmd.Flags().GetInt("delete")
	result, err := pr.CommentDelete.Call(cmd.Context(), env, pr.CommentDeleteInput{Ref: in.Ref, Comment: id})
	if err != nil {
		return true, err
	}
	if jsonOutput, _ := cmd.Flags().GetBool("json"); jsonOutput {
		return true, output.WriteJSON(cmd.OutOrStdout(), result)
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "Deleted comment #%d on pull request #%d\n", result.Comment, result.PullRequest)
	return true, err
}
