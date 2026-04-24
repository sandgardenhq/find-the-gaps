# Split Screenshot Findings Into Their Own Report — Design

**Status**: Design approved 2026-04-24. Ready for implementation planning.

## Goal

Move the "Missing Screenshots" findings out of `gaps.md` and into a new sibling
report, `screenshots.md`. `gaps.md` keeps Undocumented Code, Unmapped Features,
and Stale Documentation.

## Motivation

Both the drift findings and the screenshot findings can each run long on a
real docs site. Interleaving them in a single `gaps.md` makes both hard to
find. Splitting screenshots out is a signal-to-noise fix: each artifact is
shorter and scannable on its own.

## Non-Goals

- Splitting drift into its own file. `gaps.md` still contains Stale
  Documentation.
- Renaming `gaps.md`.
- Changing exit-code semantics. Screenshot findings still contribute to a
  non-zero exit the same way they do today.
- JSON or other structured output formats.
- Severity / priority scoring.

## Reporter API

Split `reporter.WriteGaps` and add `reporter.WriteScreenshots`.

**Before:**

```go
func WriteGaps(
    dir string,
    mapping analyzer.FeatureMap,
    allDocFeatures []string,
    drift []analyzer.DriftFinding,
    screenshotGaps []analyzer.ScreenshotGap,
) error
```

**After:**

```go
// Writes gaps.md — Undocumented Code, Unmapped Features, Stale Documentation.
// Screenshot findings are no longer part of this file.
func WriteGaps(
    dir string,
    mapping analyzer.FeatureMap,
    allDocFeatures []string,
    drift []analyzer.DriftFinding,
) error

// Writes screenshots.md. Call ONLY when the screenshot pass actually ran.
// Zero-length gaps is valid and produces a "_None found._" body; the caller
// must NOT call this function at all when --skip-screenshot-check was set.
func WriteScreenshots(dir string, gaps []analyzer.ScreenshotGap) error
```

Rationale for removing `screenshotGaps` from `WriteGaps` entirely instead of
keeping it as an optional/`nil`-tolerated argument: the caller already knows
whether the pass ran. Keeping the argument would hide that decision inside the
reporter and tempt a future change to re-couple the two reports.

## Output Format

### `gaps.md`

Unchanged shape minus the `## Missing Screenshots` section. The header, the
three existing sections (Undocumented Code, Unmapped Features, Stale
Documentation), and their "_None found._" placeholders remain exactly as today.

### `screenshots.md`

New file. Root heading is promoted from `##` to `#`:

```markdown
# Missing Screenshots

### <page-url-1>

- **Passage:** "..."
  - **Screenshot should show:** ...
  - **Alt text:** ...
  - **Insert:** ...

### <page-url-2>

- ...
```

Zero-gap body when the pass ran:

```markdown
# Missing Screenshots

_None found._
```

Page grouping preserves first-occurrence order, matching the current behavior
in `gaps.md`.

## Write Policy

Three cases the caller handles:

| Case | `gaps.md` | `screenshots.md` |
|---|---|---|
| Pass ran, ≥1 gap   | written | written (findings)    |
| Pass ran, zero gaps| written | written (_None found._) |
| Pass skipped       | written | **not written**       |

The "pass skipped" case intentionally leaves no file on disk so a user who
opted out of the pass sees that in the filesystem, not just in stdout.

## CLI Output

Replace the single-line `reports:` suffix with a multi-line block:

```
scanned 42 files, fetched 17 pages, 8 features mapped
reports:
  <dir>/mapping.md
  <dir>/gaps.md
  <dir>/screenshots.md
```

When `--skip-screenshot-check` was set, the screenshots line is annotated:

```
  <dir>/screenshots.md (skipped)
```

The screenshots line is always present — users who forgot the flag should see
immediately that the pass was skipped, not silently discover the missing file
later.

## Orchestration (CLI) Changes

In `internal/cli/analyze.go`:

1. Call `reporter.WriteGaps(projectDir, featureMap, docCoveredFeatures, driftFindings)` — 4 args, no `screenshotGaps`.
2. Update `driftOnFinding` (the progressive-write callback used as drift findings accumulate) to match the new signature. Drop the `nil` screenshot argument.
3. When `!skipScreenshotCheck`, call `reporter.WriteScreenshots(projectDir, screenshotGaps)` after the drift write.
4. Swap the one-line `reports: ...` print for the multi-line block above.

Exit code behavior is unchanged.

## Testing Strategy (TDD Order)

Per CLAUDE.md, every step is RED → GREEN → REFACTOR.

**Reporter unit tests (`internal/reporter/reporter_test.go`):**

1. `TestWriteGaps_NoLongerRendersScreenshotsSection` — call new 4-arg `WriteGaps`, assert `gaps.md` does not contain "Missing Screenshots".
2. `TestWriteScreenshots_CreatesFile_WithFindings` — two gaps across two pages, assert `screenshots.md` contents.
3. `TestWriteScreenshots_Empty_WritesNoneFound` — zero gaps, assert file exists and body is `_None found._`.
4. `TestWriteScreenshots_PreservesPageOrder` — three gaps across two pages, first-occurrence order preserved.
5. Delete obsolete `TestWriteGaps_MissingScreenshotsSection` and `TestWriteGaps_MissingScreenshotsEmpty_OmitsSection`.
6. Update every other `WriteGaps` call in this test file to the new 4-arg signature.

**CLI unit tests (`internal/cli/analyze_test.go`):**

7. After a successful run, assert `screenshots.md` exists in the project dir.
8. With `--skip-screenshot-check`, assert `screenshots.md` does NOT exist.
9. Assert stdout contains the multi-line `reports:` block and the `(skipped)` annotation when applicable.

**Testscript e2e (`cmd/find-the-gaps/testdata/script/`):**

10. Update `default_screenshot_check.txtar` to assert `screenshots.md` is written and no `(skipped)` marker in stdout.
11. Update `skip_screenshot_check.txtar` to assert `screenshots.md` is NOT written and `(skipped)` appears in stdout.

**Coverage gate:** ≥90% statement coverage on `internal/reporter/reporter.go` and on the modified paths in `internal/cli/analyze.go`.

## Verification (Real-System)

Scenario 5 in `.plans/VERIFICATION_PLAN.md` is rewritten:

### Scenario 5: Detect Missing Screenshots (updated)

**Context:** Known-good fixture repo + docs site, with a page that describes a UI moment with no image nearby.

**Steps:**

1. Run `find-the-gaps analyze --repo ./testdata/fixtures/known-good --docs-url https://<docs>`.
2. Inspect `<projectDir>/screenshots.md`.
3. Re-run with `--skip-screenshot-check`.
4. Inspect the output directory.

**Success Criteria:**

- [ ] First run writes `screenshots.md`; `gaps.md` does NOT contain a "Missing Screenshots" section.
- [ ] `screenshots.md` contains at least one gap for the known UI passage with all four fields populated.
- [ ] Stdout lists `screenshots.md` in the `reports:` block.
- [ ] Second run does NOT write `screenshots.md`.
- [ ] Second run's stdout lists `screenshots.md (skipped)`.

## Out of Scope for v1

- Splitting drift into its own `drift.md`.
- Renaming `gaps.md`.
- JSON/structured output formats.
- Per-gap severity/priority.
- Any change to exit-code semantics.
