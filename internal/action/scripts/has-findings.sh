#!/usr/bin/env bash
# Detects whether the find-the-gaps reports contain any actual findings.
#
# Why a content check (not -s): the reporter writes gaps.md / screenshots.md
# with section headers and "_None found._" placeholders even when nothing is
# wrong, so a size check would always be true. The reporter only emits:
#   - `## Undocumented Features` when at least one user-facing feature is
#     undocumented (the section is omitted entirely when empty), and
#   - `### Large` / `Medium` / `Small` priority sub-headings inside Stale
#     Documentation / Missing Screenshots / Possibly Covered / Image Issues
#     sections when at least one finding lives in that bucket.
# The presence of any of those lines unambiguously means at least one finding.
#
# Usage: has-findings.sh <gaps_path> <screenshots_path>
# Outputs: "true" or "false" on stdout. Non-existent files are treated as
# having no findings.
set -euo pipefail

gaps_path="${1:-}"
shots_path="${2:-}"

has_finding_lines() {
  local path="$1"
  [[ -f "$path" ]] || return 1
  grep -qE '^## Undocumented Features$|^### (Large|Medium|Small)$' "$path"
}

if has_finding_lines "$gaps_path" || has_finding_lines "$shots_path"; then
  echo "true"
else
  echo "false"
fi
