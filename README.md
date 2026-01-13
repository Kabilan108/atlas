# Atlas

CLI for Bitbucket Cloud PR review workflows.

## Installation

### Nix Flake

Add to your flake inputs:

```nix
inputs.atlas.url = "github:kabilan108/atlas";
```

Then either:

```nix
# Option A: Direct package reference
home.packages = [ inputs.atlas.packages.${system}.default ];

# Option B: Home-manager module (generates config file for you)
imports = [ inputs.atlas.homeManagerModules.default ];
programs.atlas = {
  enable = true;
  settings = {
    workspace = "my-workspace";
    username = "user@example.com";
    app_password = "\${env:ATLAS_APP_PASSWORD}";
  };
};
```

### Go

```bash
go install github.com/kabilan108/atlas/cmd/atlas@latest
```

## Configuration

Create a Bitbucket API token at **Personal settings → App passwords** with scopes:

| Scope | Required |
|-------|----------|
| `read:user:bitbucket` | Yes |
| `read:repository:bitbucket` | Yes |
| `read:pullrequest:bitbucket` | Yes |
| `read:snippet:bitbucket` | For snippets |
| `write:snippet:bitbucket` | For snippets |
| `delete:snippet:bitbucket` | For snippets |

If not using the home-manager module, configure manually:

```bash
export ATLAS_APP_PASSWORD="your-token"

atlas config set workspace my-workspace
atlas config set username user@example.com
atlas config set app_password '${env:ATLAS_APP_PASSWORD}'

atlas config verify
```

## Usage

```bash
# List PRs (auto-detects repo from git remote)
atlas pr list
atlas pr list --state merged --author johndoe

# View PR with comments
atlas pr view 123 --comments
atlas pr view feature/auth --comments --all  # include resolved

# Checkout PR branch locally
atlas pr checkout 123

# Snippets
atlas snippet list
atlas snippet create --title "My snippet" -f file.go
atlas snippet view abc123 --contents
```

**Flags:** `--json` for structured output, `--no-cache` to bypass cache, `-v` for verbose.

## License

Apache 2.0
