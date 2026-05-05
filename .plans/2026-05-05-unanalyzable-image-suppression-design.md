# Unanalyzable Image Suppression — Design

Date: 2026-05-05
Branch: `animated-gif-vision`

## Problem

The screenshot-gaps pass uses a vision model to judge whether an image on a docs page is relevant to nearby prose. When a page contains an image the vision model cannot or should not analyze, the tool ignores the image and may emit a "missing screenshot" finding for a section that is, in fact, illustrated.

Two cases:

1. **Vision-unsupported formats.** Anthropic's vision API rejects SVG, AVIF, ICO, BMP, TIFF, and HEIC/HEIF. We already filter these (`visionUnsupportedExts`, `visionUnsupportedDataMimes`) and never send them.
2. **Animated GIFs.** GIFs pass the format filter, but every vision provider (Anthropic, OpenAI, Groq) treats them as a single still — typically the first frame. Relevance judgments on animated demos are made on whichever frame the provider decoded, which silently misleads.

In both cases, an image that is plausibly the screenshot the maintainer's prose is talking about can be silently discarded, producing a false-positive "missing screenshot" finding.

## Decision summary

The suppression layer answers one binary question per eligible image: *"Is this plausibly the screenshot the prose is about?"* "Yes" reroutes the section's missing-screenshot finding from `## Missing Screenshots` to a new `## Possibly Covered` subsection in `screenshots.md`. "No" leaves the existing finding flow alone.

Choices made during brainstorming:

| Decision | Choice |
|---|---|
| Scope of suppression | Vision-unsupported formats + all GIFs |
| Static GIFs | Treated same as animated; not sent to vision |
| Signal precedence | HTML `width`/`height` attrs win, HEAD `Content-Length` as fallback |
| Threshold (HTML attrs) | `max(width, height) >= 400px` |
| Threshold (bytes) | `Content-Length >= 30KB` |
| No-signal default | No suppression — emit finding normally |
| Output | New `## Possibly Covered` subsection in `screenshots.md` |
| Hugo site | Renders for free through Hextra; no template changes |
| Project README/docs | Out of scope for this change |

## Scope and trigger

The suppression layer sits between image extraction and finding emission. It fires only when the image is in a vision-unsupported format **or** is a GIF (extension `.gif` or `image/gif` data URI). Images in vision-supported formats (JPEG, PNG, WebP) continue through the existing vision pass unchanged.

Behavior change worth flagging: today, GIFs flow into `## Image Issues` when the vision model judges them irrelevant. After this change, GIFs no longer appear in `## Image Issues` at all — they appear (if at all) only in `## Possibly Covered`. The previous `## Image Issues` signal on GIFs was based on a first-frame-only judgment and was unreliable.

## Signal extraction and decision

1. **Parse `width`/`height` from source markdown.** Extend `extractImages` in `internal/analyzer/screenshot_gaps.go`. Add `htmlAttrWidthRe` and `htmlAttrHeightRe` alongside the existing `htmlAttrSrcRe` / `htmlAttrAltRe`. Store parsed integer values on `imageRef` as `DeclaredWidth`, `DeclaredHeight`. Markdown `![]()` syntax cannot carry dimensions, so refs from that syntax always have zero values.
2. **HTML-attrs decision.** If `max(DeclaredWidth, DeclaredHeight) >= 400` → suppress. Skip the HEAD entirely.
3. **HEAD fallback.** Otherwise, issue a `HEAD` to the resolved absolute URL using the existing `resolveImageSrc` logic. Read `Content-Length`. If `>= 30720` (30 KB) → suppress.
4. **Data URIs.** A `data:` URI's bytes are inline. Use `len(decoded) >= 30720` rather than HEADing.
5. **No signal → no suppression.** If HTML attrs are absent or below threshold and the HEAD fails (network error, non-2xx, missing `Content-Length`, or timeout), the image gets no suppression credit and the section's missing-screenshot finding is emitted normally. We do not range-GET as a secondary fallback.

## HEAD plumbing

- **Concurrency cap:** 8 in-flight HEADs site-wide via a worker pool (semaphore `chan struct{}`).
- **Timeout:** 5 seconds per HEAD.
- **Cache:** in-memory `map[string]bool` keyed by absolute URL for the lifetime of one analyze run. Same image referenced from N pages = one HEAD.
- **Skip data: URIs.** No HEAD; use inline bytes.

## Output

`screenshots.md` structure post-change:

```markdown
# Screenshot Gaps

## Missing Screenshots
[existing — findings the suppression layer did not silence]

## Possibly Covered
### <Page title> — <section heading>
- Page: <url>
- Image: <absolute image url>
- Format: gif | svg | avif | ...
- Suppression signal: declared width=800px  (or)  Content-Length=124KB

## Image Issues
[existing — vision-judged irrelevant images for vision-supported formats only;
 GIFs no longer appear here]
```

If `## Possibly Covered` would be empty, the heading is not emitted (matches the existing `## Image Issues` convention).

## Hugo site rendering

`internal/site/materialize.go` already copies `screenshots.md` into `<projectDir>/site/screenshots/` through the Hextra theme. The new subsection is plain markdown headings and lists — Hextra's auto-generated right-rail TOC picks up `## Possibly Covered` for free. No template changes, no new content type, no new flag interactions. The `--no-site`, `--keep-site-source`, and `--site-mode={mirror,expanded}` paths all work unchanged.

## Test plan

Per CLAUDE.md, every line is RED → GREEN → REFACTOR with ≥90% statement coverage per package.

**Unit tests** (`internal/analyzer/screenshot_gaps_test.go`, white-box):

1. `extractImages` parses `width`/`height` attrs into `DeclaredWidth`/`DeclaredHeight` (table cases: present, absent, malformed, mixed quotes, both/neither).
2. `suppressionEligible(ref imageRef) bool` — true for unsupported exts, unsupported mimes, GIF extension, `image/gif` data URIs.
3. `htmlAttrsSuggestScreenshot(ref imageRef) bool` — `max(w, h) >= 400`.
4. `headSuggestsScreenshot(ctx, client, url) (bool, error)` — mocked `http.RoundTripper`: 200 + `Content-Length: 50000` → true; 200 + 1000 → false; missing header → false; 404 → error; timeout → error; data URI short-circuits.
5. `decisionForImageRef(ctx, client, ref)` orchestrator — verifies precedence (attrs win; HEAD only when attrs insufficient; data URI uses inline length).
6. `routeFindings` — given section findings and per-image suppression decisions, returns `(missing, possiblyCovered)` slices.
7. Worker pool deduplicates by URL and respects 8-concurrent cap.

**Integration test** (`internal/analyzer/screenshot_gaps_integration_test.go`):

8. End-to-end: fixture page with `<img width="800" src="demo.gif">` — assert the section's missing-screenshot finding moves to `## Possibly Covered`, not `## Missing Screenshots`. Vision call unchanged for PNG/WebP images on the same page.

**Hugo render verification:** Verification Plan Scenario 13, new sub-case — analyze a fixture with an oversized SVG/GIF, load `<projectDir>/site/screenshots/`, confirm `## Possibly Covered` renders through Hextra.

Coverage gate: `go test -coverprofile=coverage.out ./internal/analyzer/... && go tool cover -func=coverage.out` after each task; do not move on until ≥90%.

## Files touched

- `internal/analyzer/screenshot_gaps.go` — fields, regexes, suppression functions, GIF rerouting, `## Possibly Covered` emitter.
- `internal/analyzer/screenshot_gaps_test.go` — new unit tests (1–7).
- `internal/analyzer/screenshot_gaps_integration_test.go` — new end-to-end test (8).
- `.plans/VERIFICATION_PLAN.md` — Scenario 13 sub-case.

Not touched:

- `internal/site/materialize.go` and the Hextra theme.
- `README.md` and project docs (out of scope per brainstorming).

## Implementation order

One commit per task per CLAUDE.md §9.

1. `extractImages` parses `width`/`height` into `imageRef`.
2. `suppressionEligible` (extension and data-URI cases, including GIF).
3. `htmlAttrsSuggestScreenshot` (≥400px on larger axis).
4. `headSuggestsScreenshot` (mocked `RoundTripper`; cap and cache deferred to task 6).
5. `decisionForImageRef` orchestrator (precedence: attrs → HEAD → no signal).
6. Worker pool with 8-concurrency cap and per-URL cache.
7. Route GIFs out of vision and into suppression bucket; integrate `## Possibly Covered` into the markdown emitter.
8. Integration test asserting full pipeline behavior on a fixture page.
9. Update `.plans/VERIFICATION_PLAN.md` Scenario 13 sub-case.
10. Final coverage check.
