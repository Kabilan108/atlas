# AGENTS.md

## Repo Learnings

- Local validation should use `CGO_ENABLED=0 go test ./...`; plain `go test ./...` can fail on NixOS without `gcc` because cgo is enabled by default, while the flake package explicitly sets `env.CGO_ENABLED = 0`.
- `flake.nix` builds through `make build` and `make test` and installs `bin/atlas`, but this checkout may not have a Makefile. For local smoke tests, build the ignored binary directly with `CGO_ENABLED=0 go build -o bin/atlas ./cmd/atlas`.
- Bitbucket PR diffs can remain available through the API after the source branch is deleted. Structured local diffs need to fall back from fetching the remote source branch to local branches or known commit hashes.
- `atlas pr view` intentionally sends interactive markdown through terminal viewers such as `bat`/`less`; use `--raw` or non-TTY output when scripts need unrendered markdown.
- Bitbucket reviewer display names and mentions are not reliable `/users/{identifier}` inputs. Reviewer flags need stable Bitbucket identifiers such as account IDs, UUIDs, or a future lookup flow.
- When `atlas pr create --head` targets a branch other than the current branch, `--push` must push that named branch ref, not current `HEAD` under the selected branch name.
