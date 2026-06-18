---
name: bitbucket-pr-cli-design
description: Design, implement, debug, or review Atlas Bitbucket Cloud pull request CLI behavior. Use when changing `atlas pr` commands, Bitbucket PR REST payloads, reviewer handling, PR diffs, branch checkout behavior, or gh-style PR command compatibility in this repository.
---

# Bitbucket PR CLI Design

Use this skill to keep Atlas PR commands consistent, scriptable, and honest about Bitbucket Cloud limitations.

## Workflow

1. Read the current command implementation in `internal/cli/pr.go`, Bitbucket API methods in `internal/bitbucket/`, and git helpers in `internal/git/checkout.go` before changing behavior.
2. Match `gh pr` where it improves CLI ergonomics, but preserve Atlas-specific choices already established in this repo.
3. Prefer explicit flags for scripts and terminal-aware rendering only when stdout is interactive.
4. Add focused tests in `internal/cli/pr_test.go` for command parsing, branch/ref selection, and editor/body behavior.
5. Validate with `CGO_ENABLED=0 go test ./...`.

## Established Behavior

- `atlas pr view` may infer the current branch when no PR selector is passed.
- `atlas pr view --raw` and non-TTY output should preserve raw markdown for scripts.
- Interactive `atlas pr view` should prefer terminal viewers such as `bat` or `less` instead of custom markdown rendering.
- `atlas pr diff` should prefer `delta` for normal interactive diffs when available.
- `atlas pr diff -s` and `atlas pr diff --structured` should prefer `difftastic` when available, then fall back to normal patch output.
- `atlas pr diff --patch` and `--name-only` are script-oriented and should not route through visual diff tools.
- `atlas pr edit` should open `$EDITOR`, falling back to `nvim`/`vi`, only when no explicit edit flags are passed.
- `atlas pr create --head <branch> --push` must push the selected branch ref. Do not push current `HEAD` under a different branch name.

## Bitbucket Caveats

- Bitbucket closed PRs in this CLI map to Bitbucket `DECLINED` state.
- Bitbucket reviewer display names and mentions are not stable `/users/{identifier}` inputs. Avoid implying that `@Display Name` is sufficient; stable account IDs, UUIDs, or an explicit lookup flow are safer.
- Bitbucket API PR diffs can remain available after the source branch is deleted. Structured local diff generation must handle missing remote source refs by falling back to local branches or commit hashes where possible.
- Repository selectors should accept `workspace/repo` and plain `repo` forms consistently with existing Atlas config resolution.

## Review Checklist

- Check non-interactive behavior with pipes or `--raw` before changing rendering.
- Check missing tool behavior for `bat`, `delta`, and `difftastic`.
- Check closed PR and deleted-branch cases for diff code.
- Check dirty working tree behavior before changing checkout code.
- Check that cache bypass behavior still respects `--no-cache` for PR reads.
