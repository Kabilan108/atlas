package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/config"
	"github.com/kabilan108/atlas/internal/git"
	"github.com/kabilan108/atlas/internal/ops/pr"
	"github.com/spf13/cobra"
)

// cliEnv is the ops.Env for one CLI run: config file and env vars, stored
// credentials, the disk cache unless --no-cache, and git inference.
type cliEnv struct {
	cfg    *config.Config
	client *bitbucket.Client
}

func newCLIEnv() (*cliEnv, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}
	return &cliEnv{cfg: cfg}, nil
}

func (e *cliEnv) DefaultWorkspace() string {
	return e.cfg.Workspace
}

func (e *cliEnv) Bitbucket(context.Context) (*bitbucket.Client, error) {
	if e.client != nil {
		return e.client, nil
	}
	client, err := bitbucket.NewClient(bitbucket.WithNoCache(noCache))
	if err != nil {
		return nil, err
	}
	e.client = client
	return client, nil
}

// repository turns -R, the configured workspace, and the git origin remote
// into workspace/repo, in that order of precedence.
func (e *cliEnv) repository(cmd *cobra.Command) (string, error) {
	repoFlag, _ := cmd.Flags().GetString("repo")
	workspace := e.cfg.Workspace
	repo := ""
	if repoFlag != "" {
		parts := strings.Split(repoFlag, "/")
		switch len(parts) {
		case 1:
			repo = parts[0]
		case 2:
			workspace = parts[0]
			repo = parts[1]
		default:
			return "", fmt.Errorf("invalid repository %q: expected repo or workspace/repo", repoFlag)
		}
	}
	if repo == "" {
		inferredWorkspace, inferredRepo, err := git.InferRepository()
		if err != nil {
			return "", fmt.Errorf("could not infer repository: %w\nUse -R to specify", err)
		}
		if workspace == "" {
			workspace = inferredWorkspace
		}
		repo = inferredRepo
		if verbose {
			fmt.Fprintf(os.Stderr, "Using repository: %s/%s\n", workspace, repo)
		}
	}
	if workspace == "" {
		return "", fmt.Errorf("workspace not configured. Run 'atlas config set workspace <name>' or use -R workspace/repo")
	}
	return workspace + "/" + repo, nil
}

// prRef names the pull request for a selector argument. A URL names its own
// repository, so it needs neither -R nor git; an empty selector means the
// current branch.
func (e *cliEnv) prRef(cmd *cobra.Command, selector string) (pr.Ref, error) {
	sel, err := pr.ParseSelector(selector)
	if err != nil {
		return pr.Ref{}, err
	}
	ref := pr.Ref{PR: selector}
	if sel.IsURL() {
		return ref, nil
	}
	if ref.Repo, err = e.repository(cmd); err != nil {
		return pr.Ref{}, err
	}
	if sel.IsEmpty() {
		if ref.CurrentBranch, err = git.CurrentBranch(); err != nil {
			return pr.Ref{}, err
		}
	}
	return ref, nil
}
