package cli

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/config"
	snippetops "github.com/kabilan108/atlas/internal/ops/snippet"
	"github.com/kabilan108/atlas/internal/output"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

type snippetRef = snippetops.Ref

func newSnippetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "snippet",
		Short: "Work with snippets",
		Long: `Work with Bitbucket snippets.

A snippet can be supplied as an ID or a Bitbucket snippet URL.`,
	}

	cmd.AddCommand(newSnippetCloneCmd())
	cmd.AddCommand(newSnippetCreateCmd())
	cmd.AddCommand(newSnippetDeleteCmd())
	cmd.AddCommand(newSnippetEditCmd())
	cmd.AddCommand(newSnippetListCmd())
	cmd.AddCommand(newSnippetRenameCmd())
	cmd.AddCommand(newSnippetViewCmd())

	return cmd
}

func newSnippetCloneCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clone <snippet> [<directory>] [-- <gitflags>...]",
		Short: "Clone a snippet locally",
		Args:  cobra.MinimumNArgs(1),
		RunE:  runSnippetClone,
	}

	cmd.Flags().String("workspace", "", "Target workspace")
	cmd.Flags().String("protocol", "ssh", "Clone protocol: ssh or https")

	return cmd
}

func runSnippetClone(cmd *cobra.Command, args []string) error {
	dashIndex := cmd.ArgsLenAtDash()
	snippetArgs := args
	var gitFlags []string
	if dashIndex >= 0 {
		snippetArgs = args[:dashIndex]
		gitFlags = args[dashIndex:]
	}

	if len(snippetArgs) == 0 {
		return fmt.Errorf("snippet ID or URL is required")
	}
	if len(snippetArgs) > 2 {
		return fmt.Errorf("accepts at most 2 arg(s), received %d", len(snippetArgs))
	}

	ref, err := resolveExplicitSnippetRef(cmd, snippetArgs[0])
	if err != nil {
		return err
	}

	protocol, _ := cmd.Flags().GetString("protocol")
	cloneURL, err := snippetCloneURL(ref, protocol)
	if err != nil {
		return err
	}

	gitArgs := append([]string{"clone"}, gitFlags...)
	gitArgs = append(gitArgs, cloneURL)
	if len(snippetArgs) == 2 {
		gitArgs = append(gitArgs, snippetArgs[1])
	}

	gitCmd := exec.Command("git", gitArgs...)
	gitCmd.Stdin = os.Stdin
	gitCmd.Stdout = os.Stdout
	gitCmd.Stderr = os.Stderr
	return gitCmd.Run()
}

func newSnippetListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List your snippets",
	}

	cmd.Flags().String("workspace", "", "Target workspace")
	cmd.Flags().IntP("limit", "L", 10, "Maximum number of snippets to fetch (max 100)")
	cmd.Flags().String("role", "", "Filter by role: owner, contributor, member")
	cmd.Flags().Bool("public", false, "Show only public snippets")
	cmd.Flags().Bool("private", false, "Show only private snippets")

	return bind(cmd, binding[snippetops.ListInput, snippetops.ListResult]{
		op:    snippetops.List,
		input: snippetListInput,
		human: writeSnippetTable,
	})
}

func snippetListInput(cmd *cobra.Command, _ []string, _ *cliEnv) (snippetops.ListInput, error) {
	workspace, err := configuredWorkspace(cmd)
	if err != nil {
		return snippetops.ListInput{}, err
	}

	limit, _ := cmd.Flags().GetInt("limit")
	role, _ := cmd.Flags().GetString("role")
	publicOnly, _ := cmd.Flags().GetBool("public")
	privateOnly, _ := cmd.Flags().GetBool("private")

	if publicOnly && privateOnly {
		return snippetops.ListInput{}, fmt.Errorf("--public and --private cannot be used together")
	}
	if role != "" && role != "owner" && role != "contributor" && role != "member" {
		return snippetops.ListInput{}, fmt.Errorf("invalid role %q: expected owner, contributor, or member", role)
	}
	if limit < 1 {
		return snippetops.ListInput{}, fmt.Errorf("--limit must be greater than 0")
	}

	in := snippetops.ListInput{Workspace: workspace, Role: snippetops.Role(role), Limit: limit}
	switch {
	case publicOnly:
		in.Visibility = "public"
	case privateOnly:
		in.Visibility = "private"
	}
	return in, nil
}

func writeSnippetTable(w io.Writer, result snippetops.ListResult) error {
	if len(result.Snippets) == 0 {
		_, err := fmt.Fprint(w, "No snippets found.\n"+result.Note())
		return err
	}

	tw := output.NewTableWriter(w, "ID", "Title", "Files", "Visibility", "Updated")
	for _, s := range result.Snippets {
		tw.AddRow(
			s.ID,
			output.Truncate(s.Title, 40),
			fmt.Sprintf("%d", len(s.Files)),
			snippetops.VisibilityOf(s),
			output.FormatRelativeTime(s.UpdatedOn),
		)
	}

	if err := tw.Flush(); err != nil {
		return err
	}
	_, err := io.WriteString(w, result.Note())
	return err
}

func newSnippetViewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "view [<snippet>]",
		Short: "View a snippet",
		Args:  cobra.MaximumNArgs(1),
	}

	cmd.Flags().String("workspace", "", "Target workspace")
	cmd.Flags().StringP("filename", "f", "", "Display a single file from the snippet")
	cmd.Flags().Bool("files", false, "List file names from the snippet")
	cmd.Flags().BoolP("raw", "r", false, "Print raw instead of paged snippet contents")
	cmd.Flags().BoolP("web", "w", false, "Open snippet in the browser")

	return bind(cmd, binding[snippetops.ViewInput, *snippetops.ViewResult]{
		op:    snippetops.View,
		input: snippetViewInput,
		intercept: func(cmd *cobra.Command, env *cliEnv, in snippetops.ViewInput) (bool, error) {
			if web, _ := cmd.Flags().GetBool("web"); !web {
				return false, nil
			}
			client, err := env.Bitbucket(cmd.Context())
			if err != nil {
				return true, err
			}
			snippet, err := client.GetSnippet(cmd.Context(), in.Workspace, in.Snippet)
			if err != nil {
				return true, err
			}
			return true, openBrowser(snippet.Links.HTML.Href)
		},
		human: func(w io.Writer, result *snippetops.ViewResult) error {
			if filesOnly, _ := cmd.Flags().GetBool("files"); filesOnly {
				for _, filename := range snippetops.Filenames(result.Snippet) {
					fmt.Fprintln(w, filename)
				}
				return nil
			}
			rawOutput, _ := cmd.Flags().GetBool("raw")
			if err := renderSnippetFiles(w, result.FileContents, rawOutput); err != nil {
				return err
			}
			for _, name := range result.TruncatedFiles {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s truncated at %d bytes\n", name, result.FileBytes)
			}
			return nil
		},
	})
}

func snippetViewInput(cmd *cobra.Command, args []string, env *cliEnv) (snippetops.ViewInput, error) {
	ref, err := snippetArgTarget(cmd, env, args)
	if err != nil {
		return snippetops.ViewInput{}, err
	}
	filename, _ := cmd.Flags().GetString("filename")
	filesOnly, _ := cmd.Flags().GetBool("files")
	return snippetops.ViewInput{Target: ref, Filename: filename, FilesOnly: filesOnly}, nil
}

// snippetArgTarget names the snippet in args, or asks the user to pick one
// of theirs on a terminal.
func snippetArgTarget(cmd *cobra.Command, env *cliEnv, args []string) (snippetops.Target, error) {
	workspace, err := optionalWorkspace(cmd)
	if err != nil {
		return snippetops.Target{}, err
	}
	ref, err := resolveSnippetArg(cmd.Context(), env, workspace, args)
	if err != nil {
		return snippetops.Target{}, err
	}
	return snippetTarget(ref), nil
}

func snippetTarget(ref snippetRef) snippetops.Target {
	return snippetops.Target{Snippet: ref.ID, Workspace: ref.Workspace}
}

func newSnippetCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "create [<filename>... | -]",
		Aliases: []string{"new"},
		Short:   "Create a new snippet",
	}

	cmd.Flags().String("workspace", "", "Target workspace")
	cmd.Flags().String("title", "", "Snippet title")
	cmd.Flags().StringP("filename", "f", "", "Filename to use when reading from standard input")
	cmd.Flags().BoolP("public", "p", false, "List the snippet publicly")
	_ = cmd.MarkFlagRequired("title")

	return bind(cmd, binding[snippetops.CreateInput, *bitbucket.Snippet]{
		op:    snippetops.Create,
		input: snippetCreateInput,
	})
}

func snippetCreateInput(cmd *cobra.Command, args []string, _ *cliEnv) (snippetops.CreateInput, error) {
	workspace, err := configuredWorkspace(cmd)
	if err != nil {
		return snippetops.CreateInput{}, err
	}

	title, _ := cmd.Flags().GetString("title")
	stdinFilename, _ := cmd.Flags().GetString("filename")
	publicSnippet, _ := cmd.Flags().GetBool("public")

	files, err := readSnippetCreateFiles(args, stdinFilename)
	if err != nil {
		return snippetops.CreateInput{}, err
	}
	return snippetops.CreateInput{Workspace: workspace, Title: title, Files: fileStrings(files), Public: publicSnippet}, nil
}

func newSnippetEditCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "edit <snippet> [<filename>]",
		Short: "Edit a snippet",
		Args:  cobra.RangeArgs(1, 2),
	}

	cmd.Flags().String("workspace", "", "Target workspace")
	cmd.Flags().String("title", "", "New title for the snippet")
	cmd.Flags().StringP("add", "a", "", "Add a new file to the snippet")
	cmd.Flags().StringP("filename", "f", "", "Select a file to edit")
	cmd.Flags().StringP("remove", "r", "", "Remove a file from the snippet")

	return bind(cmd, binding[snippetops.EditInput, snippetops.Receipt]{
		op:    snippetops.Edit,
		input: snippetEditInput,
	})
}

// snippetEditInput reads --add from disk, or with no other change opens the
// selected file in $VISUAL/$EDITOR, and turns the result into file contents.
func snippetEditInput(cmd *cobra.Command, args []string, env *cliEnv) (snippetops.EditInput, error) {
	ref, err := resolveExplicitSnippetRef(cmd, args[0])
	if err != nil {
		return snippetops.EditInput{}, err
	}

	title, _ := cmd.Flags().GetString("title")
	addFile, _ := cmd.Flags().GetString("add")
	filenameFlag, _ := cmd.Flags().GetString("filename")
	removeFile, _ := cmd.Flags().GetString("remove")

	filename := filenameFlag
	if filename == "" && len(args) == 2 {
		filename = args[1]
	}
	if filename != "" && title != "" && addFile == "" && removeFile == "" {
		return snippetops.EditInput{}, fmt.Errorf("filename cannot be used with --title unless editing file contents or adding a file")
	}

	addFiles := make(map[string][]byte)
	var removeFiles []string

	if addFile != "" {
		content, err := os.ReadFile(addFile)
		if err != nil {
			return snippetops.EditInput{}, fmt.Errorf("failed to read file %s: %w", addFile, err)
		}
		targetFilename := filename
		if targetFilename == "" {
			targetFilename = snippetLocalFilename(addFile)
		}
		if err := addSnippetFileContent(addFiles, targetFilename, content, addFile); err != nil {
			return snippetops.EditInput{}, err
		}
	}

	if removeFile != "" {
		removeFiles = append(removeFiles, removeFile)
	}

	if title == "" && addFile == "" && removeFile == "" {
		client, err := env.Bitbucket(cmd.Context())
		if err != nil {
			return snippetops.EditInput{}, err
		}
		editedName, editedContent, err := editSnippetFile(cmd.Context(), client, ref, filename)
		if err != nil {
			return snippetops.EditInput{}, err
		}
		addFiles[editedName] = editedContent
	}

	return snippetops.EditInput{Target: snippetTarget(ref), Title: title, Files: fileStrings(addFiles), Remove: removeFiles}, nil
}

func newSnippetRenameCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rename <snippet> <old-filename> <new-filename>",
		Short: "Rename a file in a snippet",
		Args:  cobra.ExactArgs(3),
	}

	cmd.Flags().String("workspace", "", "Target workspace")

	return bind(cmd, binding[snippetops.RenameInput, snippetops.Receipt]{
		op: snippetops.Rename,
		input: func(cmd *cobra.Command, args []string, _ *cliEnv) (snippetops.RenameInput, error) {
			ref, err := resolveExplicitSnippetRef(cmd, args[0])
			if err != nil {
				return snippetops.RenameInput{}, err
			}
			return snippetops.RenameInput{Target: snippetTarget(ref), From: args[1], To: args[2]}, nil
		},
	})
}

func newSnippetDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete [<snippet>]",
		Short: "Delete a snippet",
		Args:  cobra.MaximumNArgs(1),
	}

	cmd.Flags().String("workspace", "", "Target workspace")
	cmd.Flags().Bool("yes", false, "Confirm deletion without prompting")

	return bind(cmd, binding[snippetops.DeleteInput, snippetops.Receipt]{
		op: snippetops.Delete,
		input: func(cmd *cobra.Command, args []string, env *cliEnv) (snippetops.DeleteInput, error) {
			target, err := snippetArgTarget(cmd, env, args)
			return snippetops.DeleteInput{Target: target}, err
		},
		intercept: confirmSnippetDeleteFlag,
	})
}

// confirmSnippetDeleteFlag asks before deleting unless --yes is set, and
// handles the command itself when the user declines.
func confirmSnippetDeleteFlag(cmd *cobra.Command, _ *cliEnv, in snippetops.DeleteInput) (bool, error) {
	if yes, _ := cmd.Flags().GetBool("yes"); yes {
		return false, nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return true, fmt.Errorf("refusing to delete non-interactively without --yes")
	}
	confirmed, err := confirmSnippetDelete(in.Snippet)
	if err != nil {
		return true, err
	}
	if !confirmed {
		_, err := fmt.Fprintln(cmd.OutOrStdout(), "Deletion cancelled.")
		return true, err
	}
	return false, nil
}

func fileStrings(files map[string][]byte) map[string]string {
	out := make(map[string]string, len(files))
	for name, content := range files {
		out[name] = string(content)
	}
	return out
}

func configuredWorkspace(cmd *cobra.Command) (string, error) {
	workspace, err := optionalWorkspace(cmd)
	if err != nil {
		return "", err
	}
	if workspace == "" {
		return "", errSnippetWorkspace
	}
	return workspace, nil
}

func optionalWorkspace(cmd *cobra.Command) (string, error) {
	workspaceFlag, _ := cmd.Flags().GetString("workspace")
	if workspaceFlag != "" {
		return workspaceFlag, nil
	}

	cfg, err := config.Load()
	if err != nil {
		return "", fmt.Errorf("failed to load config: %w", err)
	}
	return cfg.Workspace, nil
}

func resolveExplicitSnippetRef(cmd *cobra.Command, raw string) (snippetRef, error) {
	workspaceFlag, _ := cmd.Flags().GetString("workspace")
	ref, err := parseSnippetRef(raw, workspaceFlag)
	if err == nil {
		return ref, nil
	}

	if snippetops.IsURL(raw) || workspaceFlag != "" {
		return snippetRef{}, err
	}

	workspace, workspaceErr := configuredWorkspace(cmd)
	if workspaceErr != nil {
		return snippetRef{}, workspaceErr
	}
	return parseSnippetRef(raw, workspace)
}

func parseSnippetRef(raw, fallbackWorkspace string) (snippetRef, error) {
	if strings.TrimSpace(raw) != "" && !snippetops.IsURL(raw) && fallbackWorkspace == "" {
		return snippetRef{}, errSnippetWorkspace
	}
	return snippetops.ParseRef(raw, fallbackWorkspace)
}

var errSnippetWorkspace = errors.New("workspace not configured. Run 'atlas config set workspace <name>' or use --workspace")

func resolveSnippetArg(ctx context.Context, env *cliEnv, workspace string, args []string) (snippetRef, error) {
	if len(args) > 0 {
		return parseSnippetRef(args[0], workspace)
	}
	if workspace == "" {
		return snippetRef{}, errSnippetWorkspace
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return snippetRef{}, fmt.Errorf("snippet ID or URL is required")
	}
	client, err := env.Bitbucket(ctx)
	if err != nil {
		return snippetRef{}, err
	}
	return selectSnippet(ctx, client, workspace)
}

func selectSnippet(ctx context.Context, client *bitbucket.Client, workspace string) (snippetRef, error) {
	snippets, _, err := client.ListSnippets(ctx, workspace, &bitbucket.SnippetListOptions{Limit: 10, Role: "owner"})
	if err != nil {
		return snippetRef{}, err
	}
	if len(snippets) == 0 {
		return snippetRef{}, fmt.Errorf("no snippets found")
	}

	for i, snippet := range snippets {
		fmt.Fprintf(os.Stderr, "%d. %s  %s\n", i+1, snippet.ID, snippet.Title)
	}
	fmt.Fprint(os.Stderr, "Select snippet: ")

	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return snippetRef{}, err
	}
	choice, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || choice < 1 || choice > len(snippets) {
		return snippetRef{}, fmt.Errorf("invalid selection")
	}

	return snippetRef{Workspace: workspace, ID: snippets[choice-1].ID}, nil
}

func snippetCloneURL(ref snippetRef, protocol string) (string, error) {
	switch protocol {
	case "ssh":
		return fmt.Sprintf("git@bitbucket.org:snippets/%s/%s.git", ref.Workspace, ref.ID), nil
	case "https":
		return fmt.Sprintf("https://bitbucket.org/snippets/%s/%s.git", ref.Workspace, ref.ID), nil
	default:
		return "", fmt.Errorf("invalid protocol %q: expected ssh or https", protocol)
	}
}

func readSnippetCreateFiles(args []string, stdinFilename string) (map[string][]byte, error) {
	files := make(map[string][]byte)
	readStdin := false

	for _, arg := range args {
		if arg == "-" {
			readStdin = true
			continue
		}
		content, err := os.ReadFile(arg)
		if err != nil {
			return nil, fmt.Errorf("failed to read file %s: %w", arg, err)
		}
		filename := snippetLocalFilename(arg)
		if err := addSnippetFileContent(files, filename, content, arg); err != nil {
			return nil, err
		}
	}

	if len(args) == 0 && !term.IsTerminal(int(os.Stdin.Fd())) {
		readStdin = true
	}

	if readStdin {
		if stdinFilename == "" {
			stdinFilename = "stdin.txt"
		}
		content, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("failed to read standard input: %w", err)
		}
		if err := addSnippetFileContent(files, stdinFilename, content, "standard input"); err != nil {
			return nil, err
		}
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("at least one filename or - is required")
	}
	return files, nil
}

func addSnippetFileContent(files map[string][]byte, filename string, content []byte, source string) error {
	if _, exists := files[filename]; exists {
		return fmt.Errorf("duplicate snippet filename %q from %s", filename, source)
	}
	files[filename] = content
	return nil
}

func snippetLocalFilename(filePath string) string {
	cleaned := filepath.Clean(filePath)
	if filepath.IsAbs(cleaned) {
		return filepath.Base(cleaned)
	}
	return filepath.ToSlash(cleaned)
}

func renderSnippetFiles(w io.Writer, contents map[string]string, rawOutput bool) error {
	filenames := sortedMapKeys(contents)
	if rawOutput || !isTerminal(w) {
		for _, filename := range filenames {
			content := contents[filename]
			if len(contents) > 1 {
				fmt.Fprintf(w, "=== %s ===\n", filename)
			}
			fmt.Fprint(w, content)
			if !strings.HasSuffix(content, "\n") {
				fmt.Fprintln(w)
			}
		}
		return nil
	}

	var rendered bytes.Buffer
	for _, filename := range filenames {
		content := contents[filename]
		if len(contents) > 1 {
			fmt.Fprintf(&rendered, "=== %s ===\n", filename)
		}
		rendered.WriteString(sanitizeTerminalContent(content))
		if !strings.HasSuffix(content, "\n") {
			rendered.WriteByte('\n')
		}
	}

	if batPath, err := exec.LookPath("bat"); err == nil {
		args := []string{"--paging=always"}
		if len(filenames) == 1 {
			args = append(args, "--file-name", filenames[0])
		}
		return pipeToCommand(batPath, args, rendered.Bytes())
	}
	if lessPath, err := exec.LookPath("less"); err == nil {
		return pipeToCommand(lessPath, nil, rendered.Bytes())
	}

	_, err := w.Write(rendered.Bytes())
	return err
}

func sortedMapKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func pipeToCommand(path string, args []string, input []byte) error {
	cmd := exec.Command(path, args...)
	cmd.Stdin = bytes.NewReader(input)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func openBrowser(target string) error {
	if target == "" {
		return fmt.Errorf("snippet does not include a web URL")
	}
	if opener, err := exec.LookPath("xdg-open"); err == nil {
		return startDetached(opener, target)
	}
	if opener, err := exec.LookPath("open"); err == nil {
		return startDetached(opener, target)
	}
	return fmt.Errorf("could not find xdg-open or open")
}

func startDetached(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func sanitizeTerminalContent(content string) string {
	var b strings.Builder
	for _, r := range content {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			b.WriteRune(r)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, "\\x%02x", r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func editSnippetFile(ctx context.Context, client *bitbucket.Client, ref snippetRef, filename string) (string, []byte, error) {
	snippet, err := client.GetSnippet(ctx, ref.Workspace, ref.ID)
	if err != nil {
		return "", nil, err
	}

	if filename == "" {
		switch {
		case len(snippet.Files) == 1:
			for name := range snippet.Files {
				filename = name
			}
		case term.IsTerminal(int(os.Stdin.Fd())):
			filename, err = selectSnippetFile(snippet)
			if err != nil {
				return "", nil, err
			}
		default:
			return "", nil, fmt.Errorf("snippet has %d files; specify a filename", len(snippet.Files))
		}
	}

	var existing []byte
	if err := requireSnippetFile(snippet, ref.ID, filename); err != nil {
		return "", nil, err
	}

	existing, err = client.GetSnippetFileContent(ctx, ref.Workspace, ref.ID, filename)
	if err != nil {
		return "", nil, fmt.Errorf("failed to fetch file %s: %w", filename, err)
	}

	edited, err := editBuffer(filename, existing)
	if err != nil {
		return "", nil, err
	}
	return filename, edited, nil
}

func requireSnippetFile(snippet *bitbucket.Snippet, snippetID, filename string) error {
	if _, ok := snippet.Files[filename]; !ok {
		return fmt.Errorf("file %q not found in snippet %s; use --add to add new files", filename, snippetID)
	}
	return nil
}

func selectSnippetFile(snippet *bitbucket.Snippet) (string, error) {
	filenames := snippetops.Filenames(snippet)
	for i, filename := range filenames {
		fmt.Fprintf(os.Stderr, "%d. %s\n", i+1, filename)
	}
	fmt.Fprint(os.Stderr, "Select file: ")

	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return "", err
	}
	choice, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || choice < 1 || choice > len(filenames) {
		return "", fmt.Errorf("invalid selection")
	}

	return filenames[choice-1], nil
}

func editBuffer(filename string, content []byte) ([]byte, error) {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		return nil, fmt.Errorf("VISUAL or EDITOR must be set to edit snippets")
	}

	tempDir, err := os.MkdirTemp("", "atlas-snippet-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tempDir)

	tempPath := filepath.Join(tempDir, filepath.Base(filename))
	if err := os.WriteFile(tempPath, content, 0o600); err != nil {
		return nil, err
	}

	editorCmd := exec.Command("sh", "-c", editor+" \"$1\"", "atlas-editor", tempPath)
	editorCmd.Stdin = os.Stdin
	editorCmd.Stdout = os.Stdout
	editorCmd.Stderr = os.Stderr
	if err := editorCmd.Run(); err != nil {
		return nil, err
	}

	edited, err := os.ReadFile(tempPath)
	if err != nil {
		return nil, err
	}
	if bytes.Equal(content, edited) {
		return nil, errors.New("no changes made")
	}
	return edited, nil
}

func confirmSnippetDelete(id string) (bool, error) {
	fmt.Fprintf(os.Stderr, "Delete snippet %s? [y/N] ", id)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return false, err
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}
