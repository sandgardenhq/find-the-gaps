package analyzer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/sandgardenhq/find-the-gaps/internal/chunker"
)

// perPageSummaryBudget caps each page's Summary at this many cl100k_base
// tokens before it enters the synthesize prompt. The synthesize step
// concatenates one entry per page, so the runaway dimension is COUNT of
// pages, not size of any single page. Truncating each Summary to a small,
// uniform budget keeps the body roughly linear in page count.
const perPageSummaryBudget = 200

// synthesizeBudget is the small-tier content budget reserved for the
// synthesize prompt body (after subtracting prompt-template overhead).
// Conservative — leaves headroom for the JSON-schema response and any
// chat-template wrapping the provider adds. When the body exceeds this
// budget even after per-page compression, SynthesizeProduct falls back
// to a map-reduce pass.
const synthesizeBudget = 80_000

type synthesizeResponse struct {
	Description string   `json:"description"`
	Features    []string `json:"features"`
}

// PROMPT SCHEMA: output shape for SynthesizeProduct.
var synthesizeSchema = JSONSchema{
	Name: "synthesize_response",
	Doc: json.RawMessage(`{
      "type": "object",
      "properties": {
        "description": {"type": "string"},
        "features":    {"type": "array", "items": {"type": "string"}}
      },
      "required": ["description", "features"],
      "additionalProperties": false
    }`),
}

// PROMPT SCHEMA: output shape for the reduction step of SynthesizeProduct.
// Identical to synthesizeSchema in structure but named distinctly so test
// fakes and request logs can tell the two calls apart.
var synthesizeReduceSchema = JSONSchema{
	Name: "synthesize_reduce_response",
	Doc: json.RawMessage(`{
      "type": "object",
      "properties": {
        "description": {"type": "string"},
        "features":    {"type": "array", "items": {"type": "string"}}
      },
      "required": ["description", "features"],
      "additionalProperties": false
    }`),
}

// SynthesizeProduct combines all per-page analyses into a product summary and
// a deduplicated feature list.
//
// For typical docs sites the body fits the small-tier budget after per-page
// summary compression and SynthesizeProduct emits a single LLM call. For very
// large corpora (hundreds of pages) the compressed body itself overflows;
// SynthesizeProduct then groups the compressed pages into budget-sized
// batches, summarizes each into a partial Product, and reduces the partials
// in pairs until one remains.
func SynthesizeProduct(ctx context.Context, tiering LLMTiering, pages []PageAnalysis) (ProductSummary, error) {
	return synthesizeProductWithBudget(ctx, tiering.Small(), pages, synthesizeBudget)
}

// synthesizeProductWithBudget is the shared implementation used by
// SynthesizeProduct (production budget) and SynthesizeProductForTest
// (caller-chosen budget). The budget parameter is the NET content budget
// already excluding prompt overhead; synthesizeProductWithBudget makes no
// further overhead deduction so tests can drive the map-reduce branch with
// a small, fast-to-tokenize budget.
func synthesizeProductWithBudget(ctx context.Context, client LLMClient, pages []PageAnalysis, budget int) (ProductSummary, error) {
	compressed := compressPageSummaries(pages, perPageSummaryBudget)
	body := renderSynthesizeBody(compressed)
	if chunker.EstimateTokens(body) <= budget {
		return runSynthesizeOnce(ctx, client, body)
	}

	// Map-reduce: split compressed pages into budget-sized groups, summarize
	// each into a partial Product, then reduce the partials in pairs.
	groups := splitCompressedPages(compressed, budget)
	if len(groups) <= 1 {
		// Even one page entry didn't fit the budget. Fall back to a single
		// call anyway and rely on the ErrTokenBudgetExceeded backstop in
		// runSynthesizeOnce to log loudly. This branch is unreachable in
		// practice because perPageSummaryBudget is much smaller than any
		// reasonable budget.
		return runSynthesizeOnce(ctx, client, body)
	}
	partials := make([]ProductSummary, 0, len(groups))
	for i, g := range groups {
		gbody := renderSynthesizeBody(g)
		p, err := runSynthesizeOnce(ctx, client, gbody)
		if err != nil {
			return ProductSummary{}, fmt.Errorf("synthesize group %d/%d: %w", i+1, len(groups), err)
		}
		partials = append(partials, p)
	}
	log.Debugf("synthesize map-reduce: pages=%d groups=%d", len(pages), len(groups))
	return reducePartials(ctx, client, partials, budget)
}

// renderSynthesizeBody serializes a slice of (already compressed) page
// analyses into the per-page lines the synthesize prompt expects. Each
// entry is one URL + one summary + one feature list, separated by blank
// lines for readability and to give the chunker paragraph boundaries on
// the rare path where we further chunk the body.
func renderSynthesizeBody(pages []PageAnalysis) string {
	var sb strings.Builder
	for _, p := range pages {
		fmt.Fprintf(&sb, "URL: %s\nSummary: %s\nFeatures: %s\n\n",
			p.URL, p.Summary, strings.Join(p.Features, ", "))
	}
	return sb.String()
}

// renderPageEntry serializes one page's contribution to the synthesize body.
// Used by splitCompressedPages to size each entry against the budget. The
// output MUST match a single iteration of renderSynthesizeBody's loop so
// estimator math stays honest.
func renderPageEntry(p PageAnalysis) string {
	return fmt.Sprintf("URL: %s\nSummary: %s\nFeatures: %s\n\n",
		p.URL, p.Summary, strings.Join(p.Features, ", "))
}

// compressPageSummaries returns a copy of pages with each Summary truncated
// to perPage tokens via chunker.Fit. Page URLs and feature lists are
// preserved verbatim — only the (potentially long) prose summary is trimmed.
// This is the cheap-path levering: most synthesize calls fit single-pass
// after compression alone.
func compressPageSummaries(pages []PageAnalysis, perPage int) []PageAnalysis {
	out := make([]PageAnalysis, len(pages))
	for i, p := range pages {
		p.Summary = chunker.Fit(p.Summary, perPage)
		out[i] = p
	}
	return out
}

// splitCompressedPages greedy-packs already compressed pages into groups
// whose serialized body fits budget tokens. Groups preserve input order so
// downstream merging is deterministic across runs.
func splitCompressedPages(pages []PageAnalysis, budget int) [][]PageAnalysis {
	var groups [][]PageAnalysis
	var cur []PageAnalysis
	curTok := 0
	for _, p := range pages {
		t := chunker.EstimateTokens(renderPageEntry(p))
		if curTok+t > budget && len(cur) > 0 {
			groups = append(groups, cur)
			cur = nil
			curTok = 0
		}
		cur = append(cur, p)
		curTok += t
	}
	if len(cur) > 0 {
		groups = append(groups, cur)
	}
	return groups
}

// runSynthesizeOnce runs the per-group synthesize prompt against a single
// pre-rendered body and returns the parsed ProductSummary. This is the
// extracted single-call path used by both the cheap (single-pass) branch
// and each per-group call of the map-reduce branch.
func runSynthesizeOnce(ctx context.Context, client LLMClient, body string) (ProductSummary, error) {
	// PROMPT: Synthesizes a product-level description and a deduplicated feature list from all documentation page summaries.
	prompt := fmt.Sprintf(`You are analyzing documentation for a software product.

Here are summaries and features extracted from individual documentation pages:

%s
Populate the response with:
- "description": a 2-3 sentence summary of what this product is and what it does
- "features": a deduplicated, sorted list of all product features and capabilities (short noun phrases, max 8 words each)`, body)

	raw, err := client.CompleteJSON(ctx, prompt, synthesizeSchema)
	if err != nil {
		// Defense-in-depth: a budget overrun here means the per-page
		// compression + group split underestimated the prompt. Log loudly
		// and surface the error rather than silently dropping the call.
		if errors.Is(err, ErrTokenBudgetExceeded{}) {
			log.Warnf("SynthesizeProduct: budget exceeded after compression; "+
				"body_tokens=%d (estimator drift, page count=?)",
				chunker.EstimateTokens(body))
		}
		return ProductSummary{}, fmt.Errorf("SynthesizeProduct: %w", err)
	}

	var resp synthesizeResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ProductSummary{}, fmt.Errorf("SynthesizeProduct: invalid JSON response: %w", err)
	}
	if resp.Features == nil {
		resp.Features = []string{}
	}
	return ProductSummary(resp), nil
}

// reducePartials merges partial Product summaries into one. When the
// rendered partial-merge body itself exceeds budget, reducePartials
// recurses by pairing partials (left half / right half), reducing each
// half independently, then reducing the two halves together. Recursion
// depth is O(log N) in the number of partials; in practice N is small
// (single-digit) for any realistic docs corpus.
func reducePartials(ctx context.Context, client LLMClient, partials []ProductSummary, budget int) (ProductSummary, error) {
	if len(partials) == 0 {
		return ProductSummary{Features: []string{}}, nil
	}
	if len(partials) == 1 {
		return partials[0], nil
	}
	body := renderPartialMerge(partials)
	if chunker.EstimateTokens(body) > budget {
		half := len(partials) / 2
		left, err := reducePartials(ctx, client, partials[:half], budget)
		if err != nil {
			return ProductSummary{}, err
		}
		right, err := reducePartials(ctx, client, partials[half:], budget)
		if err != nil {
			return ProductSummary{}, err
		}
		return reducePartials(ctx, client, []ProductSummary{left, right}, budget)
	}
	return runSynthesizeReduce(ctx, client, body)
}

// renderPartialMerge serializes partial Products into the reduction prompt's
// expected body. Partials are emitted in input order so the LLM's output
// stays deterministic across runs on the same corpus.
func renderPartialMerge(partials []ProductSummary) string {
	var sb strings.Builder
	for i, p := range partials {
		fmt.Fprintf(&sb, "Partial %d:\nDescription: %s\nFeatures: %s\n\n",
			i+1, p.Description, strings.Join(p.Features, ", "))
	}
	return sb.String()
}

// runSynthesizeReduce runs the reduction prompt that merges several partial
// Product summaries into one canonical ProductSummary. The reduction prompt
// is separate from the per-group synthesize prompt so prompt-tuning the
// reduction step doesn't perturb the cheap-path single-call output.
func runSynthesizeReduce(ctx context.Context, client LLMClient, body string) (ProductSummary, error) {
	// PROMPT: Merges N partial product summaries (each summarizing a disjoint subset of documentation pages) into one canonical product. Deduplicate features across partials by lowercased name; keep the longest, most-specific product description; preserve short noun-phrase shape for feature names.
	prompt := fmt.Sprintf(`You are merging several partial product summaries into one canonical summary.

Each partial below summarizes a DIFFERENT subset of the same product's documentation pages. Your job is to combine them into a single coherent summary that covers the whole product.

%s
Populate the response with:
- "description": one canonical 2-3 sentence summary of the product. Prefer the most specific phrasing; do not concatenate partial descriptions.
- "features": a deduplicated, sorted list of every feature mentioned across the partials. Dedupe by lowercased name; preserve short noun-phrase shape (max 8 words each). Do not invent features that no partial mentioned.`, body)

	raw, err := client.CompleteJSON(ctx, prompt, synthesizeReduceSchema)
	if err != nil {
		if errors.Is(err, ErrTokenBudgetExceeded{}) {
			log.Warnf("SynthesizeProduct: reduce budget exceeded; "+
				"body_tokens=%d", chunker.EstimateTokens(body))
		}
		return ProductSummary{}, fmt.Errorf("SynthesizeProduct reduce: %w", err)
	}

	var resp synthesizeResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ProductSummary{}, fmt.Errorf("SynthesizeProduct reduce: invalid JSON response: %w", err)
	}
	if resp.Features == nil {
		resp.Features = []string{}
	}
	sort.Strings(resp.Features)
	return ProductSummary(resp), nil
}
