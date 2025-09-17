package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/kabilan108/atlas/internal/confluence"
	"github.com/kabilan108/atlas/internal/document"
	"github.com/kabilan108/atlas/internal/parse"
	"github.com/kabilan108/atlas/internal/worker"
	"github.com/spf13/cobra"
)

func newConfluenceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "confluence",
		Short: "Work with Confluence content",
	}

	cmd.AddCommand(newConfluenceSearchCmd())
	cmd.AddCommand(newConfluenceGetCmd())
	return cmd
}

func newConfluenceSearchCmd() *cobra.Command {
	var query string
	var isCQL bool
	var space string
	var limit int

	cmd := &cobra.Command{
		Use:   "search",
		Short: "Search Confluence content",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(query) == "" {
				return fmt.Errorf("query is required")
			}

			doer, err := getHTTPClient()
			if err != nil {
				return err
			}
			client, err := confluence.NewClient(doer, runtime.config)
			if err != nil {
				return err
			}

			opts := confluence.SearchOptions{
				Query: query,
				CQL:   isCQL,
				Space: space,
				Limit: limit,
			}

			ctx := cmd.Context()
			verbosef("Searching Confluence with query %q", query)
			results, err := client.SearchPages(ctx, opts)
			if err != nil {
				return err
			}

			for _, result := range results {
				doc := document.Document{
					Title:     result.Title,
					URL:       result.WebURL,
					ID:        result.ID,
					Source:    result.Source,
					Space:     result.SpaceKey,
					Body:      fmt.Sprintf("[%s](%s)", result.Title, result.WebURL),
					Author:    "",
					Repo:      "",
					Path:      "",
					Workspace: runtime.config.Workspace,
				}
				if err := printDocument(doc); err != nil {
					return err
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&query, "query", "q", "", "Search query (text or CQL)")
	cmd.Flags().BoolVar(&isCQL, "cql", false, "Interpret the query as raw CQL")
	cmd.Flags().StringVar(&space, "space", "", "Restrict search to a specific space")
	cmd.Flags().IntVarP(&limit, "limit", "l", 10, "Limit number of results (0 for all)")
	cmd.MarkFlagRequired("query")
	return cmd
}

func newConfluenceGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <url|id|->",
		Short: "Fetch Confluence pages by URL or ID",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			inputs, err := readInputs(cmd, args)
			if err != nil {
				return err
			}

			doer, err := getHTTPClient()
			if err != nil {
				return err
			}
			client, err := confluence.NewClient(doer, runtime.config)
			if err != nil {
				return err
			}

			return fetchConfluencePages(cmd.Context(), client, inputs)
		},
	}

	return cmd
}

func fetchConfluencePages(ctx context.Context, client *confluence.Client, inputs []string) error {
	pool := worker.New(ctx, runtime.concurrency)

	for _, input := range inputs {
		input := input
		poolErr := pool.Submit(func(ctx context.Context) error {
			pageID, err := parse.ConfluencePageID(input)
			if err != nil {
				return fmt.Errorf("parse confluence reference %q: %w", input, err)
			}
			verbosef("Fetching Confluence page %s", pageID)
			page, err := client.GetPage(ctx, pageID)
			if err != nil {
				return fmt.Errorf("fetch page %s: %w", pageID, err)
			}

			doc := document.Document{
				Title:     page.Title,
				URL:       page.WebURL,
				ID:        page.ID,
				Source:    page.Source,
				Space:     page.SpaceKey,
				Author:    page.Author,
				UpdatedAt: page.Updated,
				Body:      page.Markdown,
			}

			return printDocument(doc)
		})

		if poolErr != nil {
			return poolErr
		}
	}

	return pool.Wait()
}
