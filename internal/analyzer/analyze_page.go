package analyzer

import (
	"context"
	"encoding/json"
	"fmt"
)

type analyzePageResponse struct {
	Summary  string   `json:"summary"`
	Features []string `json:"features"`
}

// AnalyzePage sends doc page content to the LLM and returns a summary and feature list.
func AnalyzePage(ctx context.Context, tiering LLMTiering, pageURL, content string) (PageAnalysis, error) {
	client := tiering.Small()
	// PROMPT: Summarizes a single documentation page and extracts the product features or capabilities described on it. Responds with JSON only.
	prompt := fmt.Sprintf(`You are analyzing a documentation page for a software product.

URL: %s

Content:
%s

Return a JSON object with exactly these fields:
- "summary": a 1-2 sentence description of what this page covers
- "features": a list of product features or capabilities described on this page (short noun phrases, max 8 words each)

Respond with only the JSON object. No markdown code fences. No prose.`, pageURL, content)

	raw, err := client.Complete(ctx, prompt)
	if err != nil {
		return PageAnalysis{}, fmt.Errorf("AnalyzePage %s: %w", pageURL, err)
	}

	var resp analyzePageResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		return PageAnalysis{}, fmt.Errorf("AnalyzePage %s: invalid JSON response: %w", pageURL, err)
	}

	if resp.Features == nil {
		resp.Features = []string{}
	}

	return PageAnalysis{
		URL:      pageURL,
		Summary:  resp.Summary,
		Features: resp.Features,
	}, nil
}
