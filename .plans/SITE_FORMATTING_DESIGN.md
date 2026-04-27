+++
title = "Site Formatting Cleanup — Design"
date = "2026-04-27"
+++

# Site Formatting Cleanup — Design

Improves the rendered Hugo output of `find-the-gaps analyze`. Standalone Markdown files at the project root (produced by `internal/reporter`) are intentionally **not** modified — every change here applies only to the materialized Hugo site under `<projectDir>/site/`.

## Scope

Seven concrete fixes:

1. **Home page** — drop duplicate H1; the frontmatter `title` already renders as the page heading.
2. **Mapping page** — drop duplicate `# Feature Map` H1.
3. **Mapping page** — render `Implemented in` / `Symbols` / `Documented on` as proper sub-section lists, not comma-joined inline lines.
4. **Gaps page** — drop duplicate `# Gaps Found` H1.
5. **Screenshots** — drop duplicate H1s in three places (mirror-mode `screenshots.md`, expanded-mode section index, per-page screenshot files).
6. **Screenshots** — render the verbatim `quoted_passage` in a `` ```markdown `` fenced block (preserving real newlines), and put `should_show` / `suggested_alt` / `insertion_hint` in a Hextra `{{< callout >}}` block.
7. **`--keep-site-source`** — flip default to `true`. The Hugo source survives the run by default; users disable it with `--keep-site-source=false`.

## File-by-file changes

### Templates (site-only, edited directly)

- `internal/site/assets/templates/home.md.tmpl` — remove the `# {{ .ProjectName }}` H1 line.
- `internal/site/assets/templates/screenshot_page.md.tmpl` — remove the `# {{ .Title }}` H1 line; replace each gap's bullet block with a fenced passage + callout (layout below).
- `internal/site/assets/templates/feature.md.tmpl` — no change. Already uses sub-heading lists.

### New templates (mirror-mode rendering from structured data)

The current `materializeMirror` reads `mapping.md` / `gaps.md` / `screenshots.md` from disk and wraps each with frontmatter. To change content (lists for mapping, callout/fence for screenshots) without touching the standalone files, mirror-mode rendering of these two files moves from "read+wrap" to "render from `Inputs` via template."

- `internal/site/assets/templates/mapping_page.md.tmpl` — renders the website's `mapping.md` directly from `Inputs.Summary` and `Inputs.Mapping`. No `# Feature Map` H1. Files / Symbols / Documented-on become `#### sub-heading` + bulleted lists.
- `internal/site/assets/templates/screenshots_page_mirror.md.tmpl` — renders the website's `screenshots.md` from `Inputs.Screenshots`, grouped by page, using the same per-gap layout as the per-page expanded template.

`gaps.md` keeps the read+wrap path; only the leading `# Gaps Found` line is stripped before frontmatter is prepended. Doing so avoids re-implementing the existing feature-name linking logic in `linkFeatureNames`.

### Materialize logic

`internal/site/materialize.go`:

- `materializeMirror`:
  - For `mapping.md`: render via `mapping_page.md.tmpl` (no read of standalone file). Write to `contentDir/mapping.md` with frontmatter.
  - For `screenshots.md`: render via `screenshots_page_mirror.md.tmpl`. Write to `contentDir/screenshots.md` with frontmatter.
  - For `gaps.md`: read standalone, run `linkFeatureNames`, then strip a leading `# ...\n` line if present, then prepend frontmatter, then write.
- `materializeExpanded`:
  - Strip leading `# ...\n` from `gaps.md` after `linkFeatureNames`, before prepending frontmatter.
  - Section index for screenshots (line 184): drop the `# Missing screenshots` body; frontmatter title is enough.
  - Per-page screenshot files: covered by the `screenshot_page.md.tmpl` edit above.

A small helper `stripLeadingH1(body []byte) []byte` is added — used only by `materializeMirror` and `materializeExpanded` for `gaps.md`.

### CLI / flag default flip

`internal/cli/analyze.go:448`:

```go
cmd.Flags().BoolVar(&keepSiteSource, "keep-site-source", true,
    "preserve generated Hugo source at <projectDir>/site-src/ (default true; use --keep-site-source=false to discard)")
```

The gate at `analyze.go:420` (`if keepSiteSource && !noSite { ... }`) is unchanged.

### Verification plan

`.plans/VERIFICATION_PLAN.md` Scenario 11:

- Step 6 now reads: "Re-run with `--keep-site-source=false`. Confirm `<projectDir>/site-src/` does NOT exist."
- Default-run criteria (steps 1, 3): add a check that `<projectDir>/site-src/hugo.toml` IS present after a default run.

## Per-feature mapping section (rendered)

```
## <feature name>

> <description>

- **Layer:** <layer>
- **User-facing:** yes/no
- **Documentation status:** documented/undocumented

#### Implemented in
- `internal/foo/bar.go`

#### Symbols
- `Foo`

#### Documented on
- <https://example.com/docs/foo>
```

Heading depth: the page H1 comes from frontmatter; product-summary and the features list are H2; each feature is H3; sub-sections are H4. Hextra's TOC handles four levels comfortably.

## Per-gap screenshot layout (mirror and expanded)

```
### From: <page url>

` ` `markdown
<verbatim quoted_passage with real newlines, NOT HTML-escaped>
` ` `

{{< callout type="info" >}}
**Screenshot should show:** <should_show>

**Alt text:** `<suggested_alt>`

**Insertion hint:** <insertion_hint>
{{< /callout >}}
```

Notes:

- The fence uses `` ```markdown ``. The passage is rendered verbatim (treated as source, not interpreted).
- Risk: a passage that itself contains a fenced code block with three backticks will break the outer fence. Acceptable trade-off given the user preference; we accept that and do not switch to four-backtick fences automatically.
- The callout uses Hextra's built-in `info` style (blue accent).

## Tests (TDD — RED first)

New / updated tests in `internal/site/materialize_test.go`:

- `TestMaterializeMirror_HomeNoDuplicateH1` — `_index.md` does NOT contain `# <ProjectName>`.
- `TestMaterializeMirror_MappingNoFeatureMapH1` — `contentDir/mapping.md` does NOT contain `# Feature Map`.
- `TestMaterializeMirror_MappingHasSubHeadings` — `contentDir/mapping.md` contains `#### Implemented in`, `#### Symbols`, `#### Documented on`, and a bulleted file list (e.g. ``- `internal/foo/bar.go` ``).
- `TestMaterializeMirror_GapsNoH1` — `contentDir/gaps.md` does NOT contain `# Gaps Found`.
- `TestMaterializeMirror_ScreenshotsNoH1` — `contentDir/screenshots.md` does NOT contain `# Missing Screenshots`.
- `TestMaterializeMirror_ScreenshotsCalloutAndFence` — `contentDir/screenshots.md` contains a `` ```markdown `` fence and `{{< callout type="info" >}}`.
- `TestMaterializeExpanded_ScreenshotIndexNoH1` — `contentDir/screenshots/_index.md` does NOT contain `# Missing screenshots`.
- `TestMaterializeExpanded_ScreenshotPageCalloutAndFence` — per-page screenshot file contains the fence and callout.
- `TestMaterializeExpanded_GapsNoH1` — `contentDir/gaps.md` does NOT contain `# Gaps Found`.
- `TestStripLeadingH1` — unit tests for the helper: with H1, without H1, with leading whitespace, with no body.

New / updated tests for the flag flip (`internal/cli/analyze_site_test.go` or a sibling):

- `TestKeepSiteSource_DefaultTrue` — after a default run, `<projectDir>/site-src/hugo.toml` exists.
- Existing `--keep-site-source` test stays green (explicit `true` is a no-op).
- `TestKeepSiteSource_ExplicitFalseDiscards` — `--keep-site-source=false` removes `site-src/`.

Reporter tests (`internal/reporter/reporter_test.go`) stay green — the standalone files are not modified.

## Implementation order

1. RED: add new mapping tests (no `# Feature Map`, sub-heading lists).
2. GREEN: add `mapping_page.md.tmpl` + wire into `materializeMirror`.
3. RED: add gaps H1-strip tests (mirror + expanded).
4. GREEN: add `stripLeadingH1` helper, apply to gaps in both modes.
5. RED: add screenshot mirror-render tests (no H1, fence, callout).
6. GREEN: add `screenshots_page_mirror.md.tmpl`, wire into `materializeMirror`.
7. RED: add expanded screenshot tests (per-page fence + callout, index no-H1).
8. GREEN: edit `screenshot_page.md.tmpl`, edit expanded section-index emission.
9. RED: add home no-H1 test.
10. GREEN: edit `home.md.tmpl`.
11. RED: add `--keep-site-source` default-true test, default-discard test.
12. GREEN: flip flag default in `analyze.go`. Update help text.
13. Update `.plans/VERIFICATION_PLAN.md` Scenario 11.
14. Run full test suite + `go build ./...` + `golangci-lint run`. Update `PROGRESS.md`.

Each step ends in a commit per CLAUDE.md's frequent-commit rule.
