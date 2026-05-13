#!/usr/bin/env bash
#
# Fly Machine entrypoint for find-the-gaps.
#
# Clones a repository, runs `ftg analyze` against it with the supplied docs URL
# and --experimental-check-screenshots, packages the report artifacts as a
# tarball, uploads it to Fly Storage, and prints a presigned download URL as
# the last line of stdout.
#
# Usage (inside the image / via `fly machine run`):
#   run-job <repo-url> <docs-url>
#
# Required environment (injected by `fly storage create` as app secrets):
#   BUCKET_NAME
#   AWS_ACCESS_KEY_ID
#   AWS_SECRET_ACCESS_KEY
#   AWS_ENDPOINT_URL_S3

set -euo pipefail

current_step="startup"

on_err() {
    local rc=$?
    printf "run-job: failed during step: %s (exit %d)\n" "$current_step" "$rc" >&2
    exit "$rc"
}
trap on_err ERR

log() {
    printf "run-job: %s\n" "$*" >&2
}

# ---------------------------------------------------------------------------
# Step 1 — ftg doctor
# ---------------------------------------------------------------------------
current_step="ftg doctor"
log "running ftg doctor"
if ! ftg doctor >&2; then
    log "ftg doctor failed — external dependencies missing"
    exit 1
fi

# ---------------------------------------------------------------------------
# Step 2 — read inputs
# ---------------------------------------------------------------------------
current_step="validate inputs"
if [ "$#" -lt 2 ] || [ -z "${1:-}" ] || [ -z "${2:-}" ]; then
    printf "usage: run-job <repo-url> <docs-url>\n" >&2
    exit 2
fi
REPO_URL="$1"
DOCS_URL="$2"

# Required runtime env (fail loud if missing)
: "${BUCKET_NAME:?BUCKET_NAME is not set}"
: "${AWS_ACCESS_KEY_ID:?AWS_ACCESS_KEY_ID is not set}"
: "${AWS_SECRET_ACCESS_KEY:?AWS_SECRET_ACCESS_KEY is not set}"
: "${AWS_ENDPOINT_URL_S3:?AWS_ENDPOINT_URL_S3 is not set}"

# ---------------------------------------------------------------------------
# Step 3 — clone the repo
# ---------------------------------------------------------------------------
current_step="clone repo"
WORK_DIR="$(mktemp -d)"
REPO_NAME="$(basename "$REPO_URL" .git)"
if [ -z "$REPO_NAME" ]; then
    log "could not derive repo name from URL: $REPO_URL"
    exit 1
fi
log "cloning $REPO_URL into $WORK_DIR/$REPO_NAME"
git clone --depth=1 "$REPO_URL" "$WORK_DIR/$REPO_NAME" >&2

# ---------------------------------------------------------------------------
# Step 4 — run ftg analyze
# ---------------------------------------------------------------------------
current_step="ftg analyze"
log "running ftg analyze on $REPO_NAME against $DOCS_URL"
ftg analyze \
    --repo "$WORK_DIR/$REPO_NAME" \
    --docs "$DOCS_URL" \
    --experimental-check-screenshots >&2

# ---------------------------------------------------------------------------
# Step 5 — tarball the report artifacts
# ---------------------------------------------------------------------------
current_step="build tarball"
TIMESTAMP="$(date -u +%Y%m%dT%H%M%SZ)"
TARBALL="/tmp/${REPO_NAME}-${TIMESTAMP}.tar.gz"
PROJECT_DIR="$HOME/.find-the-gaps/$REPO_NAME"

if [ ! -d "$PROJECT_DIR/site" ]; then
    log "expected $PROJECT_DIR/site to exist after analyze; aborting"
    exit 1
fi
if [ ! -d "$PROJECT_DIR/site-src" ]; then
    log "expected $PROJECT_DIR/site-src to exist after analyze; aborting"
    exit 1
fi

shopt -s nullglob
md_files=( "$PROJECT_DIR"/*.md )
shopt -u nullglob
if [ "${#md_files[@]}" -eq 0 ]; then
    log "expected at least one markdown file in $PROJECT_DIR; aborting"
    exit 1
fi

# Build the include list as paths relative to ~/.find-the-gaps/ so the archive
# has <repo-name>/... at the top level.
rel_md=()
for f in "${md_files[@]}"; do
    rel_md+=( "${f#$HOME/.find-the-gaps/}" )
done

log "creating tarball $TARBALL"
cd "$HOME/.find-the-gaps"
tar -czf "$TARBALL" \
    "$REPO_NAME/site" \
    "$REPO_NAME/site-src" \
    "${rel_md[@]}"

# ---------------------------------------------------------------------------
# Step 6 — upload to Fly Storage
# ---------------------------------------------------------------------------
current_step="upload to Fly Storage"
KEY="${REPO_NAME}/${TIMESTAMP}.tar.gz"
log "uploading to s3://${BUCKET_NAME}/${KEY}"
aws s3 cp \
    --endpoint-url "$AWS_ENDPOINT_URL_S3" \
    "$TARBALL" \
    "s3://${BUCKET_NAME}/${KEY}" >&2

# ---------------------------------------------------------------------------
# Step 7 — presigned download URL
# ---------------------------------------------------------------------------
current_step="presign URL"
log "generating presigned download URL (30-day TTL)"
URL="$(aws s3 presign \
    --endpoint-url "$AWS_ENDPOINT_URL_S3" \
    --expires-in 2592000 \
    "s3://${BUCKET_NAME}/${KEY}")"

printf '%s\n' "$URL"
