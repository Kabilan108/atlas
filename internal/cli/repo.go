package cli

import (
	"fmt"
	"io"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/ops/repo"
	"github.com/spf13/cobra"
)

func newRepoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "repo",
		Short: "Work with repositories",
	}
	cmd.AddCommand(newRepoListCmd())
	return cmd
}

func newRepoListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List repositories in a workspace",
		Args:    cobra.NoArgs,
	}
	cmd.Flags().String("workspace", "", "Target workspace")
	cmd.Flags().String("name", "", "Only repositories whose name contains this")
	cmd.Flags().IntP("limit", "L", 100, "Maximum number of repositories to list")
	return bind(cmd, binding[repo.ListInput, *repo.ListResult]{
		op: repo.List,
		input: func(cmd *cobra.Command, _ []string, _ *cliEnv) (repo.ListInput, error) {
			workspace, _ := cmd.Flags().GetString("workspace")
			name, _ := cmd.Flags().GetString("name")
			limit, err := positiveLimit(cmd)
			return repo.ListInput{Workspace: workspace, Name: name, Limit: limit}, err
		},
	})
}

func newBranchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "branch",
		Short: "Work with repository branches",
	}
	cmd.AddCommand(newBranchListCmd())
	cmd.AddCommand(newBranchViewCmd())
	return cmd
}

func newBranchListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List branches, most recent commit first",
		Args:    cobra.NoArgs,
	}
	addPRRepoFlag(cmd)
	cmd.Flags().String("filter", "", "Only branches whose name contains this")
	cmd.Flags().IntP("limit", "L", 30, "Maximum number of branches to list")
	return bind(cmd, binding[repo.BranchListInput, *repo.BranchListResult]{
		op: repo.BranchList,
		input: func(cmd *cobra.Command, _ []string, env *cliEnv) (repo.BranchListInput, error) {
			ref, err := repoRef(cmd, env)
			if err != nil {
				return repo.BranchListInput{}, err
			}
			filter, _ := cmd.Flags().GetString("filter")
			limit, err := positiveLimit(cmd)
			return repo.BranchListInput{RepoRef: ref, Filter: filter, Limit: limit}, err
		},
	})
}

func newBranchViewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "view <branch>",
		Short: "Show a branch's head commit and merge strategy; fails if it does not exist",
		Args:  cobra.ExactArgs(1),
	}
	addPRRepoFlag(cmd)
	return bind(cmd, binding[repo.BranchViewInput, *repo.BranchViewResult]{
		op: repo.BranchView,
		input: func(cmd *cobra.Command, args []string, env *cliEnv) (repo.BranchViewInput, error) {
			ref, err := repoRef(cmd, env)
			return repo.BranchViewInput{RepoRef: ref, Branch: args[0]}, err
		},
		after: func(_ *cobra.Command, out *repo.BranchViewResult) error {
			if !out.Exists {
				return ops.Errorf(ops.NotFound, "branch %s not found in %s", out.Name, out.Repo)
			}
			return nil
		},
	})
}

func newCommitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "commit",
		Short: "Work with commits",
	}
	cmd.AddCommand(newCommitListCmd())
	return cmd
}

func newCommitListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list [<ref>]",
		Aliases: []string{"ls"},
		Short:   "List commits on a ref (default: the main branch), newest first",
		Args:    cobra.MaximumNArgs(1),
	}
	addPRRepoFlag(cmd)
	cmd.Flags().String("base", "", "Only commits not reachable from this ref")
	cmd.Flags().String("path", "", "Only commits touching this path")
	cmd.Flags().IntP("limit", "L", 30, "Maximum number of commits to list")
	return bind(cmd, binding[repo.CommitListInput, *repo.CommitListResult]{
		op: repo.CommitList,
		input: func(cmd *cobra.Command, args []string, env *cliEnv) (repo.CommitListInput, error) {
			ref, err := repoRef(cmd, env)
			if err != nil {
				return repo.CommitListInput{}, err
			}
			base, _ := cmd.Flags().GetString("base")
			path, _ := cmd.Flags().GetString("path")
			limit, err := positiveLimit(cmd)
			return repo.CommitListInput{RepoRef: ref, Ref: optionalArg(args), Base: base, Path: path, Limit: limit}, err
		},
	})
}

func newFileCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "file",
		Short: "Read repository files",
	}
	cmd.AddCommand(newFileReadCmd())
	return cmd
}

func newFileReadCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "read <path>",
		Short: "Print a file at a ref, or the same file from several repositories",
		Long: `Print a file at a ref (default: the main branch).

With --repos, read the same path from each listed repository (workspace/repo,
repo, or workspace/* for every repository) and print one section per
repository. A repository whose file could not be read is reported in its
section, and the command then exits non-zero.`,
		Args: cobra.ExactArgs(1),
	}
	addPRRepoFlag(cmd)
	cmd.Flags().String("ref", "", "Branch, tag, or commit (default: the main branch)")
	cmd.Flags().StringSlice("repos", nil, "Read from these repositories instead (comma-separated or repeated)")
	cmd.Flags().Int("max-bytes", 0, "Per-file byte cap (one file: default and max 4194304; --repos: default 8192, max 1048576)")
	return bind(cmd, binding[repo.FileReadInput, *repo.FileReadResult]{
		op: repo.FileRead,
		input: func(cmd *cobra.Command, args []string, env *cliEnv) (repo.FileReadInput, error) {
			ref, _ := cmd.Flags().GetString("ref")
			repos, _ := cmd.Flags().GetStringSlice("repos")
			maxBytes, _ := cmd.Flags().GetInt("max-bytes")
			in := repo.FileReadInput{Repos: repos, Path: args[0], Ref: ref, MaxBytes: maxBytes}
			if len(repos) > 0 {
				if repoFlag, _ := cmd.Flags().GetString("repo"); repoFlag != "" {
					return repo.FileReadInput{}, fmt.Errorf("use -R or --repos, not both")
				}
				return in, nil
			}
			if !cmd.Flags().Changed("max-bytes") {
				in.MaxBytes = repo.HardMaxFileBytes
			}
			var err error
			in.RepoRef, err = repoRef(cmd, env)
			return in, err
		},
		human: func(w io.Writer, out *repo.FileReadResult) error {
			return writeFileRead(w, cmd.ErrOrStderr(), out)
		},
	})
}

// writeFileRead prints a single text file as is, like cat, with any
// truncation notice on stderr; batches and binary files get the markdown
// summary.
func writeFileRead(w, stderr io.Writer, out *repo.FileReadResult) error {
	if !out.Batch() && len(out.Results) == 1 && out.Results[0].Status == repo.FileOK && !out.Results[0].Binary {
		entry := out.Results[0]
		if _, err := io.WriteString(w, entry.Content); err != nil {
			return err
		}
		if entry.Truncated {
			fmt.Fprintf(stderr, "File truncated at %d bytes (--max-bytes)\n", entry.Bytes)
		}
		return nil
	}
	markdown, err := out.Markdown()
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, markdown)
	return err
}

func newUserCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "user",
		Short: "Find workspace members",
	}
	cmd.AddCommand(newUserSearchCmd())
	return cmd
}

func newUserSearchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Find workspace members by name and show their account IDs and UUIDs",
		Args:  cobra.ExactArgs(1),
	}
	cmd.Flags().String("workspace", "", "Target workspace")
	cmd.Flags().IntP("limit", "L", 20, "Maximum number of members to list")
	return bind(cmd, binding[repo.UserSearchInput, *repo.UserSearchResult]{
		op: repo.UserSearch,
		input: func(cmd *cobra.Command, args []string, _ *cliEnv) (repo.UserSearchInput, error) {
			workspace, _ := cmd.Flags().GetString("workspace")
			limit, err := positiveLimit(cmd)
			return repo.UserSearchInput{Workspace: workspace, Query: args[0], Limit: limit}, err
		},
	})
}

func newWhoamiCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "whoami",
		Short: "Show the Bitbucket account your credentials belong to",
		Args:  cobra.NoArgs,
	}
	return bind(cmd, binding[repo.WhoamiInput, *bitbucket.User]{
		op: repo.Whoami,
		input: func(*cobra.Command, []string, *cliEnv) (repo.WhoamiInput, error) {
			return repo.WhoamiInput{}, nil
		},
	})
}

// repoRef names the repository from -R, the configured workspace, and the
// git origin remote.
func repoRef(cmd *cobra.Command, env *cliEnv) (ops.RepoRef, error) {
	name, err := env.repository(cmd)
	return ops.RepoRef{Repo: name}, err
}

func positiveLimit(cmd *cobra.Command) (int, error) {
	limit, _ := cmd.Flags().GetInt("limit")
	if limit < 1 {
		return 0, fmt.Errorf("--limit must be greater than 0")
	}
	return limit, nil
}
