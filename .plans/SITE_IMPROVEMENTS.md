# Rendered Site Improvements — Running List

This document tracks user-requested improvements to the Hugo site rendered by
`ftg analyze`. Each item is mapped to a TaskCreate entry; this file is the
human-readable index.

## Backlog

- **Add `ftg render` command** — Regenerate `<projectDir>/site/` from cached
  artifacts (mapping/docs maps, drift.json, screenshots.json, spider index)
  without re-running analysis. Same `--site-mode` and `--keep-site-source`
  flags as `analyze`. Uses the same project-picker logic as `serve`. Lets
  users pick up new site templates without paying the LLM/network cost
  again. **Deferred** so we can iterate on visible improvements first.

## In flight (this branch)

### Index page (`/`)
1. **At a Glance → top, as stat cards.** Each metric becomes a card showing
   a big number above its label. Cards are colored:
   - `Features` (count) — always neutral (informational).
   - `Undocumented user-facing` — `0` is good, `>0` is bad.
   - `Drift findings` — `0` is good, `>0` is bad.
   - `Missing screenshots` — `0` is good, `>0` is bad.
2. **`Generated YYYY-MM-DD ... UTC` → bottom of the page** (currently sits
   directly under the page title).

### Mapping page (`/mapping/`)
3. **Spacing between feature cards.** Adjacent cards visually distinct.
4. **Visual styling for cards.** Color + badges (layer / user-facing /
   documented).

### Gaps page (`/gaps/`)
5. **Rename `Undocumented Code` → `Undocumented Features`.** Show only
   user-facing entries; drop the `User-facing` / `Not user-facing`
   sub-headings entirely. Render each entry as a problem callout.
6. **Remove `Unmapped Features` section** entirely.
7. **Stale Documentation: card-style list items + colored priority
   headers.** Large = highest-impact problem (red), Medium (amber),
   Small (neutral). Each finding rendered as a card.

### Screenshots page (`/screenshots/`)
8. **Color Large/Medium/Small headers** the same way as on the gaps page.
9. **Card-style finding entries.** Each missing/stale-screenshot finding
   wrapped in a single card so the eye gets page → passage → what to show
   → where to insert it together. All current fields (page URL, passage,
   should-show, alt text, insertion hint, why) preserved.
10. **Card-style Possibly Covered + Image Issues.** Same treatment as
    finding entries. Preserve all fields.

## Conventions

- Custom CSS goes in
  `internal/site/assets/theme/hextra/assets/css/custom.css` (loaded after
  Hextra's compiled main.css, no Tailwind recompile needed).
- Class prefix: `ftg-` for project-specific classes. Examples:
  `.ftg-stats`, `.ftg-stat-card`, `.ftg-stat-card--good`,
  `.ftg-stat-card--bad`, `.ftg-stat-card--neutral`, `.ftg-feature-card`,
  `.ftg-badge`, `.ftg-undoc-callout`, `.ftg-priority--large`, etc.
- Templates raw-HTML emit these classes; Hugo unsafe markdown is already on.

## Done

- Index page: At-a-Glance to top as colored stat cards (good/bad/neutral).
- Index page: "Generated" timestamp moved to bottom.
- Mapping page: feature cards with vertical spacing, layer/user-facing/
  documented badges, and a left-edge color rail tied to documentation
  status.
- Gaps page: renamed "Undocumented Code" → "Undocumented Features";
  user-facing only; rendered as `.ftg-undoc` problem callouts.
- Gaps page: removed "Unmapped Features" section.
- Gaps page: stale-doc findings rendered as `.ftg-stale` cards;
  Large/Medium/Small headers wrapped in `.ftg-priority--*` for color.
- Screenshots page: missing/possibly-covered/image-issue findings
  rendered as `.ftg-shot` cards (with explicit page header, passage
  block, and labeled fields). Large/Medium/Small headers wrapped in
  `.ftg-priority--*` for color.
- Expanded mode: per-feature drift section and per-page screenshot
  pages picked up the same color + card treatment for visual
  consistency across modes.
