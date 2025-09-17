package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

const (
	version            = "0.1.0"
	defaultConcurrency = 5
)

var (
	wrapFormat  string
	concurrency int
	verbose     bool
)

var rootCmd = &cobra.Command{
	Use:   "atlas",
	Short: "A POSIX-compliant CLI for fetching Confluence and Bitbucket content",
	Long: `Atlas is a command-line tool that fetches content from Confluence and Bitbucket
and outputs it as markdown-wrapped content. It supports batch processing, multiple
output formats, and built-in retry logic.

Content is written to stdout, while messages and errors go to stderr.

Configuration is loaded from:
- $XDG_CONFIG_HOME/atlas/config.json
- ~/.config/atlas/config.json
- ./atlas.json

Authentication requires:
- ATLASSIAN_EMAIL environment variable
- ATLASSIAN_TOKEN environment variable

Examples:
  atlas confluence get https://company.atlassian.net/wiki/pages/123456
  atlas bitbucket get pr workspace/repo#42
  atlas get https://company.atlassian.net/wiki/pages/123456 --wrap=xmlish
  echo "url1\nurl2" | atlas get - --concurrency=10`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func init() {
	rootCmd.PersistentFlags().StringVar(&wrapFormat, "wrap", "fenced", "Output format (fenced|xmlish)")
	rootCmd.PersistentFlags().IntVar(&concurrency, "concurrency", defaultConcurrency, "Number of concurrent requests")
	rootCmd.PersistentFlags().BoolVar(&verbose, "verbose", false, "Enable verbose logging")

	// Add version command
	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Print the version number of atlas",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("atlas version %s\n", version)
		},
	}
	rootCmd.AddCommand(versionCmd)
}
