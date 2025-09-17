package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/kabilan108/atlas/internal/config"
	"github.com/kabilan108/atlas/internal/confluence"
	"github.com/kabilan108/atlas/internal/output"
	"github.com/kabilan108/atlas/internal/parse"
	"github.com/kabilan108/atlas/internal/worker"
)

var confluenceCmd = &cobra.Command{
	Use:   "confluence",
	Short: "Confluence operations",
	Long:  "Search and retrieve content from Confluence",
}

var confluenceSearchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search Confluence content",
	Long: `Search for content in Confluence using text search or CQL.

Examples:
  atlas confluence search --query "API documentation"
  atlas confluence search --query "API documentation" --space "DEV"
  atlas confluence search --query "space = DEV and type = page" --cql`,
	RunE: runConfluenceSearch,
}

var confluenceGetCmd = &cobra.Command{
	Use:   "get <url|id|->",
	Short: "Get Confluence content by URL or ID",
	Long: `Get Confluence content by URL, page ID, or from stdin.

Use '-' to read URLs/IDs from stdin, one per line.

Examples:
  atlas confluence get https://company.atlassian.net/wiki/pages/123456
  atlas confluence get 123456
  echo "123456" | atlas confluence get -`,
	Args: cobra.ExactArgs(1),
	RunE: runConfluenceGet,
}

var (
	confluenceSearchQuery string
	confluenceSearchSpace string
	confluenceSearchCQL   bool
	confluenceSearchLimit int
)

func init() {
	rootCmd.AddCommand(confluenceCmd)
	confluenceCmd.AddCommand(confluenceSearchCmd, confluenceGetCmd)

	confluenceSearchCmd.Flags().StringVarP(&confluenceSearchQuery, "query", "q", "", "Search query (required)")
	confluenceSearchCmd.Flags().StringVarP(&confluenceSearchSpace, "space", "s", "", "Space key to search in")
	confluenceSearchCmd.Flags().BoolVar(&confluenceSearchCQL, "cql", false, "Use CQL query mode")
	confluenceSearchCmd.Flags().IntVarP(&confluenceSearchLimit, "limit", "l", 25, "Maximum number of results")
	confluenceSearchCmd.MarkFlagRequired("query")
}

func runConfluenceSearch(cmd *cobra.Command, args []string) error {
	cfg, err := config.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	client, err := confluence.NewClient(cfg.ConfluenceSite)
	if err != nil {
		return fmt.Errorf("failed to create Confluence client: %w", err)
	}

	output.LogVerbose(verbose, "Searching Confluence with query: %s", confluenceSearchQuery)

	ctx := context.Background()
	documents, err := client.Search(ctx, confluenceSearchQuery, confluenceSearchSpace, confluenceSearchCQL, confluenceSearchLimit)
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

func runConfluenceGet(cmd *cobra.Command, args []string) error {
	input := args[0]

	cfg, err := config.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	client, err := confluence.NewClient(cfg.ConfluenceSite)
	if err != nil {
		return fmt.Errorf("failed to create Confluence client: %w", err)
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
			return processConfluenceInput(ctx, client, input, format)
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

func processConfluenceInput(ctx context.Context, client *confluence.Client, input string, format output.Format) error {
	var pageID string

	if strings.HasPrefix(input, "http") {
		info, err := parse.ParseConfluenceURL(input)
		if err != nil {
			return fmt.Errorf("failed to parse URL %s: %w", input, err)
		}
		pageID = info.PageID
	} else {
		if !parse.IsValidConfluencePageID(input) {
			return fmt.Errorf("invalid page ID: %s", input)
		}
		pageID = input
	}

	output.LogVerbose(verbose, "Fetching Confluence page: %s", pageID)

	doc, err := client.GetContent(ctx, pageID)
	if err != nil {
		return fmt.Errorf("failed to get content %s: %w", pageID, err)
	}

	if err := output.WriteDocument(doc, format); err != nil {
		return fmt.Errorf("failed to write document %s: %w", doc.ID, err)
	}

	return nil
}
