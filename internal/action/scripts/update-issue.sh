#!/usr/bin/env bash
# Opens or updates a single tracking issue with the latest findings.
# Behavior:
#   - Search OPEN issues with label `find-the-gaps`.
#   - If one exists: edit body. If findings empty, post a "no gaps" comment
#     instead of editing the body, and do not close the issue.
#   - If none exists AND findings non-empty: create a new issue.
#   - Closed issues are never reopened.
# Usage: update-issue.sh <body_file> <findings_present>
# Env: GH_TOKEN, GITHUB_REPOSITORY
set -euo pipefail

body_file="${1:?body file required}"
findings_present="${2:?true/false required}"

[[ -f "$body_file" ]] || { echo "update-issue.sh: body file not found: $body_file" >&2; exit 1; }

: "${GH_TOKEN:?GH_TOKEN must be set in env}"
: "${GITHUB_REPOSITORY:?GITHUB_REPOSITORY must be set in env}"

label="find-the-gaps"
title="Documentation gaps detected by find-the-gaps"

# --limit 1 + default sort (created-desc) picks the most recently created open
# issue deterministically when multiple exist (race conditions, manual creation).
existing=$(gh issue list --repo "$GITHUB_REPOSITORY" --state open --label "$label" --limit 1 --json number --jq '.[0].number // empty')

if [[ -n "$existing" ]]; then
  if [[ "$findings_present" == "true" ]]; then
    gh issue edit "$existing" --repo "$GITHUB_REPOSITORY" --body-file "$body_file"
    echo "Updated issue #$existing"
  else
    gh issue comment "$existing" --repo "$GITHUB_REPOSITORY" --body "Latest run found no gaps."
    echo "Commented on issue #$existing (no findings)"
  fi
else
  if [[ "$findings_present" == "true" ]]; then
    gh label create "$label" --repo "$GITHUB_REPOSITORY" --color "0075ca" --description "Documentation gap tracking by find-the-gaps" 2>/dev/null || true
    gh issue create --repo "$GITHUB_REPOSITORY" --title "$title" --label "$label" --body-file "$body_file"
    echo "Created tracking issue"
  else
    echo "No findings and no existing issue; nothing to do"
  fi
fi
