package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/config"
	"github.com/kabilan108/atlas/internal/git"
	"github.com/kabilan108/atlas/internal/jira"
	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/ops/pr"
	"github.com/spf13/cobra"
)

// cliEnv is the ops.Env for one CLI run: config file and env vars, stored
// credentials, the disk cache unless --no-cache, and git inference. Each
// client is built on first use, so Bitbucket commands never need Jira
// credentials.
type cliEnv struct {
	cmd       *cobra.Command
	cfg       *config.Config
	bitbucket func() (*bitbucket.Client, error)
	jira      func() (*jira.Client, error)
}

func newCLIEnv(cmd *cobra.Command) (*cliEnv, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}
	return &cliEnv{
		cmd: cmd,
		cfg: cfg,
		bitbucket: sync.OnceValues(func() (*bitbucket.Client, error) {
			return newBitbucketClient(cmd, true)
		}),
		jira: sync.OnceValues(func() (*jira.Client, error) {
			return newJiraClient(cmd, cfg)
		}),
	}, nil
}

func (e *cliEnv) DefaultWorkspace() string {
	return e.cfg.Workspace
}

func (e *cliEnv) Bitbucket(context.Context) (*bitbucket.Client, error) {
	return e.bitbucket()
}

func (e *cliEnv) Jira(context.Context) (*jira.Client, error) {
	return e.jira()
}

// newJiraClient builds a Jira client from the configured username (the
// Atlassian email), the Jira token, and jira_cloud_id.
func newJiraClient(cmd *cobra.Command, cfg *config.Config) (*jira.Client, error) {
	creds, err := config.JiraCredentials()
	if err != nil {
		return nil, fmt.Errorf("failed to load credentials: %w", err)
	}
	switch {
	case creds.Username == "":
		return nil, ops.Errorf(ops.Auth, "Jira needs your Atlassian email. Run 'atlas config set username <email>'")
	case creds.Token == "":
		return nil, ops.Errorf(ops.Auth, "Jira API token not configured. Set ATLAS_JIRA_TOKEN, or jira_token in the credentials file")
	case cfg.JiraCloudID == "":
		return nil, ops.Errorf(ops.Invalid, "Jira cloud ID not configured. Run 'atlas config set jira_cloud_id <id>' or set ATLAS_JIRA_CLOUD_ID")
	}
	var opts []jira.Option
	if httpClient := optionsFrom(cmd).httpClient; httpClient != nil {
		opts = append(opts, jira.WithHTTPClient(httpClient))
	}
	return jira.NewClientWithCredentials(creds.Username, creds.Token, cfg.JiraCloudID, opts...)
}

// newBitbucketClient builds a client from the stored credentials. Reads use
// the disk cache unless --no-cache is set or useCache is false; writes clear
// it either way, so later cached reads see them.
func newBitbucketClient(cmd *cobra.Command, useCache bool) (*bitbucket.Client, error) {
	creds, err := config.BitbucketCredentials()
	if err != nil {
		return nil, fmt.Errorf("failed to load credentials: %w", err)
	}
	opts := bitbucketOptions(cmd)
	if dir, err := cacheDir(); err == nil {
		cache := bitbucket.NewCache(dir)
		if noCache || !useCache {
			cache = cache.InvalidateOnly()
		}
		opts = append(opts, bitbucket.WithCache(cache))
	}
	return bitbucket.NewClientWithCredentials(creds.Username, creds.Token, opts...)
}

// bitbucketOptions are the options every CLI client gets, before the cache.
func bitbucketOptions(cmd *cobra.Command) []bitbucket.ClientOption {
	if httpClient := optionsFrom(cmd).httpClient; httpClient != nil {
		return []bitbucket.ClientOption{bitbucket.WithHTTPClient(httpClient)}
	}
	return nil
}

func cacheDir() (string, error) {
	dir := os.Getenv("XDG_CACHE_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".cache")
	}
	return filepath.Join(dir, "atlas"), nil
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
	if ref.RepoRef.Repo, err = e.repository(cmd); err != nil {
		return pr.Ref{}, err
	}
	if sel.IsEmpty() {
		if ref.CurrentBranch, err = git.CurrentBranch(); err != nil {
			return pr.Ref{}, err
		}
	}
	return ref, nil
}

// requiredPRRef is prRef for commands that must not fall back to the
// current branch, such as close: an empty or blank selector is an error.
func (e *cliEnv) requiredPRRef(cmd *cobra.Command, selector string) (pr.Ref, error) {
	sel, err := pr.ParseSelector(selector)
	if err != nil {
		return pr.Ref{}, err
	}
	if sel.IsEmpty() {
		if _, err := e.repository(cmd); err != nil {
			return pr.Ref{}, err
		}
		return pr.Ref{}, fmt.Errorf("pull request selector is required")
	}
	return e.prRef(cmd, selector)
}
