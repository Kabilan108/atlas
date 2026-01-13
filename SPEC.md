# Atlas CLI Specification

A CLI tool for interacting with Bitbucket Cloud, focused on PR review workflows.

## Overview

Atlas enables fetching PR comments and review feedback from Bitbucket Cloud in a format optimized for Claude Code agents to address reviewer comments directly.

## Authentication

- **Method**: Bitbucket App Password
- **Storage**: Config file only (`~/.config/atlas/config.toml`)
- **Secret handling**: Use `${env:VAR_NAME}` syntax in config for sensitive values
- **Supported env expansion**: Only `app_password` field supports `${env:}` syntax
- **Validation**: Env vars referenced via `${env:}` are validated eagerly on startup

## Configuration

### Config File

Location: `~/.config/atlas/config.toml`

```toml
workspace = "mycompany"
username = "user@example.com"
app_password = "${env:ATLAS_APP_PASSWORD}"
```

### Config Precedence

1. Command-line flags (highest)
2. Config file with env expansion
3. Defaults (lowest)

### Setting Credentials

```bash
atlas config set workspace mycompany
atlas config set username user@example.com
atlas config set app_password  # prompts interactively (hidden input)
echo $TOKEN | atlas config set app_password  # or via stdin
```

When storing `app_password` directly in config (not via `${env:}`), Atlas displays a security warning.

### Verifying Credentials

```bash
atlas config verify  # calls /user endpoint to verify auth works
```

## Command Structure

```
atlas pr list [--repo <repo>] [--all] [--state <state>] [--author <author>] [--reviewer <reviewer>]
atlas pr view <id|branch> [--repo <repo>] [--comments] [--all] [--json]
atlas pr checkout <id|branch> [--repo <repo>]
atlas snippet list [--workspace <workspace>]
atlas snippet view <id> [--contents]
atlas snippet create --title <title> -f <file> [-f <file>...]
atlas snippet update <id> [-f <file>...] [-r <file>...]
atlas snippet delete <id>
atlas config set <key> [<value>]
atlas config get <key> [--verbose]
atlas config verify
```

### Global Flags

- `--no-cache`: Bypass disk cache entirely
- `--verbose` / `-v`: Show inferred values (repo from git remote, etc.)

---

## Behaviors

### Repository Inference

When `--repo` is omitted, Atlas walks up from CWD to find `.git` directory and extracts workspace/repo from the `origin` remote. Use `-v` to see what was inferred.

### Branch Name Resolution

PR commands accept branch names in addition to numeric IDs:
- `atlas pr view feature/auth` resolves to the PR for that branch
- When multiple PRs exist for a branch, prefers most recent open PR

### Output Format

- Always outputs markdown unless `--json` is passed
- `--json` outputs complete structured data (no field selection)
- Non-TTY detection: markdown output preserved, but interactive prompts disabled

### Caching

- Location: XDG cache directory
- TTL: 5 minutes (uniform for all data types)
- Bypass: `--no-cache` global flag
- No user-facing cache management commands (internal implementation detail)

### Rate Limiting

- Configurable retry behavior via `--retry` flag
- Default: report limit and exit
- With `--retry`: wait and retry with backoff

### Exit Codes

POSIX-style exit codes:
- 0: Success
- 1: General error
- Standard errno-like codes for specific failures (ENOENT for not found, EACCES for auth, etc.)

### Non-Interactive Mode

Auto-detected when stdin is not a TTY. Interactive prompts are disabled; commands that would require input exit with an error instead.

---

## PR List Command

`atlas pr list` shows PRs in a repository or across workspace.

### Flags

- `--repo <repo>`: Target repository (inferred from git if omitted)
- `--all`: List PRs across all repos in workspace (ignores --repo)
- `--state <state>`: Filter by state: `open` (default), `merged`, `declined`, `superseded`
- `--author <author>`: Filter by author username
- `--reviewer <reviewer>`: Filter by reviewer username

### Output

Tabular format: `ID | Title | Author | State | Updated | Comments`

Comments column only appears if any PR has unresolved comments, showing `total/unresolved` count.

---

## PR View Command

`atlas pr view <id|branch>` shows PR details.

### Flags

- `--repo <repo>`: Target repository
- `--comments`: Include all comments
- `--all`: Include resolved comments (only with --comments)
- `--json`: Output as JSON

### Output Format (Markdown)

```markdown
# PR #123: Fix authentication bug

**Author**: @johndoe
**State**: OPEN
**Branch**: feature/auth-fix → main
**Reviewers**: @alice (approved), @bob (changes_requested), @carol (pending)

## Description

<PR description body>

12 comments (3 unresolved), 2 tasks
```

### Reviewers Display

- Shows all assigned reviewers
- Status: `approved`, `changes_requested`, `pending`
- Only shows most recent review action per reviewer

---

## PR Comments

`atlas pr view <id> --comments` shows all review comments.

### Filtering

- Default: unresolved comments only
- `--all`: include resolved comments

### Threading

- 2 levels max: parent comment + flat replies beneath
- Deeper nesting flattened to 2 levels

### Comment Types

Unified display - all comment types shown together, distinguished by presence of file/line info.

### Inline Comment Context

Shows relevant diff hunk (not full file) wrapped in markdown diff fence:

```markdown
#### `src/auth/login.go:45`

```diff
@@ -43,5 +43,5 @@
 func Login(ctx context.Context, creds Credentials) error {
     resp, err := client.Post(authURL, creds)
-    if err != nil {
+    if err != nil {  // ← comment here
         return err
     }
```

**@bob** (3 hours ago - 2024-01-15 14:30) [UNRESOLVED]:
> This error handling doesn't cover the timeout case.
```

### Multiple Comments on Same Line

Grouped under single file:line header with all comments beneath.

### HTML Content

Converted to markdown using html-to-markdown library.

### PR Author Attribution

Comments from PR author marked with `(author)` indicator.

### Timestamps

Format: `3 hours ago (2024-01-15 14:30)` - relative with absolute in parentheses.

---

## PR Tasks

Displayed in separate section after comments:

```markdown
## Tasks

- [ ] Add error handling for timeout case
- [x] Update documentation
- [ ] Add unit tests
```

Status only (no attribution for who completed).

---

## PR Checkout

`atlas pr checkout <id|branch>` fetches and checks out a PR branch locally.

- Uses the remote branch name as-is (no prefixing)
- Same-repo PRs only; fork PRs show error with manual instructions

---

## Snippet Commands

### List

`atlas snippet list` shows user's own snippets (not all workspace snippets).

### View

`atlas snippet view <id>` shows snippet metadata by default.

Add `--contents` flag to display file contents.

### Create

```bash
atlas snippet create --title "Auth helpers" -f src/auth.go -f src/auth_test.go
```

- Requires `-f` flags (no stdin support)
- `--private` flag (default): visible to workspace members only

### Update

```bash
atlas snippet update <id> -f src/auth.go -r old_file.go
```

- `-f <file>`: Add or update files (merge behavior)
- `-r <file>`: Remove files from snippet

### Delete

```bash
atlas snippet delete <id>
```

---

## JSON Output

`atlas pr view 123 --json` outputs complete PR data including comments, reviews, and tasks.

`atlas pr list --json` outputs PR list as JSON array.

Single `--json` flag outputs everything - no field selection.

---

## Error Handling

### Message Format

Actionable hints included:
- Auth errors: `Authentication failed. Run 'atlas config verify' to check credentials.`
- Not found: `PR #123 not found in repo mycompany/myrepo`
- Rate limits: Display reset time and suggest `--retry` flag

### Network Errors

Retry with backoff based on `--retry` flag, then fail with actionable suggestion.

---

## Implementation Phases

### Phase 1: Core Infrastructure

**Goal**: CLI skeleton with configuration management.

**Deliverables**:
- Cobra command structure (`root`, `pr`, `config` commands)
- Viper config loading from `~/.config/atlas/config.toml`
- `${env:VAR}` expansion for `app_password` field
- Eager validation of env var references
- `atlas config set/get` commands
- `atlas config verify` command
- Interactive password input with stdin fallback
- Security warning when storing password directly

**Testable outcome**: `atlas config set workspace mycompany` persists to config file, `atlas config get workspace` reads it back, `atlas config verify` tests credentials.

---

### Phase 1b: Nix Packaging (Parallel Track)

**Goal**: Proper flake outputs with home-manager module.

**Deliverables**:
- Update `flake.nix` to export `homeManagerModules.default`
- Create `nix/hm-module.nix` with `programs.atlas` options
- Shell completions auto-installed via package (static completions only)
- Optional overlay for `pkgs.atlas`

**Testable outcome**:
- `nix flake check` passes
- Adding `programs.atlas.enable = true` to home config installs atlas with completions
- `programs.atlas.settings.workspace = "foo"` generates valid config.toml

---

### Phase 2: Bitbucket API Client

**Goal**: HTTP client for Bitbucket Cloud REST API v2.0.

**Deliverables**:
- HTTP client with Basic Auth (username:app_password)
- Base URL: `https://api.bitbucket.org/2.0`
- Error handling with actionable hints
- Pagination support (Bitbucket uses `next` links)
- Disk cache with 5-minute TTL (XDG cache dir)
- `--no-cache` global flag support
- `--retry` flag for rate limit handling
- POSIX-style exit codes

**Endpoints needed**:
- `GET /repositories/{workspace}` - list repos
- `GET /repositories/{workspace}/{repo}/pullrequests` - list PRs
- `GET /repositories/{workspace}/{repo}/pullrequests/{id}` - PR details
- `GET /repositories/{workspace}/{repo}/pullrequests/{id}/comments` - PR comments
- `GET /repositories/{workspace}/{repo}/pullrequests/{id}/diff` - PR diff
- `GET /repositories/{workspace}/{repo}/src/{commit}/{path}` - file contents
- `GET /user` - auth verification

**Testable outcome**: Internal client can fetch and print raw JSON from `/user` endpoint.

---

### Phase 3: PR List Command

**Goal**: `atlas pr list` shows PRs.

**Deliverables**:
- `atlas pr list --repo <repo>` lists open PRs by default
- `--all` flag: list across all workspace repos
- `--state` flag: `open` (default), `merged`, `declined`, `superseded`
- `--author` flag: filter by author username
- `--reviewer` flag: filter by reviewer username
- Tabular output: `ID | Title | Author | State | Updated | [Comments]`
- Comments column appears only if any PR has unresolved comments

**Testable outcome**: `atlas pr list --repo myrepo` outputs a table of open PRs.

---

### Phase 4: PR View Command (Basic)

**Goal**: `atlas pr view <id>` shows PR metadata.

**Deliverables**:
- Accept PR ID or branch name
- Branch name resolution (prefer most recent open PR)
- Display PR title, description, author, state, source/destination branches
- Show all assigned reviewers with latest review status
- Comment/task count footer
- Markdown-formatted output

**Testable outcome**: `atlas pr view 123 --repo myrepo` displays formatted PR info with reviewer statuses.

---

### Phase 5: PR Comments (Core Feature)

**Goal**: `atlas pr view <id> --comments` shows all review comments.

**Deliverables**:
- Fetch all comments from PR (unified display)
- Filter to unresolved comments by default
- `--all` flag to include resolved comments
- 2-level threading (parent + flat replies)
- Group multiple comments on same line under single header
- Convert HTML content to markdown
- Mark PR author's comments with `(author)` indicator
- Timestamps: relative + absolute format

**Testable outcome**: `atlas pr view 123 --repo myrepo --comments` shows threaded comments with resolution status.

---

### Phase 6: Code Context for Inline Comments

**Goal**: Show diff hunk context around inline comments.

**Deliverables**:
- Display relevant diff hunk (not full file)
- Wrap in markdown diff code fence
- Handle deleted lines via diff hunk display

**Testable outcome**: Inline comments include surrounding diff context.

---

### Phase 7: PR Tasks

**Goal**: Display PR tasks in dedicated section.

**Deliverables**:
- Separate `## Tasks` section in output
- Checkbox format: `- [ ]` / `- [x]`
- Status only (no attribution)

**Testable outcome**: PR view shows tasks section with completion status.

---

### Phase 8: JSON Output

**Goal**: `--json` flag for structured output.

**Deliverables**:
- `atlas pr view 123 --json` outputs complete PR data
- `atlas pr list --json` outputs PR list as JSON array
- All-or-nothing output (no field selection)

**Testable outcome**: Output can be piped to `jq` for processing.

---

### Phase 9: Repository Inference

**Goal**: Infer repo from git remote when `--repo` omitted.

**Deliverables**:
- Walk up from CWD to find `.git` directory
- Parse `origin` remote to extract workspace/repo
- `--verbose` flag shows inferred values
- Error if no suitable remote found

**Testable outcome**: Running `atlas pr list` in a git repo works without `--repo` flag.

---

### Phase 10: PR Checkout

**Goal**: `atlas pr checkout <id|branch>` fetches PR branch.

**Deliverables**:
- Fetch and checkout PR source branch
- Use remote branch name as-is
- Same-repo PRs only
- Error with manual instructions for fork PRs

**Testable outcome**: `atlas pr checkout 123` checks out the PR branch locally.

---

### Phase 11: Snippets

**Goal**: Create, view, update, and delete Bitbucket snippets.

**Deliverables**:
- `atlas snippet list` shows user's own snippets
- `atlas snippet view <id>` displays metadata (add `--contents` for files)
- `atlas snippet create --title <title> -f <file> [-f <file>...]` creates snippet
- `atlas snippet update <id> -f <file>` adds/updates files (merge behavior)
- `atlas snippet update <id> -r <file>` removes files
- `atlas snippet delete <id>` removes snippet
- `--private` flag (default): visible to workspace members only

**Endpoints needed**:
- `GET /snippets/{workspace}` - list snippets
- `GET /snippets/{workspace}/{id}` - snippet metadata
- `GET /snippets/{workspace}/{id}/files/{path}` - file contents
- `POST /snippets/{workspace}` - create snippet (multipart)
- `PUT /snippets/{workspace}/{id}` - update snippet
- `DELETE /snippets/{workspace}/{id}` - delete snippet

**Testable outcome**: `atlas snippet create --title test -f README.md` creates a snippet and returns its ID.

---

## Future Phases (Out of Scope)

- Confluence integration
- PR creation/update
- Comment replies from CLI
- Webhook support

---

## Technical Notes

### Dependencies

Already in `go.mod`:
- `github.com/spf13/cobra` - CLI framework
- `github.com/spf13/viper` - configuration
- `github.com/JohannesKaufmann/html-to-markdown` - HTML conversion

To add:
- `github.com/fatih/color` or similar for terminal colors (optional)

### Project Structure

```
cmd/
  atlas/
    main.go           # entrypoint
internal/
  cli/
    root.go           # root command + global flags
    pr.go             # pr subcommands
    config.go         # config subcommands
    snippet.go        # snippet subcommands
  bitbucket/
    client.go         # HTTP client
    cache.go          # disk cache implementation
    types.go          # API response types
    pullrequest.go    # PR-specific methods
    comments.go       # comment fetching/formatting
    snippet.go        # snippet CRUD methods
  config/
    config.go         # viper setup, load/save, env expansion
  git/
    remote.go         # git remote parsing, repo inference
  output/
    markdown.go       # markdown formatters
    json.go           # JSON output helpers
```

---

## Nix Integration

### Flake Outputs

```nix
{
  packages.x86_64-linux.default  # atlas binary
  homeManagerModules.default     # home-manager module
  overlays.default               # nixpkgs overlay (optional)
}
```

### Shell Completions

Static completions only (no API calls during completion). Generated by Cobra at build time:

- Bash: `$out/share/bash-completion/completions/atlas`
- Zsh: `$out/share/zsh/site-functions/_atlas`
- Fish: `$out/share/fish/vendor_completions.d/atlas.fish`

### Home-Manager Module

Located at `nix/hm-module.nix`:

```nix
{ config, lib, pkgs, ... }:

let
  cfg = config.programs.atlas;
  tomlFormat = pkgs.formats.toml { };
in
{
  options.programs.atlas = {
    enable = lib.mkEnableOption "atlas CLI for Bitbucket";

    package = lib.mkOption {
      type = lib.types.package;
      default = pkgs.atlas;
      description = "The atlas package to use.";
    };

    settings = lib.mkOption {
      type = tomlFormat.type;
      default = { };
      example = lib.literalExpression ''
        {
          workspace = "mycompany";
          username = "user@example.com";
          app_password = "\${env:ATLAS_APP_PASSWORD}";
        }
      '';
      description = "Configuration written to ~/.config/atlas/config.toml";
    };
  };

  config = lib.mkIf cfg.enable {
    home.packages = [ cfg.package ];

    xdg.configFile."atlas/config.toml" = lib.mkIf (cfg.settings != { }) {
      source = tomlFormat.generate "atlas-config" cfg.settings;
    };
  };
}
```

### Example Usage in home.nix

```nix
{
  imports = [ inputs.atlas.homeManagerModules.default ];

  programs.atlas = {
    enable = true;
    settings = {
      workspace = "mycompany";
      username = "user@example.com";
      app_password = "\${env:ATLAS_APP_PASSWORD}";
    };
  };
}
```

### Implementation Notes

1. **Config format**: TOML for better Nix integration
2. **Secrets**: Use `${env:VAR}` syntax - never store plain passwords in nix store
3. **Overlay**: Optional overlay allows `pkgs.atlas` without flake input reference
