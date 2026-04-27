#!/usr/bin/env bash
# Downloads + extracts a release asset tarball to a target dir.
# Uses `gh release download` so private/internal repos work via GH_TOKEN.
# Usage: install-binary.sh <repo> <tag> <asset-name> <dest_dir>
set -euo pipefail
repo="${1:?repo required (e.g. owner/name)}"
tag="${2:?tag required}"
asset="${3:?asset name required}"
dest="${4:?dest dir required}"
mkdir -p "$dest"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
gh release download "$tag" --repo "$repo" --pattern "$asset" --dir "$tmp"
tar -xz -C "$dest" -f "$tmp/$asset"
