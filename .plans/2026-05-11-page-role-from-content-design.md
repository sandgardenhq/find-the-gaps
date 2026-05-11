# Page Role From Content — Design

## Problem

`pageRole(url)` in `internal/analyzer/page_role.go` classifies docs pages from
URL string-matching alone. The taxonomy mixes purpose (`readme`, `quickstart`,
`reference`) with structural prominence (`top-nav`, `deep`) and a fallback
(`unknown`). The keyword list is small and brittle: a page titled "Getting
Started" at `/docs/intro/` is classified as `top-nav` or `reference`, not
`quickstart`. URL is a useful supporting signal but does not actually determine
what a page is — content does.

The role label feeds three priority-rating prompts (drift adjudication and two
screenshot prompts) as a prominence hint. Bad role classification skews
priorities.

## Goal

Classify page role from page content, with URL as a tiebreaker at most.
Replace the URL-only `pageRole(url)` function and its taxonomy.

## Approach

Piggyback on the per-page LLM pass that already exists (`AnalyzePage` in
`internal/analyzer/analyze_page.go`). The pass already reads full page content
and returns a structured `PageAnalysis` cached per page. Adding a `role` field
to that response is near-zero marginal cost: a handful of output tokens, no
new LLM call, no new cache layer.

## Taxonomy

Purpose-only labels, drop the structural-prominence axis:

```
landing | quickstart | tutorial | how-to | concept | reference | changelog | faq | other
```

- `landing` — docs-site home or top-level overview (folds in today's `readme`)
- `quickstart` — first-time-user install + first command/run
- `tutorial` — walked-through guided learning of a single task end-to-end
- `how-to` — focused recipe for one task on an existing setup
- `concept` — background, architecture, design rationale; light on procedure
- `reference` — exhaustive API/CLI/config listing
- `changelog` — release notes or version history
- `faq` — Q&A or troubleshooting list
- `other` — anything else (typically `is_docs=false` pages)

Prominence (`top-nav` / `deep`) is dropped. The priority rubric already judges
prominence from other context; a separate field would duplicate that.

## Data Model

`analyzePageResponse` gains `Role *string` (pointer for the missing-field
migration trick). `PageAnalysis` gains `Role string`. The JSON schema gains:

```json
"role": {
  "type": "string",
  "enum": ["landing","quickstart","tutorial","how-to","concept",
           "reference","changelog","faq","other"]
}
```

Marked required alongside `summary`/`features`/`is_docs`. Missing `role` in a
parsed response (old cache, malformed reply) defaults to `"other"` — same
inclusive-by-default pattern used by `is_docs`.

## Prompt Change

One new bullet in `AnalyzePage`'s prompt, with inline label definitions.
Judge from content, use URL only as a tiebreaker. URL is already in the
prompt today (`URL: %s`); no plumbing change for that signal.

## Downstream Plumbing

Three current call sites of `pageRole(url)` move to reading `Role` from the
cached `PageAnalysis`:

1. `drift.go:559–565` — the "Page role hints" block builder.
2. `screenshot_gaps.go:744` — `buildScreenshotPrompt`.
3. `screenshot_gaps.go:877` — `buildDetectionPromptWithVerdicts`.
4. `screenshot_gaps.go:1154` — image-issue review prompt.

To avoid threading the analyses map through several signatures, introduce a
per-run resolver:

```go
type roleResolver func(pageURL string) string
```

Built once at the start of drift/screenshot phases from the per-page analysis
cache. Returns `"other"` for unknown URLs and empty strings.

The old `pageRole(url)` function and its URL-only tests are deleted. There
is no remaining caller for URL-only classification.

## Cache Behavior

Cache key is content-hashed; adding `role` to the response shape does not
change the key. Existing cached `PageAnalysis` files load with `Role == ""`,
which the resolver normalizes to `"other"`. No forced invalidation. Users on
stable fixtures pay no re-analysis cost.

## Edge Cases

- **Skipped pages** (token budget gate). `AnalyzePage` returns zero-value
  `PageAnalysis` with `Role == ""`. Resolver normalizes to `"other"`. No worse
  than today's `unknown`.
- **Out-of-enum hallucinations.** Structured-output enforcement at the
  provider level should reject. If a provider's enforcement is weak, the
  resolver's `"other"` fallback contains the blast radius.
- **Non-docs pages.** Still get a role (usually `other`). Existing filters
  that exclude non-docs from drift/screenshot are unchanged.

## Tests

- `analyze_page_test.go` — response with `role` parses; missing `role`
  defaults to `"other"`; out-of-enum rejected via schema validator.
- `page_role_test.go` (rewritten) — `roleResolver` returns known role,
  `"other"` for unknown, `"other"` for empty.
- `drift_test.go` — rewritten `buildPageRoleHints` test drives roles
  through the resolver.
- `screenshot_gaps_test.go` — three prompt builders include the resolved
  role; URL-only assumptions removed.

## Verification

No new scenario in `.plans/VERIFICATION_PLAN.md`. Scenarios 2/3/14 already
catch priority regressions; Scenario 14 ("priority calibration") gets a
one-sentence note that roles now come from page-analysis output.

## Implementation Order (TDD)

1. Schema + struct + parsing tests.
2. Prompt update with label definitions.
3. `roleResolver` + tests.
4. Drift hints builder rewrite.
5. Screenshot prompts (three call sites).
6. Delete `pageRole(url)` and its URL-only tests.
7. Run Scenarios 9 + 14 against a real docs site to confirm priority
   bucketing stays sane.

## Out of Scope

- Tuning the priority rubric to lean on `role` (e.g. "landing → bias large").
  Today the rubric is role-agnostic; tune in a follow-up PR after observing
  real outputs.
- Adding a separate prominence score. Revisit only if role alone is
  insufficient signal.
