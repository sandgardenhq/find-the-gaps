#!/usr/bin/env bash
# Resolves the action's git ref to a downloadable release tag.
# Usage: resolve-version.sh <action_ref>
# Outputs:
#   - "vX.Y.Z" verbatim if the ref matches a full semver tag
#   - "latest" otherwise (branches, floating majors, empty)
set -euo pipefail

ref="${1:-}"

if [[ "$ref" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "$ref"
else
  echo "latest"
fi
