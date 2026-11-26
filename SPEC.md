# Atlas CLI Specification

A CLI and MCP server for Bitbucket Cloud and Jira Cloud.

## Overview

Atlas started as a way to fetch PR comments and review feedback from Bitbucket Cloud in a format coding agents can act on directly. It now covers pull requests, pipelines, repositories, snippets, and Jira issues.

Every operation is defined once in `internal/ops` and served from that one definition as an `atlas` command and as an MCP tool on `atlas mcp serve`. MCP is the preferred interface: nothing exists in the CLI that is not on MCP, except commands bound to the local machine (`login`, `config`, `update`, `pr checkout`, `snippet clone`, and `mcp serve` itself) and presentation flags such as `--web`, the pager, and the editor. `internal/cli/parity_test.go` enforces this.

## Authentication

- **Method**: Atlassian account email plus one API token per product (Bitbucket, Jira), sent as HTTP Basic auth
- **Config storage**: `~/.config/atlas/config.toml`
- **Credential storage**: `~/.config/atlas/credentials.toml` (mode 600), or the path in `ATLAS_CREDENTIALS_FILE`
- **Environment overrides**: `ATLAS_WORKSPACE`, `ATLAS_USERNAME`, `ATLAS_BITBUCKET_TOKEN` (or `ATLAS_API_TOKEN`), `ATLAS_JIRA_TOKEN`, `ATLAS_JIRA_CLOUD_ID`, `ATLAS_CREDENTIALS_FILE`
- **Credential precedence**: per product, the environment token, then the credentials file. The file is read only when the product's environment variable is unset, so a broken entry for one product never blocks the other.

| Token | Environment (first set wins) | Credentials file key |
|---|---|---|
| Bitbucket | `ATLAS_BITBUCKET_TOKEN`, `ATLAS_API_TOKEN` | `api_token` |
| Jira | `ATLAS_JIRA_TOKEN` | `jira_token` |

Bitbucket token scopes (app: Bitbucket):

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

Jira token scopes (app: Jira, classic scopes):

```text
read:jira-work
write:jira-work
read:jira-user
read:servicedesk-request
write:servicedesk-request
```

Jira requests go through the Atlassian API gateway for the site named by the Jira cloud ID.

The MCP server reads none of this. Each request carries its own credentials in headers; see [MCP Server](#mcp-server).

## Configuration

### Config File

Location: `~/.config/atlas/config.toml`

```toml
workspace = "mycompany"
username = "user@example.com"
jira_cloud_id = "your-jira-cloud-id"
```

| Key | Environment | Meaning |
|---|---|---|
| `workspace` | `ATLAS_WORKSPACE` | Default Bitbucket workspace. Without it, Atlas uses the workspace of the git `origin` remote. |
| `username` | `ATLAS_USERNAME` | Atlassian account email, used by Bitbucket and Jira. |
| `jira_cloud_id` | `ATLAS_JIRA_CLOUD_ID` | Jira site ID. Required by every Jira command; Bitbucket commands never need it. |

### Config Precedence

1. Command-line flags (highest)
2. Environment variables
3. Config and credentials files
4. Git inference (workspace and repository from the `origin` remote)

### Setting Credentials

```bash
atlas login                                  # Bitbucket token; keeps an existing jira_token
atlas config set workspace mycompany
atlas config set username user@example.com
atlas config set jira_cloud_id <id>
```

`atlas login` writes `api_token` to the credentials file. The Jira token goes in `jira_token` in the same file, or in `ATLAS_JIRA_TOKEN`.

### Verifying Credentials

```bash
atlas config verify  # calls the Bitbucket /user endpoint
atlas whoami         # the Bitbucket account the token belongs to
atlas jira whoami    # the Jira account the token belongs to
```

## Command Structure

```
atlas pr list [-R <workspace/repo|repo>] [--repos <repo>,...] [--all] [--state open|merged|declined|superseded|all] [--author <author>] [--reviewer <reviewer>] [--updated-since <date>] [-L <limit>] [--json]
atlas pr status [-R <workspace/repo|repo>] [--json]
atlas pr create [-R <workspace/repo|repo>] [-B <branch>] [-H <branch>] (-t <title>|-f) [-b <body>|-F <file>|-e] [-r <reviewer>,...] [--push] [--draft] [--dry-run] [-w] [--json]
atlas pr view [<id|url|branch>] [-R <workspace/repo|repo>] [-c] [--all] [--activity] [--json] [-r] [-w]
atlas pr diff [<id|url|branch>] [-R <workspace/repo|repo>] [--name-only] [--patch] [-s] [-n] [--color auto|always|never] [--json]
atlas pr checks [<id|url|branch>] [-R <workspace/repo|repo>] [--commit <sha>] [--wait <duration>] [--json]
atlas pr edit [<id|url|branch>] [-R <workspace/repo|repo>] [-t <title>] [-b <body>|-F <file>] [-B <branch>] [--add-reviewer <reviewer>,...] [--remove-reviewer <reviewer>,...] [--draft|--ready] [--json]
atlas pr merge [<id|url|branch>] [-R <workspace/repo|repo>] [--strategy <strategy>] [--message <text>] [--close-source-branch] [--match-head-commit <sha>] [--wait <duration>] [--json]
atlas pr close <id|url|branch> [-R <workspace/repo|repo>] [-b <text>] [--json]
atlas pr comment [<id|url|branch>] [-R <workspace/repo|repo>] (-b <text>|-F <file>|-e) [--reply-to <id>] [--path <path>] [--line <line>] [--side new|old] [--json]
atlas pr comment [<id|url|branch>] [-R <workspace/repo|repo>] --edit <id> (-b <text>|-F <file>|-e) [--json]
atlas pr comment [<id|url|branch>] [-R <workspace/repo|repo>] --delete <id> [--json]
atlas pr approve [<id|url|branch>] [-R <workspace/repo|repo>] [-b <text>|-F <file>|-e] [--json]
atlas pr unapprove [<id|url|branch>] [-R <workspace/repo|repo>] [--json]
atlas pr request-changes [<id|url|branch>] [-R <workspace/repo|repo>] [-b <text>|-F <file>|-e] [--json]
atlas pr clear-change-request [<id|url|branch>] [-R <workspace/repo|repo>] [--json]
atlas pr review [<id|url|branch>] [-R <workspace/repo|repo>] (--approve|--request-changes|--comment) [-b <text>|-F <file>|-e] [--comment-spec <file>] [--json]
atlas pr checkout <id|url|branch> [-R <workspace/repo|repo>] [-b <name>] [--detach] [-f] [--recurse-submodules]

atlas pipeline list [-R <workspace/repo|repo>] [-b <branch>] [--commit <sha>] [-p <pipeline>] [-L <limit>] [--json]
atlas pipeline view <build>... [-R <workspace/repo|repo>] [--json]
atlas pipeline log <build>... [-R <workspace/repo|repo>] [--step <name|uuid>]... [--tail <bytes>] [--max-bytes <bytes>] [--full] [--include-setup] [--json]
atlas pipeline run [-R <workspace/repo|repo>] [-b <branch>] [--commit <sha>] [-p <pipeline>] [--var KEY=VALUE]... [--secured-var KEY=VALUE]... [--wait <duration>] [--json]
atlas pipeline stop <build> [-R <workspace/repo|repo>] [--json]

atlas repo list [--workspace <workspace>] [-q <text>] [-L <limit>] [--json]
atlas repo diff <base>...<head> [-R <workspace/repo|repo>] [--path <path>] [--stat] [-n] [--json]
atlas branch list [-R <workspace/repo|repo>] [-q <text>] [-L <limit>] [--json]
atlas branch view <branch> [-R <workspace/repo|repo>] [--json]
atlas commit list [<ref>] [-R <workspace/repo|repo>] [--base <ref>] [--path <path>] [-L <limit>] [--json]
atlas file read <path> [-R <workspace/repo|repo>] [--repos <repo>,...] [--ref <ref>] [--max-bytes <bytes>] [--json]
atlas user search <query> [--workspace <workspace>] [-L <limit>] [--json]
atlas whoami [--json]
atlas api <path> [--max-bytes <bytes>] [--json]

atlas snippet list [--workspace <workspace>] [--role <role>] [--public|--private] [-L <limit>] [--json]
atlas snippet view [<id|url>] [--workspace <workspace>] [-f <file>] [--files] [-r] [-w] [--json]
atlas snippet create [<file>... | -] [--workspace <workspace>] --title <title> [-f <file>] [-p] [--json]
atlas snippet edit <id|url> [<filename>] [--workspace <workspace>] [-f <file>] [--title <title>] [-a <file>] [-r <file>] [--json]
atlas snippet rename <id|url> <old-filename> <new-filename> [--workspace <workspace>] [--json]
atlas snippet clone <id|url> [<directory>] [--workspace <workspace>] [--protocol ssh|https] [-- <gitflags>...]
atlas snippet delete [<id|url>] [--workspace <workspace>] [--yes] [--json]

atlas jira search <jql> [--columns <field>,...] [-L <limit>] [--page-token <token>] [--json]
atlas jira view <issue> [-c <n>] [--history] [-r] [--json]
atlas jira create -p <project> -t <type> -s <summary> [-b <body>|-F <file>] [-a <user>] [-l <label>]... [--priority <name>] [--parent <key>] [--field Name=value]... [--link TYPE:KEY]... [--inward-link TYPE:KEY]... [--json]
atlas jira edit <issue> [-s <summary>] [-b <body>|-F <file>] [--append] [--force] [-a <user>] [-l <label>|-l -<label>]... [--field Name=value]... [--json]
atlas jira assign <issue> <user> [--json]
atlas jira transitions <issue> [--json]
atlas jira transition <issue> <transition-or-status> [--field Name=value]... [--force] [--json]
atlas jira comment <issue> (-b <body>|-F <file>) [--json]
atlas jira comment <issue> --delete <comment-id> [--yes] [--json]
atlas jira internal-comment <issue> (-b <body>|-F <file>) [--json]
atlas jira link <issue> <target> <type> [--inward] [--json]
atlas jira worklog list <issue> [--json]
atlas jira worklog add <issue> <duration> [-m <comment>] [--started <time>] [--json]
atlas jira worklog add -f <file> [--json]
atlas jira worklog delete <issue> <worklog-id>... [--yes] [--json]
atlas jira attach <issue> <file>... [--json]
atlas jira fields [<issue>] [-p <project> -t <type>] [--json]
atlas jira projects [-q <text>] [-L <limit>] [--json]
atlas jira whoami [--json]

atlas mcp serve [--addr <host:port>] [--allowed-host <host>]... [--workspace <workspace>] [--jira-cloud-id <id>]

atlas login
atlas config set <key> [<value>]
atlas config get <key>
atlas config verify
atlas update
```

`atlas <command> --help` is the reference for each flag.

### Global Flags

- `--no-cache`: Bypass the disk cache entirely
- `--verbose` / `-v`: Show inferred values (repo from git remote, etc.)

---

## Behaviors

### Repository Inference

When `-R, --repo` is omitted, Atlas finds the enclosing git repository (including worktrees and submodules) and extracts workspace/repo from the `origin` remote. `-R` accepts either `workspace/repo` or `repo`; a bare `repo` uses the configured workspace, else the remote's. Use `-v` to see what was inferred.

A PR URL names its own repository, so PR commands given a URL need neither `-R` nor git. Commands that touch the local repository, such as `pr checkout`, refuse a URL from a different repository.

### Branch Name Resolution

PR commands accept branch names in addition to numeric IDs and URLs:
- `atlas pr view feature/auth` resolves to the PR for that branch
- With no argument, commands that accept an optional selector resolve the PR for the current branch. `pr close` and `pr checkout` require a selector.
- When multiple PRs exist for a branch, prefers most recent open PR

### Output Format

- Markdown unless `--json` is passed
- `--json` outputs complete structured data (no field selection)
- Interactive output for `pr view`, `jira view`, and `snippet view` goes through `bat` when available, then `less`, then stdout. `--raw` or non-TTY output prints raw markdown.
- Non-TTY detection: markdown output preserved, but interactive prompts disabled

### Bounded Output

Every list has a hard maximum and stops paging there; every response read, diff, log, and file download has a byte cap. Commands report when they truncate. `--limit` and `--max-bytes` flags accept values up to the documented maximums in `--help`.

### Credential Redaction

Errors from Bitbucket and Jira never include the caller's credentials. Pipeline output redacts secured variable values.

### Caching

- Location: `$XDG_CACHE_HOME/atlas`, else `~/.cache/atlas`
- Scope: Bitbucket GET responses from the CLI, keyed by username and URL. Jira responses are not cached. The MCP server never caches.
- TTL: 5 minutes (uniform for all data types)
- Bypass: `--no-cache` global flag
- Successful Bitbucket writes clear the disk response cache, including writes made with `--no-cache`. Cache cleanup failures warn on stderr without reporting the completed write as failed.
- No user-facing cache management commands (internal implementation detail)

### Rate Limiting

Atlas does not retry rate-limited requests. A 429 fails with the time until the limit resets, when the API reports it, and exits with code 6.

### Exit Codes

One code per error kind, mapped once at the CLI boundary (`internal/cli/exit.go`) and listed in `atlas --help`:

| Code | Meaning |
|---|---|
| 0 | Success |
| 1 | Other error, including Bitbucket or Jira server errors (5xx) and failed checks |
| 2 | Usage: unknown command or flag, wrong arguments, invalid input, or a 4xx the API rejected the request with |
| 3 | Authentication: missing or rejected credentials (401), or a missing scope or access (403) |
| 4 | Not found (404, or a branch that does not exist) |
| 5 | Conflict (409), such as a merge conflict |
| 6 | Rate limited (429) |
| 7 | Partial: output was printed, but some entries of a batch failed |
| 8 | Timeout, or still pending: a merge, pipeline run, or checks still running when atlas stopped waiting |

A partial result prints its output before exiting 7.

### Non-Interactive Mode

Auto-detected when stdin is not a TTY. Interactive prompts are disabled; commands that would require input exit with an error instead.

---

## PR List Command

`atlas pr list` shows PRs in a repository, several repositories, or across a workspace.

### Flags

- `-R, --repo <workspace/repo|repo>`: Target repository (inferred from git if omitted)
- `--repos <repo>,...`: List PRs in these repositories instead (up to 50)
- `--all`: List PRs across all repos in the workspace
- `--state <state>`: `open` (default), `merged`, `declined`, `superseded`, or `all`
- `--author <author>`: Filter by author username
- `--reviewer <reviewer>`: Filter by reviewer username
- `--updated-since <date>`: Only PRs updated on or after this date (`YYYY-MM-DD` or ISO 8601)
- `-L, --limit <n>`: Maximum PRs per repository, newest updated first (default 50, max 500)

### Output

Tabular format: `ID | Title | Author | State | Updated | Feedback`

Feedback shows comment and task counts. Drafts show as `OPEN (DRAFT)`.

---

## PR View Command

`atlas pr view [<id|url|branch>]` shows PR details. With no argument, it shows the PR for the current branch.

### Flags

- `-R, --repo <workspace/repo|repo>`: Target repository
- `-c, --comments`: Include comments
- `--all`: Include resolved comments (only with `--comments`)
- `--activity`: Include the activity feed: approvals, pushes, and updates
- `--json`: Output as JSON
- `-r, --raw`: Print raw markdown to stdout
- `-w, --web`: Open the PR in Bitbucket

### Output Format (Markdown)

YAML frontmatter with the PR's metadata (number, repository, title, state, draft, author, source branch and head commit, destination branch, created and updated times, merge commit when merged, URL, comment and task counts, and each reviewer's name, account ID, and status), then the title, a review summary, the description, and the requested comment, task, and activity sections.

```markdown
# PR #42: Fix login

## Review Summary

- Reviewers: 1 approved, 1 changes requested, 1 pending
- Comments: 5 total
- Tasks: 2 total

## Description

<PR description body>
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

Each displayed comment includes its Bitbucket comment ID for use with `atlas pr comment --reply-to`, `--edit`, and `--delete`.

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

`atlas pr checkout <id|url|branch>` fetches and checks out a PR branch locally.

- Uses the remote branch name as-is unless `--branch` is passed
- Supports fork PRs by adding/fetching the source workspace remote
- Supports `--detach`, `--force`, and `--recurse-submodules`

---

## PR Create

`atlas pr create` creates a Bitbucket pull request without interactive prompts.

- Requires `--title` or `--fill`
- Uses current branch as `--head` by default
- Uses the repository main branch as `--base` by default
- Fails if the head branch is not pushed unless `--push` is passed. With `--head` naming another branch, `--push` pushes that branch.
- Supports `--body`, `--body-file`, `--reviewer`, `--dry-run`, and `--web`
- `--reviewer` takes account IDs, UUIDs, or exact display names (or nicknames) of workspace members; an ambiguous name fails with the candidates
- `--draft` creates a draft PR and is included in `--dry-run` output; it cannot be combined with `--web`
- PR lists mark drafts as `OPEN (DRAFT)`. List and view JSON expose `draft: true|false`; view markdown includes the same boolean in frontmatter.
- `-e, --editor` opens `$EDITOR`, then `nvim`, then `vi` to write the PR body

---

## PR Diff

`atlas pr diff [<id|url|branch>]` shows a PR diff. With no argument, it uses the current branch PR.

- Uses `delta` by default when available
- `-s, --structured` uses `difft` when available, then falls back to the normal diff path
- `--name-only` prints changed file names
- `--patch` prints the raw patch
- `-n, --line-numbers` prefixes each hunk line with its old and new line numbers
- `--color auto|always|never` controls color

---

## PR Checks

`atlas pr checks [<id|url|branch>]` shows build statuses on the PR's head commit, or on `--commit`. Bitbucket Pipelines statuses include their build numbers.

- `--wait <duration>` waits until no check is pending (max 10m)
- Exits 1 when a check failed, and 8 when a check is still running

---

## PR Merge

`atlas pr merge [<id|url|branch>]` merges a PR and waits for Bitbucket to finish.

- `--strategy`: `merge_commit`, `squash`, `fast_forward`, `squash_fast_forward`, `rebase_fast_forward`, or `rebase_merge` (default: the destination branch's)
- `--message`: merge commit message
- `--close-source-branch`: delete the source branch after merging (default: the PR's setting)
- `--match-head-commit <sha>`: merge only if the PR head is still this commit, so a push after the checks cannot merge unchecked code
- `--wait <duration>`: how long to wait for a queued merge (default 2m, max 10m). A merge still running at the deadline exits 8 and is not retried.

---

## PR Edit

`atlas pr edit [<id|url|branch>]` edits a PR. With no argument, it edits the current branch PR.

- `--title` updates the title
- `--body` sets the body directly
- `--body-file` reads the body from a file or stdin with `-`
- `--base` changes the destination branch
- With no explicit edits, Atlas opens the existing PR body in `$EDITOR`, then `nvim`, then `vi`
- `--draft` marks the PR as draft; `--ready` marks it ready for review. The flags are mutually exclusive and do not open the editor. Explicit `--draft=false` marks ready, and `--ready=false` marks draft.
- Explicit draft state is always sent to the API so stale cached state cannot suppress the requested change.
- `--add-reviewer` and `--remove-reviewer` update reviewers without replacing unrelated reviewers

---

## PR Close

`atlas pr close <id|url|branch>` declines a Bitbucket pull request.

- `--body` (`-b`) posts a closing comment before declining
- Branch deletion is not supported

---

## PR Comments And Review Actions

`atlas pr comment` creates, edits, or deletes a Bitbucket pull request comment.

- Creating requires exactly one body source: `--body`, `--body-file`, or `--editor`
- No selector means the current branch PR
- `--reply-to <id>` posts a reply to an existing comment
- `--path <path>` posts a file-level comment
- `--path <path> --line <line> --side new|old` posts an inline comment
- `--side new` maps to Bitbucket `inline.to`; `--side old` maps to `inline.from`
- `--edit <id>` replaces a comment's body; `--delete <id>` deletes it
- `--json` emits the created comment

`atlas pr approve` and `atlas pr request-changes` update the authenticated user's review state.

- Optional body flags post a top-level PR comment before the review action
- If the comment succeeds but the review action fails, Atlas reports that partial success, including the posted comment
- `atlas pr unapprove` withdraws the authenticated user's approval
- `atlas pr clear-change-request` withdraws the authenticated user's change request

`atlas pr review` posts zero or more comments and then finishes with one action.

- Exactly one of `--approve`, `--request-changes`, or `--comment` is required
- `--comment-spec <file>` accepts a JSON array of `{ "body", "reply_to", "path", "line", "side" }` objects
- Comments are posted in file order before the final action
- If a comment fails, Atlas stops before changing review state
- If the final action fails, Atlas reports how many comments were already posted

---

## PR Status

`atlas pr status` shows gh-like groups for the selected repository:

- Current branch
- Created by you
- Requesting your review

---

## Pipeline Commands

A build is a build number (`2090` or `#2090`), a pipeline UUID, or a result URL.

- `atlas pipeline list` lists recent runs, filtered by `--branch`, `--commit`, or `--pipeline` (a custom pipeline name or selector pattern). Each row names the trigger and who started the run.
- `atlas pipeline view <build>...` shows runs and their steps. Each run names its trigger (`MANUAL`, `PUSH`, `SCHEDULE`...) and creator: a user's display name, or a token or app's name with its kind, such as `cicd pipeline token (workspace_access_token)`. Bitbucket's GET of a run does not return the variables it was started with, so `pipeline view` does not list them; `pipeline log` does.
- `atlas pipeline log <build>...` shows the tail of step logs: failed steps by default, else running ones, or the steps named with `--step`. `--tail` sets bytes per step, `--max-bytes` the cap across all logs, and `--full` prints up to 2 MiB per step and 8 MiB in all.
  - Runner setup is cut by default. Everything Bitbucket printed before the step's script becomes one line, such as `[runner setup omitted: 3.1 KB · runner devserver4_k8s_1 · image …@sha256:c6c369ed603f · variables BRANCH=testing META=iam · include_setup shows it]`. That setup covers the runner, clone, names of the defined variables, runtime and images. Image digests are cut to 12 hex digits in the line. Setup lines that read like a problem (`error`, `failed`, `not found`, `warning`...), such as `Cache "node": Not found`, stay below the line.
  - The cut ends at the line echoing the step's first script command: `+ ` and the command's first line, from the steps API's `script_commands`. That line must be the first `+ ` line after the setup's `Default variables:` block. Otherwise the log is left whole. So a step that failed in clone, image pull or runner setup shows everything.
  - The line lists the run's pipeline variables, parsed from the setup's `Pipeline variables:` block. A secured variable shows as `KEY (secured)` with no value. That covers a bare `KEY`, a value Bitbucket masked as `$KEY`, `${KEY}` or asterisks, and placeholders such as `[masked]`. The line shows at most 25 variables, keys up to 100 bytes and values up to 200 (marked `truncated`); the rest are counted. JSON output carries the same as `setup`.
  - `--include-setup` keeps the setup, for questions such as which workspace or repository variables are defined. Without it, Atlas reads up to 32 KiB past the tail so that a log a little longer than the tail still arrives whole. The tail and `--max-bytes` count only what remains after the cut. A log longer than the tail plus 32 KiB has no setup in its tail and gets no setup line; raise `--tail` (or use `--full`) to see its variables.
- `atlas pipeline run` starts a run on `--branch` or `--commit` (both pins the run), optionally a custom `--pipeline` with `--var` and `--secured-var` variables. `--wait` (max 10m) exits non-zero unless the run finished successfully in time. A started run is never retried.
- `atlas pipeline stop <build>` stops a running pipeline

---

## Repository Commands

- `atlas repo list` lists repositories in a workspace, filtered by `--query`
- `atlas repo diff <base>...<head>` shows what head (a branch, tag, or commit) changes since its merge base with base, as a pull request from head into base would show it, without needing one: a per-file summary from Bitbucket's diffstat, then the diff. `<base>..<head>` means the same. `--path` narrows the diff to a file or directory, `--stat` prints only the summary, and `-n` numbers the diff lines. The `repo_diff` tool caps the diff at `max_bytes` (default 60000) and says when it cut it short.
- `atlas branch list` lists branches, most recent commit first; `atlas branch view <branch>` shows a branch's head commit and merge strategy and fails if it does not exist
- `atlas commit list [<ref>]` lists commits on a ref (default: the main branch), newest first. `--base` excludes commits reachable from another ref; `--path` keeps commits touching a path.
- `atlas file read <path>` prints a file at `--ref` (default: the main branch). With `--repos`, it reads the same path from each listed repository (`workspace/repo`, `repo`, or `workspace/*` for every repository) and prints one section per repository. A repository whose file could not be read is reported in its section, and the command exits 7.
- `atlas user search <query>` finds workspace members and shows their account IDs and UUIDs, for reviewer flags
- `atlas whoami` shows the Bitbucket account the credentials belong to
- `atlas api <path>` GETs a Bitbucket (`/2.0/...`) or Jira (`/rest/api/3/...`, `/rest/api/2/...`, `/rest/agile/1.0/...`, `/rest/servicedeskapi/...`) REST path and prints the body. Only GET is supported; prefer a dedicated command where one exists.

---

## Snippet Commands

### List

`atlas snippet list` shows snippets for the configured workspace.

- `-L, --limit <n>`: Maximum snippets to fetch (default 10, max 100)
- `--role owner|contributor|member`: Filter snippets by Bitbucket role
- `--public`: Show only public snippets
- `--private`: Show only private snippets
- `--json`: Emit structured output

### View

`atlas snippet view <id|url>` displays snippet file contents by default.

- Uses `bat` as the pager when available, then `less`, then stdout
- `-f, --filename <file>`: Display a single file
- `--files`: List file names only
- `-r, --raw`: Print raw contents without paging
- `-w, --web`: Open the snippet in a browser
- `--json`: Emit metadata and file contents as structured output

### Create

```bash
atlas snippet create --title "Auth helpers" src/auth.go src/auth_test.go
cat src/auth.go | atlas snippet create --title "Auth helper" --filename auth.go
```

- Files are positional
- `-` or piped input reads from standard input
- Snippets are private by default; `--public` lists the snippet publicly

### Edit

```bash
atlas snippet edit <id|url> auth.go
atlas snippet edit <id|url> --title "Auth helpers"
atlas snippet edit <id|url> --add src/auth.go --remove old_file.go
```

- With no add/remove/title flags, Atlas opens the selected snippet file in `$VISUAL` or `$EDITOR`
- If no filename is provided for a multi-file snippet, Atlas prompts for a file in interactive terminals
- `-f, --filename <file>`: Select the file to edit
- `-a, --add <file>`: Add or replace a snippet file from a local file
- `-r, --remove <file>`: Remove a file from the snippet

### Rename

```bash
atlas snippet rename <id|url> old_file.go new_file.go
```

Renames by fetching the old file content, adding it under the new filename, and removing the old filename.

### Clone

```bash
atlas snippet clone <id|url>
atlas snippet clone <id|url> ./local-dir -- --depth 1
atlas snippet clone <id|url> --protocol https
```

Clone uses SSH by default. Use `--protocol https` when SSH is not configured.

### Delete

```bash
atlas snippet delete <id|url> --yes
```

Without `--yes`, Atlas asks for confirmation in interactive terminals.

---

## Jira Commands

Jira commands need `username`, a Jira token, and `jira_cloud_id`. Summaries, descriptions, comments, and worklog comments are markdown; Atlas converts them to and from Atlassian Document Format. Users (`--assignee`, `assign`) are `me`, an email, a display name, an account ID, or `none`. `--field Name=value` sets a field by display name; repeat a name for a list, start the value with `[` or `{` for JSON, and use `null` to clear.

- `atlas jira search <jql>` searches with JQL. `--columns` adds fields by name or ID; `--page-token` continues a previous search; `-L` defaults to 50 (max 1000).
- `atlas jira view <issue>` shows an issue, its child issues, and its most recent comments (`-c`, default 10, max 100). `--history` includes the change history.
- `atlas jira create` creates an issue in `-p` with type `-t` and summary `-s`. `--parent` sets the epic for a story or the story for a subtask. `--link TYPE:KEY` and `--inward-link TYPE:KEY` link the new issue once created.
- `atlas jira edit <issue>` edits an issue. `--append` adds the body after the current description. A description holding content markdown cannot represent is not replaced unless `--force` is passed. `-l -label` removes a label.
- `atlas jira assign <issue> <user>` assigns an issue.
- `atlas jira transitions <issue>` lists the transitions an issue allows; `atlas jira transition <issue> <transition-or-status>` (alias `move`) moves it by transition name or target status, with any screen fields as `--field`. Rich-text screen fields are guarded like `edit`'s description.
- `atlas jira comment <issue>` adds a public comment; on service desk issues customers can read it. `atlas jira internal-comment <issue>` adds an agent-only note, refuses issues outside service desk projects, and fails if Jira does not confirm the note is internal. `atlas jira comment <issue> --delete <id>` deletes a comment or internal note through `jira_comment_delete` and reports its author, created time, and whether it was internal.
- `atlas jira link <issue> <target> <type>` links two issues using the type's outward wording (issue blocks target); `--inward` uses the inward wording (issue is blocked by target).
- `atlas jira worklog add <issue> <duration>` logs work (`1h 30m`, `1h30m`, `45m`, `1.5h`, or `PT1H30M`), starting now unless `--started`. `--entries-file` reads a JSON array of `{"issue","started","duration","comment"}` entries; entries already logged are skipped. `atlas jira worklog list <issue>` lists every worklog. `atlas jira worklog delete <issue> <id>...` deletes up to 100 worklogs from one issue and reports each one's author, start, and time spent; Jira adds the deleted time back to the remaining estimate (`adjustEstimate=auto`), undoing what logging it took off. With several IDs, failures are reported per ID and the command exits as a partial failure; a single failed ID returns its own error (not found or auth).
- Both Jira deletes ask for confirmation in interactive terminals and refuse to run non-interactively without `--yes`.
- `atlas jira attach <issue> <file>...` attaches files.
- `atlas jira fields` lists create fields for `-p` and `-t`, or an issue's edit fields.
- `atlas jira projects` lists projects, filtered by `--query`.
- `atlas jira whoami` shows the Jira account the credentials belong to.

---

## MCP Server

`atlas mcp serve` serves every MCP-surfaced op as a tool at `/mcp` over stateless streamable HTTP, and answers `GET /healthz` with 204.

### Credentials

The server stores no credentials, reads no config or credentials file, keeps no sessions, and never uses the disk cache. Each tool call builds its clients from the request's headers:

| Header | Needed by |
|---|---|
| `X-Atlas-Email` | Every tool |
| `X-Atlas-Bitbucket-Token` | Bitbucket tools |
| `X-Atlas-Jira-Token` | Jira tools |

A missing header fails the call with an authentication error naming the header. Request logs go to stderr as JSON and never include headers.

### Flags

| Flag | Environment | Default | Meaning |
|---|---|---|---|
| `--addr` | | `127.0.0.1:8080` | Listen address |
| `--allowed-host` (repeatable) | `ATLAS_MCP_ALLOWED_HOSTS` (comma-separated) | none | Extra `Host` names accepted on a loopback listener, such as the name a local reverse proxy forwards |
| `--workspace` | `ATLAS_MCP_WORKSPACE` | `moberg-analytics` | Workspace for `repo` arguments without one |
| `--jira-cloud-id` | `ATLAS_MCP_JIRA_CLOUD_ID` | `2a0c794c-e073-4209-873f-ba6c1c4bcd9b` | Jira site behind the Atlassian API gateway |

On a loopback listener the server rejects any `Host` header that is neither loopback nor allowed, which blocks DNS rebinding. On other addresses it accepts every `Host`.

### Container Image

`nix build .#image` (`nix/image.nix`) builds a `dockerTools.streamLayeredImage` of the package with CA certificates. It runs as UID 65532 with entrypoint `atlas mcp serve --addr :8303`; arguments after the image are appended. The release workflow pushes it to `ghcr.io/kabilan108/atlas:<version>` (linux/amd64) before tagging, and writes the digest to the job summary and the `image_digest` job output.

### Limits

- Request bodies are capped at 16 MiB, enough for `jira_attach`'s largest upload, and must arrive within 30s
- Every tool result's text is capped, including error results that carry partial output

### Tools

53 tools, annotated read-only or destructive from each op's metadata:

- Pull requests: `pr_list`, `pr_status`, `pr_view`, `pr_diff`, `pr_checks`, `pr_create`, `pr_edit`, `pr_comment`, `pr_comment_delete`, `pr_approve`, `pr_unapprove`, `pr_request_changes`, `pr_clear_change_request`, `pr_review`, `pr_merge`, `pr_close`
- Pipelines: `pipeline_list`, `pipeline_view`, `pipeline_log`, `pipeline_run`, `pipeline_stop`
- Repositories: `repo_list`, `repo_diff`, `branch_list`, `branch_view`, `commit_list`, `file_read`
- Snippets: `snippet_list`, `snippet_view`, `snippet_create`, `snippet_edit`, `snippet_rename`, `snippet_delete`
- Accounts: `whoami`, `user_search`, `jira_whoami`
- Jira: `jira_search`, `jira_issue_view`, `jira_issue_create`, `jira_issue_edit`, `jira_assign`, `jira_transition_list`, `jira_transition`, `jira_comment`, `jira_internal_comment`, `jira_comment_delete`, `jira_link`, `jira_worklog_list`, `jira_worklog_add`, `jira_worklog_delete`, `jira_attach`, `jira_fields`, `jira_projects`
- Raw API: `api`

Reads return the same markdown as the CLI's `--raw` or non-TTY output; writes return a small JSON receipt. A failed call returns a tool error with its message and any partial results.

---

## JSON Output

`atlas pr view 123 --json` outputs complete PR data including comments, reviews, and tasks.

`atlas pr list --json` outputs PR list as JSON array.

Single `--json` flag outputs everything - no field selection.

---

## Error Handling

### Message Format

Errors carry actionable hints:
- Auth errors: `Run 'atlas config verify' to check your credentials, or 'atlas login' to set them up`
- Missing scopes (403): `Check that the API token has the scopes this command needs; 'atlas login' lists them`
- Not found: `Check that the <resource> exists and you have access to it`
- Rate limits: the time until the limit resets
- Server errors: `This is a Bitbucket server error. Try again later`

Each error kind also has its own [exit code](#exit-codes).

---

## Implementation Phases

### Phase 1: Core Infrastructure

**Goal**: CLI skeleton with configuration management.

**Deliverables**:
- Cobra command structure (`root`, `pr`, `config` commands)
- Viper config loading from `~/.config/atlas/config.toml`
- `atlas config set/get` commands
- `atlas login` command
- `atlas config verify` command
- Bitbucket API token storage in `~/.config/atlas/credentials.toml`
- `ATLAS_API_TOKEN`, `ATLAS_USERNAME`, and `ATLAS_WORKSPACE` environment overrides

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
- Adding `programs.atlas.enable = true` with `programs.atlas.package` set to home config installs atlas with completions
- `programs.atlas.settings.workspace = "foo"` generates valid config.toml

---

### Phase 2: Bitbucket API Client

**Goal**: HTTP client for Bitbucket Cloud REST API v2.0.

**Deliverables**:
- HTTP client with Basic Auth (username:api_token)
- Base URL: `https://api.bitbucket.org/2.0`
- Error handling with actionable hints
- Pagination support (Bitbucket uses `next` links)
- Disk cache with 5-minute TTL (XDG cache dir)
- `--no-cache` global flag support
- Distinct exit codes per error kind (see [Exit Codes](#exit-codes))

**Endpoints needed**:
- `GET /repositories/{workspace}` - list repos
- `GET /repositories/{workspace}/{repo}/pullrequests` - list PRs
- `GET /repositories/{workspace}/{repo}/pullrequests/{id}` - PR details
- `GET /repositories/{workspace}/{repo}/pullrequests/{id}/comments` - PR comments
- `POST /repositories/{workspace}/{repo}/pullrequests/{id}/comments` - create PR comments and replies
- `POST /repositories/{workspace}/{repo}/pullrequests/{id}/approve` - approve a PR
- `DELETE /repositories/{workspace}/{repo}/pullrequests/{id}/approve` - withdraw approval
- `POST /repositories/{workspace}/{repo}/pullrequests/{id}/request-changes` - request changes
- `DELETE /repositories/{workspace}/{repo}/pullrequests/{id}/request-changes` - withdraw change request
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
- Tabular output with comment and task counts

**Testable outcome**: `atlas pr list --repo myrepo` outputs a table of open PRs.

---

### Phase 4: PR View Command (Basic)

**Goal**: `atlas pr view <id>` shows PR metadata.

**Deliverables**:
- Accept PR ID or branch name
- Branch name resolution (prefer most recent open PR)
- Display PR title, description, author, state, source/destination branches
- Show all assigned reviewers with latest review status
- Comment/task counts
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
- PR comment and review write commands expose returned API objects with `--json`
- All-or-nothing output (no field selection)

**Testable outcome**: Output can be piped to `jq` for processing.

---

### Phase 9: Repository Inference

**Goal**: Infer repo from git remote when `--repo` omitted.

**Deliverables**:
- Find the enclosing git repository, including worktrees and submodules
- Parse `origin` remote to extract workspace/repo
- `--verbose` flag shows inferred values
- Error if no suitable remote found

**Testable outcome**: Running `atlas pr list` in a git repo works without `--repo` flag.

---

### Phase 10: PR Checkout

**Goal**: `atlas pr checkout <id|branch>` fetches PR branch.

**Deliverables**:
- Fetch and checkout PR source branch
- Use remote branch name as-is unless `--branch` is passed
- Fork PRs: add and fetch the source workspace's remote

**Testable outcome**: `atlas pr checkout 123` checks out the PR branch locally.

---

### Phase 11: Snippets

**Goal**: Create, view, edit, clone, rename, and delete Bitbucket snippets.

**Deliverables**:
- `atlas snippet list` supports limit, visibility filters, role filters, and JSON output
- `atlas snippet view [<id|url>]` displays file contents through `bat`/`less` when available
- `atlas snippet create [<file>... | -] --title <title>` creates private snippets by default
- `atlas snippet edit <id|url>` opens snippet files in `$VISUAL` or `$EDITOR`
- `atlas snippet edit <id|url> --add <file>` adds/updates files
- `atlas snippet edit <id|url> --remove <file>` removes files
- `atlas snippet rename <id|url> <old> <new>` renames snippet files
- `atlas snippet clone <id|url>` clones via SSH by default
- `atlas snippet delete [<id|url>]` removes snippet
- `--public` opt-in: snippets are private by default

**Endpoints needed**:
- `GET /snippets/{workspace}` - list snippets
- `GET /snippets/{workspace}/{id}` - snippet metadata
- `GET /snippets/{workspace}/{id}/files/{path}` - file contents
- `POST /snippets/{workspace}` - create snippet (multipart)
- `PUT /snippets/{workspace}/{id}` - update snippet
- `DELETE /snippets/{workspace}/{id}` - delete snippet

**Testable outcome**: `atlas snippet create --title test README.md` creates a private snippet and returns its ID.

---

### Phase 12: Ops Layer, MCP Server, and Jira

**Goal**: Serve every operation from one definition to the CLI and to MCP, and replace the separate `jira` CLI.

**Deliverables**:
- `internal/ops`: typed ops with metadata (name, description, surfaces, read-only/destructive), input schemas, and error kinds
- `atlas mcp serve`: stateless streamable HTTP, credentials from request headers only
- PR checks, merge, and activity; pipelines; repositories, branches, commits, files, users; read-only `api`
- `internal/jira` client and `atlas jira` commands
- Exit codes per error kind; bounded lists and reads; credential redaction in errors
- `ATLAS_BITBUCKET_TOKEN`, `ATLAS_JIRA_TOKEN`, and `ATLAS_CREDENTIALS_FILE`

**Testable outcome**: every CLI command except the local-only ones has an MCP tool (`internal/cli/parity_test.go`), and `tools/list` stays within its size budget (`internal/mcpserver/budget_test.go`).

---

## Future Phases (Out of Scope)

- Confluence integration
- Webhook support

---

## Technical Notes

### Dependencies

- `github.com/spf13/cobra` - CLI framework
- `github.com/spf13/viper` - configuration
- `github.com/JohannesKaufmann/html-to-markdown` - HTML conversion
- `github.com/modelcontextprotocol/go-sdk` - MCP server
- `github.com/nyactl/go-adf` - Atlassian Document Format conversion for Jira

### Project Structure

```
cmd/
  atlas/              # entrypoint
internal/
  cli/                # cobra commands, bind() helper, cliEnv (config + git inference), exit codes, `atlas mcp serve`
  mcpserver/          # registers MCP ops, header credentials, HTTP handler, request logs
  ops/                # Op/Def types, Env, error kinds, input schemas
    catalog/          # every op
    pr/ pipeline/ repo/ snippet/ jira/ api/
  bitbucket/          # Bitbucket REST client, disk cache
  jira/               # Jira REST client (Atlassian gateway), ADF conversion
  config/             # config and credentials loading
  git/                # remote parsing, repo inference, checkout
  output/             # markdown and table renderers
  redact/             # credential redaction
  update/             # self-update and version nudges
```

`internal/ops` never imports cobra, the MCP SDK, `internal/config`, `internal/git`, or terminal code; `internal/ops/layering_test.go` enforces it.

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

Located at `nix/hm-module.nix`. Options:

- `programs.atlas.enable`: enable the module
- `programs.atlas.package`: the atlas package to install; default `null`, which installs nothing
- `programs.atlas.settings`: TOML written to `~/.config/atlas/config.toml`

### Example Usage in home.nix

```nix
{
  imports = [ inputs.atlas.homeManagerModules.default ];

  programs.atlas = {
    enable = true;
    package = inputs.atlas.packages.${pkgs.system}.default;
    settings = {
      workspace = "mycompany";
      username = "user@example.com";
      jira_cloud_id = "your-jira-cloud-id";
    };
  };
}
```

### Implementation Notes

1. **Config format**: TOML for better Nix integration
2. **Secrets**: never put tokens in `settings`; they would land in the Nix store. Keep them in the credentials file, pointed to by `ATLAS_CREDENTIALS_FILE` if a secrets manager decrypts it elsewhere, or in `ATLAS_BITBUCKET_TOKEN`/`ATLAS_JIRA_TOKEN`.
3. **Overlay**: Optional overlay allows `pkgs.atlas` without flake input reference
