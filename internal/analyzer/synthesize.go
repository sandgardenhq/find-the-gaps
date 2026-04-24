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

// SynthesizeProduct combines all per-page analyses into a product summary and
// a deduplicated feature list.
func SynthesizeProduct(ctx context.Context, tiering LLMTiering, pages []PageAnalysis) (ProductSummary, error) {
	client := tiering.Small()
	var sb strings.Builder
	for _, p := range pages {
		fmt.Fprintf(&sb, "URL: %s\nSummary: %s\nFeatures: %s\n\n",
			p.URL, p.Summary, strings.Join(p.Features, ", "))
	}

	// PROMPT: Synthesizes a product-level description and a deduplicated feature list from all documentation page summaries.
	prompt := fmt.Sprintf(`You are analyzing documentation for a software product.

Here are summaries and features extracted from individual documentation pages:

%s
Populate the response with:
- "description": a 2-3 sentence summary of what this product is and what it does
- "features": a deduplicated, sorted list of all product features and capabilities (short noun phrases, max 8 words each)`, sb.String())

	raw, err := client.CompleteJSON(ctx, prompt, synthesizeSchema)
	if err != nil {
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
