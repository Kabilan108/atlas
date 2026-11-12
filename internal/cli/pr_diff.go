package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/git"
	"github.com/kabilan108/atlas/internal/ops/pr"
	"github.com/kabilan108/atlas/internal/output"
	"github.com/spf13/cobra"
)

func newPRDiffCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diff [number|url|branch]",
		Short: "View changes in a pull request",
		Args:  cobra.MaximumNArgs(1),
	}
	addPRRepoFlag(cmd)
	cmd.Flags().String("color", "auto", "Use color in diff output: auto, always, never")
	cmd.Flags().Bool("name-only", false, "Display only names of changed files")
	cmd.Flags().Bool("patch", false, "Display diff in patch format")
	cmd.Flags().BoolP("structured", "s", false, "Use difftastic when available")
	return bind(cmd, binding[pr.DiffInput, *pr.DiffResult]{
		op: pr.Diff,
		input: func(cmd *cobra.Command, args []string, env *cliEnv) (pr.DiffInput, error) {
			ref, err := env.prRef(cmd, optionalArg(args))
			return pr.DiffInput{Ref: ref, MaxBytes: -1}, err
		},
		human: func(w io.Writer, result *pr.DiffResult) error { return writePRDiff(cmd, w, result) },
	})
}

// writePRDiff shows the diff the way a terminal user wants it: file names,
// difftastic on a matching checkout, delta, or the git pager, and the plain
// patch when piped.
func writePRDiff(cmd *cobra.Command, w io.Writer, result *pr.DiffResult) error {
	nameOnly, _ := cmd.Flags().GetBool("name-only")
	patch, _ := cmd.Flags().GetBool("patch")
	structured, _ := cmd.Flags().GetBool("structured")
	color, _ := cmd.Flags().GetString("color")
	target := result.Target()
	diff := []byte(result.Diff)
	if nameOnly {
		for _, name := range result.Files {
			if _, err := fmt.Fprintln(w, name); err != nil {
				return err
			}
		}
		return nil
	}
	if structured && !localCheckoutMatches(target.Repository) {
		fmt.Fprintf(cmd.ErrOrStderr(), "Structured diff needs a local checkout of %s; showing the API patch instead\n", target.Repository)
		structured = false
	}
	if structured {
		if err := runStructuredDiff(target.PR, color); err != nil {
			if _, lookErr := exec.LookPath("difft"); lookErr == nil {
				return fmt.Errorf("structured diff failed: %w", err)
			}
		} else {
			return nil
		}
	}
	if patch || !isTerminal(w) {
		_, err := w.Write(diff)
		return err
	}
	if deltaPath, err := exec.LookPath("delta"); err == nil {
		args := []string{}
		if color != "" {
			args = append(args, "--color-only")
		}
		return output.PipeToCommand(deltaPath, args, diff)
	}
	return pageDiff(diff)
}

func runStructuredDiff(pr *bitbucket.PullRequest, color string) error {
	difftPath, err := exec.LookPath("difft")
	if err != nil {
		return err
	}
	base := pr.Destination.Branch.Name
	head := pr.Source.Branch.Name
	baseRef := fmt.Sprintf("refs/atlas/pr/%d/base", pr.ID)
	headRef := fmt.Sprintf("refs/atlas/pr/%d/head", pr.ID)
	baseTarget := baseRef
	if base != "" {
		if err := git.Fetch("origin", fmt.Sprintf("+%s:%s", base, baseRef)); err != nil {
			if pr.Destination.Commit.Hash == "" || !git.RefExists(pr.Destination.Commit.Hash) {
				return err
			}
			baseTarget = pr.Destination.Commit.Hash
		}
	} else if pr.Destination.Commit.Hash != "" && git.RefExists(pr.Destination.Commit.Hash) {
		baseTarget = pr.Destination.Commit.Hash
	}
	sourceRemote := "origin"
	if pr.Source.Repository.FullName != "" && pr.Source.Repository.FullName != pr.Destination.Repository.FullName {
		sourceRemote = strings.Split(pr.Source.Repository.FullName, "/")[0]
		if !git.RemoteExists(sourceRemote) {
			if err := git.AddRemote(sourceRemote, "git@bitbucket.org:"+pr.Source.Repository.FullName+".git"); err != nil {
				return err
			}
		}
	}
	headTarget := headRef
	if err := git.Fetch(sourceRemote, fmt.Sprintf("+%s:%s", head, headRef)); err != nil {
		switch {
		case git.RefExists(head):
			headTarget = head
		case pr.Source.Commit.Hash != "" && git.RefExists(pr.Source.Commit.Hash):
			headTarget = pr.Source.Commit.Hash
		default:
			return err
		}
	}
	args := []string{"diff", "--ext-diff", baseTarget, headTarget}
	cmd := exec.Command("git", args...)
	cmd.Env = append(os.Environ(), "GIT_EXTERNAL_DIFF="+difftPath)
	if color != "" {
		cmd.Env = append(cmd.Env, "DFT_COLOR="+color)
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
