package analyzer

import (
	"context"
	"encoding/json"
	"fmt"
)

type analyzePageResponse struct {
	Summary  string   `json:"summary"`
	Features []string `json:"features"`
	// Pointer so we can detect "missing from response" and apply the
	// inclusive-by-default rule (treat as docs) instead of silently
	// dropping the page as not-docs. False negatives are worse than
	// false positives — see .plans/DOCS_CLASSIFIER_DESIGN.md.
	IsDocs *bool `json:"is_docs"`
}

// PROMPT SCHEMA: output shape for AnalyzePage.
var analyzePageSchema = JSONSchema{
	Name: "analyze_page_response",
	Doc: json.RawMessage(`{
      "type": "object",
      "properties": {
        "summary":  {"type": "string"},
        "features": {"type": "array", "items": {"type": "string"}},
        "is_docs":  {"type": "boolean"}
      },
      "required": ["summary", "features", "is_docs"],
      "additionalProperties": false
    }`),
}

// AnalyzePage sends doc page content to the LLM and returns a summary,
// feature list, and a binary classification of whether the page is
// product documentation.
func AnalyzePage(ctx context.Context, tiering LLMTiering, pageURL, content string) (PageAnalysis, error) {
	client := tiering.Small()
	// PROMPT: Summarizes a single documentation page, extracts the product features described on it, and classifies whether the page is product documentation. Inclusive-by-default: when uncertain, classify as docs (false negatives are worse than false positives).
	prompt := fmt.Sprintf(`You are analyzing a page on a software product's website.

URL: %s

Content:
%s

Populate the response with:
- "summary": a 1-2 sentence description of what this page covers
- "features": a list of product features or capabilities described on this page (short noun phrases, max 8 words each). May be empty.
- "is_docs": a boolean classifying whether this page is product DOCUMENTATION.

Rule for is_docs:
A page is DOCS if a user trying to USE this product would consult it for current technical information about features, APIs, configuration, or behavior.

Examples of docs (is_docs=true):
- API references, tutorials, quickstarts, configuration references
- Changelogs and release notes
- "Announcing v3"-style new-feature blog posts
- Marketing landing pages that contain code snippets or technical claims about how the product works

Examples of NOT docs (is_docs=false):
- Engineering retrospectives ("how we built X", "scaling our database")
- Customer case studies / customer logos
- Team, about, careers, legal pages
- Pricing pages without technical content
- Generic company blog posts (hiring announcements, fundraising news, holiday messages)

Set is_docs=false ONLY when you are confident the page is one of the not-docs categories above. Default to docs when unsure.`, pageURL, content)

	raw, err := client.CompleteJSON(ctx, prompt, analyzePageSchema)
	if err != nil {
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

	return PageAnalysis{
		URL:      pageURL,
		Summary:  resp.Summary,
		Features: resp.Features,
		IsDocs:   isDocs,
	}, nil
}
