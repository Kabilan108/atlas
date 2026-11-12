<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/logo-dark.svg">
    <img src="assets/logo.svg" alt="Atlas" width="400">
  </picture>
</p>

# Atlas

CLI for Bitbucket Cloud PR review workflows.

## Installation

### Install Script

```bash
curl -fsSL https://raw.githubusercontent.com/kabilan108/atlas/master/install.sh | sh
```

The script installs the latest standalone release binary to `~/.local/bin/atlas`
by default. After installing this way, update with:

```bash
atlas update
```

### Nix Flake

Add to your flake inputs:

```nix
inputs.atlas.url = "github:kabilan108/atlas";
```

Install the package:

```nix
# In environment.systemPackages or home.packages:
inputs.atlas.packages.${system}.default
```

Optionally use the home-manager module for config file generation:

```nix
imports = [ inputs.atlas.homeManagerModules.default ];
programs.atlas = {
  enable = true;
  settings = {
    workspace = "my-workspace";
    username = "user@example.com";
  };
};
```

### Go

```bash
go install github.com/kabilan108/atlas/cmd/atlas@latest
```

`atlas update` only supports standalone release binaries installed by the install
script or an equivalent manual download. Package-manager installs should be
updated through the package manager.

## Recommended Dependencies

Atlas is distributed as a standalone binary, but some commands call external
shell tools when they are available:

| Tool | Recommended | Used for |
|------|-------------|----------|
| `bat` | Yes | Preferred interactive markdown and snippet viewer. |
| `delta` | Optional | Preferred interactive PR diff viewer for normal diffs. |
| `difft` | Optional | Structured PR diffs via `atlas pr diff --structured`; provided by difftastic. |

## Configuration

Run `atlas login` and follow the prompts to create and save a Bitbucket API token.
Atlas reads each product's token from the environment first, then from the
credentials file:

| Token | Environment | Credentials file key |
|-------|-------------|----------------------|
| Bitbucket | `ATLAS_BITBUCKET_TOKEN`, then `ATLAS_API_TOKEN` | `api_token` |
| Jira | `ATLAS_JIRA_TOKEN` | `jira_token` |

The credentials file is `~/.config/atlas/credentials.toml` (mode 600), or the path
in `ATLAS_CREDENTIALS_FILE`, for example a file your secrets manager decrypts.
Keeping tokens in that file keeps them out of the environment of every process an
agent starts. Jira also needs `ATLAS_JIRA_CLOUD_ID` or `atlas config set jira_cloud_id <id>`.

Create the Bitbucket token (app: Bitbucket) with these scopes:

```text
read:user:bitbucket
read:workspace:bitbucket
read:repository:bitbucket
read:pullrequest:bitbucket
write:pullrequest:bitbucket
read:pipeline:bitbucket
write:pipeline:bitbucket
read:snippet:bitbucket
write:snippet:bitbucket
delete:snippet:bitbucket
```

Create the Jira token (app: Jira, classic scopes):

```text
read:jira-work
write:jira-work
read:jira-user
read:servicedesk-request
write:servicedesk-request
```

Configure manually or through environment variables:

```bash
export ATLAS_BITBUCKET_TOKEN="your-token"
export ATLAS_USERNAME="user@example.com"

atlas config set workspace my-workspace
atlas config set username user@example.com

atlas config verify
```

## Usage

```bash
# List PRs (auto-detects repo from git remote)
atlas pr list
atlas pr list --state merged --author johndoe

# Create, inspect, and update PRs
atlas pr create --title "Fix auth flow" --body-file PR.md --reviewer alice --push
atlas pr status
atlas pr view --comments              # current branch PR
atlas pr view --raw                   # raw markdown, no pager
atlas pr diff -s                      # difftastic when available, delta fallback
atlas pr edit --title "Fix auth flow" # opens body in $EDITOR/nvim when no body flag is passed
atlas pr comment 123 --body "Looks good overall"
atlas pr comment 123 --path internal/cli/pr.go --line 42 --body "Can this be simplified?"
atlas pr comment 123 --reply-to 456 --body "Fixed in the latest push"
atlas pr approve 123 --body "Reviewed locally"
atlas pr request-changes 123 --body-file review.md
atlas pr review 123 --request-changes --comment-spec comments.json --body "Please address these before merge"

# Checkout PR branch locally
atlas pr checkout 123
atlas pr close 123 --body "Closing in favor of a newer PR"

# Snippets
atlas snippet list
atlas snippet create --title "My snippet" file.go
atlas snippet view abc123
atlas snippet edit abc123 file.go
atlas snippet clone abc123
```

**Flags:** `--json` for structured output, `--no-cache` to bypass cache, `-v` for verbose.

## License

Apache 2.0
