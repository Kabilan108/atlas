package main

import (
	"fmt"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/confluence"
	"github.com/kabilan108/atlas/internal/parse"
	"github.com/spf13/cobra"
)

func newGetCmd() *cobra.Command {
	var includeDiff bool

	cmd := &cobra.Command{
		Use:   "get <url|->",
		Short: "Fetch content by URL or identifier",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			inputs, err := readInputs(cmd, args)
			if err != nil {
				return err
			}

			var confluenceInputs []string
			var bitbucketInputs []string

			for _, input := range inputs {
				if _, err := parse.ConfluencePageID(input); err == nil {
					confluenceInputs = append(confluenceInputs, input)
					continue
				}
				if _, err := parse.ParsePullRequestRef(input); err == nil {
					bitbucketInputs = append(bitbucketInputs, input)
					continue
				}
				return fmt.Errorf("unable to determine handler for %q", input)
			}

			ctx := cmd.Context()

			if len(confluenceInputs) > 0 {
				doer, err := getHTTPClient()
				if err != nil {
					return err
				}
				client, err := confluence.NewClient(doer, runtime.config)
				if err != nil {
					return err
				}
				if err := fetchConfluencePages(ctx, client, confluenceInputs); err != nil {
					return err
				}
			}

			if len(bitbucketInputs) > 0 {
				doer, err := getHTTPClient()
				if err != nil {
					return err
				}
				client, err := bitbucket.NewClient(doer)
				if err != nil {
					return err
				}
				if err := fetchBitbucketPullRequests(ctx, client, bitbucketInputs, includeDiff); err != nil {
					return err
				}
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&includeDiff, "diff", false, "Include diff when fetching Bitbucket pull requests")
	return cmd
}
