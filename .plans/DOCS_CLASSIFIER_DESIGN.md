# Docs Page Classifier — Design

## Problem

Today, `spider.Crawl` greedily fetches every same-host link from the docs URL, and every fetched page flows into `AnalyzePage`, drift detection, and screenshot detection. There is no concept of "is this a docs page." A blog post, team bio, customer story, or pricing page is treated identically to an API reference.

This produces wrong answers, not just slow ones:

- **False negatives on undocumented surface.** A feature mentioned only on a `/blog/announcing-v3` post counts as "documented" even when no real docs page describes it.
- **False positives on drift.** A marketing page or engineering retrospective with a stale code snippet produces a real-looking drift finding against code that's correctly documented elsewhere.
- **Noise on screenshot suggestions.** Screenshot proposals on `/team`, `/careers`, `/customers/acme` are nonsense.

The framing is **correctness**, not efficiency. Filtering non-docs pages is what makes the *answer* right.

## Design

### Binary classifier, inclusive-by-default

Each page gets one boolean: `is_docs`. The rule the model applies:

> A page is **docs** if a user trying to *use* this product would consult it for current technical information about features, APIs, configuration, or behavior. **Marketing pages and blog posts are never docs**, even when they contain code snippets or release announcements. Default to docs when unsure about a technical-looking page that is NOT clearly a marketing page or blog post.

**Docs:** API references, tutorials, quickstarts, configuration references, dedicated changelog / release-notes pages.

**Not-docs:** marketing pages (landing pages, product/feature pages, pricing, comparison pages — even with code snippets), blog posts of any kind (release/launch announcements, feature-announcement posts, deep-dives, engineering retrospectives, generic company posts), customer case studies, team / about / careers / legal pages.

The "no marketing, no blog" rule is intentionally strict: docs are the canonical reference surface a user returns to, not promotional or editorial content. A feature mentioned only on a `/blog/` post or a marketing landing page is treated as undocumented — see edge cases below.

False negatives are worse than false positives — silently dropping a real docs page hides gaps the tool was built to find. The classifier defaults to `is_docs = true` when uncertain.

### Single LLM change point: `AnalyzePage`

`AnalyzePage` (`internal/analyzer/analyze_page.go:29`) already runs once per fetched page on the cheapest tier. It produces a summary + feature list. We extend its schema with one boolean.

- `analyzePageSchema` (line 14) gains `is_docs: boolean`, required.
- `PageAnalysis` struct gains `IsDocs bool`.
- Marginal LLM cost: **zero** — same call, same tier, same content.
- The prompt is annotated `// PROMPT:` per project convention.

Spider behavior is unchanged. The crawl still fetches every same-host page. Correctness lives in one place (model + page content), not split across URL heuristics + model.

### Filter points (downstream of `AnalyzePage`)

Two surgical filters in `internal/cli/analyze.go`:

1. **Docs feature map.** Before building `docsFeatureMap`, filter `analyses` to `IsDocs == true`. Not-docs pages remain in the spider cache (cheap to keep, cheap re-classify on re-run) but their features cannot enter the map. Drift detection consumes a strictly-docs feature map by construction.

2. **Screenshot detection.** The `[]DocPage` slice fed to `DetectScreenshotGaps` (`internal/analyzer/screenshot_gaps.go:235`) is filtered against `IsDocs`. The screenshot pass itself is unchanged.

### Audit visibility

One INFO log line after analysis:

```
classified: 47 docs, 12 non-docs (use -v to list)
```

At verbose level, list each not-docs URL with its summary so the user can sanity-check exclusions.

No `mapping.md` section, no dedicated file. The audit is transient by design — if a misclassification matters, it shows up downstream as a missing drift finding the user expected, at which point they re-run with `--no-cache`.

### No overrides in v1

No config file, no `.ftgdocs` / `.ftgnotdocs` files, no CLI flags to force-include or force-exclude URL patterns. The escape hatch for misclassifications is `--no-cache`, which forces every page through `AnalyzePage` again.

If real-world misclassifications make this painful, v2 adds overrides with evidence about which patterns matter. The current decision is deliberately YAGNI.

## Edge cases

| Case | Behavior |
|---|---|
| **All pages classified as non-docs** | Log warning, exit non-zero. Refuse to produce a misleading "everything is undocumented" report. |
| **Model returns malformed `is_docs`** | JSON-schema validation fails; existing `AnalyzePage` error path skips the page (same as today's behavior for malformed summaries). |
| **Cached analysis without `is_docs` field** | Default to `true` (inclusive). Existing caches stay valid; classification warms up naturally as caches refresh. |
| **Feature mentioned only on a not-docs page** | Becomes a "missing docs" finding. This is correct under the user's framing — a feature documented only on a blog is undocumented. |

## Testing

### Unit tests (RED before code)

- `analyzer/analyze_page_test.go` — `IsDocs` plumbed from JSON for `true`, `false`, and missing.
- `analyzer/schema_test.go` — schema accepts new required boolean.
- `spider/cache_test.go` — `is_docs` round-trips through `RecordAnalysis` / `Analysis`. Old fixture cache (no field) returns `isDocs=true`.
- `cli/analyze_test.go` — mixed-classification fixture: docs feature map contains only docs-page features; screenshot input list excludes not-docs URLs; all-not-docs fixture exits non-zero with the documented warning.

### Integration test

One new `cmd/find-the-gaps/testdata/*.txtar` scenario. Stub LLM responses fix three pages: two docs, one not-docs (`/blog/our-team`). Assert:

- `mapping.md` reflects docs pages only.
- `gaps.md` does not flag drift against the team page.
- `screenshots.md` does not contain entries for the team page.
- Stdout contains `classified: 2 docs, 1 non-docs`.

### Verification plan addition (`.plans/VERIFICATION_PLAN.md`)

New scenario against a real docs site with a known blog section. Run before and after the change. Confirm:

- Drift findings against blog URLs disappear in the after-run.
- At least one previously-noisy finding is gone.
- A previously-correct docs page is still classified as docs.

## Decisions captured

| Question | Choice | Reasoning |
|---|---|---|
| Motivation | Correctness, not efficiency | Wrong answers, not slow ones. |
| FN vs FP | FN worse than FP | Silent drops are invisible; noisy findings are auditable. |
| Taxonomy | Binary (`is_docs`) | Simpler; multi-class deferred. |
| Signal | Piggyback on `AnalyzePage` | Zero marginal LLM cost; correctness lives where content lives. |
| Screenshot strictness | Same rule as drift | Trust the screenshot prompt's existing conservatism. |
| Overrides | None in v1 | YAGNI; `--no-cache` is the escape hatch. |
| Audit visibility | Console + verbose only | Transient; no new file or report section. |
