#!/usr/bin/env bash
# Render the formula template with fake values and diff against the
# golden expected output. Run from any working directory.

set -euo pipefail

cd "$(dirname "$0")"

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

cat > "$TMP/checksums.txt" <<'EOF'
aaaa000000000000000000000000000000000000000000000000000000000000  find-the-gaps_v9.9.9_darwin-arm64.tar.gz
bbbb000000000000000000000000000000000000000000000000000000000000  find-the-gaps_v9.9.9_darwin-amd64.tar.gz
cccc000000000000000000000000000000000000000000000000000000000000  find-the-gaps_v9.9.9_linux-arm64.tar.gz
dddd000000000000000000000000000000000000000000000000000000000000  find-the-gaps_v9.9.9_linux-amd64.tar.gz
EOF

./render.sh v9.9.9 "$TMP/checksums.txt" find-the-gaps.rb.tmpl "$TMP/find-the-gaps.rb"

if ! diff -u find-the-gaps.rb.expected "$TMP/find-the-gaps.rb"; then
  echo "render test FAILED: output does not match find-the-gaps.rb.expected" >&2
  exit 1
fi

echo "render test passed"
