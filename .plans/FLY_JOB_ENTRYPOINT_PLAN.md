# Fly Job Entrypoint Plan

## Goal

Wrap `ftg` in a single-shot job script that runs as the Fly Machine entrypoint. The script clones a repo, analyzes it, uploads the resulting project directory as a tarball to Fly Storage, and prints a download URL.

## Hard Scope

**Modifiable in this work:**
- New file: shell script (proposed path: `deploy/fly/run-job.sh`)
- `Dockerfile`
- `fly.toml`
- `README.md` (add a "Running on Fly.io" section)

**Forbidden:**
- Any change to `ftg` source code, flags, subcommands, or behavior.
- Any new `ftg` capability. The script is a pure wrapper.

## Script Contract

**Path inside the image:** `/usr/local/bin/run-job`

**Invocation:**
```
run-job <repo-url> <docs-url>
```
Both arguments are required positional. The script exits non-zero with a clear error if either is missing.

**Environment required at runtime (injected by `fly storage create` as app secrets):**
- `BUCKET_NAME`
- `AWS_ACCESS_KEY_ID`
- `AWS_SECRET_ACCESS_KEY`
- `AWS_ENDPOINT_URL_S3`

**Stdout (last line):** the download URL for the tarball. All progress/diagnostic output goes to stderr.

**Exit codes:**
- `0` — analysis ran, tarball uploaded, URL printed
- non-zero — fail loud, no partial success

## Script Behavior — Step by Step

The script begins with `set -euo pipefail` and an `ERR` trap that prints a clear message naming the failing step.

### Step 1 — `ftg doctor`
Run `ftg doctor`. If it exits non-zero, print the doctor output to stderr, then exit non-zero with a message like `ftg doctor failed — external dependencies missing`. No retry, no fallback.

### Step 2 — Read inputs
Parse positional args `$1` (repo URL) and `$2` (docs URL). Validate both are non-empty.

### Step 3 — Clone the repo
- Create a working directory: `mktemp -d` under `/tmp` (e.g., `/tmp/repo-<random>`).
- Derive a repo name from the URL: `basename "$REPO_URL" .git`.
- Clone: `git clone --depth=1 "$REPO_URL" "$WORK_DIR/$REPO_NAME"`.
- Shallow clone is intentional — `ftg` analyzes the working tree, not history.

### Step 4 — Run analyze
```
ftg analyze \
  --repo "$WORK_DIR/$REPO_NAME" \
  --docs "$DOCS_URL" \
  --experimental-check-screenshots
```
No other flags. If `ftg analyze` exits non-zero, propagate the exit code.

### Step 5 — Tarball the report artifacts only
- **Decision needed (D1 below):** confirm the path `ftg` writes to in this container. Plan assumes `~/.find-the-gaps/<repo-name>/` (i.e., `$HOME/.find-the-gaps/$REPO_NAME/`).
- The tarball includes **only** the report artifacts `ftg` produces — not the full project directory:
  - `site/` — the rendered Hugo site
  - `site-src/` — the Hugo source tree (kept by default; matches `ftg`'s `--keep-site-source=true` default)
  - Any `*.md` file at the top level of the project directory (`gaps.md`, `mapping.md`, and — because Step 4 passes `--experimental-check-screenshots` — `screenshots.md`)
- Everything else under the project directory is excluded (JSON files like `drift.json` / `screenshots.json` / `screenshots-cache.json`, the `scan/` directory, any other caches).
- Timestamp: `TIMESTAMP=$(date -u +%Y%m%dT%H%M%SZ)`.
- Tarball path: `/tmp/${REPO_NAME}-${TIMESTAMP}.tar.gz`.
- Command (assemble the include list with glob expansion at array-assignment time so spaces and missing-glob cases are handled safely):
  ```
  cd "$HOME/.find-the-gaps"
  md_files=( "$REPO_NAME"/*.md )
  tar -czf "$TARBALL" \
    "$REPO_NAME/site" \
    "$REPO_NAME/site-src" \
    "${md_files[@]}"
  ```
  This produces a tarball whose top-level entry is `<repo-name>/`, containing only the three artifact groups above. Users get a sensibly-named directory when they extract.
- **Decision needed (D6 below):** behavior if `site/`, `site-src/`, or any markdown is missing. Plan assumes fail loud — if `ftg analyze` succeeded but didn't produce the expected outputs, that's a real signal, not something to paper over.

### Step 6 — Upload to Fly Storage
Use `aws` CLI v2 (added to the image in the Dockerfile change below).
- Key: `${REPO_NAME}/${TIMESTAMP}.tar.gz`
- Command:
  ```
  aws s3 cp \
    --endpoint-url "$AWS_ENDPOINT_URL_S3" \
    "$TARBALL" \
    "s3://${BUCKET_NAME}/${REPO_NAME}/${TIMESTAMP}.tar.gz"
  ```
- If the upload fails, propagate the exit code.

### Step 7 — Print a download URL
- Generate a presigned URL for the uploaded object:
  ```
  aws s3 presign \
    --endpoint-url "$AWS_ENDPOINT_URL_S3" \
    --expires-in 2592000 \
    "s3://${BUCKET_NAME}/${REPO_NAME}/${TIMESTAMP}.tar.gz"
  ```
- Print it as the **last line of stdout**, with no surrounding decoration, so `fly logs | tail -1` retrieves it cleanly.
- TTL `2592000` seconds = 30 days. See decision D2.

## `Dockerfile` Changes

Three changes to the runtime stage:

1. **Install runtime deps** that the script needs but the base image doesn't have:
   - `git` (for `git clone`)
   - AWS CLI v2 (for `aws s3 cp` and `aws s3 presign`)

   AWS CLI v2 ships as a self-contained zip from Amazon; install it with the standard amd64/arm64 conditional already used for Hugo. Adds ~85 MB to the image.

2. **Copy the script** into `/usr/local/bin/run-job` with execute permissions.

3. **Swap the entrypoint:**
   ```
   ENTRYPOINT ["/usr/local/bin/run-job"]
   ```
   Remove the existing `CMD ["--help"]` line.

The build stage is unchanged. `ftg` itself is unchanged.

## `fly.toml` Changes

Remove the `[processes]` block. The job has a single flow now (script → analyze → upload), so there is nothing to name separately. Keep `app`, `primary_region`, `[build]`, `[[vm]]`.

The resulting `fly.toml` will look like:
```
app = 'find-the-gaps'
primary_region = 'ord'

[build]
  [build.args]
    GO_VERSION = '1.26.2'

[[vm]]
  memory  = '1gb'
  cpu_kind = 'shared'
  cpus    = 1
  memory_mb = 1024
```

## `README.md` Changes

Add one new top-level section, **"Running on Fly.io"**, near the end of the README (after existing usage docs, before any contributing/license sections). The section covers, in order:

1. **Prerequisites** — Fly account, `flyctl` installed and authenticated.
2. **One-time setup** — `fly storage create` against the app to provision a Fly Storage (Tigris) bucket and inject `BUCKET_NAME`, `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_ENDPOINT_URL_S3` as app secrets.
3. **Deploying the image** — `fly deploy` against this repo to publish the wrapped image to `registry.fly.io/<app>`.
4. **Running a job** — the `fly machine run … -- <repo-url> <docs-url>` invocation, with `--rm` and `--region` notes.
5. **Retrieving the report** — read the last line of stdout (foreground run) or `fly logs | tail -1` (detached run); the presigned URL is valid for 30 days.
6. **Cleanup expectations** — Machines auto-destroy with `--rm`; tarballs in the bucket are kept forever unless the operator sets a lifecycle rule (mention the operator's responsibility, do not invent retention policy).

The section MUST NOT change any existing README content describing local `ftg` usage. Fly is purely additive.

Decision needed (D5 below): confirm placement / heading text.

## Invocation (informational — no work to do here)

Once the image is deployed and `fly storage create` has been run against the app:
```
fly machine run registry.fly.io/find-the-gaps:latest \
  --app find-the-gaps --rm --region ord \
  -- https://github.com/owner/repo https://owner.example.com/docs
```
The two positional args are appended after `--` and reach the script as `$1` and `$2`.

## Decisions to Confirm Before Writing Code

| ID | Question | Plan assumes |
|---|---|---|
| D1 | Where does `ftg analyze` write its project directory inside this container? | `$HOME/.find-the-gaps/<repo-name>/` |
| D2 | Presigned URL TTL? | 30 days (`2592000` seconds) |
| D3 | Confirm AWS CLI v2 as the upload + presign tool (alternatives: `s5cmd`, `mc`, hand-rolled Go) | AWS CLI v2 |
| D4 | Script source path in the repo? | `deploy/fly/run-job.sh` |
| D5 | README section heading + placement? | `## Running on Fly.io`, near end, before contributing/license |
| D6 | Behavior if `site/`, `site-src/`, or any markdown is missing after analyze? | Fail loud (non-zero exit, no upload) |

## Out of Scope (Explicit)

- Bucket provisioning (`fly storage create`) — operator's responsibility, run once
- Secret provisioning — handled by `fly storage create` setting app secrets
- Local-machine usage of `ftg` — unchanged, not touched by this work
- Any retention/cleanup of old tarballs in the bucket — not addressed
- Any change to `ftg`'s output format, project-dir layout, flags, or subcommands — forbidden
