<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/logo-dark.svg">
    <img src="assets/logo.svg" alt="Atlas" width="400">
  </picture>
</p>

# Atlas

A CLI and MCP server for Bitbucket Cloud and Jira Cloud: pull requests,
pipelines, repositories, snippets, and Jira issues. Every operation is defined
once and served both as an `atlas` command and as an MCP tool.

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

Optionally use the home-manager module to write `~/.config/atlas/config.toml`.
The module installs nothing unless you set `package`:

```nix
imports = [ inputs.atlas.homeManagerModules.default ];
programs.atlas = {
  enable = true;
  package = inputs.atlas.packages.${system}.default;
  settings = {
    workspace = "my-workspace";
    username = "user@example.com";
    jira_cloud_id = "your-jira-cloud-id";
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

This section covers the CLI. The MCP server reads no config file and no stored
credentials; see [MCP server](#mcp-server).

### Settings

`atlas config set <key> <value>` writes `~/.config/atlas/config.toml`. An
environment variable overrides the file:

| Key | Environment | Used for |
|-----|-------------|----------|
| `workspace` | `ATLAS_WORKSPACE` | Default Bitbucket workspace for `-R repo` and workspace-wide commands. Without it, Atlas uses the workspace of the git `origin` remote. |
| `username` | `ATLAS_USERNAME` | Your Atlassian account email. Bitbucket and Jira both authenticate with it. |
| `jira_cloud_id` | `ATLAS_JIRA_CLOUD_ID` | The Jira site behind the Atlassian API gateway. Required for `atlas jira`. Your site's ID is the `cloudId` at `https://<your-site>.atlassian.net/_edge/tenant_info`. |

### Credentials

Bitbucket and Jira each take their own API token. For each one, Atlas uses the
first environment variable that is set, then falls back to the credentials file:

| Token | Environment | Credentials file key |
|-------|-------------|----------------------|
| Bitbucket | `ATLAS_BITBUCKET_TOKEN`, then `ATLAS_API_TOKEN` | `api_token` |
| Jira | `ATLAS_JIRA_TOKEN` | `jira_token` |

The credentials file is `~/.config/atlas/credentials.toml` (mode 600), or the path
in `ATLAS_CREDENTIALS_FILE`, for example a file your secrets manager decrypts.
Keeping tokens in that file keeps them out of the environment of every process an
agent starts. `atlas login` prompts for the Bitbucket token, verifies it, and
writes `api_token` to the credentials file, keeping any `jira_token` already
there. Add `jira_token` to the file yourself, or set `ATLAS_JIRA_TOKEN`.

```toml
# ~/.config/atlas/credentials.toml
api_token = "bitbucket-api-token"
jira_token = "jira-api-token"
```

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

Then check the setup:

```bash
atlas config set workspace my-workspace
atlas config set username user@example.com
atlas config set jira_cloud_id your-jira-cloud-id

atlas config verify   # Bitbucket credentials
atlas jira whoami     # Jira credentials
```

## Usage

Commands that take a repository infer it from the git `origin` remote. Pass
`-R repo` or `-R workspace/repo` to pick another one. Most commands accept
`--json`. The global flags are `--no-cache` (bypass the 5-minute disk cache of
Bitbucket reads) and `-v` (show inferred values such as the repository).

### Pull requests

```bash
atlas pr list                                   # open PRs in this repo
atlas pr list --state merged --author johndoe
atlas pr list --repos api,web --updated-since 2026-11-01
atlas pr status

atlas pr create --title "Fix auth flow" --body-file PR.md --reviewer "Jane Doe" --push
atlas pr view --comments                        # current branch PR
atlas pr view 123 --activity
atlas pr view --raw                             # raw markdown, no pager
atlas pr diff -s                                # difftastic when available, delta fallback
atlas pr diff 123 --line-numbers
atlas pr edit --title "Fix auth flow"           # opens body in $EDITOR/nvim when no body flag is passed
atlas pr edit 123 --add-reviewer "{account-uuid}" --ready

atlas pr comment 123 --body "Looks good overall"
atlas pr comment 123 --path internal/cli/pr.go --line 42 --body "Can this be simplified?"
atlas pr comment 123 --reply-to 456 --body "Fixed in the latest push"
atlas pr comment 123 --edit 456 --body "Corrected wording"
atlas pr approve 123 --body "Reviewed locally"
atlas pr request-changes 123 --body-file review.md
atlas pr review 123 --request-changes --comment-spec comments.json --body "Please address these before merge"

atlas pr checks 123 --wait 5m                   # build statuses on the head commit
atlas pr merge 123 --strategy squash --match-head-commit <sha>
atlas pr checkout 123
atlas pr close 123 --body "Closing in favor of a newer PR"
```

Reviewer flags take Bitbucket account IDs, UUIDs, or exact display names. Find
them with `atlas user search <name>`.

### Pipelines

```bash
atlas pipeline list --branch main
atlas pipeline view 2090                        # a build number, pipeline UUID, or result URL
atlas pipeline log 2090                         # tail of the failed steps' logs
atlas pipeline run --branch main --pipeline db-backup --var TARGET=staging --wait 10m
atlas pipeline stop 2090
```

### Repositories, branches, commits, and files

```bash
atlas repo list --query api
atlas branch list --query feature/
atlas branch view main
atlas commit list main --path internal/cli --limit 10
atlas file read go.mod --ref v1.2.0
atlas file read CODEOWNERS --repos 'my-workspace/*'   # the same file from every repository
atlas api /2.0/repositories/my-workspace/app/refs/tags   # read-only GET of a REST path
```

`atlas api` takes Bitbucket paths (`/2.0/...`) and Jira paths (`/rest/api/3/...`,
`/rest/api/2/...`, `/rest/agile/1.0/...`, `/rest/servicedeskapi/...`).

### Snippets

```bash
atlas snippet list
atlas snippet create --title "My snippet" file.go
atlas snippet view abc123
atlas snippet edit abc123 file.go
atlas snippet rename abc123 old.go new.go
atlas snippet clone abc123
atlas snippet delete abc123 --yes
```

### Jira

Bodies, descriptions, and comments are markdown; Atlas converts them to and
from Jira's document format.

```bash
atlas jira search 'project = CSM AND status = "In Progress" ORDER BY updated DESC'
atlas jira view CSM-12 --comments 20 --history
atlas jira create -p CSM -t Task -s "Rotate API keys" --body-file task.md --label security
atlas jira edit CSM-12 --summary "Rotate all API keys" --field "Story Points=3"
atlas jira assign CSM-12 me
atlas jira transitions CSM-12
atlas jira transition CSM-12 "In Review"
atlas jira comment CSM-12 --body "Deployed to staging"
atlas jira internal-comment CSM-12 --body "Agent-only note"   # service desk issues only
atlas jira link CSM-12 CSM-2 Blocks                          # CSM-12 blocks CSM-2
atlas jira worklog add CSM-12 1h30m --comment "Pairing"
atlas jira worklog add --entries-file worklogs.json
atlas jira worklog list CSM-12
atlas jira attach CSM-12 screenshot.png
atlas jira fields -p CSM -t Task                             # fields the create screen accepts
atlas jira projects
```

Run `atlas <command> --help` for every flag.

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | Other error, including Bitbucket or Jira server errors and failed checks |
| 2 | Usage: unknown command or flag, bad arguments, or input the API rejected |
| 3 | Authentication: missing or rejected credentials, or a missing scope |
| 4 | Not found |
| 5 | Conflict, such as a merge conflict |
| 6 | Rate limited |
| 7 | Partial: output was printed, but some entries of a batch failed |
| 8 | Timeout or still pending: a merge, pipeline run, or checks still running when Atlas stopped waiting |

## MCP Server

`atlas mcp serve` serves the same operations as MCP tools at `/mcp`, over
stateless streamable HTTP. It also answers `GET /healthz` with 204.

```bash
atlas mcp serve                                   # http://127.0.0.1:8080/mcp
atlas mcp serve --addr 127.0.0.1:9000 --allowed-host atlas.example.internal
```

The server stores no credentials. It reads no config file or credentials file,
keeps no sessions, and never uses the disk cache. Each request carries the
caller's credentials in headers:

| Header | Needed by |
|--------|-----------|
| `X-Atlas-Email` | Every tool: your Atlassian account email |
| `X-Atlas-Bitbucket-Token` | Bitbucket tools |
| `X-Atlas-Jira-Token` | Jira tools |

A Bitbucket tool never needs the Jira header, and the reverse. The tokens need
the same scopes as the CLI's. Request logs go to stderr and never include
headers.

| Flag | Environment | Default |
|------|-------------|---------|
| `--addr` | | `127.0.0.1:8080` |
| `--allowed-host` (repeatable) | `ATLAS_MCP_ALLOWED_HOSTS` (comma-separated) | none |
| `--workspace` | `ATLAS_MCP_WORKSPACE` | `moberg-analytics` |
| `--jira-cloud-id` | `ATLAS_MCP_JIRA_CLOUD_ID` | `2a0c794c-e073-4209-873f-ba6c1c4bcd9b` |

`--workspace` is the workspace for `repo` arguments that don't name one.
`--jira-cloud-id` picks the Jira site. On a loopback address the server only
accepts loopback `Host` headers, which blocks DNS rebinding. Add
`--allowed-host` for each name a reverse proxy on the same machine forwards. On
any other address the server accepts every `Host` header.

The server has 51 tools:

| Area | Tools |
|------|-------|
| Pull requests | `pr_list`, `pr_status`, `pr_view`, `pr_diff`, `pr_checks`, `pr_create`, `pr_edit`, `pr_comment`, `pr_comment_delete`, `pr_approve`, `pr_unapprove`, `pr_request_changes`, `pr_clear_change_request`, `pr_review`, `pr_merge`, `pr_close` |
| Pipelines | `pipeline_list`, `pipeline_view`, `pipeline_log`, `pipeline_run`, `pipeline_stop` |
| Repositories | `repo_list`, `branch_list`, `branch_view`, `commit_list`, `file_read` |
| Snippets | `snippet_list`, `snippet_view`, `snippet_create`, `snippet_edit`, `snippet_rename`, `snippet_delete` |
| Accounts | `whoami`, `user_search`, `jira_whoami` |
| Jira | `jira_search`, `jira_issue_view`, `jira_issue_create`, `jira_issue_edit`, `jira_assign`, `jira_transition_list`, `jira_transition`, `jira_comment`, `jira_internal_comment`, `jira_link`, `jira_worklog_list`, `jira_worklog_add`, `jira_attach`, `jira_fields`, `jira_projects` |
| Raw API | `api`: read-only GET of a Bitbucket or Jira REST path |

Tools are annotated read-only or destructive so clients can gate them. Only the
commands that act on the local machine stay CLI-only: `pr checkout`,
`snippet clone`, `login`, `config`, `update`, and `mcp serve` itself.

## Connecting an MCP Client

Each config below reads the email and tokens from `ATLAS_USERNAME`,
`ATLAS_BITBUCKET_TOKEN`, and `ATLAS_JIRA_TOKEN` in the client's environment, so
no token is written into a config file. Replace the URL if the server listens
elsewhere. Drop the Jira header if you only use Bitbucket tools.

### Claude Code

Add the server from the CLI. Single quotes keep the `${VAR}` references
unexpanded, so Claude Code stores them and expands them when it connects:

```bash
claude mcp add --transport http atlas http://127.0.0.1:8080/mcp \
  --header 'X-Atlas-Email: ${ATLAS_USERNAME}' \
  --header 'X-Atlas-Bitbucket-Token: ${ATLAS_BITBUCKET_TOKEN}' \
  --header 'X-Atlas-Jira-Token: ${ATLAS_JIRA_TOKEN}'
```

Add `--scope project` to write a shareable `.mcp.json` instead. Or write
`.mcp.json` yourself:

```json
{
  "mcpServers": {
    "atlas": {
      "type": "http",
      "url": "http://127.0.0.1:8080/mcp",
      "headers": {
        "X-Atlas-Email": "${ATLAS_USERNAME}",
        "X-Atlas-Bitbucket-Token": "${ATLAS_BITBUCKET_TOKEN}",
        "X-Atlas-Jira-Token": "${ATLAS_JIRA_TOKEN}"
      }
    }
  }
}
```

### Codex

In `~/.codex/config.toml`, `env_http_headers` maps each header to the name of
the environment variable that holds its value:

```toml
[mcp_servers.atlas]
url = "http://127.0.0.1:8080/mcp"
env_http_headers = { "X-Atlas-Email" = "ATLAS_USERNAME", "X-Atlas-Bitbucket-Token" = "ATLAS_BITBUCKET_TOKEN", "X-Atlas-Jira-Token" = "ATLAS_JIRA_TOKEN" }
```

### Executor

[Executor](https://executor.sh) keeps credentials as its own secrets and
renders them into headers on each call, so agents behind it never see the
tokens. Register Atlas as a remote MCP integration whose API-key method
declares the three headers as variables:

```bash
executor call executor mcp addServer '{
  "name": "Atlas",
  "endpoint": "http://atlas.example.internal:8080/mcp",
  "remoteTransport": "streamable-http",
  "authenticationTemplate": [{
    "type": "apiKey",
    "label": "Atlassian email and API tokens",
    "headers": {
      "X-Atlas-Email": [{ "type": "variable", "name": "email" }],
      "X-Atlas-Bitbucket-Token": [{ "type": "variable", "name": "bitbucket_token" }],
      "X-Atlas-Jira-Token": [{ "type": "variable", "name": "jira_token" }]
    }
  }]
}'
```

Then connect the integration in Executor's web UI and enter the email and both
tokens there. Executor stores them as secrets. The endpoint must be reachable
from Executor: a server in a container or on another host can't reach Atlas on
`127.0.0.1`, so run `atlas mcp serve --addr` on an address it can reach, or
behind a reverse proxy with `--allowed-host`.

## License

Apache 2.0
