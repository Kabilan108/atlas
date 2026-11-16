#!/usr/bin/env bash
# Writes the GitHub release body for version $1 to stdout: the version's
# CHANGELOG.md section when there is one, then every change merged to master
# since tag $2. A squash merge contributes its subject; a merge commit
# contributes its pull request title, which GitHub puts in the body.
set -euo pipefail

version=$1
previous=${2:-}

notes=$(awk -v v="$version" '/^## /{ if (p) exit; p = ($2 == v); next } p' CHANGELOG.md)
if [ -n "${notes//[[:space:]]/}" ]; then
  printf '%s\n\n' "$notes"
fi

range=HEAD
if [ -n "$previous" ] && git rev-parse -q --verify "refs/tags/$previous" >/dev/null; then
  range="$previous..HEAD"
fi

echo "## Changes since ${previous:-the first commit}"
echo
git log --first-parent --format='%H %P' "$range" | while read -r commit parents; do
  if [ "$(wc -w <<<"$parents")" -gt 1 ]; then
    title=$(git log -1 --format=%b "$commit" | sed '/^[[:space:]]*$/d' | head -n 1)
    pr=$(git log -1 --format=%s "$commit" | sed -n 's/^Merge pull request \(#[0-9]*\).*/\1/p')
    echo "- ${title:-$(git log -1 --format=%s "$commit")}${pr:+ ($pr)}"
  else
    echo "- $(git log -1 --format=%s "$commit")"
  fi
done

cat <<'INSTALL'

## Installation

```bash
curl -fsSL https://raw.githubusercontent.com/kabilan108/atlas/master/install.sh | sh
```

Or `atlas update` from an existing install. Binaries for Linux and macOS (amd64, arm64) and `checksums.txt` are attached.
INSTALL
