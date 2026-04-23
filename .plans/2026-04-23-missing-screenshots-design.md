# Missing Screenshots Detection — Design

**Status**: Design approved 2026-04-23. Ready for implementation planning.

## Goal

Add a new detector to `find-the-gaps` that, for every fetched docs page, identifies moments where the prose describes a user-facing interaction (a screen, a terminal session, a button, a dialog) that should have a screenshot nearby but does not. Each finding tells the maintainer:

- What passage describes the missing visual.
- What the screenshot should depict.
- What alt text / caption to use.
- Where to insert it.

## Non-Goals

- Judging the quality of existing screenshots.
- Generating the screenshots.
- Detecting stale or mismatched screenshots (separate capability, future work).
- Ranking findings by priority (every flagged gap is assumed actionable; LLM is instructed to suggest only gaps that would materially help a reader).

## Module Layout

New files, all following existing `internal/analyzer/` conventions:

- `internal/analyzer/screenshot_gaps.go` — entry point, orchestration, prompt, response parsing.
- `internal/analyzer/screenshot_gaps_test.go` — unit tests.
- Types added to `internal/analyzer/types.go`.
- Reporter changes in `internal/reporter/reporter.go` + tests.
- CLI flag wiring in `cmd/find-the-gaps/cli/` + a testscript e2e test.

No new concurrency, no new cache layer, no new client.

## Public API

```go
// internal/analyzer/screenshot_gaps.go

type ScreenshotGap struct {
    PageURL       string
    PagePath      string
    QuotedPassage string // verbatim quote from the doc page
    ShouldShow    string // literal description of what the screenshot depicts
    SuggestedAlt  string // alt text / caption
    InsertionHint string // e.g. "after the paragraph ending '…click Save.'"
}

type ScreenshotProgressFunc func(done, total int, currentPage string)

func DetectScreenshotGaps(
    ctx context.Context,
    client LLMClient,
    pages []DocPage,
    progress ScreenshotProgressFunc,
) ([]ScreenshotGap, error)
```

One LLM call per page, using the same `batcher.go` concurrency used by drift detection.

## Detection Strategy

**Approach: LLM-only, with mandatory passage citation (Q1 option C).**

The LLM cannot emit a finding without quoting the exact passage that describes the user-facing moment. If it cannot cite a passage, no finding. This mirrors the discipline in `drift.go` and is the main lever against noise.

**Prompt-level rules (encoded in the prompt, PROMPT-tagged per project convention):**

1. Return a JSON array of gap objects. Empty array is a valid response.
2. For each gap, provide: `quoted_passage`, `should_show`, `suggested_alt`, `insertion_hint`.
3. Quote the passage verbatim — no paraphrase.
4. `should_show` must be concrete ("the dashboard with two open PRs and the 'New PR' button visible in the top right"), not abstract ("a screenshot of the feature").
5. `insertion_hint` must reference existing prose ("after the paragraph ending '…and press Enter.'"), not a line number.
6. Only suggest a screenshot where one would materially help a reader. Reference pages, API tables, and pure prose without UI moments should return an empty array.
7. Do not flag a moment that is already covered by a nearby image (see locality rule below).

## Coverage Check (Locality Rule)

**Rule (Q2 option B):** An existing image covers a passage only if it appears within the same markdown section heading as the passage, or within 3 paragraphs before/after the passage.

The LLM cannot reliably enforce this on its own, so we pre-compute the inputs in Go:

1. Parse the page markdown to extract image positions. Both syntaxes:
   - `![alt](url)`
   - `<img src="..." alt="...">`
2. For each image, record: section heading it lives under, paragraph index within the page.
3. Pass the resulting section → image-list map into the prompt alongside the page content.
4. Prompt instructs the model to treat a passage as covered if any image appears in the same section or within 3 paragraphs on either side.

The parse is deterministic and fully unit-tested. The judgment of "is this image actually showing this moment" is left to the LLM — which is what it is good at.

## Granularity

**One finding per missing screenshot (Q3 option B).** A single page with three distinct user-facing moments produces three `ScreenshotGap` entries. Each entry is independently actionable.

## Scope

**Every fetched docs page (Q5 option A).** The detector runs on all pages returned by the spider, regardless of whether the page is mapped to a code feature in `DocsFeatureMap`. User-facing moments live in tutorials, quickstarts, philosophy pages, and generic guides — not only in feature-mapped pages.

The prompt itself is the filter: reference-only pages return an empty array.

## Pipeline Placement

**Separate pass after drift detection (Q6 option B).** New module, new prompt, new LLM call per page. Not folded into `analyze_page.go`.

**Flag:** `--skip-screenshot-check` (bool, default `false`). When set, the screenshot-gap pass is skipped entirely and contributes nothing to findings or exit code.

Orchestration sketch in `cmd/find-the-gaps/cli`:

```go
// ... drift detection runs first ...

if !cfg.SkipScreenshotCheck {
    gaps, err := analyzer.DetectScreenshotGaps(ctx, client, pages, progressFn)
    if err != nil {
        return err
    }
    report.ScreenshotGaps = gaps
}
```

## Progress UX

- New progress callback `ScreenshotProgressFunc`, mirroring `DriftProgressFunc`.
- Progress phase label: `"checking screenshots"`.
- Same plain/TUI progress rendering the drift phase uses.

## Report Placement

**New top-level section "Missing Screenshots" (Q7 option A).** Rendered in `internal/reporter/reporter.go` after the drift section, grouped by page. Example output:

```markdown
## Missing Screenshots

### docs/quickstart.md — https://example.com/quickstart

- **Passage:** "Run `find-the-gaps analyze` and you'll see a summary report in your terminal."
  - **Screenshot should show:** Terminal output of the analyze command, with the summary section visible (exit code, findings count, section headings).
  - **Alt text:** "Terminal output of `find-the-gaps analyze` showing a summary report."
  - **Insert:** After the paragraph ending "…in your terminal."

- **Passage:** "The dashboard highlights pages that have drifted from the code."
  - **Screenshot should show:** The dashboard view with at least one drifted page highlighted in red/yellow.
  - **Alt text:** "Find the Gaps dashboard with a drifted page highlighted."
  - **Insert:** After the heading "## Dashboard".
```

When `--skip-screenshot-check` is set, the section is omitted entirely (not rendered as "no findings" — omitted).

## Exit Code

Screenshot gaps count as findings. They contribute to the non-zero exit code the analyzer already emits when findings exist. When `--skip-screenshot-check` is set, they contribute nothing.

## Error Handling

Follows the same pattern as `analyze_page.go` and `drift.go`:

- One retry on JSON parse failure.
- On second failure, log and continue — do not fail the whole run because one page's response was malformed.
- Network / context errors propagate up, same as drift.

## Caching

Does **not** introduce a new cache layer. If per-page results are already cached by content hash, screenshot-gap results get added to the same cache entry keyed by page content + prompt version. If there is no cache today, this feature does not add one.

(Confirm status of existing cache during implementation planning.)

## Testing Strategy (TDD Order)

Per CLAUDE.md, every step is RED → GREEN → REFACTOR. In this order:

1. **Image-position parser** — given markdown with various image syntaxes and headings, extract `(section heading, paragraph index, alt, src)` tuples.
2. **Coverage map builder** — given parser output, produce the section → image-list structure passed to the prompt.
3. **Prompt builder** — given a `DocPage` + coverage map, produces the expected prompt string (snapshot test on a small fixture).
4. **Response parser** — given a raw JSON string, produces `[]ScreenshotGap`. Covers valid JSON, empty array, malformed JSON (triggers retry), and missing required fields.
5. **Orchestrator** — `DetectScreenshotGaps` with a fake `LLMClient`: happy path, empty pages, per-page empty result, parse-failure retry, per-page error isolation.
6. **Reporter** — new "Missing Screenshots" section renders correctly, handles zero gaps (section omitted), respects page grouping order.
7. **CLI (testscript)** — `--skip-screenshot-check` actually skips the pass; default run includes it.

**Coverage gate:** ≥90% statement coverage on `screenshot_gaps.go` and the reporter changes, per CLAUDE.md.

## Verification (Real-System)

Add a scenario to `.plans/VERIFICATION_PLAN.md`:

### Scenario: Detect Missing Screenshots

**Context:** Known-good fixture repo + docs site, but the docs site has a page that describes a UI moment with no image nearby.

**Steps:**
1. Run `find-the-gaps analyze --repo ./testdata/fixtures/known-good --docs-url https://<docs>`.
2. Inspect the report.
3. Re-run with `--skip-screenshot-check`.

**Success Criteria:**
- [ ] First run's report contains a "Missing Screenshots" section with at least one gap that names the known UI passage.
- [ ] Each gap has all four fields populated (passage, should_show, alt, insertion_hint).
- [ ] Second run's report omits the "Missing Screenshots" section entirely.

## Open Questions

None for v1. Record during implementation if any surface.

## Out of Scope for v1

- Detecting stale screenshots (image present but shows old UI).
- Detecting screenshots on pages that describe something else (mismatched images).
- Per-gap priority/severity scoring.
- Auto-generating suggested screenshots via a browser automation pass.
