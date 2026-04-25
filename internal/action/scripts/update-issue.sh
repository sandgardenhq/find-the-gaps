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

: "${GH_TOKEN:?GH_TOKEN must be set in env}"
: "${GITHUB_REPOSITORY:?GITHUB_REPOSITORY must be set in env}"

label="find-the-gaps"
title="Documentation gaps detected by find-the-gaps"

existing=$(gh issue list --repo "$GITHUB_REPOSITORY" --state open --label "$label" --json number --jq '.[0].number // empty')

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
    gh issue create --repo "$GITHUB_REPOSITORY" --title "$title" --label "$label" --body-file "$body_file"
    echo "Created tracking issue"
  else
    echo "No findings and no existing issue; nothing to do"
  fi
fi
