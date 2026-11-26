# AGENTS.md

## Repo Learnings

- Local validation should use `CGO_ENABLED=0 go test ./...`; plain `go test ./...` can fail on NixOS without `gcc` because cgo is enabled by default, while the flake package explicitly sets `env.CGO_ENABLED = 0`.
- `nix/package.nix` (wired up by `flake.nix`) builds through `make build` and `make test` from the lowercase `makefile` and installs `bin/atlas`. For local smoke tests, build the ignored binary directly with `CGO_ENABLED=0 go build -o bin/atlas ./cmd/atlas`.
- Every operation lives once in `internal/ops` and is bound to both the CLI and the MCP server. A new CLI command needs an MCP tool unless it is local-only (`internal/cli/parity_test.go`). README.md and SPEC.md list commands and tools by name, so update them when the catalog changes.
- Bitbucket PR diffs can remain available through the API after the source branch is deleted. Structured local diffs need to fall back from fetching the remote source branch to local branches or known commit hashes.
- `atlas pr view` intentionally sends interactive markdown through terminal viewers such as `bat`/`less`; use `--raw` or non-TTY output when scripts need unrendered markdown.
- Bitbucket reviewer display names and mentions are not reliable `/users/{identifier}` inputs. Reviewer inputs send account IDs and UUIDs to `/users`; anything else resolves as an exact display name (or nickname) among the workspace members, and an ambiguous name fails with the candidates.
- When `atlas pr create --head` targets a branch other than the current branch, `--push` must push that named branch ref, not current `HEAD` under the selected branch name.
- `nix build .#image` produces a script, not a tarball: `./result | docker load` to run it locally. The release workflow writes it to a tar and pushes with `skopeo --insecure-policy`, because the GitHub runner has no `/etc/containers/policy.json`. New untracked nix files must be `git add`ed before the flake can see them.
