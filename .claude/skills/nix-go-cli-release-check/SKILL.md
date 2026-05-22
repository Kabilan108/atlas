---
name: nix-go-cli-release-check
description: Validate and prepare Atlas Go CLI release changes in this Nix flake repository. Use when bumping `flake.nix`, committing release changes, changing Go dependencies, validating builds/tests, or preparing local smoke-test binaries for Atlas.
---

# Nix Go CLI Release Check

Use this skill when a change needs to be release-ready for the Atlas flake package.

## Checklist

1. Inspect `git status --short` and avoid staging unrelated user files.
2. If this is a patch release, bump `version` in `flake.nix` by one patch version.
3. If `go.mod` or `go.sum` changed, verify whether `vendorHash` in `flake.nix` must change.
4. Run `CGO_ENABLED=0 go test ./...`.
5. Run `git diff --cached --check` before committing staged release changes.
6. Commit only the intended release files.

## Local Build Notes

- The flake package sets `env.CGO_ENABLED = 0`; use the same setting for local validation.
- Plain `go test ./...` can fail on NixOS without `gcc` because cgo is enabled by default.
- For smoke testing the CLI in this checkout, build the ignored binary directly:

```bash
CGO_ENABLED=0 go build -o bin/atlas ./cmd/atlas
```

- `bin/atlas` is a generated local artifact and should not be committed.

## Flake Notes

- `flake.nix` currently builds through `make build VERSION=${version}` and checks through `make test`.
- If dependency changes require a `vendorHash` update, prefer using the Nix build error's suggested hash rather than guessing.
- Keep the Home Manager module version in `nix/hm-module.nix` aligned when the CLI package version is surfaced there.

## Commit Hygiene

- Review staged files with `git diff --cached --stat`.
- Include the version bump in the same commit when the user asks for release-ready changes.
- Leave post-commit learning artifacts, local notes, and ignored binaries uncommitted unless explicitly requested.
