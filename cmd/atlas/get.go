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
	"github.com/kabilan108/atlas/internal/confluence"
	"github.com/kabilan108/atlas/internal/output"
	"github.com/kabilan108/atlas/internal/parse"
	"github.com/kabilan108/atlas/internal/worker"
)

var getCmd = &cobra.Command{
	Use:   "get <url|->",
	Short: "Get content from URL (auto-detects Confluence or Bitbucket)",
	Long: `Get content from a URL by auto-detecting whether it's from Confluence or Bitbucket.

Use '-' to read URLs from stdin, one per line.

Examples:
  atlas get https://company.atlassian.net/wiki/pages/123456
  atlas get https://bitbucket.org/workspace/repo/pull-requests/42
  echo "https://company.atlassian.net/wiki/pages/123456" | atlas get -`,
	Args: cobra.ExactArgs(1),
	RunE: runGet,
}

var (
	getDiff bool
)

func init() {
	rootCmd.AddCommand(getCmd)

	getCmd.Flags().BoolVar(&getDiff, "diff", false, "Include diff in output (for Bitbucket PRs)")
}

func runGet(cmd *cobra.Command, args []string) error {
	input := args[0]

	cfg, err := config.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
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
			return processUniversalInput(ctx, cfg, input, format, getDiff)
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

func processUniversalInput(ctx context.Context, cfg *config.Config, input string, format output.Format, includeDiff bool) error {
	urlType := parse.DetectURLType(input)

	switch urlType {
	case parse.URLTypeConfluence:
		return processUniversalConfluence(ctx, cfg, input, format)
	case parse.URLTypeBitbucket:
		return processUniversalBitbucket(ctx, cfg, input, format, includeDiff)
	default:
		return fmt.Errorf("unable to determine URL type for: %s", input)
	}
}

func processUniversalConfluence(ctx context.Context, cfg *config.Config, input string, format output.Format) error {
	client, err := confluence.NewClient(cfg.ConfluenceSite)
	if err != nil {
		return fmt.Errorf("failed to create Confluence client: %w", err)
	}

	info, err := parse.ParseConfluenceURL(input)
	if err != nil {
		return fmt.Errorf("failed to parse Confluence URL %s: %w", input, err)
	}

	output.LogVerbose(verbose, "Fetching Confluence page: %s", info.PageID)

	doc, err := client.GetContent(ctx, info.PageID)
	if err != nil {
		return fmt.Errorf("failed to get Confluence content %s: %w", info.PageID, err)
	}

	if err := output.WriteDocument(doc, format); err != nil {
		return fmt.Errorf("failed to write document %s: %w", doc.ID, err)
	}

	return nil
}

func processUniversalBitbucket(ctx context.Context, cfg *config.Config, input string, format output.Format, includeDiff bool) error {
	client, err := bitbucket.NewClient("")
	if err != nil {
		return fmt.Errorf("failed to create Bitbucket client: %w", err)
	}

	prInfo, err := parse.ParseBitbucketPR(input)
	if err != nil {
		return fmt.Errorf("failed to parse Bitbucket URL %s: %w", input, err)
	}

	output.LogVerbose(verbose, "Fetching Bitbucket PR: %s/%s#%d", prInfo.Workspace, prInfo.Repo, prInfo.PRID)

	doc, err := client.GetPullRequest(ctx, prInfo.Workspace, prInfo.Repo, prInfo.PRID, includeDiff)
	if err != nil {
		return fmt.Errorf("failed to get Bitbucket PR %s/%s#%d: %w", prInfo.Workspace, prInfo.Repo, prInfo.PRID, err)
	}

	if err := output.WriteDocument(doc, format); err != nil {
		return fmt.Errorf("failed to write document %s: %w", doc.ID, err)
	}

	return nil
}
