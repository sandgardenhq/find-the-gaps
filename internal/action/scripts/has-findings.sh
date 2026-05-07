#!/usr/bin/env bash
# Detects whether the find-the-gaps reports contain any actual findings.
#
# Why a content check (not -s): the reporter writes gaps.md / screenshots.md
# with section headers and "_None found._" placeholders even when nothing is
# wrong, so a size check would always be true. The new HTML-card output emits
# a `.ftg-undoc`, `.ftg-stale`, or `.ftg-shot` div per finding; none of those
# classes appear in the placeholder output, so a single grep is unambiguous.
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
  grep -qE 'class="ftg-(undoc|stale|shot)' "$path"
}

if has_finding_lines "$gaps_path" || has_finding_lines "$shots_path"; then
  echo "true"
else
  echo "false"
fi
