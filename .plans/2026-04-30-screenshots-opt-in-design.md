# Screenshots Opt-In Design

**Date:** 2026-04-30
**Status:** Approved, ready for implementation plan

## Motivation

The missing-screenshot detection pass currently runs on every `analyze` invocation and is opted out via `--skip-screenshot-check`. The feature works but is not yet polished enough to be on by default — findings can be noisy and the underlying prompt is still being tuned. Until the feature graduates, it should be off by default and behind a flag whose name communicates that it is experimental.

This is a temporary inversion. When the feature graduates, the default flips back on and the experimental flag is removed (or kept as a deprecated alias for one release).

## CLI surface

A single new flag replaces the existing `--skip-screenshot-check`:

```
--experimental-check-screenshots    enable experimental missing-screenshot detection (default: false)
```

`--skip-screenshot-check` is removed entirely. There is no need for a "skip wins" veto matrix; the pass simply does not run unless the user opts in.

The flag name carries the experimental warning. No additional stderr notice is needed when the pass runs.

## GitHub Action surface

`action.yml` drops the `skip-screenshot-check` input and adds:

```yaml
experimental-check-screenshots:
  description: 'Run the experimental missing-screenshot detection pass'
  required: false
  default: 'false'
```

The composite step appends `--experimental-check-screenshots` to the CLI invocation only when the input is `'true'`. Workflow consumers who still pass `skip-screenshot-check:` will see GitHub's standard "Unexpected input" warning until they update.

The self-test workflow (`.github/workflows/action-self-test.yml`) currently passes `skip-screenshot-check: 'true'`. That line is deleted; the pass is now off by default, which is what the self-test wants.

## Code changes

- `internal/cli/analyze.go` — replace the `skipScreenshotCheck` variable and its `BoolVar` registration with `experimentalCheckScreenshots`. The pass-runs condition flips from `!skipScreenshotCheck` to `experimentalCheckScreenshots`. Help text on the new flag includes the word "experimental".
- `internal/cli/analyze_test.go` and `internal/cli/analyze_skip_drift_test.go` — update any `--skip-screenshot-check` invocations to either drop the flag (default-off path) or pass `--experimental-check-screenshots` (opt-in path).
- `cmd/find-the-gaps/testdata/script/skip_screenshot_check.txtar` — delete.
- `cmd/find-the-gaps/testdata/script/default_screenshot_check.txtar` — rewrite to assert the new default (no `screenshots.md`, `screenshots.md (skipped)` line in `reports:`).
- New testscript fixture `cmd/find-the-gaps/testdata/script/experimental_check_screenshots.txtar` — covers the opt-in path: passing the flag writes `screenshots.md` and stdout lists it without the `(skipped)` annotation.
- `internal/reporter/reporter.go` — no change. The reporter's `WriteScreenshots` already does the right thing; only the call-site condition in `analyze.go` flips.
- `internal/action/manifest_test.go` and `internal/action/build_issue_body_test.go` — update fixture inputs to the new name.

## Living-doc updates

- **`README.md`** — the help-block excerpt picks up the new flag automatically via the existing help-sync. The `screenshots.md` section description and the action-input table are edited to reference `--experimental-check-screenshots` / `experimental-check-screenshots`.
- **`.plans/VERIFICATION_PLAN.md` Scenario 5** — flip the steps and success criteria. Default run does not write `screenshots.md`; opt-in run does.
- **`CHANGELOG.md`** — single Unreleased entry covering both breaking changes:
  ```
  ### Changed
  - **BREAKING:** missing-screenshot detection is now off by default
    and marked experimental. Pass `--experimental-check-screenshots`
    (CLI) or set `experimental-check-screenshots: 'true'` (Action) to
    opt in.
  - **BREAKING:** removed `--skip-screenshot-check` flag and the
    `skip-screenshot-check` Action input. Workflows passing the old
    input will see an "Unexpected input" warning until updated.
  ```

Historical plan docs (`.plans/2026-04-23-missing-screenshots-design.md`, `.plans/2026-04-24-split-screenshot-report-*.md`, `.plans/MISSING_SCREENSHOTS_IMPLEMENTATION_PLAN.md`) are not edited. They describe the feature as it shipped; rewriting them would falsify the history.

## Out of scope

- Tuning the screenshot prompt. The maturity-driven flip is independent of any prompt work; that lives in its own future plan.
- A deprecation path for `--skip-screenshot-check`. The flag is removed outright, not deprecated. Users hit a clear "unknown flag" error rather than a silent no-op.
- Re-graduating the feature. When the prompt and signal quality reach the bar, a follow-up design will flip the default back to on and decide whether to retire `--experimental-check-screenshots` or keep it as an alias for one release.
