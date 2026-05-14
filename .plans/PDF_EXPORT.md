# PDF Export

## Goal

`ftg analyze` emits `<projectDir>/report.pdf` alongside the existing `mapping.md`, `gaps.md`, `screenshots.md`, and `site/` outputs. The PDF is a single self-contained artifact a maintainer can read offline, attach to a Slack thread, or hand to a stakeholder. Styling evokes — does not exactly mirror — the Hextra-rendered site: same palette, same priority-pill language, same section ordering.

## Non-Goals

- **Pixel-identical fidelity to the Hugo site.** We are not running a browser engine. The PDF is hand-laid-out, sharing only colors/typography with the site.
- **A separate `ftg pdf` subcommand.** Renders alongside `analyze` only. A standalone subcommand can come later if anyone asks.
- **Page-size configurability.** Letter (8.5" × 11") is the only supported size in this iteration.
- **Per-feature deep links from the site to the PDF (or vice versa).** Out of scope.
- **Embedding screenshots from the docs site into the PDF.** The screenshot section names gaps; it does not render any images.

## User-Facing Surface

- Default: `ftg analyze --repo R --docs D` writes `<projectDir>/report.pdf`.
- Opt out: `--no-pdf` skips PDF generation.
- The stdout `reports:` block gains a line:
  ```
  reports:
    <projectDir>/mapping.md
    <projectDir>/gaps.md (...)
    <projectDir>/screenshots.md (...)
    <projectDir>/site/
    <projectDir>/report.pdf
  ```
  When `--no-pdf`, the line reads `<projectDir>/report.pdf (skipped)` — same convention as `site/` and `screenshots.md`.
- The PDF lives at `<projectDir>/report.pdf`, **not** inside `site/`. The Hugo site does not link to it.

## Architecture

In-memory pass-through. Same pattern as `reporter.WriteMapping`. No new on-disk JSON artifacts.

```
analyze.go
  └── reporter.WriteMapping(projectDir, summary, featureMap, docsFeatureMap)
  └── reporter.WriteScreenshotsJSON(projectDir, screenshotResult)   // if enabled
  └── site.Build(ctx, site.Inputs{...}, site.BuildOptions{...})     // if !noSite
  └── pdf.WriteReport(projectDir, pdf.Inputs{                       // if !noPDF      (NEW)
          ProjectName:    projectName,
          RepoURL:        ...,
          DocsURL:        ...,
          GeneratedAt:    time.Now(),
          Summary:        productSummary,
          Mapping:        featureMap,
          DocsMap:        docsFeatureMap,
          Drift:          driftFindings,
          Screenshots:    screenshotResult,        // zero-value when disabled
          ScreenshotsRan: experimentalCheckScreenshots,
      })
```

`pdf.Inputs` is the same shape as `site.Inputs` plus run-metadata fields. No re-parsing markdown, no reading caches.

## PDF Layout (Hybrid)

```
┌─ Cover ─────────────────────────────────────────────┐
│  Find the Gaps                                      │
│  <Project Name>                                     │
│  Repo:  <repo URL>                                  │
│  Docs:  <docs URL>                                  │
│  Generated: 2026-05-13 14:32 UTC                    │
│                                                     │
│  <N> features  ·  <M> gaps  ·  <K> screenshot issues │
└─────────────────────────────────────────────────────┘
                       page 1

┌─ Table of Contents ─────────────────────────────────┐
│  Features                                  ......  3│
│    <Feature A>                              ......  3│
│    <Feature B>                              ......  4│
│    ...                                              │
│  Gaps                                      ...... 11│
│    Large                                    ...... 11│
│    Medium                                   ...... 13│
│    Small                                    ...... 15│
│  Screenshots                               ...... 17│
│    Missing                                  ...... 17│
│      Large/Medium/Small                             │
│    Image Issues                             ...... 19│
│    Possibly Covered                         ...... 20│
└─────────────────────────────────────────────────────┘
                       page 2

§ Features                                    [anchor: features]
  Feature A                                   [anchor: feat-<slug>]
    > description (italic blockquote)
    Layer · User-facing · Documentation status
    Implemented in: <files>
    Documented on: <pages>

  Feature B
    ...

§ Gaps                                        [anchor: gaps]
  Large
    <feature name> → ...                      [link to feat-<slug>]
      • <drift issue> · <page label>
    ...
  Medium
    ...
  Small
    ...

§ Screenshots                                 [anchor: screenshots]
  Missing
    Large
      <page label> → ...                      [link to feat-<slug> if resolvable]
        Should show: <should_show>
        Suggested alt: <suggested_alt>
        Insertion: <insertion_hint>
    Medium
    Small
  Image Issues
    Large/Medium/Small
  Possibly Covered
    Large/Medium/Small
```

### Cross-Linking Rules

- **Drift → Feature.** `DriftFinding.Feature` is the feature name; linked to `feat-<slug>` anchor.
- **Screenshot → Feature.** `ScreenshotGap.PageURL` is mapped to feature(s) via reversed `DocsFeatureMap` (page → features). When the page resolves to exactly one feature, the screenshot links to that feature's anchor. When it resolves to zero or many, no link is rendered (we say "no single feature owner" — same conservative rule as today's reporter).
- **TOC entries.** Every TOC line is a clickable internal link to the corresponding anchor.

### Empty Buckets

A priority bucket with zero findings is omitted entirely — heading, sub-heading, and TOC entry — matching `gaps.md` and `screenshots.md` behavior.

### Conditional Sections

- `Screenshots` section is omitted (along with its TOC entry) when `ScreenshotsRan == false`. This mirrors the `(skipped)` convention.
- `Image Issues` is omitted when empty (which happens on every non-vision run).
- `Possibly Covered` is omitted when empty.

## Styling

Constants live in `internal/pdf/style.go` and are derived from the Hextra theme override at `internal/site/templates/hextra-custom.css`. We do not load the CSS at runtime — we hard-code matching values, with a comment cross-referencing the CSS source so a future Hextra override sync stays easy to track.

- **Palette.** Brand accent (links, headings underline), three priority pill colors (Large = red-ish, Medium = amber, Small = neutral), body gray, muted gray for metadata.
- **Type.** Sans-serif body (fpdf's `Helvetica` core font — no embedded font files, smallest PDF size). Monospace (`Courier`) for code-ish strings (file paths, URLs).
- **Priority pill.** A small rounded rectangle behind the priority word, drawn via fpdf's `RoundedRect`.
- **Page margins.** 1" top/bottom, 0.75" left/right.
- **Header/footer.** Footer shows `<project name> · page <n> of <total>`. Header is plain; cover page suppresses both.

## Library Choice

**`github.com/go-pdf/fpdf`** (the maintained fork of `jung-kurt/gofpdf`).

- Pure Go, zero CGO, builds clean on every goreleaser target.
- Stable API; clickable internal anchors via `AddLink` / `SetLink` / `WriteWithLink`, which we need for the TOC and the drift-to-feature cross-links.
- Two-pass page numbering supported via `AliasNbPages`.
- Built-in core fonts (Helvetica, Courier, Times) — no font-embedding fragility.

**Considered and rejected:**
- `maroto/v2` — nice layout DSL on top of gofpdf, but its public API does not surface internal anchor links cleanly. Forcing it would add an abstraction that fights the feature we most need.
- `unidoc/unipdf` — non-OSS license. Hard no for a maintainer-distributed CLI.
- `signintech/gopdf` — UTF-8 support requires bundling fonts; we lose the "core fonts only" simplicity.

**For tests:** `github.com/ledongthuc/pdf` text extraction — pure Go, fine for parse-back assertions. Not a render-quality tool, but we only need to assert text content.

## File-Level Breakdown

### New

- `internal/pdf/pdf.go`
  - `type Inputs struct { ... }` — the in-memory bundle described above.
  - `func WriteReport(dir string, in Inputs) error` — entry point. Constructs the fpdf doc, calls into cover/toc/sections, writes to `<dir>/report.pdf`.
- `internal/pdf/cover.go`
  - `renderCover(pdf, in)` — title block + run metadata + summary counts.
- `internal/pdf/toc.go`
  - `tocEntry`, `collectTOC(in) []tocEntry` — pre-computes section structure (so page-number resolution can run in a second pass).
  - `renderTOC(pdf, entries, anchors)` — emits the TOC page with clickable internal links.
- `internal/pdf/sections.go`
  - `renderFeatures(pdf, in, anchors)`
  - `renderGaps(pdf, in, anchors)`
  - `renderScreenshots(pdf, in, anchors)` — only called when `in.ScreenshotsRan == true`.
- `internal/pdf/anchors.go`
  - `type anchorTable map[string]int` — maps anchor name (e.g. `feat-<slug>`) to fpdf link id. Allocated up front so any renderer can request `anchors.For("feat-foo")` and get a stable link id even before its page is written.
  - `slugify(name string) string` — kebab-case slugger reused from `internal/site` if available; otherwise mirrors it.
- `internal/pdf/style.go`
  - Palette constants, priority-pill colors, helpers (`drawPriorityPill`, `applyHeading1`, etc.).
- `internal/pdf/pdf_test.go` — see TDD section below.
- `internal/pdf/cover_test.go`
- `internal/pdf/sections_test.go`
- `cmd/ftg/testdata/script/analyze_pdf_default.txtar` — default emission.
- `cmd/ftg/testdata/script/analyze_no_pdf.txtar` — `--no-pdf` opt-out + reports-block annotation.

### Modified

- `internal/cli/analyze.go`
  - New `noPDF` bool, bound to `--no-pdf` (default `false`).
  - New call to `pdf.WriteReport` just after the `if !noSite { site.Build(...) }` block (keeps PDF generation independent of site).
  - New `reports:` block line for `report.pdf` (with `(skipped)` annotation when `--no-pdf`).
- `internal/cli/render.go` — the `render` subcommand re-emits markdown reports and rebuilds the site. Wire `pdf.WriteReport` here too so `ftg render` produces a consistent set, behind the same `--no-pdf` flag. (Confirmed in recon: `render.go:22` describes this exact responsibility.)
- `.plans/VERIFICATION_PLAN.md` — append Scenario 18 (below).
- `go.mod` / `go.sum` — `github.com/go-pdf/fpdf`, `github.com/ledongthuc/pdf` (test-only).

## TDD Task Order

Each task is one RED → GREEN → REFACTOR cycle. Each becomes one commit. After every commit: `go test ./...`, `golangci-lint run`, `go build ./...` all green, then update `PROGRESS.md`.

### Task 1 — Package skeleton

- **RED:** `internal/pdf/pdf_test.go` — `TestWriteReport_EmitsFile`. Calls `WriteReport(tmp, Inputs{ProjectName: "x"})`, asserts `report.pdf` exists, opens it with `ledongthuc/pdf`, asserts page count ≥ 1.
- **GREEN:** `pdf.go` builds an `fpdf.New("P", "in", "Letter", "")`, adds one empty page, saves to `<dir>/report.pdf`.
- **REFACTOR:** Extract `newDoc()` constructor seam.

### Task 2 — Cover page content

- **RED:** `cover_test.go` — `TestRenderCover_ContainsMetadata`. Asserts extracted text contains: project name, repo URL, docs URL, timestamp formatted as `YYYY-MM-DD HH:MM UTC`, the summary counts (built from `len(Mapping)`, drift issue total, screenshot total).
- **GREEN:** `renderCover` writes the title block.

### Task 3 — Page header/footer

- **RED:** `pdf_test.go` — `TestFooter_HasPageNumbers`. Render a doc with enough features to span 3+ pages; extract text; assert `page 1 of N`, `page 2 of N`, `page 3 of N` (or matching format) on the appropriate pages. Cover page MUST NOT carry the footer.
- **GREEN:** Register `SetFooterFunc`; use `AliasNbPages`; gate on `pdf.PageNo() > 1`.

### Task 4 — Anchor table + TOC scaffold

- **RED:** `pdf_test.go` — `TestTOC_ListsTopLevelSections`. Render an Inputs with at least one feature, one drift finding, screenshots enabled with one missing gap. Assert TOC page contains: `Features`, `Gaps`, `Screenshots`, each with a page-number column. Also assert that the anchors `features`, `gaps`, `screenshots` are registered in the doc (call `pdf.Link()`-style assertion or, more practically, assert at least one internal link annotation exists per section heading).
- **GREEN:** Build `anchors` table; allocate link ids up front; emit the TOC page with clickable rows.

### Task 5 — Features section

- **RED:** `sections_test.go` — `TestRenderFeatures_OneBlockPerFeature`. Two features, one with description + layer + docs pages, one minimal. Assert both names appear, the documented one's pages appear, the user-facing/layer/status fields appear. Assert anchor `feat-<slug>` is registered for each. Mirror `reporter.WriteMapping`'s field set.
- **GREEN:** `renderFeatures` walks `in.Mapping`, computes docs status from `in.DocsMap` (same logic reporter.go uses).

### Task 6 — Gaps section, priority-bucketed, cross-linked

- **RED:** `sections_test.go` — `TestRenderGaps_BucketsByPriority`. Inputs with three drift findings of mixed priority. Assert order in the output: Large first, then Medium, then Small. Assert empty bucket is omitted. Assert the feature name in each finding has a clickable link annotation pointing at the matching `feat-<slug>` anchor.
- **GREEN:** Group drift issues by `Priority`, render in fixed order, suppress empty buckets, emit `WriteWithLink`-style cross-links.

### Task 7 — Screenshots section (gated)

- **RED:** `sections_test.go` — three sub-tests:
  - `TestRenderScreenshots_Skipped_WhenNotRun`: `ScreenshotsRan=false` → no Screenshots heading, no TOC entry.
  - `TestRenderScreenshots_MissingBucketed`: missing gaps of mixed priority → Large/Medium/Small sub-headings in correct order, empty buckets omitted.
  - `TestRenderScreenshots_PageToFeatureCrosslink`: a `PageURL` whose `DocsMap` maps to exactly one feature → link to that feature's anchor. A `PageURL` mapping to zero or many features → no link (assert via "no link annotation in that span").
- **GREEN:** Implement `renderScreenshots`; reuse priority-bucket helper from Task 6; build a `page → features` index inside the renderer.

### Task 8 — TOC sub-entries

- **RED:** `pdf_test.go` — `TestTOC_HasSubEntries`. Render an Inputs with two features, drift findings spanning Large + Medium, and screenshots Missing in Small only. Assert TOC includes:
  - Features header + one row per feature.
  - Gaps header + Large, Medium rows (no Small).
  - Screenshots header + Missing row + Small row (no Medium/Large under Missing, no Image Issues, no Possibly Covered).
- **GREEN:** Extend `collectTOC` to walk the same data the renderers walk and emit sub-entries; refactor renderers to update a shared page-number map after rendering each anchor.

### Task 9 — Wire into `ftg analyze` + `ftg render`

- **RED:**
  - `cmd/ftg/testdata/script/analyze_pdf_default.txtar` — analyzes the stub repo, asserts `report.pdf` is created and the stdout `reports:` block includes a `report.pdf` line.
  - `cmd/ftg/testdata/script/analyze_no_pdf.txtar` — runs with `--no-pdf`, asserts `report.pdf` does NOT exist, asserts the `reports:` block includes `report.pdf (skipped)`.
  - Add equivalent assertions to an existing `render` testscript if one exists; otherwise leave `render` covered by integration-only.
- **GREEN:**
  - Add `--no-pdf` flag in `internal/cli/analyze.go`.
  - Call `pdf.WriteReport` after the `site.Build` block.
  - Update the `Fprintf` reports-block formatter.
  - Mirror the wiring in `internal/cli/render.go`.

### Task 10 — Verification plan update

- **RED:** N/A (docs).
- **GREEN:** Append Scenario 18 (full text in next section) to `.plans/VERIFICATION_PLAN.md`.

## Verification (Scenario 18)

Sketch — exact text refined when the task lands. Lives under `### Scenario 18: PDF Report Export` in `.plans/VERIFICATION_PLAN.md`.

**Context.** Verify the `report.pdf` artifact is produced by default, opts out cleanly, and its contents match `drift.json` / `screenshots.json` / `mapping.md` on a real fixture.

**Steps.**
1. Run `find-the-gaps analyze --repo ./testdata/fixtures/known-good --docs https://<docs>` (default; no `--no-pdf`).
2. Confirm `<projectDir>/report.pdf` exists.
3. Open the PDF in a viewer (Preview, Skim, or `pdftotext`). Inspect cover, TOC, sections.
4. Run `pdftotext -layout <projectDir>/report.pdf -` and grep for every `feature_id` in `drift.json` and every `page_url` in `screenshots.json`. All must appear in the extracted text.
5. Click each TOC entry in a real PDF reader; confirm it jumps to the right anchor.
6. Re-run with `--no-pdf`. Confirm `<projectDir>/report.pdf` does NOT exist. Confirm stdout `reports:` block lists `report.pdf (skipped)`.
7. Re-run with `--experimental-check-screenshots`; confirm the Screenshots section appears in the PDF and includes Image Issues / Possibly Covered when those buckets are non-empty.

**Success Criteria.**
- [ ] Step 2: file exists; non-zero size.
- [ ] Step 3: cover shows project name, repo URL, docs URL, timestamp; TOC lists Features / Gaps / Screenshots (Screenshots present iff `--experimental-check-screenshots`).
- [ ] Step 4: every drift `feature` and every screenshot `page_url` appears in extracted text.
- [ ] Step 5: TOC links resolve.
- [ ] Step 6: file absent; stdout annotation present.
- [ ] Step 7: Screenshots section rendered with the expected sub-sections; gated correctly.

**If Blocked.** If extracted text is empty (font-encoding regression in fpdf) capture the PDF and the extractor output; do not paper over with a different extractor. If TOC links fail to resolve, audit the anchor table for unrendered ids before adjusting renderer call order.

## Risks / Open Questions

1. **Long URLs in cells.** Docs URLs can exceed the column width. Use fpdf's `MultiCell` with explicit width and let it wrap; if the result is ugly we fall back to truncation + a footnote-style "full URL on cover page" pointer.
2. **Internal-link page resolution.** fpdf requires `SetLink(id, y, page)` to be called *after* the target page has been rendered. The anchor table must therefore be populated in a single pass over the renderers, not declaratively up-front. Verified by reading `go-pdf/fpdf` README; flagged here so the implementer doesn't get surprised.
3. **Feature-name slugs colliding.** Two features that slugify to the same string would clash on `feat-<slug>`. Disambiguate by suffixing a stable counter (`feat-<slug>-2`); add a unit test.
4. **PDF determinism for tests.** fpdf writes a creation timestamp into the PDF metadata; this would break any byte-golden test. We avoid this by using text-extraction asserts only (the chosen path). If a future regression demands byte-stability, override `pdf.CreationDate` to a fixed value first.

## Out of Scope For This Plan (Future Work)

- Embedding rendered screenshots from the docs site.
- A standalone `ftg pdf <projectDir>` subcommand.
- Page-size flag (`--pdf-size=A4`).
- Cover-page logo / project branding hooks.
