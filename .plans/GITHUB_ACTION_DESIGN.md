# GitHub Action Design

**Date:** 2026-04-24
**Status:** Validated, ready for implementation planning
**Source branch:** `feat/github-action`

## Purpose

Package Find the Gaps as a GitHub Action so maintainers can audit their docs automatically — on a schedule, before a release, or on demand — without installing anything locally. The action runs the existing CLI against the checked-out repo and a user-supplied docs URL, then surfaces findings as a workflow artifact and (optionally) a tracking issue.

## Non-Goals (v1)

- **Per-PR comments / PR-gated runs.** Out of scope.
- **Cross-platform runners.** Linux-amd64 only.
- **Customization beyond the four inputs below.** No issue title/label overrides, no version pin, no working-directory input. Add only when a real user asks.

## Interface

**Location.** `action.yml` at the root of the `find-the-gaps` repo. Released alongside the CLI: tag `v1.2.3` publishes both. Users pin as `sandgarden/find-the-gaps@v1.2.3` or `@v1`.

**Kind.** Composite action. Shell steps download the matching release binary of `find-the-gaps`, install `mdfetch`, then invoke `find-the-gaps analyze`.

**Inputs.**

| Name | Required | Default | Notes |
|---|---|---|---|
| `docs-url` | yes | — | Passed to `--docs-url` |
| `bifrost-api-key` | yes | — | Set as env var for the CLI; user provides via `${{ secrets.BIFROST_API_KEY }}` |
| `create-issue` | no | `true` | When false, skip the issue step |
| `skip-screenshot-check` | no | `false` | Passes `--skip-screenshot-check` |

## Composite Steps

1. **Resolve version.** Read `github.action_ref` to determine which `find-the-gaps` release to install. If the ref is a branch (not a tag), fall back to `latest`. Log which version was selected.
2. **Install `find-the-gaps`.** `curl` the tarball from the matching GitHub Release (`find-the-gaps_<version>_linux_amd64.tar.gz`), extract to `$RUNNER_TEMP/ftg/`, chmod, and prepend to `$GITHUB_PATH`.
3. **Install `mdfetch`.** Same pattern from its own repo. Version is a constant in `action.yml`, bumped deliberately.
4. **Run analysis.** Invoke `find-the-gaps analyze --repo . --docs-url "$DOCS_URL"` (plus `--skip-screenshot-check` if set). Stream output to logs.
5. **Upload artifact.** `actions/upload-artifact@v7` attaches `gaps.md` and `screenshots.md` as `find-the-gaps-report-<run_id>`. Always runs.
6. **Open/update issue** (only if `create-issue=true` and findings exist). Use `gh issue` with `GITHUB_TOKEN`: search for an open issue labeled `find-the-gaps`; if one exists, edit its body; otherwise create one. Never reopen closed issues.
7. **Exit status.** Always `0` unless infrastructural failure (binary download, CLI crash, auth). Findings are not failures — the issue/artifact is the signal.

## Issue Format

- **Title (constant):** `Documentation gaps detected by find-the-gaps`
- **Label (constant):** `find-the-gaps`
- **Body:** contents of `gaps.md` verbatim, prefixed with run URL, commit SHA, timestamp. If `screenshots.md` exists, append under a `## Screenshot Gaps` heading.

## Example Workflows

Three examples ship under `docs/examples/`:

### Schedule (nightly drift)

```yaml
name: Docs drift
on:
  schedule: [{ cron: '0 7 * * *' }]
  workflow_dispatch:
jobs:
  audit:
    runs-on: ubuntu-latest
    permissions: { contents: read, issues: write }
    steps:
      - uses: actions/checkout@v6
      - uses: sandgarden/find-the-gaps@v1
        with:
          docs-url: https://docs.example.com
          bifrost-api-key: ${{ secrets.BIFROST_API_KEY }}
```

### Release (pre-tag check, artifact only)

```yaml
on: { push: { tags: ['v*'] } }
jobs:
  audit:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v6
      - uses: sandgarden/find-the-gaps@v1
        with:
          docs-url: https://docs.example.com
          bifrost-api-key: ${{ secrets.BIFROST_API_KEY }}
          create-issue: 'false'
```

### Manual

`workflow_dispatch` is just an `on:` entry — no extra action support needed.

## Error Handling & Edge Cases

- **Binary download failures:** fail fast with a message naming the URL that 404'd. No retry loop.
- **Missing `BIFROST_API_KEY`:** action exits 1 with a message pointing at the input name before invoking the CLI.
- **`mdfetch` fetch errors:** propagate the CLI exit code. False negatives from a silent failure are worse than a loud one.
- **No findings:** artifacts always upload (empty if needed). Issue step skipped. If a prior open issue exists and findings are empty, **do not auto-close** — leave a comment: `Latest run found no gaps.`
- **Closed issues:** search filters `is:open`. Never reopened.
- **Permissions:** examples document `issues: write` and `contents: read`.
- **Concurrency:** no locking. Worst case: maintainer dedups one issue.
- **Non-Linux runners:** action errors with "linux-amd64 only" if `runner.os != 'Linux'`.

## Testing

- **Shell logic in isolation.** Extract non-trivial pieces (version resolution, issue search/update) into scripts under `.github/action-scripts/`. Test with `bats` or Go test wrappers. Keep `action.yml` thin.
- **Self-test workflow.** `.github/workflows/action-self-test.yml` runs the action against this repo's own docs on every PR touching `action.yml` or the action scripts. End-to-end coverage of the composite.
- **Verification scenario.** Add Scenario 10 to `.plans/VERIFICATION_PLAN.md` covering tagged-release usage, `create-issue: false`, and the closed-issue dismissal behavior.
- **No mocks.** Per VERIFICATION_PLAN: real `mdfetch`, real Bifrost, real GitHub API.

## Versions Verified

- `actions/checkout@v6.0.2` — Node 24
- `actions/upload-artifact@v7.0.1` — Node 24
