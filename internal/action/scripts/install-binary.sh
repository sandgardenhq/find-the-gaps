#!/usr/bin/env bash
# Downloads + extracts a tarball release asset to a target dir.
# Usage: install-binary.sh <download_url> <dest_dir>
set -euo pipefail
url="${1:?download url required}"
dest="${2:?dest dir required}"
mkdir -p "$dest"
curl --fail --silent --show-error --location "$url" | tar -xz -C "$dest"
