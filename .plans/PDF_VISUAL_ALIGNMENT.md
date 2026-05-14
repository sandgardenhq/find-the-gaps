# PDF Visual Alignment

## Goal

Bring `report.pdf` in line with the Hextra-rendered site so the two outputs read as the same product. Today the PDF uses Helvetica + a hand-picked blue, while the site uses Inter + the `--ftg-*` token palette plus card-based findings and pill-shaped priority headers. This plan closes that gap.

Out of scope (deferred): embedding the Inter TTF font. Core fonts (Helvetica + Courier) stay; palette and shapes do the heavy lifting.

## Source of truth

Every visual decision in this plan is derived from one file:

`internal/site/assets/theme/hextra/assets/css/custom.css`

When the site CSS changes, this plan and the constants in `internal/pdf/style.go` are the only things that need to follow. We don't parse the CSS at runtime — we mirror it once into Go constants and cross-reference each block in a comment.

## Design tokens (extracted from custom.css)

| Token (Go const)        | Hex       | CSS source              | Used for                                             |
| ----------------------- | --------- | ----------------------- | ---------------------------------------------------- |
| `colorGoodFg`           | `#16a34a` | `--ftg-good`            | "documented" badge text, good stripe                 |
| `colorGoodBg`           | `#dcfce7` | `--ftg-good-bg`         | "documented" badge fill                              |
| `colorGoodBorder`       | `#86efac` | `--ftg-good-border`     | "documented" badge border                            |
| `colorBadFg`            | `#dc2626` | `--ftg-bad`             | Large priority text, undoc stripe                    |
| `colorBadBg`            | `#fee2e2` | `--ftg-bad-bg`          | Large priority pill fill                             |
| `colorBadBorder`        | `#fca5a5` | `--ftg-bad-border`      | Large priority pill border                           |
| `colorWarnFg`           | `#d97706` | `--ftg-warn`            | Medium priority text                                 |
| `colorWarnBg`           | `#fef3c7` | `--ftg-warn-bg`         | Medium priority pill fill                            |
| `colorWarnBorder`       | `#fcd34d` | `--ftg-warn-border`     | Medium priority pill border                          |
| `colorNeutralFg`        | `#475569` | `--ftg-neutral`         | Small priority text, badge text                      |
| `colorNeutralBg`        | `#f1f5f9` | `--ftg-neutral-bg`      | Small priority pill fill, badge fill                 |
| `colorNeutralBorder`    | `#cbd5e1` | `--ftg-neutral-border`  | Small priority pill border, stale card left stripe   |
| `colorCardBg`           | `#ffffff` | `--ftg-card-bg`         | Default card fill                                    |
| `colorCardBorder`       | `#e2e8f0` | `--ftg-card-border`     | Card border                                          |
| `colorBodyFg`           | `#0f172a` | (default body)          | Body text — Tailwind's slate-900                     |
| `colorMutedFg`          | `#64748b` | `--ftg-muted`           | Metadata, sub-text, generated-at line                |
| `colorLinkFg`           | `#2563eb` | (Hextra default link)   | Cross-link rendering — Tailwind's blue-600           |

Existing `colorBrand*` / `colorBody*` / `colorLarge*` / `colorMedium*` / `colorSmall*` constants get retired in favor of the table above. The current values are close but not on-token.

## Components

### Severity card (used by drift findings, screenshot findings)

```
+--+-------------------------------------------------+
||| Feature name           <- bold, body color       |
||| Issue text — wraps via MultiCell                 |
||| Why: priority reason   <- italic, muted color    |
||| (https://docs.example/page)                      |
+--+-------------------------------------------------+
```

- Outer rect: 1pt stroke `colorCardBorder`, 0.08" radius, `colorCardBg` fill, drawn before content.
- Left stripe: 0.06" wide rect filling left edge, same height as card, severity color (`colorBadFg` / `colorWarnFg` / `colorNeutralBorder`).
- Body padding: 0.18" left (after stripe), 0.14" right/top/bottom.
- Card height is measured by pre-flighting MultiCell wrap heights so we know the box size before drawing the border (fpdf needs explicit height for `RoundedRect`).

### Priority pill (used at top of every non-empty bucket)

```
+------------+
|   LARGE    |
+------------+
```

- Width = `GetStringWidth("LARGE") + 0.32"` (padding both sides).
- Height = 0.28".
- Corner radius 0.06".
- Fill: `colorBadBg` / `colorWarnBg` / `colorNeutralBg`.
- Border: 0.5pt, severity color.
- Text: 9pt, bold, uppercase, +1px letter-spacing approximated by `GetStringWidth` calculation, color = severity foreground.

### Feature card (Features section)

```
+--+-----------------------------------------+
||| Feature name             <- h3-size      |
||| > description            <- italic, muted|
||| [layer] [user-facing] [doc status]       |
||| Implemented in: file.go, file2.go        |
||| Symbols: A, B, C                         |
||| Documented on: https://docs/page         |
+--+-----------------------------------------+
```

- Same card shell as severity card.
- Left stripe color: green if `len(docPages) > 0` (documented), red if undocumented + user-facing, neutral otherwise. Mirrors `.ftg-feature-card--documented` / `--undocumented`.
- Metadata renders as **pill badges** (same shape as priority pill but smaller — 8pt text, 0.22" height) instead of `Layer: api` text rows. Badge colors:
  - `[layer]` — neutral
  - `[user-facing]` — warn-tinted; `[internal]` — neutral
  - `[documented]` — good-tinted; `[undocumented]` — bad-tinted
- Files / Symbols / Documented-on stay as wrapped lines.

### Hero cover page

```
+----------------------------------------------------+
|                                                    |
|              Find the Gaps                         |
|              Acme Widget API                       |
|                                                    |
|        Repo:  github.com/acme/widget-api           |
|        Docs:  docs.acme.example/widget-api/        |
|        Generated: 2026-05-13 14:32 UTC             |
|                                                    |
|     +-----------+ +-----------+ +-----------+      |
|     |    4      | |    3      | |    4      |      |
|     | features  | |   gaps    | | screenshot|      |
|     +-----------+ +-----------+ +-----------+      |
|                                                    |
+----------------------------------------------------+
```

- Centered title block (Find the Gaps + project name).
- Three "stat cards" in a row showing the same counts the cover currently has as plain text. Same card shell, big number (24pt bold, severity color when non-zero, neutral when zero), label below (9pt muted).
- Stat colors:
  - features → neutral
  - gaps → bad if > 0, good if 0
  - screenshot issues → bad if > 0, good if 0, hidden if `!ScreenshotsRan`
- Whole cover sits on a faint background band — a full-width filled rect from y=1.0 to y=4.5 in `#fafafa` so the hero feels distinct from the table-of-contents page that follows.

## File-level breakdown

### Modified
- `internal/pdf/style.go` — replace existing color constants with the `colorGoodFg` … `colorLinkFg` table above. Each block carries a `// matches --ftg-bad in custom.css` comment.
- `internal/pdf/cover.go` — replace plain text cover with hero layout + stat-card row.
- `internal/pdf/sections.go` — replace `priorityHeading` with a pill renderer; replace `renderDriftFinding`, `renderMissingGap`, `renderImageIssue`, and `renderFeatureBlock` with card-based renderers.

### New
- `internal/pdf/card.go` — primitives:
  - `card(doc, x, y, w, h, stripeColor)` — outer border + left stripe.
  - `pill(doc, x, y, label, fg, bg, border)` — rounded-rect pill, returns width drawn.
  - `measureCard(doc, content) (height float64)` — pre-flight wrap measurement so `RoundedRect` can be sized.
  - `statCard(doc, x, y, w, h, number, label, fg)` — hero variant with big number.
- `internal/pdf/card_test.go` — unit tests for measurement math, pill width calculation, and card height pre-flight.

## Card height pre-flighting

fpdf's `RoundedRect` needs explicit height before any text is drawn — but the text height depends on how `MultiCell` wraps long lines, which depends on font metrics. Sequence:

1. Compute available width: `pageWidth - 2*cardPadX - stripeW`.
2. For each line of card content, call `doc.SplitText(text, availW)` to get wrapped line count.
3. Sum line counts × per-line height + inter-line gaps + top/bottom padding.
4. Draw `RoundedRect` with computed height.
5. Move cursor inside the card and emit text with the same widths and heights used for measurement.

A future regression where a renderer changes its text layout without updating measurement will be caught by a golden-test on extracted text overlay coordinates.

## TDD task order

Each task is one RED → GREEN → REFACTOR cycle, one commit. Tests use the existing `extractText` + manual page-walk helpers; new geometry tests use `card_test.go`.

1. **Palette swap.** Replace `colorBrand*` / `colorBody*` / `colorLarge*` / `colorMedium*` / `colorSmall*` with the new `colorGoodFg` … `colorLinkFg` set. Update every caller. Existing tests stay green (they assert content, not colors).
2. **Pill renderer.** Add `card.go` with `pill()`. RED: `TestPill_DrawsBoundedRectWithLabel`. Replace `priorityHeading()` body with a pill call. Golden assertions: extracted text still contains "LARGE" (now uppercase); buckets still appear in order.
3. **Severity card (drift).** Add `card()` and `measureCard()`. RED: `TestRenderDriftFinding_DrawsCard` — render one finding, assert page contains issue text AND assert a rectangular region was filled at the expected coords (via post-render inspection of the PDF byte stream for `re f` operators).
4. **Severity card (screenshots).** Same shell, applied to `renderMissingGap` and `renderImageIssue`.
5. **Feature card.** Replace `renderFeatureBlock` with card shell + badge row. RED: `TestRenderFeatureBlock_HasBadges` — extracted text includes badge labels ("api", "user-facing", "documented"); the documented feature has a green stripe (assert byte-stream contains the green fill operator at the expected x range).
6. **Hero cover.** Add `statCard()`. Replace cover layout. RED: `TestRenderCover_HeroLayout` — three stat numbers appear in extracted text in left-to-right order; cover background-band rect drawn at expected y range.
7. **Regenerate sample-report.pdf.** Re-run `cmd/ftg-sample`, verify each page against `/tmp/sample-page-*.png`, commit.
8. **Verification plan update.** Append a "Visual alignment" sub-clause to Scenario 18: render the fixture PDF, open in a viewer, eyeball card stripes / pill colors / hero stats against the rendered Hextra site at `<projectDir>/site/index.html`. Pass/fail by reviewer judgment.

## Risks / open questions

1. **Card height measurement is fragile.** fpdf doesn't expose a "what would MultiCell take?" API directly. We approximate by counting wrapped lines via `SplitText` and multiplying by line height. If our line-count estimate is off by one, the card border crops the last line. Mitigation: add a small `cardPadBottom` slack and unit-test the measurement helper against known-width inputs.
2. **Stat-card sizing on long projects.** A project with 99 features blows past the 24pt bold number width on the cover. Mitigation: cap card width at 1.6" and shrink the font to 18pt if the number is 3+ digits.
3. **Faint-band background color clash with dark-mode users**. PDF is fixed-light. The site has a dark mode (`.dark` block in custom.css); the PDF will only ever look like the light-mode site. Document this explicitly in the plan; not a defect.
4. **Hextra theme drift.** If the site CSS changes the palette, the PDF will silently fall behind. No automatic sync. Mitigation: a single `// SYNC WITH custom.css` comment block in `style.go` so the failure mode is obvious during review.

## Acceptance

- Sample PDF (`.plans/sample-report.pdf`) renders side-by-side with `<projectDir>/site/index.html` and the two read as the same product (same palette, same severity language, same finding card shapes).
- All existing pdf-package tests stay green.
- New card-geometry tests stay green.
- `go build ./...` clean.
