package cli

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/config"
	"github.com/kabilan108/atlas/internal/output"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

type snippetRef struct {
	Workspace string
	ID        string
}

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
		RunE:    runSnippetList,
	}

	cmd.Flags().String("workspace", "", "Target workspace")
	cmd.Flags().IntP("limit", "L", 10, "Maximum number of snippets to fetch")
	cmd.Flags().String("role", "", "Filter by role: owner, contributor, member")
	cmd.Flags().Bool("public", false, "Show only public snippets")
	cmd.Flags().Bool("private", false, "Show only private snippets")
	cmd.Flags().Bool("json", false, "Output as JSON")

	return cmd
}

func runSnippetList(cmd *cobra.Command, args []string) error {
	workspace, err := configuredWorkspace(cmd)
	if err != nil {
		return err
	}

	limit, _ := cmd.Flags().GetInt("limit")
	role, _ := cmd.Flags().GetString("role")
	publicOnly, _ := cmd.Flags().GetBool("public")
	privateOnly, _ := cmd.Flags().GetBool("private")
	jsonOutput, _ := cmd.Flags().GetBool("json")

	if publicOnly && privateOnly {
		return fmt.Errorf("--public and --private cannot be used together")
	}
	if role != "" && role != "owner" && role != "contributor" && role != "member" {
		return fmt.Errorf("invalid role %q: expected owner, contributor, or member", role)
	}
	if limit < 1 {
		return fmt.Errorf("--limit must be greater than 0")
	}

	client, err := bitbucket.NewClient(bitbucket.WithNoCache(noCache))
	if err != nil {
		return err
	}

	listLimit := limit
	if publicOnly || privateOnly {
		listLimit = 0
	}
	snippets, err := client.ListSnippets(workspace, &bitbucket.SnippetListOptions{
		Limit: listLimit,
		Role:  role,
	})
	if err != nil {
		return err
	}
	snippets = filterSnippetsByVisibility(snippets, publicOnly, privateOnly)
	snippets = limitSnippets(snippets, limit)

	if jsonOutput {
		return output.WriteJSON(os.Stdout, snippets)
	}

	if len(snippets) == 0 {
		fmt.Println("No snippets found.")
		return nil
	}

	tw := output.NewTableWriter(os.Stdout, "ID", "Title", "Files", "Visibility", "Updated")
	for _, s := range snippets {
		tw.AddRow(
			s.ID,
			output.Truncate(s.Title, 40),
			fmt.Sprintf("%d", len(s.Files)),
			snippetVisibility(s),
			output.FormatRelativeTime(s.UpdatedOn),
		)
	}

	return tw.Flush()
}

func newSnippetViewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "view [<snippet>]",
		Short: "View a snippet",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runSnippetView,
	}

	cmd.Flags().String("workspace", "", "Target workspace")
	cmd.Flags().StringP("filename", "f", "", "Display a single file from the snippet")
	cmd.Flags().Bool("files", false, "List file names from the snippet")
	cmd.Flags().BoolP("raw", "r", false, "Print raw instead of paged snippet contents")
	cmd.Flags().BoolP("web", "w", false, "Open snippet in the browser")
	cmd.Flags().Bool("json", false, "Output as JSON")

	return cmd
}

type SnippetViewJSON struct {
	*bitbucket.Snippet
	FileContents map[string]string `json:"file_contents,omitempty"`
}

func runSnippetView(cmd *cobra.Command, args []string) error {
	workspace, err := optionalWorkspace(cmd)
	if err != nil {
		return err
	}

	client, err := bitbucket.NewClient(bitbucket.WithNoCache(noCache))
	if err != nil {
		return err
	}

	ref, err := resolveSnippetArg(client, workspace, args)
	if err != nil {
		return err
	}

	filename, _ := cmd.Flags().GetString("filename")
	filesOnly, _ := cmd.Flags().GetBool("files")
	rawOutput, _ := cmd.Flags().GetBool("raw")
	webOutput, _ := cmd.Flags().GetBool("web")
	jsonOutput, _ := cmd.Flags().GetBool("json")

	snippet, err := client.GetSnippet(ref.Workspace, ref.ID)
	if err != nil {
		return err
	}

	if webOutput {
		return openBrowser(snippet.Links.HTML.Href)
	}

	if jsonOutput {
		result := SnippetViewJSON{Snippet: snippet}
		if !filesOnly {
			contents, err := snippetFileContents(client, ref, snippet, filename)
			if err != nil {
				return err
			}
			result.FileContents = contents
		}
		return output.WriteJSON(os.Stdout, result)
	}

	if filesOnly {
		return printSnippetFiles(snippet)
	}

	contents, err := snippetFileContents(client, ref, snippet, filename)
	if err != nil {
		return err
	}
	return renderSnippetFiles(contents, rawOutput)
}

func newSnippetCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "create [<filename>... | -]",
		Aliases: []string{"new"},
		Short:   "Create a new snippet",
		RunE:    runSnippetCreate,
	}

	cmd.Flags().String("workspace", "", "Target workspace")
	cmd.Flags().String("title", "", "Snippet title")
	cmd.Flags().StringP("filename", "f", "", "Filename to use when reading from standard input")
	cmd.Flags().BoolP("public", "p", false, "List the snippet publicly")
	cmd.Flags().Bool("json", false, "Output as JSON")
	_ = cmd.MarkFlagRequired("title")

	return cmd
}

func runSnippetCreate(cmd *cobra.Command, args []string) error {
	workspace, err := configuredWorkspace(cmd)
	if err != nil {
		return err
	}

	title, _ := cmd.Flags().GetString("title")
	stdinFilename, _ := cmd.Flags().GetString("filename")
	publicSnippet, _ := cmd.Flags().GetBool("public")
	jsonOutput, _ := cmd.Flags().GetBool("json")

	files, err := readSnippetCreateFiles(args, stdinFilename)
	if err != nil {
		return err
	}

	client, err := bitbucket.NewClient(bitbucket.WithNoCache(noCache))
	if err != nil {
		return err
	}

	snippet, err := client.CreateSnippet(workspace, title, files, !publicSnippet)
	if err != nil {
		return err
	}

	if jsonOutput {
		return output.WriteJSON(os.Stdout, snippet)
	}

	fmt.Printf("Created snippet: %s\n", snippet.ID)
	fmt.Printf("URL: %s\n", snippet.Links.HTML.Href)

	return nil
}

func newSnippetEditCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "edit <snippet> [<filename>]",
		Short: "Edit a snippet",
		Args:  cobra.RangeArgs(1, 2),
		RunE:  runSnippetEdit,
	}

	cmd.Flags().String("workspace", "", "Target workspace")
	cmd.Flags().String("title", "", "New title for the snippet")
	cmd.Flags().StringP("add", "a", "", "Add a new file to the snippet")
	cmd.Flags().StringP("filename", "f", "", "Select a file to edit")
	cmd.Flags().StringP("remove", "r", "", "Remove a file from the snippet")

	return cmd
}

func runSnippetEdit(cmd *cobra.Command, args []string) error {
	ref, err := resolveExplicitSnippetRef(cmd, args[0])
	if err != nil {
		return err
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
		return fmt.Errorf("filename cannot be used with --title unless editing file contents or adding a file")
	}

	client, err := bitbucket.NewClient(bitbucket.WithNoCache(noCache))
	if err != nil {
		return err
	}

	addFiles := make(map[string][]byte)
	var removeFiles []string

	if addFile != "" {
		content, err := os.ReadFile(addFile)
		if err != nil {
			return fmt.Errorf("failed to read file %s: %w", addFile, err)
		}
		targetFilename := filename
		if targetFilename == "" {
			targetFilename = snippetLocalFilename(addFile)
		}
		if err := addSnippetFileContent(addFiles, targetFilename, content, addFile); err != nil {
			return err
		}
	}

	if removeFile != "" {
		removeFiles = append(removeFiles, removeFile)
	}

	if title == "" && addFile == "" && removeFile == "" {
		editedName, editedContent, err := editSnippetFile(client, ref, filename)
		if err != nil {
			return err
		}
		addFiles[editedName] = editedContent
	}

	if len(addFiles) == 0 && len(removeFiles) == 0 && title == "" {
		return fmt.Errorf("nothing to edit")
	}

	if err := client.UpdateSnippet(ref.Workspace, ref.ID, title, addFiles, removeFiles); err != nil {
		return err
	}

	fmt.Printf("Updated snippet: %s\n", ref.ID)
	return nil
}

func newSnippetRenameCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rename <snippet> <old-filename> <new-filename>",
		Short: "Rename a file in a snippet",
		Args:  cobra.ExactArgs(3),
		RunE:  runSnippetRename,
	}

	cmd.Flags().String("workspace", "", "Target workspace")

	return cmd
}

func runSnippetRename(cmd *cobra.Command, args []string) error {
	ref, err := resolveExplicitSnippetRef(cmd, args[0])
	if err != nil {
		return err
	}

	oldFilename := args[1]
	newFilename := args[2]

	client, err := bitbucket.NewClient(bitbucket.WithNoCache(noCache))
	if err != nil {
		return err
	}

	content, err := client.GetSnippetFileContent(ref.Workspace, ref.ID, oldFilename)
	if err != nil {
		return fmt.Errorf("failed to fetch file %s: %w", oldFilename, err)
	}

	if err := client.UpdateSnippet(ref.Workspace, ref.ID, "", map[string][]byte{newFilename: content}, []string{oldFilename}); err != nil {
		return err
	}

	fmt.Printf("Renamed %s to %s in snippet %s\n", oldFilename, newFilename, ref.ID)
	return nil
}

func newSnippetDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete [<snippet>]",
		Short: "Delete a snippet",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runSnippetDelete,
	}

	cmd.Flags().String("workspace", "", "Target workspace")
	cmd.Flags().Bool("yes", false, "Confirm deletion without prompting")

	return cmd
}

func runSnippetDelete(cmd *cobra.Command, args []string) error {
	workspace, err := optionalWorkspace(cmd)
	if err != nil {
		return err
	}

	client, err := bitbucket.NewClient(bitbucket.WithNoCache(noCache))
	if err != nil {
		return err
	}

	ref, err := resolveSnippetArg(client, workspace, args)
	if err != nil {
		return err
	}

	yes, _ := cmd.Flags().GetBool("yes")
	if !yes {
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			return fmt.Errorf("refusing to delete non-interactively without --yes")
		}
		confirmed, err := confirmSnippetDelete(ref.ID)
		if err != nil {
			return err
		}
		if !confirmed {
			fmt.Println("Deletion cancelled.")
			return nil
		}
	}

	if err := client.DeleteSnippet(ref.Workspace, ref.ID); err != nil {
		return err
	}

	fmt.Printf("Deleted snippet: %s\n", ref.ID)

	return nil
}

func configuredWorkspace(cmd *cobra.Command) (string, error) {
	workspace, err := optionalWorkspace(cmd)
	if err != nil {
		return "", err
	}
	if workspace == "" {
		return "", fmt.Errorf("workspace not configured. Run 'atlas config set workspace <name>' or use --workspace")
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

	if isURLLike(raw) || workspaceFlag != "" {
		return snippetRef{}, err
	}

	workspace, workspaceErr := configuredWorkspace(cmd)
	if workspaceErr != nil {
		return snippetRef{}, workspaceErr
	}
	return parseSnippetRef(raw, workspace)
}

func parseSnippetRef(raw, fallbackWorkspace string) (snippetRef, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return snippetRef{}, fmt.Errorf("snippet ID or URL is required")
	}

	parsed, err := url.Parse(raw)
	if err == nil && parsed.Host != "" {
		host := parsed.Hostname()
		parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		switch {
		case host == "bitbucket.org" && len(parts) >= 3 && parts[0] == "snippets":
			return snippetRefFromURLParts(parts[1], parts[2])
		case host == "bitbucket.org" && len(parts) >= 4 && parts[1] == "workspace" && parts[2] == "snippets":
			return snippetRefFromURLParts(parts[0], parts[3])
		case isBitbucketHost(host) && len(parts) >= 4 && parts[0] == "2.0" && parts[1] == "snippets":
			return snippetRefFromURLParts(parts[2], parts[3])
		default:
			return snippetRef{}, fmt.Errorf("unsupported snippet URL: %s", raw)
		}
	}

	if fallbackWorkspace == "" {
		return snippetRef{}, fmt.Errorf("workspace not configured. Run 'atlas config set workspace <name>' or use --workspace")
	}
	return snippetRef{Workspace: fallbackWorkspace, ID: raw}, nil
}

func isBitbucketHost(host string) bool {
	return host == "bitbucket.org" || strings.HasSuffix(host, ".bitbucket.org")
}

func snippetRefFromURLParts(workspacePart, idPart string) (snippetRef, error) {
	workspace, err := url.PathUnescape(workspacePart)
	if err != nil {
		return snippetRef{}, fmt.Errorf("invalid snippet workspace in URL: %w", err)
	}
	id, err := url.PathUnescape(strings.TrimSuffix(idPart, ".git"))
	if err != nil {
		return snippetRef{}, fmt.Errorf("invalid snippet ID in URL: %w", err)
	}
	if workspace == "" || id == "" {
		return snippetRef{}, fmt.Errorf("snippet URL must include workspace and ID")
	}
	return snippetRef{Workspace: workspace, ID: id}, nil
}

func isURLLike(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && parsed.Host != ""
}

func resolveSnippetArg(client *bitbucket.Client, workspace string, args []string) (snippetRef, error) {
	if len(args) > 0 {
		return parseSnippetRef(args[0], workspace)
	}
	if workspace == "" {
		return snippetRef{}, fmt.Errorf("workspace not configured. Run 'atlas config set workspace <name>' or use --workspace")
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return snippetRef{}, fmt.Errorf("snippet ID or URL is required")
	}
	return selectSnippet(client, workspace)
}

func selectSnippet(client *bitbucket.Client, workspace string) (snippetRef, error) {
	snippets, err := client.ListSnippets(workspace, &bitbucket.SnippetListOptions{Limit: 10, Role: "owner"})
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

func filterSnippetsByVisibility(snippets []bitbucket.Snippet, publicOnly, privateOnly bool) []bitbucket.Snippet {
	if !publicOnly && !privateOnly {
		return snippets
	}

	filtered := make([]bitbucket.Snippet, 0, len(snippets))
	for _, snippet := range snippets {
		if publicOnly && !snippet.IsPrivate {
			filtered = append(filtered, snippet)
		}
		if privateOnly && snippet.IsPrivate {
			filtered = append(filtered, snippet)
		}
	}
	return filtered
}

func limitSnippets(snippets []bitbucket.Snippet, limit int) []bitbucket.Snippet {
	if limit > 0 && len(snippets) > limit {
		return snippets[:limit]
	}
	return snippets
}

func snippetVisibility(snippet bitbucket.Snippet) string {
	if snippet.IsPrivate {
		return "private"
	}
	return "public"
}

func printSnippetFiles(snippet *bitbucket.Snippet) error {
	filenames := snippetFilenames(snippet)
	for _, filename := range filenames {
		fmt.Println(filename)
	}
	return nil
}

func snippetFileContents(client *bitbucket.Client, ref snippetRef, snippet *bitbucket.Snippet, filename string) (map[string]string, error) {
	filenames := []string{}
	if filename != "" {
		if _, ok := snippet.Files[filename]; !ok {
			return nil, fmt.Errorf("file %q not found in snippet %s", filename, ref.ID)
		}
		filenames = append(filenames, filename)
	} else {
		filenames = snippetFilenames(snippet)
	}

	contents := make(map[string]string, len(filenames))
	for _, name := range filenames {
		content, err := client.GetSnippetFileContent(ref.Workspace, ref.ID, name)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch file %s: %w", name, err)
		}
		contents[name] = string(content)
	}
	return contents, nil
}

func renderSnippetFiles(contents map[string]string, rawOutput bool) error {
	filenames := sortedMapKeys(contents)
	if rawOutput || !term.IsTerminal(int(os.Stdout.Fd())) {
		for _, filename := range filenames {
			content := contents[filename]
			if len(contents) > 1 {
				fmt.Printf("=== %s ===\n", filename)
			}
			fmt.Print(content)
			if !strings.HasSuffix(content, "\n") {
				fmt.Println()
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

	_, err := os.Stdout.Write(rendered.Bytes())
	return err
}

func snippetFilenames(snippet *bitbucket.Snippet) []string {
	filenames := make([]string, 0, len(snippet.Files))
	for filename := range snippet.Files {
		filenames = append(filenames, filename)
	}
	sort.Strings(filenames)
	return filenames
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

func editSnippetFile(client *bitbucket.Client, ref snippetRef, filename string) (string, []byte, error) {
	snippet, err := client.GetSnippet(ref.Workspace, ref.ID)
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

	existing, err = client.GetSnippetFileContent(ref.Workspace, ref.ID, filename)
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
	filenames := snippetFilenames(snippet)
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
