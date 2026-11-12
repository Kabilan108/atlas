package cli

import (
	"fmt"

	"github.com/kabilan108/atlas/internal/ops/pr"
	"github.com/spf13/cobra"
)

func newPRViewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "view [number|url|branch]",
		Short: "View a pull request",
		Args:  cobra.MaximumNArgs(1),
	}
	addPRRepoFlag(cmd)
	cmd.Flags().BoolP("comments", "c", false, "Include comments")
	cmd.Flags().Bool("all", false, "Include resolved comments (only with --comments)")
	cmd.Flags().BoolP("web", "w", false, "Open the pull request in the browser")
	return bind(cmd, binding[pr.ViewInput, *pr.ViewResult]{
		op:        pr.View,
		input:     prViewInput,
		intercept: openPRInBrowser,
		pager:     func(result *pr.ViewResult) string { return fmt.Sprintf("pr-%d.md", result.ID) },
	})
}

func prViewInput(cmd *cobra.Command, args []string, env *cliEnv) (pr.ViewInput, error) {
	ref, err := env.prRef(cmd, optionalArg(args))
	if err != nil {
		return pr.ViewInput{}, err
	}
	showComments, _ := cmd.Flags().GetBool("comments")
	includeResolved, _ := cmd.Flags().GetBool("all")
	jsonOutput, _ := cmd.Flags().GetBool("json")
	return pr.ViewInput{
		Ref:             ref,
		Comments:        showComments && !jsonOutput,
		IncludeResolved: includeResolved,
		JSON:            jsonOutput,
	}, nil
}

func openPRInBrowser(cmd *cobra.Command, env *cliEnv, in pr.ViewInput) (bool, error) {
	if web, _ := cmd.Flags().GetBool("web"); !web {
		return false, nil
	}
	target, err := in.Resolve(cmd.Context(), env)
	if err != nil {
		return true, err
	}
	return true, openBrowser(target.PR.Links.HTML.Href)
}
