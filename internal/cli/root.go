package cli

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	updatepkg "github.com/kabilan108/atlas/internal/update"
	"github.com/spf13/cobra"
)

var (
	noCache bool
	verbose bool
)

// Option configures NewRootCmd.
type Option func(*rootOptions)

type rootOptions struct {
	httpClient *http.Client
}

// WithHTTPClient sends every Bitbucket and Jira request the CLI makes through
// client, so tests can serve them in-process.
func WithHTTPClient(client *http.Client) Option {
	return func(o *rootOptions) {
		o.httpClient = client
	}
}

type rootOptionsKey struct{}

// optionsFrom returns the options NewRootCmd was given, or none for a command
// built outside it.
func optionsFrom(cmd *cobra.Command) *rootOptions {
	if cmd != nil && cmd.Context() != nil {
		if o, ok := cmd.Context().Value(rootOptionsKey{}).(*rootOptions); ok {
			return o
		}
	}
	return &rootOptions{}
}

// wrapRunE gives every command's RunE the root options through its context,
// whichever Execute variant runs the tree, adds CLI hints to its errors, and
// marks them as raised by the command, which ExitCode tells from cobra's
// command-line errors.
func wrapRunE(cmd *cobra.Command, options *rootOptions) {
	if run := cmd.RunE; run != nil {
		cmd.RunE = func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			cmd.SetContext(context.WithValue(ctx, rootOptionsKey{}, options))
			if err := withCLIHint(run(cmd, args)); err != nil {
				return &ranError{err: err}
			}
			return nil
		}
	}
	for _, child := range cmd.Commands() {
		wrapRunE(child, options)
	}
}

// rejectUnknownSubcommands makes `atlas pr bogus` a usage error. Cobra
// answers an unknown subcommand of a group that cannot run itself with
// help and success; a group that prints its help as its own run rejects
// arguments instead. The root already rejects unknown commands.
func rejectUnknownSubcommands(cmd *cobra.Command) {
	if cmd.HasParent() && cmd.HasSubCommands() && cmd.Run == nil && cmd.RunE == nil {
		cmd.Args = cobra.NoArgs
		cmd.RunE = func(cmd *cobra.Command, _ []string) error { return cmd.Help() }
	}
	for _, child := range cmd.Commands() {
		rejectUnknownSubcommands(child)
	}
}

func NewRootCmd(version string, opts ...Option) *cobra.Command {
	options := &rootOptions{}
	for _, opt := range opts {
		opt(options)
	}
	rootCmd := &cobra.Command{
		Use:     "atlas",
		Short:   "CLI tool for interacting with Bitbucket Cloud",
		Long:    "Atlas enables fetching PR comments and review feedback from Bitbucket Cloud\nin a format optimized for Claude Code agents to address reviewer comments directly.\n\n" + exitCodesHelp,
		Version: version,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			maybePrintUpdateNudge(cmd, version)
		},
	}

	rootCmd.PersistentFlags().BoolVar(&noCache, "no-cache", false, "Bypass disk cache entirely")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Show inferred values (repo from git remote, etc.)")

	rootCmd.AddCommand(newConfigCmd())
	rootCmd.AddCommand(newLoginCmd())
	rootCmd.AddCommand(newMCPCmd(version))
	rootCmd.AddCommand(newPRCmd())
	rootCmd.AddCommand(newPipelineCmd())
	rootCmd.AddCommand(newSnippetCmd())
	rootCmd.AddCommand(newUpdateCmd(version))
	rootCmd.AddCommand(newAPICmd())
	rootCmd.AddCommand(newBranchCmd())
	rootCmd.AddCommand(newCommitCmd())
	rootCmd.AddCommand(newFileCmd())
	rootCmd.AddCommand(newRepoCmd())
	rootCmd.AddCommand(newUserCmd())
	rootCmd.AddCommand(newWhoamiCmd())
	rootCmd.AddCommand(newJiraCmd())
	rejectUnknownSubcommands(rootCmd)
	wrapRunE(rootCmd, options)

	return rootCmd
}

// Execute runs the command line and returns the process exit code. Any
// output a partial result produced is already written.
func Execute(version string) int {
	return ExitCode(NewRootCmd(version).Execute())
}

func maybePrintUpdateNudge(cmd *cobra.Command, version string) {
	if !shouldCheckForUpdate(cmd) {
		return
	}

	nudge, err := (updatepkg.NudgeChecker{
		Client: updatepkg.NewHTTPClient(3 * time.Second),
	}).Check(version)
	if err != nil || nudge.Latest == "" {
		return
	}

	message := nudge.Message
	if message == "" {
		message = "A newer Atlas CLI is available. Run: atlas update"
	}
	if nudge.Required {
		fmt.Fprintf(cmd.ErrOrStderr(), "Atlas %s is older than the minimum recommended version %s. %s\n", version, nudge.Minimum, message)
		return
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "Atlas %s is available. %s\n", nudge.Latest, message)
}

func shouldCheckForUpdate(cmd *cobra.Command) bool {
	fields := strings.Fields(cmd.CommandPath())
	if len(fields) < 2 {
		return false
	}
	switch fields[1] {
	case "login", "pr", "snippet", "pipeline", "jira", "repo", "branch", "commit", "file", "user", "whoami", "api":
		return true
	default:
		return false
	}
}
