# Changelog

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
