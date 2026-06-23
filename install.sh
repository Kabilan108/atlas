#!/usr/bin/env sh
set -eu

repo="kabilan108/atlas"
version="latest"
bin_dir="${HOME}/.local/bin"

usage() {
  cat <<EOF
Install Atlas as a standalone binary.

Usage:
  install.sh [--version vX.Y.Z] [--bin-dir DIR]

Defaults:
  --version latest
  --bin-dir ${HOME}/.local/bin
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --version)
      if [ "$#" -lt 2 ]; then
        echo "missing value for --version" >&2
        exit 1
      fi
      version="$2"
      shift 2
      ;;
    --bin-dir)
      if [ "$#" -lt 2 ]; then
        echo "missing value for --bin-dir" >&2
        exit 1
      fi
      bin_dir="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

need() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "required command not found: $1" >&2
    exit 1
  fi
}

need curl

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"

case "$os" in
  linux|darwin) ;;
  *)
    echo "unsupported operating system: $os" >&2
    exit 1
    ;;
esac

case "$arch" in
  x86_64|amd64)
    arch="amd64"
    ;;
  aarch64|arm64)
    arch="arm64"
    ;;
  *)
    echo "unsupported architecture: $arch" >&2
    exit 1
    ;;
esac

if [ "$version" = "latest" ]; then
  version="$(
    curl -fsSL "https://api.github.com/repos/${repo}/releases/latest" \
      | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' \
      | head -n 1
  )"
fi

if [ -z "$version" ]; then
  echo "failed to determine latest Atlas version" >&2
  exit 1
fi

case "$version" in
  v*) ;;
  *) version="v${version}" ;;
esac

asset="atlas-${os}-${arch}"
base_url="https://github.com/${repo}/releases/download/${version}"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT INT TERM

curl -fsSL "${base_url}/${asset}" -o "${tmp_dir}/${asset}"
curl -fsSL "${base_url}/checksums.txt" -o "${tmp_dir}/checksums.txt"

expected="$(
  awk -v asset="$asset" '$NF == asset { print $1 }' "${tmp_dir}/checksums.txt"
)"
if [ -z "$expected" ]; then
  echo "checksum for ${asset} not found in checksums.txt" >&2
  exit 1
fi

if command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "${tmp_dir}/${asset}" | awk '{ print $1 }')"
elif command -v shasum >/dev/null 2>&1; then
  actual="$(shasum -a 256 "${tmp_dir}/${asset}" | awk '{ print $1 }')"
else
  echo "required command not found: sha256sum or shasum" >&2
  exit 1
fi

if [ "$actual" != "$expected" ]; then
  echo "checksum mismatch for ${asset}" >&2
  exit 1
fi

mkdir -p "$bin_dir"
chmod +x "${tmp_dir}/${asset}"
install -m 0755 "${tmp_dir}/${asset}" "${bin_dir}/atlas"

echo "Installed atlas ${version} to ${bin_dir}/atlas"

case ":$PATH:" in
  *":$bin_dir:"*) ;;
  *)
    echo "Add ${bin_dir} to PATH to run atlas from your shell."
    ;;
esac
