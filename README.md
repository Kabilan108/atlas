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
Atlas always prefers `ATLAS_API_TOKEN` when it is set, then falls back to saved
credentials from `~/.config/atlas/credentials.toml`.

Atlas appends this footer to PR descriptions and comments it submits:

```md
<!-- atlas:attribution -->
---
Submitted via Atlas CLI.
```

Disable it per command with `--no-attribution`, or globally with:

```bash
atlas config set attribution false
```

Create the token with these scopes:

```text
read:user:bitbucket
read:repository:bitbucket
write:repository:bitbucket
read:pullrequest:bitbucket
write:pullrequest:bitbucket
read:snippet:bitbucket
write:snippet:bitbucket
delete:snippet:bitbucket
```

Configure manually or through environment variables:

```bash
export ATLAS_API_TOKEN="your-token"
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
atlas pr close 123 --comment "Closing in favor of a newer PR"

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
