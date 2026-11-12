package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/git"
	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/ops/pr"
	"github.com/kabilan108/atlas/internal/output"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newPRCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pr",
		Short: "Work with pull requests",
	}

	cmd.AddCommand(newPRListCmd())
	cmd.AddCommand(newPRViewCmd())
	cmd.AddCommand(newPRCheckoutCmd())
	cmd.AddCommand(newPRDiffCmd())
	cmd.AddCommand(newPREditCmd())
	cmd.AddCommand(newPRCloseCmd())
	cmd.AddCommand(newPRCommentCmd())
	cmd.AddCommand(newPRApproveCmd())
	cmd.AddCommand(newPRUnapproveCmd())
	cmd.AddCommand(newPRRequestChangesCmd())
	cmd.AddCommand(newPRClearChangeRequestCmd())
	cmd.AddCommand(newPRReviewCmd())
	cmd.AddCommand(newPRCreateCmd())
	cmd.AddCommand(newPRStatusCmd())

	return cmd
}

func addPRRepoFlag(cmd *cobra.Command) {
	cmd.Flags().StringP("repo", "R", "", "Select another repository using the workspace/repo or repo format")
}

func pageDiff(diff []byte) error {
	pager := git.GitPager()
	if pager == "" {
		_, err := os.Stdout.Write(diff)
		return err
	}
	parts := strings.Fields(pager)
	return output.PipeToCommand(parts[0], parts[1:], diff)
}

func pageMarkdown(markdown []byte, filename string) error {
	if batPath, err := exec.LookPath("bat"); err == nil {
		return output.PipeToCommand(batPath, []string{"--paging=always", "--language=markdown", "--file-name", filename}, markdown)
	}
	if lessPath, err := exec.LookPath("less"); err == nil {
		return output.PipeToCommand(lessPath, nil, markdown)
	}
	_, err := os.Stdout.Write(markdown)
	return err
}

func newPRCheckoutCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "checkout <number|url|branch>",
		Short: "Checkout a PR branch locally",
		Args:  cobra.ExactArgs(1),
		RunE:  runPRCheckout,
	}
	addPRRepoFlag(cmd)
	cmd.Flags().StringP("branch", "b", "", "Local branch name to use")
	cmd.Flags().Bool("detach", false, "Checkout PR with a detached HEAD")
	cmd.Flags().BoolP("force", "f", false, "Reset existing local branch to the latest state")
	cmd.Flags().Bool("recurse-submodules", false, "Update all submodules after checkout")
	return cmd
}

func runPRCheckout(cmd *cobra.Command, args []string) error {
	localBranch, _ := cmd.Flags().GetString("branch")
	detach, _ := cmd.Flags().GetBool("detach")
	force, _ := cmd.Flags().GetBool("force")
	recurse, _ := cmd.Flags().GetBool("recurse-submodules")
	env, err := newCLIEnv(cmd)
	if err != nil {
		return err
	}
	ref, err := env.prRef(cmd, args[0])
	if err != nil {
		return err
	}
	target, err := ref.Resolve(cmd.Context(), env)
	if err != nil {
		return err
	}
	pr := target.PR
	if !localCheckoutMatches(target.Repository) {
		return fmt.Errorf("pull request #%d belongs to %s, but the current directory is not a checkout of it; run checkout from a clone of %s", pr.ID, target.Repository, target.Repository)
	}
	remote := "origin"
	if pr.Source.Repository.FullName != "" && pr.Source.Repository.FullName != pr.Destination.Repository.FullName {
		remote = strings.Split(pr.Source.Repository.FullName, "/")[0]
		if !git.RemoteExists(remote) {
			if err := git.AddRemote(remote, "git@bitbucket.org:"+pr.Source.Repository.FullName+".git"); err != nil {
				return err
			}
		}
	}
	sourceBranch := pr.Source.Branch.Name
	if err := git.Fetch(remote, sourceBranch); err != nil {
		return err
	}
	startPoint := "FETCH_HEAD"
	if detach {
		err = git.CheckoutDetached(startPoint)
	} else {
		if localBranch == "" {
			localBranch = sourceBranch
		}
		err = git.CheckoutBranch(localBranch, startPoint, force)
	}
	if err != nil {
		return err
	}
	if recurse {
		if err := git.UpdateSubmodules(); err != nil {
			return err
		}
	}
	if detach {
		fmt.Printf("Checked out pull request #%d in detached HEAD\n", pr.ID)
	} else {
		fmt.Printf("Switched to branch '%s' for pull request #%d\n", localBranch, pr.ID)
	}
	return nil
}

func localCheckoutMatches(target ops.Repository) bool {
	workspace, repo, err := git.InferRepository()
	if err != nil {
		return false
	}
	return strings.EqualFold(workspace, target.Workspace) && strings.EqualFold(repo, target.Repo)
}

func optionalArg(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}

type commentBodyFlags struct {
	body        string
	bodySet     bool
	bodyFile    string
	bodyFileSet bool
	editor      bool
}

func addCommentBodyFlags(cmd *cobra.Command) {
	cmd.Flags().StringP("body", "b", "", "Comment body")
	cmd.Flags().StringP("body-file", "F", "", "Read comment body from file (use - to read from stdin)")
	cmd.Flags().BoolP("editor", "e", false, "Open the editor to write the comment body")
}

func getCommentBodyFlags(cmd *cobra.Command) commentBodyFlags {
	body, _ := cmd.Flags().GetString("body")
	bodyFile, _ := cmd.Flags().GetString("body-file")
	editor, _ := cmd.Flags().GetBool("editor")
	return commentBodyFlags{
		body:        body,
		bodySet:     cmd.Flags().Changed("body"),
		bodyFile:    bodyFile,
		bodyFileSet: cmd.Flags().Changed("body-file"),
		editor:      editor,
	}
}

func validateCommentBodySources(flags commentBodyFlags, required bool) error {
	count := 0
	if flags.bodySet || flags.body != "" {
		count++
	}
	if flags.bodyFileSet || flags.bodyFile != "" {
		count++
	}
	if flags.editor {
		count++
	}
	if count > 1 {
		return fmt.Errorf("--body, --body-file, and --editor are mutually exclusive")
	}
	if required && count == 0 {
		return fmt.Errorf("one of --body, --body-file, or --editor is required")
	}
	return nil
}

func readCommentBodyFromFlags(cmd *cobra.Command, required bool) (string, bool, error) {
	flags := getCommentBodyFlags(cmd)
	if err := validateCommentBodySources(flags, required); err != nil {
		return "", false, err
	}
	hasBody := flags.bodySet || flags.body != "" || flags.bodyFileSet || flags.bodyFile != "" || flags.editor
	if !hasBody {
		return "", false, nil
	}

	var body string
	switch {
	case flags.bodySet || flags.body != "":
		body = flags.body
	case flags.bodyFileSet || flags.bodyFile != "":
		bodyBytes, err := readBodyFile(flags.bodyFile)
		if err != nil {
			return "", false, err
		}
		body = string(bodyBytes)
	case flags.editor:
		edited, err := editTextInEditor("")
		if err != nil {
			return "", false, err
		}
		body = edited
	}
	if strings.TrimSpace(body) == "" {
		return "", false, fmt.Errorf("comment body cannot be empty")
	}
	return body, true, nil
}

func readBodyFile(path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(path)
}

func shouldEditBodyInEditor(title, body, bodyFile, addReviewers, removeReviewers string) bool {
	return body == "" && bodyFile == "" && title == "" && addReviewers == "" && removeReviewers == ""
}

func editTextInEditor(initial string) (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", fmt.Errorf("cannot open editor when stdin is not a terminal; use --body or --body-file")
	}
	editor := strings.TrimSpace(os.Getenv("EDITOR"))
	if editor == "" {
		if _, err := exec.LookPath("nvim"); err == nil {
			editor = "nvim"
		} else if _, err := exec.LookPath("vi"); err == nil {
			editor = "vi"
		} else {
			return "", fmt.Errorf("no editor found; set EDITOR or install nvim")
		}
	}
	tempDir, err := os.MkdirTemp("", "atlas-pr-edit-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tempDir)
	tempPath := filepath.Join(tempDir, "body.md")
	if err := os.WriteFile(tempPath, []byte(initial), 0o600); err != nil {
		return "", err
	}
	editorCmd := exec.Command("sh", "-c", editor+" \"$1\"", "atlas-editor", tempPath)
	editorCmd.Stdin = os.Stdin
	editorCmd.Stdout = os.Stdout
	editorCmd.Stderr = os.Stderr
	if err := editorCmd.Run(); err != nil {
		return "", err
	}
	data, err := os.ReadFile(tempPath)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func splitIdentifiers(value string) []string {
	var ids []string
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			ids = append(ids, part)
		}
	}
	return ids
}

func reviewerSummary(p bitbucket.PullRequest) string {
	approved, changes, pending := pr.ReviewCounts(p)
	parts := []string{}
	if approved > 0 {
		parts = append(parts, styleApproved(fmt.Sprintf("%d approved", approved)))
	}
	if changes > 0 {
		parts = append(parts, styleChanges(fmt.Sprintf("%d changes requested", changes)))
	}
	if pending > 0 {
		parts = append(parts, stylePending(fmt.Sprintf("%d pending", pending)))
	}
	if len(parts) == 0 {
		return styleMuted("no reviewers")
	}
	return strings.Join(parts, ", ")
}

func shouldStyleTerminal() bool {
	return term.IsTerminal(int(os.Stdout.Fd())) && strings.ToLower(os.Getenv("NO_COLOR")) == ""
}

func ansiStyle(code, value string) string {
	if !shouldStyleTerminal() || value == "" {
		return value
	}
	return "\x1b[" + code + "m" + value + "\x1b[0m"
}

func styleBold(value string) string {
	return ansiStyle("1", value)
}

func styleMuted(value string) string {
	return ansiStyle("2", value)
}

func stylePRID(id int) string {
	return ansiStyle("36", fmt.Sprintf("#%d", id))
}

func stylePRState(pr bitbucket.PullRequest) string {
	state := styleState(pr.State)
	if pr.Draft {
		state += " (DRAFT)"
	}
	return state
}

func styleState(state string) string {
	switch strings.ToUpper(state) {
	case "OPEN":
		return ansiStyle("32", state)
	case "MERGED":
		return ansiStyle("35", state)
	case "DECLINED", "SUPERSEDED":
		return ansiStyle("31", state)
	default:
		return state
	}
}

func styleApproved(value string) string {
	return ansiStyle("32", value)
}

func styleChanges(value string) string {
	return ansiStyle("31", value)
}

func stylePending(value string) string {
	return ansiStyle("33", value)
}
