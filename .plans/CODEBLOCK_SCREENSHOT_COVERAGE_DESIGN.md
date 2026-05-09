# Code-Block Coverage for Screenshot Detection

**Status**: Design — not yet implemented
**Date**: 2026-05-09
**Touches**: `internal/analyzer/screenshot_gaps.go`, `internal/cli/screenshot_audit.go`, `internal/cli/screenshots_cache.go`, `internal/reporter/screenshots_writer.go`, `.plans/VERIFICATION_PLAN.md`

## Problem

Docs sites often substitute a code block for a screenshot. Three observed cases:

1. **CLI / terminal output** — prose says "you'll see output like…" and a fenced block right below shows the literal output.
2. **API responses / config** — prose describes the shape of a JSON / YAML / TOML response or config and a code block shows it verbatim.
3. **Rendered HTML / component previews** — a code block contains the literal HTML / JSX / CSS source that produces the UI being described.

The screenshot-gap detector has one narrow rule today: "Terminal sessions whose output is already shown inline in a code block — do NOT flag." That rule (a) doesn't generalize to JSON / config / HTML cases, and (b) doesn't always hold even for terminal output, because the prompt has no structured locality signal pointing the model at where code blocks actually live on the page. Result: false-positive missing-screenshot findings whose visual moment is already covered.

## Approach

Mirror the existing image-coverage system. Code blocks become first-class coverage signals on the page, fed deterministically into the detection prompt with section-heading and paragraph-index locality data, and the prompt's "covered" rule generalizes from "image" to "visual artifact." No new LLM call. No vision dependency.

## Section 1 — Extraction & data model

Add a sibling to `imageRef` in `internal/analyzer/screenshot_gaps.go`:

```go
type codeBlockRef struct {
    Language       string // from the fence opener: "bash", "json", "html", "" if absent
    LineCount      int    // body lines, excluding the opener/closer fences
    SectionHeading string // most recent ATX heading above the block; "" if none
    ParagraphIndex int    // 0-based block position, same scheme as imageRef
    OriginalIndex  int    // 1-based "code-N" label, mirrors imageRef.OriginalIndex
}
```

Extraction reuses the existing single-pass fence state machine in `extractImages`. The loop already toggles `inFence` on lines starting with ` ``` ` or `~~~` and increments `pIdx` on blank-line block boundaries. Extend the same loop (or split into a new `extractRefs` returning both slices) so the markdown is walked once. On fence open, capture the language from the opener (everything after ` ``` ` / `~~~` on the opening line, trimmed). On fence close, emit a `codeBlockRef` with the captured metadata.

**Do not capture the body.** The page content already contains every code block verbatim and the model reads the full content. Duplicating block bodies in the coverage summary would blow `ScreenshotPromptBudget` on reference-heavy pages — the same pages that already trip the budget today.

**Do capture just enough for locality.** Language + line count + section + paragraph index. The model uses this to answer "is there a topically-matching code block in the same section or within ±3 paragraphs?"

## Section 2 — Prompt changes

Two prompts change identically: `buildScreenshotPrompt` (no-vision path) and `buildDetectionPromptWithVerdicts` (vision path).

**Coverage summary gains a code-block list**, right after the image list:

```
Existing code blocks on this page (if any):
- code-1, section "Quickstart", paragraph 4: language=bash, 12 lines
- code-2, section "Response", paragraph 7: language=json, 8 lines
- code-3, section "Preview", paragraph 11: language=html, 24 lines
```

If there are no fences: `No code blocks on this page.` (parallel to the existing image branch.)

**Coverage rule generalizes** from "image" to "visual artifact." A passage is covered when:

- An image's alt text plausibly matches the topic AND the image is in the same section heading or ±3 paragraphs (existing rule), OR
- A code block sits in the same section or ±3 paragraphs AND its language plausibly matches the moment in prose:
  - `bash` / `console` / `shell` / `text` / `sh` for terminal output,
  - `json` / `yaml` / `toml` / `xml` for response or config shapes,
  - `html` / `jsx` / `tsx` / `vue` / `svelte` / `css` for rendered UI source.

The full block body is already in the page content — the model reasons about topical fit by reading it directly. The coverage list just gives it deterministic locality so the locality math is consistent across runs.

**"Do NOT flag" list grows from one bullet to three:**

1. Terminal sessions whose output is shown inline in a nearby code block.
2. API responses, config files, or data shapes already shown verbatim in a nearby `json` / `yaml` / `toml` / `xml` code block under the locality rule.
3. Rendered UI whose source is already shown in a nearby `html` / `jsx` / `tsx` / `vue` / `svelte` / `css` code block where the prose describes how the resulting UI looks.

**Output schema gains `suppressed_by_code_block`.** Mirrors `suppressed_by_image` exactly — same six fields (`quoted_passage`, `should_show`, `suggested_alt`, `insertion_hint`, `priority`, `priority_reason`). Audit-only, not rendered to users. This lets us measure whether the new rule fires in practice; if it never fires, the prompt isn't landing and we retune; if it fires constantly, we may be over-suppressing real gaps.

## Section 3 — Audit stats, reporter, cache, tests

**`ScreenshotPageStats` gains two fields**:

```go
CodeBlocksSeen        int
SuppressedByCodeBlock int  // count of suppressed_by_code_block items the model returned
```

`PossiblyCovered` stays as the union count across both suppression sources; the new field is the disaggregated code-block-only count.

**Audit log line.** `internal/cli/screenshot_audit.go` extends to:

```
images_seen=N code_blocks=M relevance_batches=R issues=I missing=X possibly=Y (img:A code:B)
```

So `-v` runs surface the new signal without users grepping JSON.

**Reporter is essentially untouched.** Code-block-suppressed items flow through `PossiblyCovered` alongside image-suppressed items — same shape, same rendering. Users reading "Possibly Covered" don't need to know which signal fired.

**Cache.** Key is `URL + content_hash` — unchanged. `ScreenshotsCachedPage` picks up the two new stats fields. Old cached entries deserialize cleanly (zero-value ints) and continue to be honored on hit. The worst case is one stale audit line for a page whose content hasn't changed. No schema bump.

**Tests, in TDD order:**

1. `TestExtractCodeBlocks` — language detection from openers, ` ``` ` and `~~~` variants, section-heading association, paragraph indexing, body NOT included.
2. `TestBuildScreenshotPrompt_CodeBlockCoverage` — golden-string assertion that the prompt lists code blocks with locality info.
3. `TestBuildDetectionPromptWithVerdicts_CodeBlockCoverage` — same assertion on the vision path.
4. `TestDetectScreenshotGaps_SuppressedByCodeBlock` — integration test with a stub `LLMClient` that returns a `suppressed_by_code_block` item; assert it lands in `PossiblyCovered` and `SuppressedByCodeBlock=1`.
5. Extend `screenshot_gaps_integration_test.go` happy-path fixture with one CLI-output, one JSON-response, one HTML-preview passage; assert zero false-positive missing findings.
6. Update Scenario 5 in `.plans/VERIFICATION_PLAN.md` to assert a known terminal-output passage on the real fixture is no longer flagged.

## Non-goals

- No semantic / vision check on code blocks. The text model already reads them in the page content; we just give it locality.
- No new cache schema version. Old entries continue to be honored.
- No change to the user-rendered shape of `screenshots.md` (only the audit log changes).
- No new LLM call. The new signal lands inside the existing detection call's output schema.
