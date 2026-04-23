# LLM Call Site Audit: Find the Gaps

This report inventories every LLM invocation in the "Find the Gaps" codebase, a CLI tool that analyzes codebases and documentation sites to identify gaps and drift.

## Call Site 1: Extract Features from Code

**Location:** `internal/analyzer/code_features.go:57`

**Function/Context:** `ExtractFeaturesFromCode()` — called from the main `analyze` command to identify product features from the codebase's exported symbols.

**Purpose:** Analyzes batches of exported symbols from the scanned codebase and identifies product features they implement. Returns a deduplicated, sorted list of features with descriptions and metadata (layer, user-facing flag).

**The Prompt:**
```
// PROMPT: Identifies product features implemented by a portion of the codebase.
// Returns a JSON array of objects with name, description, layer, and user_facing fields.

You are analyzing a codebase to identify the product features it implements.

Code files and their exported symbols (format: "file/path: Symbol1, Symbol2"):
<symbol lines>

Return a JSON array of product features. Each element must have:
- "name": short noun phrase (max 8 words) naming the feature
- "description": 1-2 sentences describing what the feature does and its role in the product
- "layer": a short label for which part of the system owns this (e.g. "cli", "analysis engine", "caching", "reporting") — choose freely based on the code
- "user_facing": true if an end user interacts with this directly, false if it is internal plumbing

Deduplicate and sort by name alphabetically.
Respond with only the JSON array. No markdown code fences. No prose.
```

**Model Currently Used:**
- **Default:** `claude-sonnet-4-6` (Anthropic)
- **Selection:** Via CLI flag `--llm-model` or environment variable selection (see `internal/cli/llm_client.go:64-67`)
- **Provider routing:** Controlled by `--llm-provider` flag (default: `anthropic`); also supports `openai`, `ollama`, `lmstudio`, `openai-compatible`

**Tools/Features Used:**
- Simple completion API (`Complete()`)
- Structured JSON output parsing
- No tool use, no streaming, no agentic loops

**Estimated Call Frequency:**
- **One per batched group of symbols** (scales with codebase size)
- Batches are token-budget constrained (80,000 token budget via `MapperTokenBudget`)
- Formula: `N = ceil(total_symbol_tokens / (80000 - preamble_tokens))`
- For a 10,000-symbol codebase (~30k tokens), expect ~1-2 calls
- For a 100,000+ symbol codebase, could be 5-20 calls

**Output Shape:**
- JSON array of `CodeFeature` objects: `[{name, description, layer, user_facing}, ...]`
- Parsed into `[]CodeFeature` slice; features with empty names are filtered out
- Results deduplicated by name (map-based accumulation across batches)

**Size/Difficulty:** **Medium**
- Requires understanding feature patterns across multiple symbols
- Needs judgment about feature names and layer assignments
- Structured output with multiple fields
- **Recommended tier:** Sonnet (current default) or Opus for large, complex codebases

---

## Call Site 2: Analyze Single Documentation Page

**Location:** `internal/analyzer/analyze_page.go:16`

**Function/Context:** `AnalyzePage()` — called from the main `analyze` command for each documentation page to extract summary and feature list.

**Purpose:** Summarizes a single documentation page and extracts the product features or capabilities it describes. Output is cached per page to avoid re-analysis.

**The Prompt:**
```
// PROMPT: Summarizes a single documentation page and extracts the product
// features or capabilities described on it. Responds with JSON only.

You are analyzing a documentation page for a software product.

URL: <page_url>

Content:
<page_content>

Return a JSON object with exactly these fields:
- "summary": a 1-2 sentence description of what this page covers
- "features": a list of product features or capabilities described on this page
  (short noun phrases, max 8 words each)

Respond with only the JSON object. No markdown code fences. No prose.
```

**Model Currently Used:**
- Same as Call Site 1: `claude-sonnet-4-6` by default
- All pages use the same LLM client instance created at the start of the analyze run

**Tools/Features Used:**
- Simple completion API (`Complete()`)
- Structured JSON output (2 fields: summary string + features array)
- No tool use, no streaming, no agentic loops

**Estimated Call Frequency:**
- **One per documentation page** (scales with documentation size)
- Each page is analyzed independently unless cached (`--no-cache` disables caching)
- For a typical project: 10-50 pages → 10-50 calls
- For large projects: 100-500+ pages → 100-500+ calls
- Cache hits are recorded; fresh analyses only happen if page content changes or cache is cleared

**Output Shape:**
- JSON object: `{summary: string, features: [string, ...]}`
- Parsed into `PageAnalysis{URL, Summary, Features}` struct
- Features list is normalized to empty array if null
- Accumulates across all pages for product synthesis

**Size/Difficulty:** **Small/Medium**
- Simple text summarization (1-2 sentences)
- Feature extraction from natural language (no code)
- Low context requirement — only one page at a time
- **Recommended tier:** Haiku (cost-effective) or Sonnet (if page content is dense)

---

## Call Site 3: Synthesize Product Summary

**Location:** `internal/analyzer/synthesize.go:24`

**Function/Context:** `SynthesizeProduct()` — called once after all pages are analyzed to produce a product-level description and deduplicated feature list.

**Purpose:** Combines per-page analysis results (summaries + features) into a single product summary and canonical feature list. Results are cached.

**The Prompt:**
```
// PROMPT: Synthesizes a product-level description and a deduplicated feature
// list from all documentation page summaries. Responds with JSON only.

You are analyzing documentation for a software product.

Here are summaries and features extracted from individual documentation pages:

URL: <url1>
Summary: <summary1>
Features: <feature1>, <feature2>, ...

...

Based on the above, return a JSON object with exactly these fields:
- "description": a 2-3 sentence summary of what this product is and what it does
- "features": a deduplicated, sorted list of all product features and
  capabilities (short noun phrases, max 8 words each)

Respond with only the JSON object. No markdown code fences. No prose.
```

**Model Currently Used:**
- Same as Call Sites 1 & 2: `claude-sonnet-4-6` by default

**Tools/Features Used:**
- Simple completion API (`Complete()`)
- Structured JSON output (2 fields)
- No tool use, no streaming, no agentic loops

**Estimated Call Frequency:**
- **Once per analyze run** (one-shot)
- Only triggered if not all pages are cache hits (fresh pages trigger synthesis)
- When all pages are cached, synthesis result is reused from cache
- Typical: 1 call per full analysis, 0 calls if all pages cached

**Output Shape:**
- JSON object: `{description: string, features: [string, ...]}`
- Parsed into `ProductSummary{Description, Features}` struct
- Features are sorted and deduplicated by the LLM
- Cached in the project's index for future runs

**Size/Difficulty:** **Small**
- Takes pre-summarized input (page summaries already written)
- Task is rollup: deduplication and 2-3 sentence product description
- Limited token budget (only page summaries as input, not full content)
- **Recommended tier:** Haiku (ideal for this rollup task)

---

## Call Site 4: Map Features to Code (Symbol-Level)

**Location:** `internal/analyzer/mapper.go:109`

**Function/Context:** `MapFeaturesToCode()` — called from main analyze to map product features to code files and exported symbols.

**Purpose:** Identifies which code files and exported symbols implement each product feature. Results are cached; called in parallel with docs mapping.

**The Prompt:**
```
// PROMPT: Maps product features to the code files and symbols most likely
// to implement them. Returns a JSON array only.

You are mapping product features to their code implementations.

Product features:
["feature1", "feature2", "feature3", ...]

Code symbols (format: "file/path: Symbol1, Symbol2"):
internal/auth/handler.go: Authenticate, Login
internal/upload/upload.go: Upload, Download
...

For each feature, identify which code files and exported symbols are most
relevant to implementing it.
Return a JSON array where each element has:
- "feature": the feature name exactly as provided
- "files": list of relevant file paths (empty array if none)
- "symbols": list of relevant exported symbol names (empty array if none)

Respond with only the JSON array. No markdown code fences. No prose.
```

**Model Currently Used:**
- `claude-sonnet-4-6` by default

**Tools/Features Used:**
- Simple completion API (`Complete()`)
- Structured JSON output (3 fields per entry: feature name + files array + symbols array)
- No tool use, no streaming, but **includes adaptive batching + splitting**
- `MapProgressFunc` callback allows incremental cache updates after each batch
- Provider-exact token counting (calls Anthropic's `count_tokens` API for validation)

**Estimated Call Frequency:**
- **Multiple batches** (scales with codebase and budget constraints)
- **Batching strategy:**
  - Initial batches created via `batchSymLines()` (local tiktoken estimate)
  - Each batch validated with provider's exact token counter
  - If batch exceeds budget, split in half and revalidated (recursive)
  - Budget: `MapperTokenBudget = 80,000` tokens
- For typical codebases: 2-5 calls
- For large codebases (100k+ symbols): 10-20+ calls

**Output Shape:**
- JSON array of mapping entries: `[{feature: string, files: [string], symbols: [string]}, ...]`
- Parsed into `FeatureMap` (slice of `FeatureEntry` structs)
- Accumulation across batches: maps are merged per-feature (sets deduplicate)
- Cached to disk after each batch completes
- `--no-symbols` flag disables symbol analysis (files-only mode)

**Size/Difficulty:** **Large/Complex**
- Requires matching semantic feature names to code structure
- Multi-symbol context (up to 80k tokens of code index per call)
- Returns multiple structured fields per feature
- **Recommended tier:** Opus (frontier reasoning needed for code structure understanding)

---

## Call Site 5: Map Features to Docs

**Location:** `internal/analyzer/docs_mapper.go:53`

**Function/Context:** `mapPageToFeatures()` (called by `MapFeaturesToDocs()`) — maps product features to documentation pages that cover them.

**Purpose:** For each documentation page, identifies which product features from a canonical list are covered. Executed in parallel (concurrent goroutines with semaphore).

**The Prompt:**
```
// PROMPT: Maps a single documentation page to the canonical product features
// it covers. Returns a JSON array of matching feature strings only.

You are analyzing a documentation page to identify which product features it covers.

Product features:
["feature1", "feature2", "feature3", ...]

Documentation page URL: <page_url>

Documentation page content:
<page_content>

Return a JSON array of feature strings (exact matches from the list above)
that this page covers.
Only include features that are clearly addressed on this page.
Respond with only the JSON array. No markdown code fences. No prose.
```

**Model Currently Used:**
- `claude-sonnet-4-6` by default

**Tools/Features Used:**
- Simple completion API (`Complete()`)
- Structured JSON output (array of strings only — minimal output)
- No tool use, no streaming
- **Concurrent execution:** spawned via goroutines with semaphore-based rate limiting (default: 5 workers)
- Token truncation: page content is truncated if it exceeds available budget (`DocsMapperPageBudget = 40,000` tokens)

**Estimated Call Frequency:**
- **One per documentation page** (concurrent)
- Runs in parallel: `min(len(pages), workers)` calls in flight at once
- Total: N calls where N = number of documentation pages
- For a typical project: 10-50 pages → 10-50 calls
- Timing: if 50 pages with 5 workers, ~10 serial rounds of 5 parallel calls each

**Output Shape:**
- JSON array of matching feature strings: `["feature1", "feature3", ...]`
- Parsed into `[]string` slice
- Accumulated via `DocsMapProgressFunc` callback (called after each page completes)
- Converted to `DocsFeatureMap` (slice of `DocsFeatureEntry{Feature: string, Pages: []string}`)

**Size/Difficulty:** **Small**
- Simple feature matching (does this page discuss feature X? yes/no)
- Output is just a short string array
- Content is truncated if too long (budget: 40k tokens)
- Limited reasoning needed — mostly keyword matching and coverage detection
- **Recommended tier:** Haiku (ideal for this matching task)

---

## Call Site 6: Detect Drift (Agentic Multi-Turn)

**Location:** `internal/analyzer/drift.go:102`

**Function/Context:** `detectDriftForFeature()` (called by `DetectDrift()`) — agentic drift detection using tool use.

**Purpose:** For each documented feature, investigates code and documentation to identify specific inaccuracies (e.g., outdated parameters, removed features, missing information). Uses a multi-turn agentic loop with tool calls to read source files and pages.

**The Prompt (System Message for Tool-Use Conversation):**
```
// PROMPT: Reviews documentation accuracy for one feature using tool calls to
// read source files and cached doc pages. Returns a JSON array of specific
// inaccuracies expressed as documentation feedback.

You are reviewing documentation accuracy for a software feature.

Feature: <feature_name>
Code description: <feature_description>
Implemented in: <file1>, <file2>, ...
Symbols: <symbol1>, <symbol2>, ...

Documentation pages:
- <page_url_1>
- <page_url_2>
...

You have tools available to read source files and documentation pages in full.
Use them to investigate as needed before producing your findings.

Identify specific inaccuracies, missing information, or outdated content in the
documentation relative to what the code actually does. This includes:
- Features or behaviors documented but no longer present in code
- Parameters, fields, or requirements not mentioned in docs
- Incorrect descriptions of how something works
- Any other misleading or stale content

Do NOT flag entire features as undocumented — only report inaccuracies or gaps
within documentation that already exists for this feature.

Express each finding as documentation feedback — describe what is wrong or
missing in the docs, not what the code does. One finding per specific issue.

When you are done investigating, return a JSON array of objects:
[{"page": "<url or empty string>", "issue": "<one or two sentences>"}]

If no issues are found, return [].
Respond with only the JSON array. No markdown code fences. No prose.
```

**Model Currently Used:**
- `claude-sonnet-4-6` by default
- **Constraint:** Only Anthropic and OpenAI providers fully support tool use; local providers (ollama, lmstudio) have degraded quality (tool calls are ignored)

**Tools/Features Used:**
- **Tool-use conversation** (`CompleteWithTools()` API)
- **Two tools available:**
  - `read_file`: reads source files (with repo-root boundary checks)
  - `read_page`: reads cached doc pages
- **Agentic loop:** up to `driftMaxRounds = 20` iterations
  - Each round: LLM responds with tool calls or final JSON
  - If tool calls present, they are executed and results fed back
  - Loop terminates when LLM returns non-tool-call response or 20 rounds exhausted
- No streaming, no thinking mode

**Estimated Call Frequency:**
- **One per documented feature that has code files** (scales with feature count)
- **Within each feature:**
  - 1 initial `CompleteWithTools()` call
  - 0-20 subsequent calls (one per round of tool invocations)
  - Typical: 2-4 rounds per feature (read file, read page, synthesize)
  - Total LLM calls for drift: `(num_features_with_docs * avg_rounds)` = 10-50 calls for typical project
- Filtering: pages matching release-note/changelog patterns are excluded
- Classification: non-release-note pages are pre-validated via `isReleaseNotePage()` (additional `Complete()` calls)

**Output Shape:**
- JSON array of drift issues: `[{page: string, issue: string}, ...]`
- Parsed into `[]DriftIssue` slice
- Accumulated into `[]DriftFinding{Feature: string, Issues: []DriftIssue}`
- Results written to `gaps.md` report

**Size/Difficulty:** **Large/Complex**
- Multi-turn agentic reasoning with tool use
- Requires reading and comparing code vs documentation
- Judgment calls: is the docs out of date or accurate?
- Up to 20-round feedback loops (expensive if all rounds used)
- **Recommended tier:** Opus (frontier model needed for agentic drift detection; Sonnet acceptable but lower quality)

---

## Call Site 7: Classify Release-Note Pages

**Location:** `internal/analyzer/drift.go:320`

**Function/Context:** `isReleaseNotePage()` — called by `classifyDriftPages()` to filter out release notes and changelog pages before drift detection.

**Purpose:** Determines whether a page is release notes/changelog or current feature documentation. Pages classified as release notes are excluded from drift detection.

**The Prompt:**
```
// PROMPT: Classifies whether a documentation page contains release notes,
// a changelog, or version history rather than current feature documentation.

Does this page contain release notes, a changelog, or version history?
Answer only "yes" or "no".

URL: <page_url>

Content preview:
<first_1000_chars_of_content>
```

**Model Currently Used:**
- `claude-sonnet-4-6` by default

**Tools/Features Used:**
- Simple completion API (`Complete()`)
- Yes/no classification (extremely lightweight)
- Content is truncated to 1000 characters for efficiency

**Estimated Call Frequency:**
- **One per documentation page that might be release notes**
- Called only if URL doesn't match release-note patterns (fails open)
- Typical: 5-20 calls per analyze run (depends on doc site structure)
- If the content read fails, page is included anyway (fail-open design)

**Output Shape:**
- String response (checked for "yes" substring, case-insensitive)
- Boolean result: `true` if release notes, `false` if feature documentation
- No structured output, just a binary decision

**Size/Difficulty:** **Small**
- Simple yes/no classification
- Content truncated to 1000 chars
- Low-token prompt
- **Recommended tier:** Haiku (ideal for binary classification)

---

## Summary Table

Frequency columns assume a cold run (no cache). Scaling units:
- **Small repo** ≈ 10 doc pages, ~10k symbols, ~3 features with docs
- **Typical repo** ≈ 30 doc pages, ~30k symbols, ~5 features with docs
- **Large repo** ≈ 200 doc pages, ~100k symbols, ~20 features with docs

| # | Prompt | Location | What it does | Scales with | Per-unit calls | Small | Typical | Large | Tier |
|---|--------|----------|--------------|-------------|---------------|-------|---------|-------|------|
| 1 | Extract Features from Code | `code_features.go:57` | Given batches of exported symbols, names product features with description/layer/user-facing flag | Symbol tokens ÷ 80k budget | 1 per batch | 1 | 1-2 | 5-20 | Sonnet (keep) |
| 2 | Analyze Single Doc Page | `analyze_page.go:16` | Summarizes a single doc page in 1-2 sentences and extracts the features it mentions | Doc pages (fresh only) | 1 per page | ~10 | ~30 | ~200 | **Haiku** |
| 3 | Synthesize Product Summary | `synthesize.go:24` | Rolls up per-page summaries into a 2-3 sentence product description and canonical feature list | Runs | 1 per run | 1 | 1 | 1 | **Haiku** |
| 4 | Map Features to Code | `mapper.go:93` / `:109` | Matches each product feature to the files and exported symbols that implement it. `mapper.go:93` is the `--no-symbols` files-only variant; `:109` is the default files+symbols variant. Same call site, one prompt string chosen at runtime. | Symbol tokens ÷ 80k budget | 1 per batch (plus splits) | 1-2 | 2-5 | 10-20 |  **Opus** |
| 5 | Map Features to Docs | `docs_mapper.go:53` | For one doc page, returns which canonical features it covers (runs concurrently across pages) | Doc pages | 1 per page | ~10 | ~30 | ~200 | **Haiku** |
| 6 | Detect Drift (agentic) | `drift.go:102` | Multi-turn tool-use loop: reads code + doc pages, returns specific inaccuracies/stale content per feature | Documented features × 2-20 rounds | ~2-4 rounds per feature (up to 20) | 6-12 | 10-20 | 40-80 | **Opus** |
| 7 | Classify Release-Note Page | `drift.go:320` | Binary yes/no: is this page a changelog/release-notes page? (filter before drift) | Doc pages not pre-matched by URL | 1 per candidate page | 3-8 | 5-20 | 30-100 | **Haiku** |

**Total LLM calls on a cold typical run:** roughly **80-100**. Breakdown:
- Sonnet (current default): 1-2 (prompt 1)
- Haiku-eligible (prompts 2, 3, 5, 7): ~60-80
- Opus-eligible (prompts 4, 6): ~15-25

Cache hits collapse prompts 2, 3, 4, and 5 to near-zero on repeat runs over the same repo/docs.

---

## Model Configuration & Routing

**Current State:**
- **All call sites use the same hardcoded default model:** `claude-sonnet-4-6` (Anthropic)
- Model is resolved once at CLI startup in `newLLMClient()` (`internal/cli/llm_client.go:20-72`)
- Model can be overridden via `--llm-model` flag, but applies globally to all 7 call sites
- **No per-call-site routing exists**

**Provider Selection:**
- Default provider: Anthropic
- OpenAI support: uses Bifrost SDK for chat completions
- Ollama/LM Studio: OpenAI-compatible client (tool use not fully supported)
- Token counting: uses Anthropic API for Anthropic models, local tiktoken for OpenAI/Ollama

---

## Optimization Opportunities

### Candidates for Haiku (cheaper, faster)

- **Call Site 2: Analyze Single Page** — simple summarization, high frequency
- **Call Site 3: Synthesize Product** — rollup from pre-summarized input
- **Call Site 5: Map Features to Docs** — feature matching, highest frequency
- **Call Site 7: Classify Release Notes** — binary yes/no classification

### Must Stay on Frontier (Opus preferred)

- **Call Site 4: Map Features to Code** — semantic code-to-feature matching drives downstream quality
- **Call Site 6: Detect Drift** — multi-turn agentic reasoning + tool use; judgment-heavy

### Middle Tier (Sonnet)

- **Call Site 1: Extract Features from Code** — current Sonnet default is reasonable; Opus only for complex codebases

### Estimated Cost Impact

For a typical run with 30 pages and 5 features-with-docs:

| Tier | Before (all Sonnet) | After |
|------|---------------------|-------|
| Haiku | 0 | ~71 calls (pages × 2 + synth + release notes) |
| Sonnet | ~85 calls | ~2 calls (feature extraction) |
| Opus | 0 | ~12 calls (mapping batches + drift) |

Rough overall cost reduction: **30-50%** depending on repo size, with improved drift quality from Opus promotion.

---

## Centralization Recommendation

### Current State: Scattered

Every LLM call shares one `LLMClient`:
1. **No per-call-site routing** — all 7 sites receive the same instance
2. **No feature-level configuration** — cannot mix Haiku/Opus in one run
3. **Bifrost client holds both provider and model** — cannot swap models mid-run without a new client

### Recommended: Tiered Client Factory

Add a lightweight tiering layer in `internal/cli/llm_client.go`:

```go
type LLMTiering struct {
    Small  LLMClient // Haiku
    Medium LLMClient // Sonnet (current default)
    Large  LLMClient // Opus
}
```

Pass `LLMTiering` (or a task-keyed lookup function) into analyzer functions. Each call site then picks the tier that matches its reasoning demand.

**CLI flags:**
- `--llm-small-model` (default: `claude-haiku-4-5`)
- `--llm-model` (default: `claude-sonnet-4-6`) — existing
- `--llm-large-model` (default: `claude-opus-4-7`)

**Backward compatible:** unset large/small → fall back to `--llm-model` so behavior doesn't change unless opted in.

**Estimated implementation effort:** 4-6 hours (3 clients, wire through 7 call sites, add flags, update tests).

---

## Additional Notes

1. **Caching is extensive** — many results are cached, so repeat runs on the same repo are far cheaper than cold runs. The audit assumes cold analysis.

2. **Token counting overhead** — every batch in Call Site 4 triggers a provider-exact token count call (Anthropic API). Adds latency but keeps batches under budget.

3. **Agentic loop risk in Call Site 6** — the 20-round limit is a safety valve; well-written prompts should finish in 2-4 rounds. Watch for reports of 20-round timeouts as a prompt-quality signal.

4. **Provider-specific notes:**
   - Ollama / LM Studio: tool use not supported; drift detection degrades
   - OpenAI: supported via Bifrost SDK
   - Anthropic: full support; accurate token counting

5. **Prompt injection surface:** code and documentation content are directly inserted into prompts without escaping. Worth a separate security-review pass.
