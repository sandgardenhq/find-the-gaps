# Context Window Overflow Audit

Audit of every LLM call site in the codebase where input size could exceed a model's context window. Severities reflect the likelihood of overflow in real-world repos/docs sites, not just theoretical unboundedness.

## HIGH severity

- **`internal/analyze/analyze_page.go:50`** — Per-page feature-extraction prompt embeds the entire doc page markdown raw. No pre-call cap; `ErrTokenBudgetExceeded` is caught after the fact and the page is silently skipped.
- **`internal/drift/drift.go:423`** — Drift investigator system prompt concatenates full feature description + every symbol + every page URL. Initial turn can exceed budget before the agent loop starts. No per-feature pre-estimate.
- **`internal/drift/drift.go:654`** — Judge receives all investigator observations in one prompt. Compaction (`chunkObservationsToFit`) exists, but only triggers after a failed call; no upfront sizing.
- **`internal/screenshot/screenshot_gaps.go:731` and `:853`** — Detection prompt embeds full page content + all extracted images + all code blocks + priority rubric. `fitContentToBudget()` truncates only when overhead doesn't fit; large reference pages (30K+) are at risk.
- **`internal/analyze/synthesize.go:31`** — `SynthesizeProduct` concatenates every page's summary + features into a single prompt with no batching and no per-page cap. Unbounded in the number of docs pages.

## MEDIUM severity

- **`internal/analyze/mapper.go:77`** — Symbol-index batcher caps batches at 80K tokens, but a single oversized symbol line can't be split further — split-and-retry can spin on the same batch.
- **`internal/analyze/docs_mapper.go:48`** — Page truncated to fit `DocsMapperPageBudget` using a tiktoken estimate; estimator can undercount dense code blocks, so the trimmed payload may still overflow.
- **`internal/analyze/code_features.go:50`** — `ExtractFeaturesFromCode` inherits the same batcher as `mapper.go` and the same oversized-line failure mode.
- **`internal/drift/drift.go:867`** — `isReleaseNotePage` truncates only the classifier preview (1000 chars). The investigator's `read_page` tool returns full content, so the observation buffer can balloon downstream.

## LOW severity

- **`internal/screenshot/screenshot_gaps.go:1159`** — `buildRelevancePrompt` batches ≤5 images per call (Groq cap). Bounded by structure, but worth confirming the split happens at the right layer.
- **`internal/drift/drift.go:94`** — `budgetForFeature` clamps rounds at 100; for features spanning 50+ files / 20+ pages, that ceiling is still loose.

## Patterns worth noting

- The codebase consistently relies on **post-failure detection** (`ErrTokenBudgetExceeded` + skip, or judge-compaction retry) rather than **pre-call sizing**. The HIGH-severity sites all share this shape.
- `synthesize.go` is the only call site that is unbounded in *count of items* rather than *size of a single item* — it's structurally different from the others and likely needs batching, not just truncation.
- The Groq 5-image cap in `screenshot_gaps.go:1159` is hard-coded; if `--llm-small` resolves to a different vision provider with a higher limit, throughput is left on the table but it's not an overflow risk.
