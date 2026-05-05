# Finding Priority — Design

## Overview

Every prioritized finding the analyzer produces gains a `priority` field — `large`, `medium`, or `small` — based on user impact (how badly a reader following the docs will be misled or blocked). Maintainers see the most important findings first and can stop reading when they hit `small`.

## Scope

### In scope (gains `priority` + `priority_reason`)

1. Drift issues (code/doc inaccuracies)
2. Missing screenshots
3. Image issues (screenshot present but irrelevant or wrong)
4. Possibly covered (the suppression-layer entries from #60)

### Out of scope (untouched)

- Undocumented Code section of `gaps.md`
- Unmapped Features section of `gaps.md`
- Exit-code gating (`--fail-on=large`)
- GitHub Action issue body restructuring
- Filter UI in the rendered site

The four in-scope categories are exactly the LLM-judged findings. Heuristic-only sections do not get priority in v1.

## Approach

Priority is folded into the existing prompts that already produce each finding. **No new LLM calls.** The prompts gain two output fields (`priority` enum and `priority_reason` one-liner) plus a shared rubric block in the prompt text and a deterministic `page_role` hint as input context.

## Rubric

The LLM receives this verbatim in each prompt:

- **Large** — a reader following the docs will fail or be actively misled.
  Examples: stale signature in a copy-paste-able example; removed feature still presented as the recommended way; quickstart references a function that no longer exists; missing screenshot for a UI step prose alone cannot carry.
- **Medium** — a reader will be confused or have to dig elsewhere, but won't outright fail.
  Examples: drifted parameter description (default value changed, call still works); image present but only loosely related to the passage; missing screenshot for a UI moment prose mostly covers.
- **Small** — a reader probably won't notice or can shrug it off.
  Examples: drift on internal/edge-case behavior; cosmetic image issue (slightly outdated UI but message intact); missing screenshot where prose stands alone.

## Page-prominence hint

A deterministic hint (`page_role: "quickstart" | "readme" | "top-nav" | "reference" | "deep" | "unknown"`) is computed for each finding's page from the spider's `index.json` (top-of-nav-tree, common filenames, URL depth) and passed in as prompt context. The LLM still makes the final call; the hint is bias, not constraint.

## Data model

### Shared type (new)

```go
// internal/analyzer/types.go
type Priority string

const (
    PriorityLarge  Priority = "large"
    PriorityMedium Priority = "medium"
    PrioritySmall  Priority = "small"
)
```

### Existing finding structs gain two fields

Applied to `DriftIssue`, `ScreenshotGap`, `ImageIssue`, and the possibly-covered struct:

```go
Priority       Priority `json:"priority"`
PriorityReason string   `json:"priority_reason"`
```

### Validation

Structured-output parsing fails closed if `priority` is missing or holds an unknown value. The existing retry path handles this. No silent defaulting — a finding without a priority is a bug.

### Backward compatibility

Adding fields is additive. Old `drift.json` cache files lacking `priority` are treated as a cache miss and recomputed. This rule lives in the existing drift-cache compatibility section.

## Prompt changes

Four prompts gain the same two output fields, shared rubric block, and `page_role` input:

1. Drift prompt (`internal/analyzer/drift.go`)
2. Screenshot-gap prompt (`internal/analyzer/screenshot_gaps.go`)
3. Image-issue / vision-relevance prompt (vision branch in `screenshot_gaps.go`)
4. Possibly-covered classifier (suppression layer)

Shared rubric snippet lives in one place (e.g., `internal/analyzer/prompts/priority.go`) and is referenced from each prompt's template, marked with the `// PROMPT:` convention from CLAUDE.md.

## On-disk JSON

### `drift.json` (existing — additive change only)

Each issue gains `priority` and `priority_reason`. No reordering, no other schema changes. Cache compatibility per the rule above.

### `screenshots.json` (new)

Holds missing screenshots, image issues, and possibly-covered findings, each with `priority` and `priority_reason`. Stable original order within each list — no priority-driven reordering in the JSON. Consumers sort however they want; the Markdown and site impose ordering at render time.

## Markdown rendering

### `gaps.md` — drift section only

Section header stays `## Stale Documentation`. Three sub-headings in fixed order, empty buckets omitted:

```
### Large
- [feature/page] issue text
  _why: priority_reason_

### Medium
...

### Small
...
```

Undocumented Code and Unmapped Features sections render exactly as today.

### `screenshots.md`

Each top-level section (`## Missing Screenshots`, `## Image Issues`, `## Possibly Covered`) gains the same `### Large / Medium / Small` sub-grouping. Stable original order within a bucket. Empty buckets omitted.

## Hugo site

Same grouping as Markdown. Plus a small visual treatment: a colored badge before each finding title — `large` red, `medium` amber, `small` gray. Implementation is a tiny inline `<span>` styled in the existing custom CSS layer under `internal/site/assets/`. No new theme file, no filter UI, no JS. The existing right-rail TOC picks up the new sub-headings automatically.

## Stdout summary

The reports block at the end of `analyze` appends per-priority counts for prioritized files only:

```
reports:
  drift.json (12 issues: 3L · 5M · 4S)
  screenshots.md (8 missing: 2L · 4M · 2S; 3 image issues: 1L · 2M · 0S)
```

Always shown. No flag, no opt-out.

## Testing

### Unit (TDD per CLAUDE.md)

- **Schema parse** — each prompt's structured output fails closed when `priority` is absent or invalid; succeeds with each valid value.
- **Reporter grouping** — given a fixed slice of findings with mixed priorities, `WriteGaps` and `WriteScreenshots` produce sub-sections in `Large → Medium → Small` order, omit empty buckets, preserve stable order within a bucket.
- **Site rendering** — golden-file test for the Hugo templates with one finding per priority, asserting badge class and sub-heading order.

### Integration

- `analyzer_test.go` round-trip — parse a real LLM response with priorities through to the on-disk `drift.json` and `screenshots.json`, assert the fields are preserved.
- `testscript` scenario in `cmd/find-the-gaps/testdata/` for the stdout summary line.

## Verification plan additions

Update `.plans/VERIFICATION_PLAN.md`:

- **Scenario 3 (drift detection)** — assert each drift finding has a non-empty `priority` and `priority_reason`; assert ordering in `gaps.md`.
- **Scenario 5 (screenshots)** — same assertions on `screenshots.md` and `screenshots.json`.
- **New Scenario 14 — calibration smoke test** — against the Scenario 9 fixture, assert no priority bucket holds >80% of findings (sanity check that the rubric isn't degenerating to "everything is medium").

## Cost

Net delta: **zero new LLM calls**. Four existing prompts each get two extra output fields.
