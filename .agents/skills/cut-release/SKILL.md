---
name: cut-release
description: Cut a new Atlas CLI release. Use when asked to release Atlas, bump the release version, create or update CHANGELOG.md, document user-facing changes, validate the Go/Nix build, create the release commit, tag the release, and push branch plus tag.
---

# Cut Release

## Workflow

1. Inspect state first:
   - `git status --short --branch`
   - `git tag --list 'v*' --sort=-v:refname`
   - `git log --oneline --decorate -20`
   - `sed -n '1,220p' flake.nix`

2. Choose the release version:
   - Treat `flake.nix` `version = "...";` as the package version.
   - If `flake.nix` is already ahead of the latest tag, release that version only when it has not already been tagged.
   - Otherwise bump the patch version unless the user explicitly requested a minor/major release.
   - Use a `vX.Y.Z` git tag for version `X.Y.Z`.

3. Document the release:
   - Create `CHANGELOG.md` if missing.
   - Add a top entry `## X.Y.Z - YYYY-MM-DD`.
   - Include concise user-facing bullets under `### Added`, `### Changed`, or `### Fixed`.
   - Include any already-version-bumped but untagged prior release notes if they are missing.
   - Update `SPEC.md` only for behavior or CLI contract changes.

4. Edit release files:
   - Update `flake.nix` with the selected version.
   - Format `flake.nix` with `nixfmt` or `alejandra`.

5. Validate:
   - Run `CGO_ENABLED=0 go test ./...`.
   - Run `nix flake check`.
   - If validation fails, fix the issue before committing.

6. Commit safely:
   - Check for large files with `find . -type f -size +1M -not -path './.git/*' -print`; ignored build outputs such as `bin/atlas` must not be staged.
   - Stage only intentional release files.
   - Use a commit message like `Release atlas vX.Y.Z` with bullets summarizing the version bump, changelog, and behavior changes.

7. Tag and push:
   - Create an annotated tag: `git tag -a vX.Y.Z -m "Release vX.Y.Z"`.
   - Push the current branch and tag together: `git push origin <branch> vX.Y.Z`.
   - Finish with the commit hash, tag, pushed branch, and validation commands.

## Project Notes

- On NixOS, prefer `CGO_ENABLED=0 go test ./...`; plain `go test ./...` may fail when cgo is enabled.
- Do not remove or overwrite unrelated user changes. If unrelated changes are present, leave them unstaged unless the release cannot proceed without them.
- The ignored `bin/` directory may contain a large local binary; do not commit it.
