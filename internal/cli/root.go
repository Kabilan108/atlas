package cli

import (
	"fmt"
	"strings"
	"time"

	updatepkg "github.com/kabilan108/atlas/internal/update"
	"github.com/spf13/cobra"
)

var (
	noCache bool
	verbose bool
)

func NewRootCmd(version string) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:     "atlas",
		Short:   "CLI tool for interacting with Bitbucket Cloud",
		Long:    "Atlas enables fetching PR comments and review feedback from Bitbucket Cloud\nin a format optimized for Claude Code agents to address reviewer comments directly.",
		Version: version,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			maybePrintUpdateNudge(cmd, version)
		},
	}

	rootCmd.PersistentFlags().BoolVar(&noCache, "no-cache", false, "Bypass disk cache entirely")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Show inferred values (repo from git remote, etc.)")

	rootCmd.AddCommand(newConfigCmd())
	rootCmd.AddCommand(newLoginCmd())
	rootCmd.AddCommand(newPRCmd())
	rootCmd.AddCommand(newSnippetCmd())
	rootCmd.AddCommand(newUpdateCmd(version))

	return rootCmd
}

func Execute(version string) error {
	return NewRootCmd(version).Execute()
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
	case "login", "pr", "snippet":
		return true
	default:
		return false
	}
}
