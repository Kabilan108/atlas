# Changelog

## 0.2.1 - 2026-11-14

### Changed

- Name each reviewer's verdict in MCP `pr_list` results (approved, requested changes, pending) and show an explicit Draft column.
- Include the source and destination head commits in PR write receipts.
- Tune MCP tool descriptions from replays of real Bitbucket and Jira workflows, so agents reach for the right tool and trust receipts instead of re-reading.
- Group pipeline runs that Bitbucket failed for the same reason in `pipeline list`.

### Fixed

- Ask Bitbucket's PR list endpoint for reviewers and participants, which it omits by default, so `pr list` shows reviews and the reviewer filter matches.
- Show why Bitbucket failed a pipeline run before any step ran, such as a `bitbucket-pipelines.yml` syntax error, in `pipeline run`, `view`, `log`, and `list`, with secured variable values redacted.
- Reject `atlas api` paths that still hold a template placeholder such as `{workspace}` in the workspace or repository slot, instead of sending them to Bitbucket.

## 0.2.0 - 2026-11-14

### Added

- Serve Atlas as an MCP server with `atlas mcp serve`: 51 tools over stateless streamable HTTP at `/mcp`. The server stores no credentials; each request sends `X-Atlas-Email`, `X-Atlas-Bitbucket-Token`, and `X-Atlas-Jira-Token`.
- Add Jira commands under `atlas jira`: search, view, create, edit, assign, transitions and transition, comment, internal comments on service desk issues, links, worklogs (including batches from a file), attachments, fields, projects, and whoami.
- Show PR build statuses with `atlas pr checks`, optionally waiting for pending checks, and merge PRs with `atlas pr merge`, guarded by `--match-head-commit`.
- Show the PR activity feed with `atlas pr view --activity`. PR view now includes the head commit, created date, merge commit, and reviewer account IDs.
- Edit and delete PR comments with `atlas pr comment --edit` and `--delete`, and change a PR's destination branch with `atlas pr edit --base`.
- List PRs across several repositories with `atlas pr list --repos`, filter with `--updated-since`, list `--state all`, and cap results with `--limit`.
- Prefix diff lines with old and new line numbers using `atlas pr diff --line-numbers`.
- Add `atlas pipeline list`, `view`, `log`, `run`, and `stop`.
- Add `atlas repo list`, `branch list`, `branch view`, `commit list`, `file read` (including one path across many repositories with `--repos`), `user search`, and `whoami`.
- Add `atlas api` for read-only GETs of Bitbucket and Jira REST paths.
- Exit with a distinct code per error kind: usage 2, authentication 3, not found 4, conflict 5, rate limited 6, partial 7, and timeout or still pending 8.
- Read the Bitbucket token from `ATLAS_BITBUCKET_TOKEN` (`ATLAS_API_TOKEN` still works), the Jira token from `ATLAS_JIRA_TOKEN` or `jira_token`, and the credentials file path from `ATLAS_CREDENTIALS_FILE`.
- Add the `jira_cloud_id` config key and `ATLAS_JIRA_CLOUD_ID`.

### Changed

- Accept exact workspace display names, as well as account IDs and UUIDs, in PR reviewer flags.
- Bound every list, response read, diff, log, and file download, and say when output was truncated.
- Redact caller credentials from every Bitbucket and Jira error, and secured variable values from pipeline output.
- List the current Bitbucket token scopes in `atlas login`: add workspace and pipeline scopes, drop `write:repository:bitbucket`.

### Fixed

- Use a PR URL's own repository for every `atlas pr` subcommand, and refuse local git work for a PR URL from another repository.
- Infer the repository inside git worktrees and submodules.

## 0.1.3 - 2026-09-20

### Added

- Create draft pull requests with `atlas pr create --draft`, and toggle existing PRs with `atlas pr edit --draft` or `--ready`.
- Show draft state in PR list and view output, including JSON.

### Fixed

- Invalidate cached reads after creating or editing a PR so subsequent views, lists, and branch lookups reflect the write, including writes made with `--no-cache`.

## 0.1.2 - 2026-07-07

### Fixed

- Point authentication errors to `atlas login` when credentials need setup.

## 0.1.1 - 2026-06-24

### Changed

- Stop appending Atlas attribution footers to submitted PR descriptions and comments.
- Remove the `--no-attribution` flags and `attribution` config key.

## 0.1.0 - 2026-06-23

### Added

- Add standalone Atlas installation through `install.sh`.
- Add `atlas update` for self-updating standalone release binaries.
- Publish Linux and macOS release binaries for amd64 and arm64 with checksums.
- Add cached update nudges with `latest` and `minimum` version metadata.

### Changed

- Set the minimum recommended Atlas version to `0.1.0`.

## 0.0.10 - 2026-06-23

### Added

- Add `atlas login` for Bitbucket API token setup, verification, and secure local token storage.
- Support `ATLAS_API_TOKEN`, `ATLAS_USERNAME`, and `ATLAS_WORKSPACE` environment overrides.

### Changed

- Stop reading or accepting the legacy `app_password` config key.

## 0.0.9 - 2026-06-18

### Added

- Include Bitbucket comment IDs in `atlas pr view --comments` output, including replies, so IDs can be copied directly into `atlas pr comment --reply-to`.

## 0.0.8 - 2026-06-10

### Added

- Normalize standalone Bitbucket and Atlassian links in PR markdown into inline-card markdown links.

### Fixed

- Preserve list markers, fenced code blocks, existing markdown links, and already tagged inline-card links while normalizing PR links.
