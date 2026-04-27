#!/usr/bin/env bash
# Render the find-the-gaps Homebrew formula from the template.
#
# Usage: render.sh <VERSION> <checksums-file> <template-file> <output-file>
#   VERSION is the git tag, e.g. v1.2.3
#   The leading "v" is stripped before substitution into the formula's
#   version field, but kept in URLs and tarball filenames.

set -euo pipefail

if [ "$#" -ne 4 ]; then
  echo "usage: $0 <VERSION> <checksums-file> <template-file> <output-file>" >&2
  exit 2
fi

VERSION="$1"
CHECKSUMS="$2"
TEMPLATE="$3"
OUTPUT="$4"

V="${VERSION#v}"

sha() {
  local target="$1"
  awk -v f="find-the-gaps_${VERSION}_${target}.tar.gz" '$2==f{print $1}' "$CHECKSUMS"
}

SHA_DARWIN_ARM64=$(sha darwin-arm64)
SHA_DARWIN_AMD64=$(sha darwin-amd64)
SHA_LINUX_ARM64=$(sha linux-arm64)
SHA_LINUX_AMD64=$(sha linux-amd64)

for var in SHA_DARWIN_ARM64 SHA_DARWIN_AMD64 SHA_LINUX_ARM64 SHA_LINUX_AMD64; do
  if [ -z "${!var}" ]; then
    echo "render.sh: missing checksum for $var (no entry for find-the-gaps_${VERSION}_*.tar.gz in $CHECKSUMS)" >&2
    exit 1
  fi
done

sed \
  -e "s|{{VERSION}}|$V|g" \
  -e "s|{{SHA_DARWIN_ARM64}}|$SHA_DARWIN_ARM64|" \
  -e "s|{{SHA_DARWIN_AMD64}}|$SHA_DARWIN_AMD64|" \
  -e "s|{{SHA_LINUX_ARM64}}|$SHA_LINUX_ARM64|" \
  -e "s|{{SHA_LINUX_AMD64}}|$SHA_LINUX_AMD64|" \
  "$TEMPLATE" > "$OUTPUT"
