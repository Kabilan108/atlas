# Changelog

## 0.0.9 - 2026-06-18

### Added

- Include Bitbucket comment IDs in `atlas pr view --comments` output, including replies, so IDs can be copied directly into `atlas pr comment --reply-to`.

## 0.0.8 - 2026-06-10

### Added

- Normalize standalone Bitbucket and Atlassian links in PR markdown into inline-card markdown links.

### Fixed

- Preserve list markers, fenced code blocks, existing markdown links, and already tagged inline-card links while normalizing PR links.
