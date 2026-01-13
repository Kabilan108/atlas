package cli

import (
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
	}

	rootCmd.PersistentFlags().BoolVar(&noCache, "no-cache", false, "Bypass disk cache entirely")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Show inferred values (repo from git remote, etc.)")

	rootCmd.AddCommand(newConfigCmd())
	rootCmd.AddCommand(newPRCmd())
	rootCmd.AddCommand(newSnippetCmd())

	return rootCmd
}

func Execute(version string) error {
	return NewRootCmd(version).Execute()
}
