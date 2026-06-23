# Changelog

## 0.0.10 - 2026-06-23

### Added

- Add `atlas login` for Bitbucket API token setup, verification, and secure local token storage.
- Support `ATLAS_API_TOKEN`, `ATLAS_USERNAME`, and `ATLAS_WORKSPACE` environment overrides.
- Append a marked `Submitted via Atlas CLI.` footer to PR descriptions and comments submitted by Atlas, with `--no-attribution` and `attribution = false` opt-outs.

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
