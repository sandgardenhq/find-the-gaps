package analyzer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type synthesizeResponse struct {
	Description string   `json:"description"`
	Features    []string `json:"features"`
}

// SynthesizeProduct combines all per-page analyses into a product summary and
// a deduplicated feature list.
func SynthesizeProduct(ctx context.Context, tiering LLMTiering, pages []PageAnalysis) (ProductSummary, error) {
	client := tiering.Small()
	var sb strings.Builder
	for _, p := range pages {
		fmt.Fprintf(&sb, "URL: %s\nSummary: %s\nFeatures: %s\n\n",
			p.URL, p.Summary, strings.Join(p.Features, ", "))
	}

	// PROMPT: Synthesizes a product-level description and a deduplicated feature list from all documentation page summaries. Responds with JSON only.
	prompt := fmt.Sprintf(`You are analyzing documentation for a software product.

Here are summaries and features extracted from individual documentation pages:

%s
Based on the above, return a JSON object with exactly these fields:
- "description": a 2-3 sentence summary of what this product is and what it does
- "features": a deduplicated, sorted list of all product features and capabilities (short noun phrases, max 8 words each)

Respond with only the JSON object. No markdown code fences. No prose.`, sb.String())

	raw, err := client.Complete(ctx, prompt)
	if err != nil {
		return ProductSummary{}, fmt.Errorf("SynthesizeProduct: %w", err)
	}

	var resp synthesizeResponse
	if err := json.Unmarshal([]byte(stripCodeFence(raw)), &resp); err != nil {
		return ProductSummary{}, fmt.Errorf("SynthesizeProduct: invalid JSON response: %w", err)
	}

	if resp.Features == nil {
		resp.Features = []string{}
	}

	return ProductSummary(resp), nil
}
