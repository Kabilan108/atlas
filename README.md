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

Bitbucket and Jira each take their own API token. Environment variables are the
primary way to supply them, and the credentials file is a fallback. To set the
variables without storing tokens anywhere, see [1Password](#1password). For each
token, Atlas uses the first environment variable that is set, then falls back to
the credentials file:

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

#### 1Password

`op run` from the [1Password CLI](https://developer.1password.com/docs/cli/)
resolves secret references into the environment of the one command it starts.
Nothing is written to disk. Put the references in an env file; it holds no
secrets itself. Replace the vault, item, and field names with your own:

```bash
# ~/.config/atlas/op.env
ATLAS_USERNAME=user@example.com
ATLAS_BITBUCKET_TOKEN=op://Dev/atlas-bitbucket/credential
ATLAS_JIRA_TOKEN=op://Dev/atlas-jira/credential
```

Run the CLI under `op run`, or alias it:

```bash
op run --env-file="$HOME/.config/atlas/op.env" -- atlas config verify
alias atlas='op run --env-file="$HOME/.config/atlas/op.env" -- atlas'
```

`op run` masks secret values in the command's stdout and stderr by default.
Masking is best effort; `--no-masking` turns it off. For MCP clients, use a
header helper instead (see [Connecting an MCP Client](#connecting-an-mcp-client)):
a helper's output is what the client sends, so masking would break it.

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
atlas pipeline view 2090                        # by number, UUID, or result URL: trigger, creator, steps
atlas pipeline log 2090                         # failed steps' logs; runner setup becomes one line with the run's variables
atlas pipeline log 2090 --include-setup         # keep the setup: clone, defined variable names, images
atlas pipeline run --branch main --pipeline db-backup --var TARGET=staging --wait 10m
atlas pipeline stop 2090
```

### Repositories, branches, commits, and files

```bash
atlas repo list --query api
atlas branch list --query feature/
atlas branch view main
atlas commit list main --path internal/cli --limit 10
atlas repo diff main...feature/login --stat   # what a branch changes, before its PR exists
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
atlas jira comment CSM-12 --delete 10450 --yes               # comments and internal notes
atlas jira link CSM-12 CSM-2 Blocks                          # CSM-12 blocks CSM-2
atlas jira worklog add CSM-12 1h30m --comment "Pairing"
atlas jira worklog add --entries-file worklogs.json
atlas jira worklog list CSM-12
atlas jira worklog delete CSM-12 100028 100029 --yes        # time goes back to the estimate
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

The server has 53 tools:

| Area | Tools |
|------|-------|
| Pull requests | `pr_list`, `pr_status`, `pr_view`, `pr_diff`, `pr_checks`, `pr_create`, `pr_edit`, `pr_comment`, `pr_comment_delete`, `pr_approve`, `pr_unapprove`, `pr_request_changes`, `pr_clear_change_request`, `pr_review`, `pr_merge`, `pr_close` |
| Pipelines | `pipeline_list`, `pipeline_view`, `pipeline_log`, `pipeline_run`, `pipeline_stop` |
| Repositories | `repo_list`, `repo_diff`, `branch_list`, `branch_view`, `commit_list`, `file_read` |
| Snippets | `snippet_list`, `snippet_view`, `snippet_create`, `snippet_edit`, `snippet_rename`, `snippet_delete` |
| Accounts | `whoami`, `user_search`, `jira_whoami` |
| Jira | `jira_search`, `jira_issue_view`, `jira_issue_create`, `jira_issue_edit`, `jira_assign`, `jira_transition_list`, `jira_transition`, `jira_comment`, `jira_internal_comment`, `jira_comment_delete`, `jira_link`, `jira_worklog_list`, `jira_worklog_add`, `jira_worklog_delete`, `jira_attach`, `jira_fields`, `jira_projects` |
| Raw API | `api`: read-only GET of a Bitbucket or Jira REST path |

Tools are annotated read-only or destructive so clients can gate them. Only the
commands that act on the local machine stay CLI-only: `pr checkout`,
`snippet clone`, `login`, `config`, `update`, and `mcp serve` itself.

## Connecting an MCP Client

Every client ends up sending the three headers from [MCP Server](#mcp-server).
What differs is where the values come from. When an agent goes through
Executor, Executor holds them. On your own machine, a script the client runs at
connect time prints them.

### Executor v1

[Executor](https://executor.sh) is an MCP gateway that stores each user's
secrets. Point your agent at the office Executor, not at Atlas. Executor
forwards each call to the shared `atlas mcp serve` with your own headers, so
your machine needs no helper script, no 1Password, and no tokens in its
environment.

Store three values in Executor:

| Header | Value |
|--------|-------|
| `X-Atlas-Email` | Your Atlassian account email |
| `X-Atlas-Bitbucket-Token` | A Bitbucket API token with the scopes under [Credentials](#credentials) |
| `X-Atlas-Jira-Token` | A Jira API token with the scopes under [Credentials](#credentials). Skip it if you only use Bitbucket tools. |

Executor's own docs cover where to enter them and how to connect an agent to
it.

Whoever runs Executor registers Atlas's `/mcp` URL as a streamable HTTP MCP
server whose three headers come from each user's secrets. Executor must be able
to reach that URL. A container or another host can't reach `127.0.0.1`, so
start `atlas mcp serve --addr` on an address Executor can reach, or put it
behind a reverse proxy with `--allowed-host`.

### Executor v2

Executor v2, still in beta, doesn't register MCP servers. Integrations are
apps: TypeScript source whose default export is `defineApp`, deployed into
Executor. An Atlas app has three parts:

- A provider, the account type. It lists the hosts its credentials may reach,
  the fields a user enters, and a health check.
- Tools, built from Atlas's MCP server with `mcpRouter`.
- Approval rules, written in the app with `withApprovals`.

The example below runs against `apps` `0.0.1-beta.138`, and Jira and Bitbucket
calls succeed through it. Put the three files in one directory.

`provider.ts`:

```ts
import { defineProvider, object, plain, secrets, string } from "apps"
import { mcpHealth } from "apps/mcp"

export const url = "http://127.0.0.1:8303/mcp"

type Fields = { email: string; bitbucketToken: string; jiraToken: string }

export const headers = (account: { fields: Fields }) => ({
  "X-Atlas-Email": account.fields.email,
  "X-Atlas-Bitbucket-Token": account.fields.bitbucketToken,
  "X-Atlas-Jira-Token": account.fields.jiraToken,
})

export const provider = defineProvider({
  name: "Atlas",
  hosts: ["127.0.0.1:8303"],
  auth: {
    apiKey: secrets({
      label: "Atlassian email and API tokens",
      guide: "Atlas stores nothing; each call sends these as X-Atlas-* headers.",
      fields: object({
        email: plain(string({ minLength: 1, title: "Atlassian email" })),
        bitbucketToken: string({ minLength: 1, title: "Bitbucket API token" }),
        jiraToken: string({ minLength: 1, title: "Jira API token" }),
      }),
    }),
  },
  health: (check) => mcpHealth(check, { url, headers: headers(check.account) }),
})
```

`index.ts`:

```ts
import { defineApp, withApprovals } from "apps"
import { mcpRouter } from "apps/mcp"
import { always } from "apps/operations/approval"
import { headers, provider, url } from "./provider.ts"

export default defineApp({ app: { accounts: { atlas: provider } } }, async ({ app, signal, cache }) => ({
  tools: withApprovals(
    await mcpRouter({ url, account: app.accounts.atlas, cache, headers: headers(app.accounts.atlas), signal }),
    // Everyday Jira and Bitbucket writes run; merging, closing or approving a PR and deletes ask.
    (_tool, name) => (/^(pr_merge|pr_close|pr_approve|\w+_delete)$/.test(name) ? always() : undefined),
  ),
}))
```

`package.json`:

```json
{ "name": "atlas", "private": true, "type": "module",
  "dependencies": { "apps": "0.0.1-beta.138", "@modelcontextprotocol/sdk": "1.32.1" } }
```

An app can also pass `icons` to `defineApp`. The example leaves them out.

#### What to change

| Item | Change |
|------|--------|
| `url` in `provider.ts` | Atlas's `/mcp` URL as Executor reaches it |
| `hosts` in `provider.ts` | The `host:port` of that URL. Executor sends the two tokens only to listed hosts, so a mismatch refuses every call. |
| `apps` in `package.json` | The exact version your host runs, which `executor apps framework` prints |
| Account scope in `index.ts` | One shared account, or one per user. See below. |

Settle the provider before anyone connects an account. Changing its name,
labels, or fields creates a new provider, and adding a host makes users
reconnect.

The example declares an app account, `app: { accounts: { atlas: provider } }`.
Someone who can edit the app connects it once, and every caller uses those
tokens. That fits a personal Executor. On a shared Executor each person should
enter their own tokens, so declare a user account instead. Executor's docs pair
user accounts with `.many()` and `accountRouter`:

```ts
import { accountRouter, defineApp, withApprovals } from "apps"
import { mcpRouter } from "apps/mcp"
import { always } from "apps/operations/approval"
import { headers, provider, url } from "./provider.ts"

export default defineApp({ user: { accounts: { atlas: provider.many() } } }, async ({ user, signal, cache }) => ({
  tools: await accountRouter(
    user?.accounts.atlas ?? [],
    async (account) =>
      withApprovals(
        await mcpRouter({ url, account, cache, headers: headers(account), signal }),
        (_tool, name) => (/^(pr_merge|pr_close|pr_approve|\w+_delete)$/.test(name) ? always() : undefined),
      ),
    { signal },
  ),
}))
```

Each caller then sees tools built from the accounts they linked, and each tool
takes `{ accountId, input }`. `user` is undefined when Executor runs the app
without a caller, hence the `?? []`. This form follows Executor's docs and
hasn't been tested end to end.

Executor must be able to reach `url`. Self-hosted Executor refuses plain HTTP
and private addresses, loopback included, by default. For an Atlas on
`127.0.0.1:8303`, set both of these on the Executor server:

```sh
EXECUTOR_URL_ALLOW_HTTP_ORIGINS='["http://127.0.0.1:8303"]'
EXECUTOR_APPS_ALLOW_PRIVATE_FETCH=true
```

The first allows that exact origin, with no path or trailing slash. The second
lets app code reach private addresses at all. Inside a container, `127.0.0.1` is
the container itself, as with v1. Hosted Executor can't reach a private Atlas
at all, so it needs a public URL. See
[Self-host with Docker](https://executor.sh/docs/run/self-host#configure-it).

#### Approvals

Executor v2 has no organization-wide approval policy. Each app sets its own
rules, and nothing is inferred from a tool looking read-only. In the examples,
the `withApprovals` callback returns `always()` for the tools that should ask a
person first. Returning `undefined` keeps the tool's own approval, and
`mcpRouter` attaches none, so those tools run without asking.

Atlas marks its tools read-only or destructive, so a rule can use those hints
instead of names. This one, with `toolAnnotations` imported from `apps`, asks
before more tools than the example does, including PR and issue edits,
comments, and transitions:

```ts
(tool) => (toolAnnotations(tool)?.destructiveHint === true ? always() : undefined)
```

See [Tools and approvals](https://executor.sh/docs/concepts/tools-and-approvals).

#### Deploy and connect

Deploy the directory with the [Executor CLI](https://executor.sh/docs/run/cli),
or have an agent deploy it through Executor's own MCP tools:

```sh
executor apps create --name Atlas --files ./atlas    # prints the app id
executor apps deploy --app <app-id> --files ./atlas
```

Then open the app's **Accounts** tab in the dashboard and connect an account
with your email and the two tokens, which need the scopes under
[Credentials](#credentials). [Author an app](https://executor.sh/docs/author-an-app)
and [Connect an account](https://executor.sh/docs/connect-an-account) cover the
details.

### Claude Code or Codex, directly

Run `atlas mcp serve` yourself and give the client a header helper: a command
that prints a JSON object of headers to stdout. The client runs it when it
connects and sends its output as headers. The tokens never land in a config
file or in any process environment, so an agent's shell commands can't see
them.

This helper fills both tokens from 1Password with one `op inject` call. Replace
the email and the vault, item, and field names with your own:

```sh
#!/bin/sh
# ~/.config/atlas/headers.sh (chmod +x)
exec op inject <<'EOF'
{
  "X-Atlas-Email": "user@example.com",
  "X-Atlas-Bitbucket-Token": "{{ op://Dev/atlas-bitbucket/credential }}",
  "X-Atlas-Jira-Token": "{{ op://Dev/atlas-jira/credential }}"
}
EOF
```

1Password is one example. Any command that prints that JSON works, such as a
script that reads `pass` or your OS keychain.

In Claude Code, `headersHelper` names the helper. `claude mcp add` has no flag
for it, so add the entry as JSON:

```bash
claude mcp add-json -s user atlas \
  '{"type":"http","url":"http://127.0.0.1:8080/mcp","headersHelper":"/home/me/.config/atlas/headers.sh"}'
```

The same entry works under `mcpServers` in a project's `.mcp.json`:

```json
{
  "mcpServers": {
    "atlas": {
      "type": "http",
      "url": "http://127.0.0.1:8080/mcp",
      "headersHelper": "/home/me/.config/atlas/headers.sh"
    }
  }
}
```

In Codex, set `http_headers_helper` in `~/.codex/config.toml`. `codex mcp add`
has no flag for it either:

```toml
[mcp_servers.atlas]
url = "http://127.0.0.1:8080/mcp"
http_headers_helper = "/home/me/.config/atlas/headers.sh"
```

The Codex setup follows Codex's docs and source and hasn't been tested end to
end. Things to know before writing a helper:

- Don't use `op run` in a helper. It masks secrets in the command's stdout,
  which is the helper's output, so Atlas receives the literal text
  `<concealed by 1Password>`. Use `op inject` or `op read`, which print the
  values.
- Give the helper an absolute path. Claude Code runs a user-scope helper in
  `~/.claude` and a project helper in the project directory.
- Codex runs the helper with `sh -c` in a cleared environment: only `HOME`,
  `LOGNAME`, `PATH`, `SHELL`, `USER`, `LANG`, `LC_ALL`, `TERM`, `TMPDIR`, `TZ`,
  and CA-bundle variables. It discards the helper's stderr. A 1Password service
  account token in Codex's environment never reaches the helper, so the script
  must load `OP_SERVICE_ACCOUNT_TOKEN` itself, for example from a file. Codex
  supports helpers only for local MCP servers.
- Claude Code removes variables whose names contain `TOKEN`, `SECRET`, `KEY`,
  `AUTH`, or `PASSWORD` before it runs a helper from a project `.mcp.json`, so
  `OP_SERVICE_ACCOUNT_TOKEN` is gone there too. User and local scope helpers
  get the full environment. Project and local scope helpers run only after you
  trust the folder.
- The helper runs once per connection, at session start and on each reconnect,
  and again after a 401 or 403. Each run has 10 seconds. If `op` waits for you
  to unlock the 1Password app, that wait counts toward the 10 seconds.

## License

Apache 2.0
