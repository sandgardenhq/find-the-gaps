#!/usr/bin/env bash
# Detects whether the find-the-gaps reports contain any actual findings.
#
# Why a content check (not -s): the reporter writes gaps.md with section
# headers and "_None found._" placeholders even when nothing is wrong, so a
# size check would always be true. A finding is signaled by a "- " bullet:
# undocumented/unmapped/stale entries in gaps.md, and "- **Passage:**" lines
# in screenshots.md. "### " is unreliable (gaps.md always emits "### User-facing"
# and "### Not user-facing" subsection headers).
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
  grep -qE '^- ' "$path"
}

if has_finding_lines "$gaps_path" || has_finding_lines "$shots_path"; then
  echo "true"
else
  echo "false"
fi
