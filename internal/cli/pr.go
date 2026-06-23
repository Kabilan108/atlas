package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/config"
	"github.com/kabilan108/atlas/internal/git"
	"github.com/kabilan108/atlas/internal/output"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

type prContext struct {
	workspace string
	repo      string
	client    *bitbucket.Client
}

type prRef struct {
	workspace string
	repo      string
	id        int
	branch    string
}

type PRViewJSON struct {
	*bitbucket.PullRequest
	Comments []bitbucket.Comment `json:"comments,omitempty"`
}

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

func prCommandContext(cmd *cobra.Command, allowAll bool) (*prContext, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	repoFlag, _ := cmd.Flags().GetString("repo")
	workspace := cfg.Workspace
	repo := ""
	if repoFlag != "" {
		parts := strings.Split(repoFlag, "/")
		switch len(parts) {
		case 1:
			repo = parts[0]
		case 2:
			workspace = parts[0]
			repo = parts[1]
		default:
			return nil, fmt.Errorf("invalid repository %q: expected repo or workspace/repo", repoFlag)
		}
	}

	if repo == "" && !allowAll {
		inferredWS, inferredRepo, err := git.InferRepository()
		if err != nil {
			return nil, fmt.Errorf("could not infer repository: %w\nUse -R to specify", err)
		}
		if workspace == "" {
			workspace = inferredWS
		}
		repo = inferredRepo
		if verbose {
			fmt.Fprintf(os.Stderr, "Using repository: %s/%s\n", workspace, repo)
		}
	}

	if workspace == "" {
		return nil, fmt.Errorf("workspace not configured. Run 'atlas config set workspace <name>' or use -R workspace/repo")
	}

	client, err := bitbucket.NewClient(bitbucket.WithNoCache(noCache))
	if err != nil {
		return nil, err
	}

	return &prContext{workspace: workspace, repo: repo, client: client}, nil
}

func resolvePR(ctx *prContext, selector string, allowCurrentBranch bool) (*bitbucket.PullRequest, error) {
	ref, err := parsePRRef(selector)
	if err != nil {
		return nil, err
	}
	workspace := ctx.workspace
	repo := ctx.repo
	if ref.workspace != "" {
		workspace = ref.workspace
	}
	if ref.repo != "" {
		repo = ref.repo
	}
	if repo == "" {
		return nil, fmt.Errorf("repository is required. Use -R workspace/repo")
	}
	if ref.id > 0 {
		return ctx.client.GetPullRequest(workspace, repo, ref.id)
	}
	branch := ref.branch
	if branch == "" {
		if !allowCurrentBranch {
			return nil, fmt.Errorf("pull request selector is required")
		}
		branch, err = git.CurrentBranch()
		if err != nil {
			return nil, err
		}
	}
	return ctx.client.FindPullRequestByBranch(workspace, repo, branch)
}

func parsePRRef(selector string) (prRef, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return prRef{}, nil
	}
	if parsed, err := url.Parse(selector); err == nil && parsed.Host == "bitbucket.org" {
		parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		if len(parts) >= 4 && parts[2] == "pull-requests" {
			id, err := strconv.Atoi(parts[3])
			if err != nil {
				return prRef{}, fmt.Errorf("invalid pull request URL %q", selector)
			}
			return prRef{workspace: parts[0], repo: parts[1], id: id}, nil
		}
	}
	number := strings.TrimPrefix(selector, "#")
	if id, err := strconv.Atoi(number); err == nil {
		return prRef{id: id}, nil
	}
	return prRef{branch: selector}, nil
}

func newPRListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List pull requests",
		RunE:  runPRList,
	}
	addPRRepoFlag(cmd)
	cmd.Flags().Bool("all", false, "List PRs across all repos in workspace")
	cmd.Flags().String("state", "open", "Filter by state: open, merged, declined, superseded")
	cmd.Flags().String("author", "", "Filter by author username")
	cmd.Flags().String("reviewer", "", "Filter by reviewer username")
	cmd.Flags().Bool("json", false, "Output as JSON")
	return cmd
}

func runPRList(cmd *cobra.Command, args []string) error {
	allRepos, _ := cmd.Flags().GetBool("all")
	state, _ := cmd.Flags().GetString("state")
	author, _ := cmd.Flags().GetString("author")
	reviewer, _ := cmd.Flags().GetString("reviewer")
	jsonOutput, _ := cmd.Flags().GetBool("json")

	ctx, err := prCommandContext(cmd, allRepos)
	if err != nil {
		return err
	}

	apiState, err := normalizePRState(state)
	if err != nil {
		return err
	}
	opts := &bitbucket.PRListOptions{State: apiState, Author: author, Reviewer: reviewer}
	var prs []bitbucket.PullRequest
	if allRepos {
		prs, err = ctx.client.ListAllPullRequests(ctx.workspace, opts)
	} else {
		prs, err = ctx.client.ListPullRequests(ctx.workspace, ctx.repo, opts)
	}
	if err != nil {
		return err
	}
	if jsonOutput {
		return output.WriteJSON(os.Stdout, prs)
	}
	return printPRTable(prs)
}

func printPRTable(prs []bitbucket.PullRequest) error {
	if len(prs) == 0 {
		fmt.Println("No pull requests found.")
		return nil
	}
	hasComments := false
	for _, pr := range prs {
		if pr.CommentCount > 0 || pr.TaskCount > 0 {
			hasComments = true
			break
		}
	}
	headers := []string{"ID", "Title", "Author", "State", "Updated"}
	if hasComments {
		headers = append(headers, "Feedback")
	}
	if term.IsTerminal(int(os.Stdout.Fd())) {
		for i, header := range headers {
			headers[i] = styleBold(header)
		}
	}
	tw := output.NewTableWriter(os.Stdout, headers...)
	for _, pr := range prs {
		row := []string{
			stylePRID(pr.ID),
			output.Truncate(pr.Title, 50),
			pr.Author.DisplayName,
			styleState(pr.State),
			output.FormatRelativeTime(pr.UpdatedOn),
		}
		if hasComments {
			row = append(row, feedbackSummary(pr))
		}
		tw.AddRow(row...)
	}
	return tw.Flush()
}

func normalizePRState(state string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "", "open":
		return "OPEN", nil
	case "merged":
		return "MERGED", nil
	case "declined", "closed":
		return "DECLINED", nil
	case "superseded":
		return "SUPERSEDED", nil
	default:
		return "", fmt.Errorf("invalid PR state %q: expected open, merged, declined, closed, or superseded", state)
	}
}

func newPRViewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "view [number|url|branch]",
		Short: "View a pull request",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runPRView,
	}
	addPRRepoFlag(cmd)
	cmd.Flags().BoolP("comments", "c", false, "Include comments")
	cmd.Flags().Bool("all", false, "Include resolved comments (only with --comments)")
	cmd.Flags().Bool("json", false, "Output as JSON")
	cmd.Flags().BoolP("raw", "r", false, "Print raw markdown to stdout")
	cmd.Flags().BoolP("web", "w", false, "Open the pull request in the browser")
	return cmd
}

func runPRView(cmd *cobra.Command, args []string) error {
	showComments, _ := cmd.Flags().GetBool("comments")
	includeResolved, _ := cmd.Flags().GetBool("all")
	jsonOutput, _ := cmd.Flags().GetBool("json")
	rawOutput, _ := cmd.Flags().GetBool("raw")
	webOutput, _ := cmd.Flags().GetBool("web")
	ctx, err := prCommandContext(cmd, false)
	if err != nil {
		return err
	}
	pr, err := resolvePR(ctx, optionalArg(args), true)
	if err != nil {
		return err
	}
	if webOutput {
		return openBrowser(pr.Links.HTML.Href)
	}
	if jsonOutput {
		result := PRViewJSON{PullRequest: pr}
		comments, err := ctx.client.ListPullRequestComments(ctx.workspace, ctx.repo, pr.ID)
		if err != nil {
			return fmt.Errorf("failed to fetch comments: %w", err)
		}
		result.Comments = comments
		return output.WriteJSON(os.Stdout, result)
	}

	markdown, err := renderPRMarkdown(ctx, pr, showComments, includeResolved)
	if err != nil {
		return err
	}
	if !rawOutput && term.IsTerminal(int(os.Stdout.Fd())) {
		return pageMarkdown(markdown, fmt.Sprintf("pr-%d.md", pr.ID))
	}
	_, err = os.Stdout.Write(markdown)
	return err
}

func renderPRMarkdown(ctx *prContext, pr *bitbucket.PullRequest, showComments, includeResolved bool) ([]byte, error) {
	var buf bytes.Buffer
	var comments []bitbucket.Comment
	var tasks []bitbucket.Task
	var diff []byte
	commentsLoaded := false
	tasksLoaded := false
	if showComments {
		var err error
		comments, err = ctx.client.ListPullRequestComments(ctx.workspace, ctx.repo, pr.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch comments: %w", err)
		}
		commentsLoaded = true
		tasks, err = ctx.client.ListPullRequestTasks(ctx.workspace, ctx.repo, pr.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch tasks: %w", err)
		}
		tasksLoaded = true
		diff, _ = ctx.client.GetPullRequestDiff(ctx.workspace, ctx.repo, pr.ID)
	}
	mdWriter := output.NewPRMarkdownWriter(&buf)
	knownUsers := append([]bitbucket.User{pr.Author}, pr.Reviewers...)
	for _, participant := range pr.Participants {
		knownUsers = append(knownUsers, participant.User)
	}
	for _, comment := range comments {
		knownUsers = append(knownUsers, comment.User)
	}
	for _, task := range tasks {
		knownUsers = append(knownUsers, task.Comment.User)
	}
	resolveUser := newUserResolver(ctx.client, knownUsers...)
	mdWriter.SetUserResolver(resolveUser)
	mdWriter.SetContext(ctx.workspace, ctx.repo, comments, commentsLoaded, tasks, tasksLoaded)
	if err := mdWriter.WritePR(pr); err != nil {
		return nil, err
	}
	if showComments {
		fmt.Fprintln(&buf)
		commentWriter := output.NewCommentWriter(&buf, pr.Author)
		commentWriter.SetUserResolver(resolveUser)
		if len(diff) > 0 {
			commentWriter.SetDiff(diff)
		}
		if err := commentWriter.WriteComments(comments, includeResolved); err != nil {
			return nil, err
		}
		if len(tasks) > 0 {
			fmt.Fprintln(&buf)
			taskWriter := output.NewTaskWriter(&buf)
			taskWriter.SetUserResolver(resolveUser)
			if err := taskWriter.WriteTasks(tasks); err != nil {
				return nil, err
			}
		}
	}
	return buf.Bytes(), nil
}

func newUserResolver(client *bitbucket.Client, users ...bitbucket.User) func(string) (bitbucket.User, bool) {
	cache := make(map[string]bitbucket.User)
	for _, user := range users {
		if user.AccountID != "" {
			cache[user.AccountID] = user
		}
	}
	return func(accountID string) (bitbucket.User, bool) {
		if user, ok := cache[accountID]; ok {
			return user, true
		}
		user, err := client.GetUser(accountID)
		if err != nil {
			return bitbucket.User{}, false
		}
		cache[accountID] = *user
		return *user, true
	}
}

func newPRDiffCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diff [number|url|branch]",
		Short: "View changes in a pull request",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runPRDiff,
	}
	addPRRepoFlag(cmd)
	cmd.Flags().String("color", "auto", "Use color in diff output: auto, always, never")
	cmd.Flags().Bool("name-only", false, "Display only names of changed files")
	cmd.Flags().Bool("patch", false, "Display diff in patch format")
	cmd.Flags().BoolP("structured", "s", false, "Use difftastic when available")
	return cmd
}

func runPRDiff(cmd *cobra.Command, args []string) error {
	nameOnly, _ := cmd.Flags().GetBool("name-only")
	patch, _ := cmd.Flags().GetBool("patch")
	structured, _ := cmd.Flags().GetBool("structured")
	color, _ := cmd.Flags().GetString("color")
	ctx, err := prCommandContext(cmd, false)
	if err != nil {
		return err
	}
	pr, err := resolvePR(ctx, optionalArg(args), true)
	if err != nil {
		return err
	}
	diff, err := ctx.client.GetPullRequestDiff(ctx.workspace, ctx.repo, pr.ID)
	if err != nil {
		return err
	}
	if nameOnly {
		for _, name := range diffFileNames(diff) {
			fmt.Println(name)
		}
		return nil
	}
	if structured {
		if err := runStructuredDiff(pr, color); err != nil {
			if _, lookErr := exec.LookPath("difft"); lookErr == nil {
				return fmt.Errorf("structured diff failed: %w", err)
			}
		} else {
			return nil
		}
	}
	if patch || !term.IsTerminal(int(os.Stdout.Fd())) {
		_, err := os.Stdout.Write(diff)
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

func diffFileNames(diff []byte) []string {
	seen := make(map[string]bool)
	var names []string
	for _, line := range strings.Split(string(diff), "\n") {
		if !strings.HasPrefix(line, "diff --git ") {
			continue
		}
		parts := strings.Split(line, " ")
		if len(parts) < 4 {
			continue
		}
		name := strings.TrimPrefix(parts[3], "b/")
		if name == "/dev/null" {
			name = strings.TrimPrefix(parts[2], "a/")
		}
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	return names
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

func newPREditCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "edit [number|url|branch]",
		Short: "Edit a pull request",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runPREdit,
	}
	addPRRepoFlag(cmd)
	cmd.Flags().StringP("title", "t", "", "Set the new title")
	cmd.Flags().StringP("body", "b", "", "Set the new body")
	cmd.Flags().StringP("body-file", "F", "", "Read body text from file (use - to read from stdin)")
	cmd.Flags().String("add-reviewer", "", "Add reviewers by identifier")
	cmd.Flags().String("remove-reviewer", "", "Remove reviewers by identifier")
	addAttributionFlag(cmd)
	return cmd
}

func runPREdit(cmd *cobra.Command, args []string) error {
	title, _ := cmd.Flags().GetString("title")
	body, _ := cmd.Flags().GetString("body")
	bodyFile, _ := cmd.Flags().GetString("body-file")
	addReviewers, _ := cmd.Flags().GetString("add-reviewer")
	removeReviewers, _ := cmd.Flags().GetString("remove-reviewer")
	if body != "" && bodyFile != "" {
		return fmt.Errorf("--body and --body-file cannot be used together")
	}
	ctx, err := prCommandContext(cmd, false)
	if err != nil {
		return err
	}
	pr, err := resolvePR(ctx, optionalArg(args), true)
	if err != nil {
		return err
	}

	update := bitbucket.PullRequestUpdate{}
	changed := false
	bodyChanged := cmd.Flags().Changed("body")
	if title != "" && title != pr.Title {
		update.Title = &title
		changed = true
	}
	if bodyFile != "" {
		bodyBytes, err := readBodyFile(bodyFile)
		if err != nil {
			return err
		}
		body = string(bodyBytes)
		bodyChanged = true
	}
	if !bodyChanged && shouldEditBodyInEditor(title, body, bodyFile, addReviewers, removeReviewers) {
		edited, err := editTextInEditor(pr.Description)
		if err != nil {
			return err
		}
		body = edited
		bodyChanged = true
	}
	attribution, err := attributionEnabled(cmd)
	if err != nil {
		return err
	}
	if description, hasDescriptionChange := buildPRDescriptionUpdate(pr.Description, body, bodyChanged, attribution); hasDescriptionChange {
		update.Description = description
		changed = true
	}
	if addReviewers != "" || removeReviewers != "" {
		reviewers, err := editReviewers(ctx.client, pr.Reviewers, splitIdentifiers(addReviewers), splitIdentifiers(removeReviewers))
		if err != nil {
			return err
		}
		update.Reviewers = &reviewers
		changed = true
	}
	if !changed {
		fmt.Println("No changes.")
		return nil
	}
	updated, err := ctx.client.UpdatePullRequest(ctx.workspace, ctx.repo, pr.ID, update)
	if err != nil {
		return err
	}
	fmt.Printf("Updated pull request #%d: %s\n%s\n", updated.ID, updated.Title, updated.Links.HTML.Href)
	return nil
}

func newPRCloseCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "close <number|url|branch>",
		Short: "Close a pull request",
		Args:  cobra.ExactArgs(1),
		RunE:  runPRClose,
	}
	addPRRepoFlag(cmd)
	cmd.Flags().StringP("comment", "c", "", "Leave a closing comment")
	addAttributionFlag(cmd)
	return cmd
}

func runPRClose(cmd *cobra.Command, args []string) error {
	comment, _ := cmd.Flags().GetString("comment")
	attribution, err := attributionEnabled(cmd)
	if err != nil {
		return err
	}
	ctx, err := prCommandContext(cmd, false)
	if err != nil {
		return err
	}
	pr, err := resolvePR(ctx, args[0], false)
	if err != nil {
		return err
	}
	if comment != "" {
		comment = applyAttribution(comment, attribution)
		if _, err := ctx.client.CreatePullRequestCommentText(ctx.workspace, ctx.repo, pr.ID, comment); err != nil {
			return err
		}
	}
	closed, err := ctx.client.DeclinePullRequest(ctx.workspace, ctx.repo, pr.ID)
	if err != nil {
		if comment != "" {
			return fmt.Errorf("posted closing comment but failed to close pull request: %w", err)
		}
		return err
	}
	fmt.Printf("Closed pull request #%d: %s\n", closed.ID, closed.Title)
	return nil
}

func newPRCommentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "comment [number|url|branch]",
		Short: "Comment on a pull request",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runPRComment,
	}
	addPRRepoFlag(cmd)
	addCommentBodyFlags(cmd)
	addAttributionFlag(cmd)
	cmd.Flags().Int("reply-to", 0, "Reply to an existing comment")
	cmd.Flags().String("path", "", "Attach the comment to a file path")
	cmd.Flags().Int("line", 0, "Attach the comment to a line in --path")
	cmd.Flags().String("side", "new", "Line side for inline comments: new or old")
	cmd.Flags().Bool("json", false, "Output as JSON")
	return cmd
}

func runPRComment(cmd *cobra.Command, args []string) error {
	body, _, err := readCommentBodyFromFlags(cmd, true)
	if err != nil {
		return err
	}
	attribution, err := attributionEnabled(cmd)
	if err != nil {
		return err
	}
	body = applyAttribution(body, attribution)
	replyTo, _ := cmd.Flags().GetInt("reply-to")
	path, _ := cmd.Flags().GetString("path")
	line, _ := cmd.Flags().GetInt("line")
	side, _ := cmd.Flags().GetString("side")
	if !cmd.Flags().Changed("side") && line == 0 {
		side = ""
	}
	jsonOutput, _ := cmd.Flags().GetBool("json")

	input, err := buildCommentCreate(commentSpec{
		Body:    body,
		ReplyTo: replyTo,
		Path:    path,
		Line:    line,
		Side:    side,
	})
	if err != nil {
		return err
	}

	ctx, err := prCommandContext(cmd, false)
	if err != nil {
		return err
	}
	pr, err := resolvePR(ctx, optionalArg(args), true)
	if err != nil {
		return err
	}
	comment, err := ctx.client.CreatePullRequestComment(ctx.workspace, ctx.repo, pr.ID, input)
	if err != nil {
		return err
	}
	if jsonOutput {
		return output.WriteJSON(os.Stdout, comment)
	}
	fmt.Printf("Created comment #%d on pull request #%d\n", comment.ID, pr.ID)
	return nil
}

func newPRApproveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "approve [number|url|branch]",
		Short: "Approve a pull request",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runPRApprove,
	}
	addPRRepoFlag(cmd)
	addCommentBodyFlags(cmd)
	addAttributionFlag(cmd)
	cmd.Flags().Bool("json", false, "Output as JSON")
	return cmd
}

func runPRApprove(cmd *cobra.Command, args []string) error {
	return runPRReviewAction(cmd, args, "approve")
}

func newPRUnapproveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unapprove [number|url|branch]",
		Short: "Remove your approval from a pull request",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runPRUnapprove,
	}
	addPRRepoFlag(cmd)
	cmd.Flags().Bool("json", false, "Output as JSON")
	return cmd
}

func runPRUnapprove(cmd *cobra.Command, args []string) error {
	jsonOutput, _ := cmd.Flags().GetBool("json")
	ctx, err := prCommandContext(cmd, false)
	if err != nil {
		return err
	}
	pr, err := resolvePR(ctx, optionalArg(args), true)
	if err != nil {
		return err
	}
	if err := ctx.client.UnapprovePullRequest(ctx.workspace, ctx.repo, pr.ID); err != nil {
		return err
	}
	if jsonOutput {
		return output.WriteJSON(os.Stdout, map[string]any{
			"approved":     false,
			"pull_request": pr.ID,
		})
	}
	fmt.Printf("Removed approval from pull request #%d\n", pr.ID)
	return nil
}

func newPRRequestChangesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "request-changes [number|url|branch]",
		Short: "Request changes on a pull request",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runPRRequestChanges,
	}
	addPRRepoFlag(cmd)
	addCommentBodyFlags(cmd)
	addAttributionFlag(cmd)
	cmd.Flags().Bool("json", false, "Output as JSON")
	return cmd
}

func runPRRequestChanges(cmd *cobra.Command, args []string) error {
	return runPRReviewAction(cmd, args, "request-changes")
}

func newPRClearChangeRequestCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clear-change-request [number|url|branch]",
		Short: "Remove your change request from a pull request",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runPRClearChangeRequest,
	}
	addPRRepoFlag(cmd)
	cmd.Flags().Bool("json", false, "Output as JSON")
	return cmd
}

func runPRClearChangeRequest(cmd *cobra.Command, args []string) error {
	jsonOutput, _ := cmd.Flags().GetBool("json")
	ctx, err := prCommandContext(cmd, false)
	if err != nil {
		return err
	}
	pr, err := resolvePR(ctx, optionalArg(args), true)
	if err != nil {
		return err
	}
	if err := ctx.client.ClearPullRequestChanges(ctx.workspace, ctx.repo, pr.ID); err != nil {
		return err
	}
	if jsonOutput {
		return output.WriteJSON(os.Stdout, map[string]any{
			"changes_requested": false,
			"pull_request":      pr.ID,
		})
	}
	fmt.Printf("Removed change request from pull request #%d\n", pr.ID)
	return nil
}

func newPRReviewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "review [number|url|branch]",
		Short: "Post review comments and finish a pull request review",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runPRReview,
	}
	addPRRepoFlag(cmd)
	addCommentBodyFlags(cmd)
	addAttributionFlag(cmd)
	cmd.Flags().Bool("approve", false, "Finish by approving")
	cmd.Flags().Bool("request-changes", false, "Finish by requesting changes")
	cmd.Flags().Bool("comment", false, "Finish with comments only")
	cmd.Flags().String("comment-spec", "", "JSON file of comments to post before the final review action")
	cmd.Flags().Bool("json", false, "Output as JSON")
	return cmd
}

type prReviewOutput struct {
	PullRequest int                           `json:"pull_request"`
	Action      string                        `json:"action"`
	Comments    []bitbucket.Comment           `json:"comments,omitempty"`
	Result      *bitbucket.ReviewActionResult `json:"result,omitempty"`
}

func runPRReview(cmd *cobra.Command, args []string) error {
	action, err := reviewActionFromFlags(cmd)
	if err != nil {
		return err
	}
	body, hasSummary, err := readCommentBodyFromFlags(cmd, false)
	if err != nil {
		return err
	}
	attribution, err := attributionEnabled(cmd)
	if err != nil {
		return err
	}
	specPath, _ := cmd.Flags().GetString("comment-spec")
	specs, err := readCommentSpecs(specPath)
	if err != nil {
		return err
	}
	if action == "comment" && len(specs) == 0 && !hasSummary {
		return fmt.Errorf("comment-only review requires --body, --body-file, --editor, or --comment-spec")
	}

	ctx, err := prCommandContext(cmd, false)
	if err != nil {
		return err
	}
	pr, err := resolvePR(ctx, optionalArg(args), true)
	if err != nil {
		return err
	}

	comments, err := postCommentSpecs(ctx, pr, specs, attribution)
	if err != nil {
		return err
	}
	if hasSummary {
		body = applyAttribution(body, attribution)
		comment, err := ctx.client.CreatePullRequestCommentText(ctx.workspace, ctx.repo, pr.ID, body)
		if err != nil {
			return fmt.Errorf("posted %d comments but failed to post review summary: %w", len(comments), err)
		}
		comments = append(comments, *comment)
	}

	var result *bitbucket.ReviewActionResult
	switch action {
	case "approve":
		result, err = ctx.client.ApprovePullRequest(ctx.workspace, ctx.repo, pr.ID)
	case "request-changes":
		result, err = ctx.client.RequestPullRequestChanges(ctx.workspace, ctx.repo, pr.ID)
	}
	if err != nil {
		return fmt.Errorf("posted %d comments but failed to %s pull request: %w", len(comments), reviewActionPhrase(action), err)
	}

	jsonOutput, _ := cmd.Flags().GetBool("json")
	if jsonOutput {
		return output.WriteJSON(os.Stdout, prReviewOutput{
			PullRequest: pr.ID,
			Action:      action,
			Comments:    comments,
			Result:      result,
		})
	}
	switch action {
	case "approve":
		fmt.Printf("Posted %d comments and approved pull request #%d\n", len(comments), pr.ID)
	case "request-changes":
		fmt.Printf("Posted %d comments and requested changes on pull request #%d\n", len(comments), pr.ID)
	default:
		fmt.Printf("Posted %d comments on pull request #%d\n", len(comments), pr.ID)
	}
	return nil
}

func runPRReviewAction(cmd *cobra.Command, args []string, action string) error {
	body, hasComment, err := readCommentBodyFromFlags(cmd, false)
	if err != nil {
		return err
	}
	jsonOutput, _ := cmd.Flags().GetBool("json")
	attribution, err := attributionEnabled(cmd)
	if err != nil {
		return err
	}
	ctx, err := prCommandContext(cmd, false)
	if err != nil {
		return err
	}
	pr, err := resolvePR(ctx, optionalArg(args), true)
	if err != nil {
		return err
	}
	if hasComment {
		body = applyAttribution(body, attribution)
		if _, err := ctx.client.CreatePullRequestCommentText(ctx.workspace, ctx.repo, pr.ID, body); err != nil {
			return err
		}
	}

	var result *bitbucket.ReviewActionResult
	switch action {
	case "approve":
		result, err = ctx.client.ApprovePullRequest(ctx.workspace, ctx.repo, pr.ID)
	case "request-changes":
		result, err = ctx.client.RequestPullRequestChanges(ctx.workspace, ctx.repo, pr.ID)
	default:
		return fmt.Errorf("unknown review action %q", action)
	}
	if err != nil {
		if hasComment {
			return fmt.Errorf("posted comment but failed to %s pull request: %w", reviewActionPhrase(action), err)
		}
		return err
	}
	if jsonOutput {
		return output.WriteJSON(os.Stdout, result)
	}
	switch action {
	case "approve":
		fmt.Printf("Approved pull request #%d\n", pr.ID)
	case "request-changes":
		fmt.Printf("Requested changes on pull request #%d\n", pr.ID)
	}
	return nil
}

func newPRCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a pull request",
		RunE:  runPRCreate,
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
	cmd.Flags().BoolP("web", "w", false, "Open the browser to create a pull request")
	addAttributionFlag(cmd)
	return cmd
}

func runPRCreate(cmd *cobra.Command, args []string) error {
	base, _ := cmd.Flags().GetString("base")
	head, _ := cmd.Flags().GetString("head")
	title, _ := cmd.Flags().GetString("title")
	body, _ := cmd.Flags().GetString("body")
	bodyFile, _ := cmd.Flags().GetString("body-file")
	editor, _ := cmd.Flags().GetBool("editor")
	fill, _ := cmd.Flags().GetBool("fill")
	reviewerFlag, _ := cmd.Flags().GetString("reviewer")
	push, _ := cmd.Flags().GetBool("push")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	webCreate, _ := cmd.Flags().GetBool("web")
	attribution, err := attributionEnabled(cmd)
	if err != nil {
		return err
	}
	if body != "" && bodyFile != "" {
		return fmt.Errorf("--body and --body-file cannot be used together")
	}
	if editor && (body != "" || bodyFile != "") {
		return fmt.Errorf("--editor cannot be used with --body or --body-file")
	}
	ctx, err := prCommandContext(cmd, false)
	if err != nil {
		return err
	}
	repo, err := ctx.client.GetRepository(ctx.workspace, ctx.repo)
	if err != nil {
		return err
	}
	if base == "" {
		base = repo.MainBranch.Name
	}
	if base == "" {
		base = "main"
	}
	if head == "" {
		head, err = git.CurrentBranch()
		if err != nil {
			return err
		}
	}
	if bodyFile != "" {
		bodyBytes, err := readBodyFile(bodyFile)
		if err != nil {
			return err
		}
		body = string(bodyBytes)
	}
	if fill {
		filledTitle, filledBody, err := fillPRText(base, head)
		if err != nil {
			return err
		}
		if title == "" {
			title = filledTitle
		}
		if body == "" && bodyFile == "" {
			body = filledBody
		}
	}
	if editor {
		edited, err := editTextInEditor(body)
		if err != nil {
			return err
		}
		body = edited
	}
	if webCreate {
		target := fmt.Sprintf("https://bitbucket.org/%s/%s/pull-requests/new?source=%s&dest=%s", ctx.workspace, ctx.repo, url.QueryEscape(head), url.QueryEscape(base))
		return openBrowser(target)
	}
	if title == "" {
		return fmt.Errorf("--title or --fill is required")
	}
	if !git.RemoteBranchExists("origin", head) {
		if !push {
			return fmt.Errorf("head branch %q is not pushed to origin; rerun with --push", head)
		}
		if !dryRun {
			currentBranch, err := git.CurrentBranch()
			if err != nil {
				return err
			}
			if currentBranch == head {
				err = git.PushCurrentBranch("origin", head)
			} else {
				err = git.PushBranch("origin", head)
			}
			if err != nil {
				return err
			}
		}
	}
	reviewers, err := resolveReviewers(ctx.client, splitIdentifiers(reviewerFlag))
	if err != nil {
		return err
	}
	input := buildPRCreateInput(title, body, head, base, reviewers, attribution)
	if dryRun {
		return json.NewEncoder(os.Stdout).Encode(input)
	}
	pr, err := ctx.client.CreatePullRequest(ctx.workspace, ctx.repo, input)
	if err != nil {
		return err
	}
	fmt.Println(pr.Links.HTML.Href)
	return nil
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

func newPRStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show status of relevant pull requests",
		RunE:  runPRStatus,
	}
	addPRRepoFlag(cmd)
	cmd.Flags().Bool("json", false, "Output as JSON")
	return cmd
}

type prStatus struct {
	CurrentBranch   *bitbucket.PullRequest  `json:"currentBranch,omitempty"`
	CreatedByYou    []bitbucket.PullRequest `json:"createdByYou"`
	ReviewRequested []bitbucket.PullRequest `json:"reviewRequested"`
}

func runPRStatus(cmd *cobra.Command, args []string) error {
	jsonOutput, _ := cmd.Flags().GetBool("json")
	ctx, err := prCommandContext(cmd, false)
	if err != nil {
		return err
	}
	currentUser, err := ctx.client.GetCurrentUser()
	if err != nil {
		return err
	}
	opts := &bitbucket.PRListOptions{State: "OPEN"}
	prs, err := ctx.client.ListPullRequests(ctx.workspace, ctx.repo, opts)
	if err != nil {
		return err
	}
	status := prStatus{}
	branch, _ := git.CurrentBranch()
	for _, pr := range prs {
		if branch != "" && pr.Source.Branch.Name == branch {
			prCopy := pr
			status.CurrentBranch = &prCopy
		}
		if pr.Author.SharesStableIdentity(*currentUser) {
			status.CreatedByYou = append(status.CreatedByYou, pr)
		}
		if pullRequestHasReviewer(pr, *currentUser) {
			status.ReviewRequested = append(status.ReviewRequested, pr)
		}
	}
	if jsonOutput {
		return output.WriteJSON(os.Stdout, status)
	}
	fmt.Println("Current branch")
	if status.CurrentBranch == nil {
		fmt.Println("  " + styleMuted("No pull request"))
	} else {
		printStatusPR(*status.CurrentBranch)
	}
	fmt.Println()
	fmt.Println("Created by you")
	printStatusPRs(status.CreatedByYou)
	fmt.Println()
	fmt.Println("Requesting your review")
	printStatusPRs(status.ReviewRequested)
	return nil
}

func printStatusPRs(prs []bitbucket.PullRequest) {
	if len(prs) == 0 {
		fmt.Println("  " + styleMuted("None"))
		return
	}
	for _, pr := range prs {
		printStatusPR(pr)
	}
}

func printStatusPR(pr bitbucket.PullRequest) {
	feedback := feedbackSummary(pr)
	if feedback != "" {
		feedback = " " + styleMuted(feedback)
	}
	fmt.Printf("  %s %s %s %s%s\n", stylePRID(pr.ID), pr.Title, styleMuted("["+pr.Source.Branch.Name+"]"), reviewerSummary(pr), feedback)
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
	ctx, err := prCommandContext(cmd, false)
	if err != nil {
		return err
	}
	pr, err := resolvePR(ctx, args[0], false)
	if err != nil {
		return err
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

type commentSpec struct {
	Body    string `json:"body"`
	ReplyTo int    `json:"reply_to,omitempty"`
	Path    string `json:"path,omitempty"`
	Line    int    `json:"line,omitempty"`
	Side    string `json:"side,omitempty"`
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

func buildCommentCreate(spec commentSpec) (bitbucket.CommentCreate, error) {
	body := spec.Body
	if strings.TrimSpace(body) == "" {
		return bitbucket.CommentCreate{}, fmt.Errorf("comment body cannot be empty")
	}
	if spec.ReplyTo < 0 {
		return bitbucket.CommentCreate{}, fmt.Errorf("--reply-to must be greater than zero")
	}
	if spec.Line < 0 {
		return bitbucket.CommentCreate{}, fmt.Errorf("--line must be greater than zero")
	}

	path := strings.TrimSpace(spec.Path)
	side := strings.ToLower(strings.TrimSpace(spec.Side))
	if spec.ReplyTo > 0 {
		if path != "" || spec.Line != 0 || side != "" {
			return bitbucket.CommentCreate{}, fmt.Errorf("--reply-to cannot be combined with --path, --line, or --side")
		}
		return bitbucket.CommentCreate{
			Content: bitbucket.ContentInput{Raw: body},
			Parent:  &bitbucket.ParentInput{ID: spec.ReplyTo},
		}, nil
	}
	if spec.Line > 0 && path == "" {
		return bitbucket.CommentCreate{}, fmt.Errorf("--line requires --path")
	}
	if side != "" && path == "" {
		return bitbucket.CommentCreate{}, fmt.Errorf("--side requires --path")
	}
	if side != "" && spec.Line == 0 {
		return bitbucket.CommentCreate{}, fmt.Errorf("--side requires --line")
	}

	input := bitbucket.CommentCreate{Content: bitbucket.ContentInput{Raw: body}}
	if path == "" {
		return input, nil
	}

	inline := bitbucket.InlineInput{Path: path}
	if spec.Line > 0 {
		switch side {
		case "", "new":
			inline.To = &spec.Line
		case "old":
			inline.From = &spec.Line
		default:
			return bitbucket.CommentCreate{}, fmt.Errorf("invalid --side %q: expected new or old", spec.Side)
		}
	} else if side != "" && side != "new" && side != "old" {
		return bitbucket.CommentCreate{}, fmt.Errorf("invalid --side %q: expected new or old", spec.Side)
	}
	input.Inline = &inline
	return input, nil
}

func readCommentSpecs(path string) ([]commentSpec, error) {
	if path == "" {
		return nil, nil
	}
	data, err := readBodyFile(path)
	if err != nil {
		return nil, err
	}
	var specs []commentSpec
	if err := json.Unmarshal(data, &specs); err != nil {
		return nil, fmt.Errorf("failed to parse comment spec: %w", err)
	}
	return specs, nil
}

func buildCommentCreates(specs []commentSpec, attribution bool) ([]bitbucket.CommentCreate, error) {
	inputs := make([]bitbucket.CommentCreate, 0, len(specs))
	for index, spec := range specs {
		spec.Body = applyAttribution(spec.Body, attribution)
		input, err := buildCommentCreate(spec)
		if err != nil {
			return nil, fmt.Errorf("invalid comment spec %d: %w", index+1, err)
		}
		inputs = append(inputs, input)
	}
	return inputs, nil
}

func postCommentSpecs(ctx *prContext, pr *bitbucket.PullRequest, specs []commentSpec, attribution bool) ([]bitbucket.Comment, error) {
	inputs, err := buildCommentCreates(specs, attribution)
	if err != nil {
		return nil, err
	}
	comments := make([]bitbucket.Comment, 0, len(inputs))
	for index, input := range inputs {
		comment, err := ctx.client.CreatePullRequestComment(ctx.workspace, ctx.repo, pr.ID, input)
		if err != nil {
			return nil, fmt.Errorf("posted %d comments but failed to post comment %d: %w", len(comments), index+1, err)
		}
		comments = append(comments, *comment)
	}
	return comments, nil
}

func reviewActionFromFlags(cmd *cobra.Command) (string, error) {
	approve, _ := cmd.Flags().GetBool("approve")
	requestChanges, _ := cmd.Flags().GetBool("request-changes")
	commentOnly, _ := cmd.Flags().GetBool("comment")

	actions := []string{}
	if approve {
		actions = append(actions, "approve")
	}
	if requestChanges {
		actions = append(actions, "request-changes")
	}
	if commentOnly {
		actions = append(actions, "comment")
	}
	if len(actions) != 1 {
		return "", fmt.Errorf("exactly one of --approve, --request-changes, or --comment is required")
	}
	return actions[0], nil
}

func reviewActionPhrase(action string) string {
	switch action {
	case "approve":
		return "approve"
	case "request-changes":
		return "request changes on"
	default:
		return action
	}
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

type markdownLine struct {
	text    string
	newline string
}

type markdownFence struct {
	marker byte
	length int
}

const bitbucketInlineCardSuffix = "{: data-inline-card='' }"

const atlasAttributionMarker = "<!-- atlas:attribution -->"

const atlasAttributionFooter = atlasAttributionMarker + "\n---\nSubmitted via Atlas CLI."

func addAttributionFlag(cmd *cobra.Command) {
	cmd.Flags().Bool("no-attribution", false, "Do not append the Atlas attribution footer")
}

func attributionEnabled(cmd *cobra.Command) (bool, error) {
	withoutAttribution, _ := cmd.Flags().GetBool("no-attribution")
	if withoutAttribution {
		return false, nil
	}
	cfg, err := config.Load()
	if err != nil {
		return false, err
	}
	return cfg.Attribution, nil
}

func applyAttribution(body string, enabled bool) string {
	if !enabled || strings.Contains(body, atlasAttributionMarker) {
		return body
	}

	trimmed := strings.TrimRight(body, " \t\r\n")
	if trimmed == "" {
		return atlasAttributionFooter
	}
	return trimmed + "\n\n" + atlasAttributionFooter
}

func buildPRCreateInput(title, body, head, base string, reviewers []bitbucket.User, attribution bool) bitbucket.PullRequestCreate {
	return bitbucket.PullRequestCreate{
		Title:       title,
		Description: applyAttribution(normalizePRDescriptionMarkdown(body), attribution),
		Source:      bitbucket.PullRequestRefInput{Branch: bitbucket.Branch{Name: head}},
		Destination: bitbucket.PullRequestRefInput{Branch: bitbucket.Branch{Name: base}},
		Reviewers:   reviewers,
	}
}

func buildPRDescriptionUpdate(current, body string, bodyChanged bool, attribution bool) (*string, bool) {
	if !bodyChanged {
		return nil, false
	}
	normalized := applyAttribution(normalizePRDescriptionMarkdown(body), attribution)
	if normalized == current {
		return nil, false
	}
	return &normalized, true
}

func normalizePRDescriptionMarkdown(body string) string {
	lines := splitMarkdownLines(body)
	if len(lines) == 0 {
		return body
	}

	var normalized strings.Builder
	normalized.Grow(len(body))
	inFence := false
	openFence := markdownFence{}
	changed := false
	for i, line := range lines {
		trimmedLine := strings.TrimSpace(line.text)
		lineInFence := inFence
		currentFence, hasCurrentFence := parseMarkdownFence(trimmedLine)
		if hasCurrentFence {
			if inFence && currentFence.closes(openFence) {
				inFence = false
			} else if !inFence {
				inFence = true
				openFence = currentFence
			}
		}

		lineText := line.text
		if !lineInFence && !hasCurrentFence {
			var normalizedInlineCard bool
			lineText, normalizedInlineCard = normalizeInlineCardLine(lineText)
			changed = changed || normalizedInlineCard
		}

		normalized.WriteString(lineText)
		normalized.WriteString(line.newline)
		if line.newline == "" || lineInFence || hasCurrentFence || i+1 >= len(lines) {
			continue
		}
		if isPRSectionLabel(trimmedLine) && isMarkdownListItem(lines[i+1].text) {
			normalized.WriteString(line.newline)
			changed = true
		}
	}
	if !changed {
		return body
	}
	return normalized.String()
}

func normalizeInlineCardLine(line string) (string, bool) {
	if strings.Contains(line, bitbucketInlineCardSuffix) {
		return line, false
	}

	if normalized, ok := normalizePlainInlineCardLine(line); ok {
		return normalized, true
	}

	prefix, content, ok := splitMarkdownListContent(line)
	if !ok {
		return line, false
	}
	normalized, ok := normalizePlainInlineCardLine(content)
	if !ok {
		return line, false
	}
	return prefix + normalized, true
}

func normalizePlainInlineCardLine(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if !isInlineCardURL(trimmed) {
		return line, false
	}
	leadingLength := len(line) - len(strings.TrimLeft(line, " \t"))
	trailingLength := len(line) - len(strings.TrimRight(line, " \t"))
	return line[:leadingLength] + formatInlineCardLink(trimmed) + line[len(line)-trailingLength:], true
}

func isInlineCardURL(rawURL string) bool {
	parsedURL, err := url.Parse(rawURL)
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		return false
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return false
	}

	hostname := strings.ToLower(parsedURL.Hostname())
	return hostname == "bitbucket.org" || strings.HasSuffix(hostname, ".atlassian.net")
}

func formatInlineCardLink(rawURL string) string {
	return "[" + rawURL + "](" + rawURL + ")" + bitbucketInlineCardSuffix
}

func splitMarkdownLines(text string) []markdownLine {
	if text == "" {
		return nil
	}
	lines := make([]markdownLine, 0, strings.Count(text, "\n")+1)
	start := 0
	for start < len(text) {
		newlineIndex := strings.IndexByte(text[start:], '\n')
		if newlineIndex == -1 {
			lines = append(lines, markdownLine{text: text[start:]})
			return lines
		}
		end := start + newlineIndex
		lineText := text[start:end]
		newline := "\n"
		if strings.HasSuffix(lineText, "\r") {
			lineText = strings.TrimSuffix(lineText, "\r")
			newline = "\r\n"
		}
		lines = append(lines, markdownLine{text: lineText, newline: newline})
		start = end + 1
	}
	return lines
}

func isPRSectionLabel(line string) bool {
	return line != "" && strings.HasSuffix(line, ":")
}

func splitMarkdownListContent(line string) (string, string, bool) {
	leadingLength := len(line) - len(strings.TrimLeft(line, " \t"))
	remaining := line[leadingLength:]
	if len(remaining) < 3 {
		return "", "", false
	}

	switch remaining[0] {
	case '-', '*', '+':
		if !isMarkdownWhitespace(remaining[1]) {
			return "", "", false
		}
		contentStart := consumeMarkdownWhitespace(remaining, 1)
		return line[:leadingLength] + remaining[:contentStart], remaining[contentStart:], true
	}

	digitEnd := 0
	for digitEnd < len(remaining) && remaining[digitEnd] >= '0' && remaining[digitEnd] <= '9' {
		digitEnd++
	}
	if digitEnd == 0 || digitEnd+1 >= len(remaining) {
		return "", "", false
	}
	if remaining[digitEnd] != '.' && remaining[digitEnd] != ')' {
		return "", "", false
	}
	if !isMarkdownWhitespace(remaining[digitEnd+1]) {
		return "", "", false
	}
	contentStart := consumeMarkdownWhitespace(remaining, digitEnd+1)
	return line[:leadingLength] + remaining[:contentStart], remaining[contentStart:], true
}

func isMarkdownListItem(line string) bool {
	trimmedLine := strings.TrimLeft(line, " \t")
	if len(trimmedLine) < 3 {
		return false
	}
	switch trimmedLine[0] {
	case '-', '*', '+':
		return isMarkdownWhitespace(trimmedLine[1])
	}

	digitEnd := 0
	for digitEnd < len(trimmedLine) && trimmedLine[digitEnd] >= '0' && trimmedLine[digitEnd] <= '9' {
		digitEnd++
	}
	if digitEnd == 0 || digitEnd+1 >= len(trimmedLine) {
		return false
	}
	if trimmedLine[digitEnd] != '.' && trimmedLine[digitEnd] != ')' {
		return false
	}
	return isMarkdownWhitespace(trimmedLine[digitEnd+1])
}

func isMarkdownWhitespace(char byte) bool {
	return char == ' ' || char == '\t'
}

func consumeMarkdownWhitespace(text string, start int) int {
	for start < len(text) && isMarkdownWhitespace(text[start]) {
		start++
	}
	return start
}

func parseMarkdownFence(line string) (markdownFence, bool) {
	if len(line) < 3 {
		return markdownFence{}, false
	}
	marker := line[0]
	if marker != '`' && marker != '~' {
		return markdownFence{}, false
	}
	length := 0
	for length < len(line) && line[length] == marker {
		length++
	}
	if length < 3 {
		return markdownFence{}, false
	}
	return markdownFence{marker: marker, length: length}, true
}

func (f markdownFence) closes(open markdownFence) bool {
	return f.marker == open.marker && f.length >= open.length
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

func resolveReviewers(client *bitbucket.Client, ids []string) ([]bitbucket.User, error) {
	reviewers := make([]bitbucket.User, 0, len(ids))
	for _, id := range ids {
		user, err := client.GetUser(strings.TrimPrefix(id, "@"))
		if err != nil {
			return nil, fmt.Errorf("failed to resolve reviewer %q: %w", id, err)
		}
		reviewers = append(reviewers, *user)
	}
	return reviewers, nil
}

func editReviewers(client *bitbucket.Client, current []bitbucket.User, addIDs, removeIDs []string) ([]bitbucket.User, error) {
	reviewers := make([]bitbucket.User, 0, len(current)+len(addIDs))
	for _, reviewer := range current {
		remove := false
		for _, id := range removeIDs {
			if reviewer.MatchesStableIdentifier(id) {
				remove = true
				break
			}
		}
		if !remove {
			reviewers = append(reviewers, reviewer)
		}
	}
	added, err := resolveReviewers(client, addIDs)
	if err != nil {
		return nil, err
	}
	for _, reviewer := range added {
		exists := false
		for _, existing := range reviewers {
			if existing.SharesStableIdentity(reviewer) {
				exists = true
				break
			}
		}
		if !exists {
			reviewers = append(reviewers, reviewer)
		}
	}
	return reviewers, nil
}

func pullRequestHasReviewer(pr bitbucket.PullRequest, user bitbucket.User) bool {
	for _, reviewer := range pr.Reviewers {
		if reviewer.SharesStableIdentity(user) {
			return true
		}
	}
	for _, participant := range pr.Participants {
		if participant.Role == "REVIEWER" && participant.User.SharesStableIdentity(user) {
			return true
		}
	}
	return false
}

func reviewerSummary(pr bitbucket.PullRequest) string {
	approved := 0
	changes := 0
	pending := 0
	for _, reviewer := range pr.Reviewers {
		pending++
		for _, participant := range pr.Participants {
			if participant.Role != "REVIEWER" || !participant.User.SharesStableIdentity(reviewer) {
				continue
			}
			pending--
			if participant.Approved {
				approved++
			} else if participant.State == "changes_requested" {
				changes++
			} else {
				pending++
			}
			break
		}
	}
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

func feedbackSummary(pr bitbucket.PullRequest) string {
	parts := []string{}
	if pr.CommentCount > 0 {
		parts = append(parts, fmt.Sprintf("%d comments", pr.CommentCount))
	}
	if pr.TaskCount > 0 {
		parts = append(parts, fmt.Sprintf("%d tasks", pr.TaskCount))
	}
	if len(parts) == 0 {
		return ""
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
