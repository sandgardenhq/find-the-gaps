package analyzer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/sandgardenhq/find-the-gaps/internal/chunker"
)

// budgetForPageAnalysis returns the per-call content budget for the
// analyze-page prompt, by tier. Conservative numbers chosen so the
// prompt template, JSON-schema overhead, and a generous response cap
// all fit comfortably alongside the content blob. When the content
// exceeds this budget, AnalyzePage splits it via chunker.Chunk and
// merges the per-chunk results.
func budgetForPageAnalysis(tier Tier) int {
	switch tier {
	case TierSmall:
		return 30_000
	case TierTypical:
		return 60_000
	default:
		return 100_000
	}
}

type analyzePageResponse struct {
	Summary  string   `json:"summary"`
	Features []string `json:"features"`
	// Pointer so we can detect "missing from response" and apply the
	// inclusive-by-default rule (treat as docs) instead of silently
	// dropping the page as not-docs. False negatives are worse than
	// false positives — see .plans/DOCS_CLASSIFIER_DESIGN.md.
	IsDocs *bool `json:"is_docs"`
	// Pointer so we can detect "missing from response" and apply the
	// inclusive-by-default rule (treat as "other") instead of erroring.
	Role *string `json:"role"`
}

// PROMPT SCHEMA: output shape for AnalyzePage.
var analyzePageSchema = JSONSchema{
	Name: "analyze_page_response",
	Doc: json.RawMessage(`{
      "type": "object",
      "properties": {
        "summary":  {"type": "string"},
        "features": {"type": "array", "items": {"type": "string"}},
        "is_docs":  {"type": "boolean"},
        "role": {
          "type": "string",
          "enum": ["landing","quickstart","tutorial","how-to","concept","reference","changelog","faq","other"]
        }
      },
      "required": ["summary", "features", "is_docs", "role"],
      "additionalProperties": false
    }`),
}

// AnalyzePage sends doc page content to the LLM and returns a summary,
// feature list, and a binary classification of whether the page is
// product documentation.
//
// For pages whose content exceeds the small-tier per-call budget, the
// content is preemptively split via chunker.Chunk and each chunk is
// analyzed independently; the resulting feature lists are merged by
// lowercase name (first occurrence wins; see mergePageAnalysis).
func AnalyzePage(ctx context.Context, tiering LLMTiering, pageURL, content string) (PageAnalysis, error) {
	client := tiering.Small()
	budget := budgetForPageAnalysis(TierSmall)
	if chunker.EstimateTokens(content) <= budget {
		return runAnalyzePageOnce(ctx, client, pageURL, content)
	}
	chunks := chunker.Chunk(content, budget)
	if len(chunks) <= 1 {
		return runAnalyzePageOnce(ctx, client, pageURL, content)
	}
	var merged PageAnalysis
	for i, c := range chunks {
		part, err := runAnalyzePageOnce(ctx, client, pageURL, c)
		if err != nil {
			return PageAnalysis{}, fmt.Errorf("AnalyzePage %s: chunk %d/%d: %w",
				pageURL, i+1, len(chunks), err)
		}
		// A per-chunk skip (zero-value PageAnalysis, nil error) from
		// the ErrTokenBudgetExceeded backstop should not poison the
		// merge — merge it as a no-op and continue.
		merged = mergePageAnalysis(merged, part)
	}
	merged.URL = pageURL
	log.Debugf("page chunked: url=%s chunks=%d features_after_merge=%d",
		pageURL, len(chunks), len(merged.Features))
	return merged, nil
}

// runAnalyzePageOnce runs the analyze-page prompt against a single
// content payload. Callers that need preemptive chunking (oversize
// pages) drive this function once per chunk and merge results via
// mergePageAnalysis. The ErrTokenBudgetExceeded backstop here should
// be unreachable in practice once chunking is in place; it is kept as
// defense-in-depth so an estimator-vs-tokenizer drift surfaces as a
// loud warning instead of a hard abort of the whole analyze run.
func runAnalyzePageOnce(ctx context.Context, client LLMClient, pageURL, content string) (PageAnalysis, error) {
	// PROMPT: Summarizes a single documentation page, extracts the product features described on it, and classifies whether the page is product documentation. Inclusive-by-default: when uncertain, classify as docs (false negatives are worse than false positives).
	prompt := fmt.Sprintf(`You are analyzing a page on a software product's website.

URL: %s

Content:
%s

Populate the response with:
- "summary": a 1-2 sentence description of what this page covers
- "features": a list of product features or capabilities described on this page (short noun phrases, max 8 words each). May be empty.
- "is_docs": a boolean classifying whether this page is product DOCUMENTATION.
- "role": the kind of page this is — one of "landing", "quickstart", "tutorial", "how-to", "concept", "reference", "changelog", "faq", "other". Judge from the content; use the URL only as a tiebreaker.

Rule for is_docs:
A page is DOCS if a user trying to USE this product would consult it for current technical information about features, APIs, configuration, or behavior. Marketing pages and blog posts are NEVER docs, even when they contain code snippets, release announcements, or technical claims — docs is the canonical reference surface, not promotional or editorial content.

Role definitions:
- "landing": the docs-site home or a section index page; primarily navigation and links, not explanatory prose.
- "quickstart": a first-time-user install + first command/run page; the reader's goal is "get something working in N minutes".
- "tutorial": a walked-through, end-to-end guided learning of a single task. Interleaves explanation with steps and explains *why*, not just *how*. Reader is following along to learn.
- "how-to": recipe-style guide (numbered steps, minimal explanation). Reader's goal is to accomplish one task; conceptual context is minimal or omitted.
- "concept": background, architecture, design rationale, or model explanation; light on procedure.
- "reference": exhaustive API / CLI / config / option listing; not a guide.
- "changelog": release notes, version history, or "what's new".
- "faq": Q&A format or a troubleshooting list.
- "other": anything else, including non-docs pages (marketing, blog, team, careers, legal). Pages with is_docs=false should typically be "other".

Examples of docs (is_docs=true):
- API references, tutorials, quickstarts, configuration references
- Changelogs and release notes (when published as a dedicated changelog/release-notes page, not as a blog post)

Examples of NOT docs (is_docs=false):
- Marketing pages (landing pages, product/feature pages, "why choose us", pricing, comparison pages) — even if they include code snippets or technical claims
- Blog posts of any kind, including release/launch announcements, feature-announcement posts, deep-dives, engineering retrospectives ("how we built X", "scaling our database"), and generic company posts (hiring, fundraising, holidays)
- Customer case studies / customer logos
- Team, about, careers, legal pages

Treat any URL under a /blog/ path (or equivalent: /news/, /posts/, /updates/) as a blog post. Treat the site's home page and top-level product pages as marketing.

Set is_docs=false when the page is one of the not-docs categories above. Default to docs when unsure about a technical-looking page that is NOT clearly a marketing page or blog post.`, pageURL, content)

	raw, err := client.CompleteJSON(ctx, prompt, analyzePageSchema)
	if err != nil {
		// Oversize page hit the per-model budget gate. Log + skip the
		// page so the rest of the run continues. The caller sees a
		// zero-value PageAnalysis with no error; the page contributes
		// nothing this run, and a re-run with --llm-small=<bigger-model>
		// or a smaller page reaches the analyzer.
		if errors.Is(err, ErrTokenBudgetExceeded{}) {
			log.Warnf("AnalyzePage: skipping %s: %v", pageURL, err)
			return PageAnalysis{}, nil
		}
		return PageAnalysis{}, fmt.Errorf("AnalyzePage %s: %w", pageURL, err)
	}

	var resp analyzePageResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return PageAnalysis{}, fmt.Errorf("AnalyzePage %s: invalid JSON response: %w", pageURL, err)
	}

	if resp.Features == nil {
		resp.Features = []string{}
	}

	// Inclusive-by-default: a missing is_docs field (e.g. an old cached
	// response or a malformed-but-still-parseable LLM reply) must NOT
	// silently drop the page as not-docs. The hard-floor guard added
	// later in the pipeline catches the all-non-docs failure mode.
	isDocs := true
	if resp.IsDocs != nil {
		isDocs = *resp.IsDocs
	}

	// Inclusive-by-default: a missing role (e.g. an old cached response
	// or a token-budget skip) resolves to "other" so downstream consumers
	// can treat it uniformly with explicitly low-prominence pages.
	role := "other"
	if resp.Role != nil {
		role = *resp.Role
	}

	return PageAnalysis{
		URL:      pageURL,
		Summary:  resp.Summary,
		Features: resp.Features,
		IsDocs:   isDocs,
		Role:     role,
	}, nil
}

// mergePageAnalysis combines two PageAnalysis results from per-chunk
// runs of the analyze-page prompt.
//
//   - URL:      first non-empty wins (the caller usually overwrites
//     this with the page URL anyway).
//   - Summary:  first non-empty wins (chunks share a page-level
//     summary so we prefer the earliest non-empty one).
//   - IsDocs:   OR — any chunk classifying the page as docs makes
//     the merged page docs. Inclusive-by-default: a false
//     negative is worse than a false positive.
//   - Role:     first non-empty/non-"other" wins; falls back to "other".
//   - Features: deduped by lowercase name; first occurrence wins for
//     the surface form (the feature list is []string, so there
//     is no "longest description" to compare).
func mergePageAnalysis(a, b PageAnalysis) PageAnalysis {
	out := a
	if out.URL == "" {
		out.URL = b.URL
	}
	if out.Summary == "" {
		out.Summary = b.Summary
	}
	out.IsDocs = a.IsDocs || b.IsDocs
	// Role: prefer the first specific (non-empty, non-"other") role we encounter.
	// If no chunk has a specific role, fall through with whatever was first set
	// (including "other"), since that's the inclusive-by-default behavior.
	if (out.Role == "" || out.Role == "other") && b.Role != "" && b.Role != "other" {
		out.Role = b.Role
	} else if out.Role == "" {
		out.Role = b.Role
	}
	seen := make(map[string]struct{}, len(out.Features)+len(b.Features))
	for _, f := range out.Features {
		seen[strings.ToLower(f)] = struct{}{}
	}
	for _, f := range b.Features {
		key := strings.ToLower(f)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out.Features = append(out.Features, f)
	}
	if out.Features == nil {
		out.Features = []string{}
	}
	return out
}
