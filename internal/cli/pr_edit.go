package cli

import (
	"fmt"
	"io"

	"github.com/kabilan108/atlas/internal/ops/pr"
	"github.com/spf13/cobra"
)

func newPREditCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "edit [number|url|branch]",
		Short: "Edit a pull request",
		Args:  cobra.MaximumNArgs(1),
	}
	addPRRepoFlag(cmd)
	cmd.Flags().StringP("title", "t", "", "Set the new title")
	cmd.Flags().StringP("body", "b", "", "Set the new body")
	cmd.Flags().StringP("body-file", "F", "", "Read body text from file (use - to read from stdin)")
	cmd.Flags().StringP("base", "B", "", "Change the branch the pull request merges into")
	cmd.Flags().String("add-reviewer", "", "Add reviewers by identifier")
	cmd.Flags().String("remove-reviewer", "", "Remove reviewers by identifier")
	cmd.Flags().Bool("draft", false, "Mark the pull request as draft")
	cmd.Flags().Bool("ready", false, "Mark the pull request as ready for review")
	cmd.MarkFlagsMutuallyExclusive("draft", "ready")
	return bind(cmd, binding[pr.EditInput, *pr.EditResult]{
		op:    pr.Edit,
		input: prEditInput,
		human: func(w io.Writer, result *pr.EditResult) error {
			if !result.Changed {
				_, err := fmt.Fprintln(w, "No changes.")
				return err
			}
			_, err := fmt.Fprintf(w, "Updated pull request #%d: %s\n%s\n", result.ID, result.Title, result.Links.HTML.Href)
			return err
		},
	})
}

// prEditInput reads the edit flags. With none of them it opens the editor
// on the current description, which needs the pull request fetched first.
func prEditInput(cmd *cobra.Command, args []string, env *cliEnv) (pr.EditInput, error) {
	title, _ := cmd.Flags().GetString("title")
	body, _ := cmd.Flags().GetString("body")
	bodyFile, _ := cmd.Flags().GetString("body-file")
	base, _ := cmd.Flags().GetString("base")
	addReviewers, _ := cmd.Flags().GetString("add-reviewer")
	removeReviewers, _ := cmd.Flags().GetString("remove-reviewer")
	if body != "" && bodyFile != "" {
		return pr.EditInput{}, fmt.Errorf("--body and --body-file cannot be used together")
	}
	ref, err := env.prRef(cmd, optionalArg(args))
	if err != nil {
		return pr.EditInput{}, err
	}
	in := pr.EditInput{
		Ref:             ref,
		Title:           title,
		Base:            base,
		AddReviewers:    splitIdentifiers(addReviewers),
		RemoveReviewers: splitIdentifiers(removeReviewers),
	}
	draftChanged := cmd.Flags().Changed("draft") || cmd.Flags().Changed("ready")
	if draftChanged {
		draft, _ := cmd.Flags().GetBool("draft")
		if cmd.Flags().Changed("ready") {
			ready, _ := cmd.Flags().GetBool("ready")
			draft = !ready
		}
		in.Draft = &draft
	}
	bodyChanged := cmd.Flags().Changed("body")
	if bodyFile != "" {
		bodyBytes, err := readBodyFile(bodyFile)
		if err != nil {
			return pr.EditInput{}, err
		}
		body, bodyChanged = string(bodyBytes), true
	}
	if !bodyChanged && !draftChanged && base == "" && shouldEditBodyInEditor(title, body, bodyFile, addReviewers, removeReviewers) {
		target, err := ref.Resolve(cmd.Context(), env)
		if err != nil {
			return pr.EditInput{}, err
		}
		if body, err = editTextInEditor(target.PR.Description); err != nil {
			return pr.EditInput{}, err
		}
		bodyChanged = true
	}
	if bodyChanged {
		in.Body = &body
	}
	return in, nil
}
