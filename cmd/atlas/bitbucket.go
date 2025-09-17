package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/config"
	"github.com/kabilan108/atlas/internal/output"
	"github.com/kabilan108/atlas/internal/parse"
	"github.com/kabilan108/atlas/internal/worker"
)

var bitbucketCmd = &cobra.Command{
	Use:   "bitbucket",
	Short: "Bitbucket operations",
	Long:  "Search and retrieve content from Bitbucket",
}

var bitbucketSearchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search Bitbucket repositories or pull requests",
	Long: `Search for repositories or pull requests in Bitbucket.

Examples:
  atlas bitbucket search --type repos --query "api" --workspace "myworkspace"
  atlas bitbucket search --type prs --query "bug fix" --workspace "myworkspace"`,
	RunE: runBitbucketSearch,
}

var bitbucketGetCmd = &cobra.Command{
	Use:   "get",
	Short: "Get Bitbucket content",
	Long:  "Get content from Bitbucket (currently supports pull requests)",
}

var bitbucketGetPRCmd = &cobra.Command{
	Use:   "pr <url|workspace/repo#id|->",
	Short: "Get pull request details",
	Long: `Get pull request details by URL, workspace/repo#id format, or from stdin.

Use '-' to read URLs/identifiers from stdin, one per line.

Examples:
  atlas bitbucket get pr https://bitbucket.org/workspace/repo/pull-requests/42
  atlas bitbucket get pr workspace/repo#42
  echo "workspace/repo#42" | atlas bitbucket get pr -`,
	Args: cobra.ExactArgs(1),
	RunE: runBitbucketGetPR,
}

var (
	bitbucketSearchType      string
	bitbucketSearchQuery     string
	bitbucketSearchWorkspace string
	bitbucketSearchRepo      string
	bitbucketSearchLimit     int
	bitbucketGetPRDiff       bool
)

func init() {
	rootCmd.AddCommand(bitbucketCmd)
	bitbucketCmd.AddCommand(bitbucketSearchCmd, bitbucketGetCmd)
	bitbucketGetCmd.AddCommand(bitbucketGetPRCmd)

	bitbucketSearchCmd.Flags().StringVarP(&bitbucketSearchType, "type", "t", "", "Search type: repos or prs (required)")
	bitbucketSearchCmd.Flags().StringVarP(&bitbucketSearchQuery, "query", "q", "", "Search query")
	bitbucketSearchCmd.Flags().StringVarP(&bitbucketSearchWorkspace, "workspace", "w", "", "Workspace to search in")
	bitbucketSearchCmd.Flags().StringVarP(&bitbucketSearchRepo, "repo", "r", "", "Repository to search in (for PRs)")
	bitbucketSearchCmd.Flags().IntVarP(&bitbucketSearchLimit, "limit", "l", 25, "Maximum number of results")
	bitbucketSearchCmd.MarkFlagRequired("type")

	bitbucketGetPRCmd.Flags().BoolVar(&bitbucketGetPRDiff, "diff", false, "Include diff in output")
}

func runBitbucketSearch(cmd *cobra.Command, args []string) error {
	cfg, err := config.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	client, err := bitbucket.NewClient("")
	if err != nil {
		return fmt.Errorf("failed to create Bitbucket client: %w", err)
	}

	workspace := bitbucketSearchWorkspace
	if workspace == "" {
		workspace = cfg.Workspace
	}

	output.LogVerbose(verbose, "Searching Bitbucket %s with query: %s", bitbucketSearchType, bitbucketSearchQuery)

	ctx := context.Background()
	var documents []output.Document

	switch bitbucketSearchType {
	case "repos":
		documents, err = client.SearchRepositories(ctx, workspace, bitbucketSearchQuery, bitbucketSearchLimit)
	case "prs":
		if bitbucketSearchRepo == "" {
			return fmt.Errorf("--repo flag is required when searching for pull requests")
		}
		documents, err = client.SearchPullRequests(ctx, workspace, bitbucketSearchRepo, bitbucketSearchQuery, bitbucketSearchLimit)
	default:
		return fmt.Errorf("invalid search type: %s (must be 'repos' or 'prs')", bitbucketSearchType)
	}

	if err != nil {
		return fmt.Errorf("search failed: %w", err)
	}

	format := output.Format(wrapFormat)
	for _, doc := range documents {
		if err := output.WriteDocument(&doc, format); err != nil {
			output.LogError("Failed to write document %s: %v", doc.ID, err)
		}
	}

	output.LogVerbose(verbose, "Found %d documents", len(documents))
	return nil
}

func runBitbucketGetPR(cmd *cobra.Command, args []string) error {
	input := args[0]

	_, err := config.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	client, err := bitbucket.NewClient("")
	if err != nil {
		return fmt.Errorf("failed to create Bitbucket client: %w", err)
	}

	var inputs []string
	if input == "-" {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line != "" {
				inputs = append(inputs, line)
			}
		}
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("failed to read from stdin: %w", err)
		}
	} else {
		inputs = []string{input}
	}

	if len(inputs) == 0 {
		output.LogError("No input provided")
		return fmt.Errorf("no input provided")
	}

	ctx := context.Background()
	pool := worker.NewPool(ctx, concurrency)
	defer pool.Cancel()

	format := output.Format(wrapFormat)

	for _, inp := range inputs {
		input := inp
		pool.Submit(func(ctx context.Context) error {
			return processBitbucketPRInput(ctx, client, input, format, bitbucketGetPRDiff)
		})
	}

	pool.Close()

	go func() {
		pool.Wait()
	}()

	for err := range pool.Results() {
		if err != nil {
			output.LogError("Processing error: %v", err)
		}
	}

	output.LogVerbose(verbose, "Processed %d inputs", len(inputs))
	return nil
}

func processBitbucketPRInput(ctx context.Context, client *bitbucket.Client, input string, format output.Format, includeDiff bool) error {
	var workspace, repo string
	var prID int
	var err error

	if strings.HasPrefix(input, "http") {
		prInfo, err := parse.ParseBitbucketPR(input)
		if err != nil {
			return fmt.Errorf("failed to parse URL %s: %w", input, err)
		}
		workspace = prInfo.Workspace
		repo = prInfo.Repo
		prID = prInfo.PRID
	} else {
		prInfo, err := parse.ParseBitbucketPR(input)
		if err != nil {
			return fmt.Errorf("failed to parse input %s: %w", input, err)
		}
		workspace = prInfo.Workspace
		repo = prInfo.Repo
		prID = prInfo.PRID
	}

	output.LogVerbose(verbose, "Fetching Bitbucket PR: %s/%s#%d", workspace, repo, prID)

	doc, err := client.GetPullRequest(ctx, workspace, repo, prID, includeDiff)
	if err != nil {
		return fmt.Errorf("failed to get PR %s/%s#%d: %w", workspace, repo, prID, err)
	}

	if err := output.WriteDocument(doc, format); err != nil {
		return fmt.Errorf("failed to write document %s: %w", doc.ID, err)
	}

	return nil
}
