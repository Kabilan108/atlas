package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/document"
	"github.com/kabilan108/atlas/internal/parse"
	"github.com/kabilan108/atlas/internal/worker"
	"github.com/spf13/cobra"
)

func newBitbucketCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bitbucket",
		Short: "Work with Bitbucket content",
	}

	cmd.AddCommand(newBitbucketSearchCmd())
	cmd.AddCommand(newBitbucketGetCmd())
	return cmd
}

func newBitbucketSearchCmd() *cobra.Command {
	var searchType string
	var query string
	var workspace string
	var limit int
	var repo string
	var includeClosed bool

	cmd := &cobra.Command{
		Use:   "search",
		Short: "Search Bitbucket repositories or pull requests",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(query) == "" {
				return fmt.Errorf("query is required")
			}
			if workspace == "" {
				workspace = runtime.config.Workspace
			}
			if strings.TrimSpace(workspace) == "" {
				return fmt.Errorf("workspace is required")
			}

			doer, err := getHTTPClient()
			if err != nil {
				return err
			}
			client, err := bitbucket.NewClient(doer)
			if err != nil {
				return err
			}

			ctx := cmd.Context()
			switch searchType {
			case "repos":
				verbosef("Searching Bitbucket repos in %s for %q", workspace, query)
				repos, err := client.ListRepositories(ctx, workspace, 0)
				if err != nil {
					return err
				}

				matcher := strings.ToLower(query)
				count := 0
				for _, repoItem := range repos {
					if !strings.Contains(strings.ToLower(repoItem.Name), matcher) &&
						!strings.Contains(strings.ToLower(repoItem.Slug), matcher) &&
						!strings.Contains(strings.ToLower(repoItem.FullName), matcher) {
						continue
					}
					doc := document.Document{
						Title:     repoItem.Name,
						URL:       repoItem.WebURL,
						ID:        repoItem.Slug,
						Source:    "bitbucket",
						Workspace: workspace,
						Repo:      repoItem.Slug,
						Body:      repoItem.FullName,
					}
					if err := printDocument(doc); err != nil {
						return err
					}
					count++
					if limit > 0 && count >= limit {
						break
					}
				}
				return nil
			case "prs":
				verbosef("Searching Bitbucket PRs in %s for %q", workspace, query)
				opts := bitbucket.SearchPROptions{
					Query:         query,
					Repo:          repo,
					Limit:         limit,
					IncludeClosed: includeClosed,
				}
				prs, err := client.SearchPullRequests(ctx, workspace, opts)
				if err != nil {
					return err
				}

				for _, pr := range prs {
					doc := document.Document{
						Title:     fmt.Sprintf("%s (#%d)", pr.Title, pr.ID),
						URL:       pr.WebURL,
						ID:        fmt.Sprintf("%d", pr.ID),
						Source:    "bitbucket",
						Workspace: pr.Workspace,
						Repo:      pr.RepoSlug,
						Author:    pr.Author,
						UpdatedAt: pr.Updated,
						Body:      pr.Description,
					}
					if err := printDocument(doc); err != nil {
						return err
					}
				}
				return nil
			default:
				return fmt.Errorf("unknown search type %q", searchType)
			}
		},
	}

	cmd.Flags().StringVar(&searchType, "type", "repos", "Search target: repos or prs")
	cmd.Flags().StringVarP(&query, "query", "q", "", "Search query string")
	cmd.Flags().StringVar(&workspace, "workspace", "", "Bitbucket workspace (defaults to config)")
	cmd.Flags().IntVarP(&limit, "limit", "l", 10, "Limit number of results (0 for all)")
	cmd.Flags().StringVar(&repo, "repo", "", "Restrict PR search to a specific repository slug")
	cmd.Flags().BoolVar(&includeClosed, "include-closed", false, "Include closed pull requests in results")
	cmd.MarkFlagRequired("query")
	return cmd
}

func newBitbucketGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get",
		Short: "Fetch Bitbucket resources",
	}

	cmd.AddCommand(newBitbucketGetPRCmd())
	return cmd
}

func newBitbucketGetPRCmd() *cobra.Command {
	var includeDiff bool

	cmd := &cobra.Command{
		Use:   "pr <url|workspace/repo#id|->",
		Short: "Fetch Bitbucket pull request details",
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
			client, err := bitbucket.NewClient(doer)
			if err != nil {
				return err
			}

			return fetchBitbucketPullRequests(cmd.Context(), client, inputs, includeDiff)
		},
	}

	cmd.Flags().BoolVar(&includeDiff, "diff", false, "Include the diff in the output")
	return cmd
}

func fetchBitbucketPullRequests(ctx context.Context, client *bitbucket.Client, inputs []string, includeDiff bool) error {
	pool := worker.New(ctx, runtime.concurrency)

	for _, input := range inputs {
		input := input
		poolErr := pool.Submit(func(ctx context.Context) error {
			ref, err := parse.ParsePullRequestRef(input)
			if err != nil {
				return fmt.Errorf("parse pull request reference %q: %w", input, err)
			}
			verbosef("Fetching Bitbucket PR %s/%s#%d", ref.Workspace, ref.RepoSlug, ref.ID)
			pr, err := client.GetPullRequest(ctx, ref, includeDiff)
			if err != nil {
				return err
			}

			body := pr.Description
			if includeDiff && pr.Diff != "" {
				body = strings.TrimSpace(body)
				if body != "" {
					body += "\n\n"
				}
				body += "```diff\n" + strings.TrimSuffix(pr.Diff, "\n") + "\n```"
			}

			doc := document.Document{
				Title:     fmt.Sprintf("%s (#%d)", pr.Title, pr.ID),
				URL:       pr.WebURL,
				ID:        fmt.Sprintf("%d", pr.ID),
				Source:    "bitbucket",
				Workspace: pr.Workspace,
				Repo:      pr.RepoSlug,
				Author:    pr.Author,
				UpdatedAt: pr.Updated,
				Body:      body,
				Path:      fmt.Sprintf("%s -> %s", pr.SourceBranch, pr.DestinationBranch),
			}

			return printDocument(doc)
		})

		if poolErr != nil {
			return poolErr
		}
	}

	return pool.Wait()
}
