package analyzer

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/charmbracelet/log"
)

// ScreenshotPromptBudget caps the per-page screenshot-detection prompt size,
// measured by the local cl100k_base estimator. It must absorb two sources of
// drift between what we measure and what Claude charges:
//   - Tokenizer drift: cl100k_base undercounts vs Claude's tokenizer on
//     code-heavy reference pages by ~13% in practice (observed:
//     bun.com/reference/node/crypto produced 201,871 actual tokens at ~179K
//     cl100k under a 180K budget).
//   - Tool-use overhead: the Anthropic CompleteJSON path injects a forced
//     "respond" tool whose JSON Schema parameters count toward the input
//     window but are not visible to our local estimator.
//
// 150K * a 1.2x worst-case drift factor stays under the 200K window with
// headroom for the response.
const ScreenshotPromptBudget = 150_000

// imageRef is one image occurrence on a docs page.
type imageRef struct {
	AltText        string
	Src            string
	SectionHeading string // most recent "# ..." or "## ..." heading above this image; "" if none
	ParagraphIndex int    // 0-based index of the paragraph block containing this image
}

var markdownImageRe = regexp.MustCompile(`!\[([^\]]*)\]\(([^)]+)\)`)
var htmlImgRe = regexp.MustCompile(`(?i)<img\s+([^>]+?)>`)
var htmlAttrSrcRe = regexp.MustCompile(`(?i)\bsrc\s*=\s*(?:"([^"]*)"|'([^']*)')`)
var htmlAttrAltRe = regexp.MustCompile(`(?i)\balt\s*=\s*(?:"([^"]*)"|'([^']*)')`)
var atxHeadingRe = regexp.MustCompile(`^#{1,6}\s`)

// extractImages returns all image references in the markdown, annotated with their
// containing section heading and paragraph index. Paragraphs are blank-line
// separated. Lines inside fenced code blocks (``` or ~~~) are excluded from
// heading detection so shell/Python comments like `# install foo` don't get
// mistaken for Markdown headings.
func extractImages(md string) []imageRef {
	var refs []imageRef
	currentHeading := ""
	inFence := false
	pIdx := 0
	var blockLines []string

	flush := func() {
		if len(blockLines) == 0 {
			return
		}
		block := strings.Join(blockLines, "\n")
		for _, m := range markdownImageRe.FindAllStringSubmatch(block, -1) {
			refs = append(refs, imageRef{
				AltText:        m[1],
				Src:            m[2],
				SectionHeading: currentHeading,
				ParagraphIndex: pIdx,
			})
		}
		for _, m := range htmlImgRe.FindAllStringSubmatch(block, -1) {
			attrs := m[1]
			src, alt := "", ""
			if mm := htmlAttrSrcRe.FindStringSubmatch(attrs); mm != nil {
				src = mm[1]
				if src == "" {
					src = mm[2]
				}
			}
			if mm := htmlAttrAltRe.FindStringSubmatch(attrs); mm != nil {
				alt = mm[1]
				if alt == "" {
					alt = mm[2]
				}
			}
			refs = append(refs, imageRef{
				AltText:        alt,
				Src:            src,
				SectionHeading: currentHeading,
				ParagraphIndex: pIdx,
			})
		}
		blockLines = nil
	}

	for line := range strings.SplitSeq(md, "\n") {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			blockLines = append(blockLines, line)
			continue
		}

		if !inFence {
			if trimmed == "" {
				if len(blockLines) > 0 {
					flush()
					pIdx++
				}
				continue
			}
			if atxHeadingRe.MatchString(trimmed) {
				currentHeading = strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
			}
		}

		blockLines = append(blockLines, line)
	}
	flush()

	return refs
}

// splitImageBatches groups refs into chunks of size <= max, preserving order.
// Returns nil for empty input or non-positive max. Used by the vision relevance
// pass to keep each multimodal call within Groq's 5-image-per-request cap.
func splitImageBatches(refs []imageRef, max int) [][]imageRef {
	if len(refs) == 0 || max <= 0 {
		return nil
	}
	out := make([][]imageRef, 0, (len(refs)+max-1)/max)
	for i := 0; i < len(refs); i += max {
		end := i + max
		if end > len(refs) {
			end = len(refs)
		}
		out = append(out, refs[i:end])
	}
	return out
}

// buildCoverageMap groups image references by their containing section heading.
// Passed into the prompt so the LLM can apply the locality rule.
func buildCoverageMap(refs []imageRef) map[string][]imageRef {
	if len(refs) == 0 {
		return map[string][]imageRef{}
	}
	out := make(map[string][]imageRef)
	for _, r := range refs {
		out[r.SectionHeading] = append(out[r.SectionHeading], r)
	}
	return out
}

// buildScreenshotPrompt assembles the LLM prompt for one docs page.
func buildScreenshotPrompt(pageURL, content string, coverage map[string][]imageRef) string {
	var coverageSummary string
	if len(coverage) == 0 {
		coverageSummary = "No existing images on this page."
	} else {
		sections := make([]string, 0, len(coverage))
		for s := range coverage {
			sections = append(sections, s)
		}
		sort.Strings(sections)
		var lines []string
		for _, s := range sections {
			heading := s
			if heading == "" {
				heading = "(no heading)"
			}
			for _, r := range coverage[s] {
				lines = append(lines, fmt.Sprintf("- section %q, paragraph %d: src=%q alt=%q",
					heading, r.ParagraphIndex, r.Src, r.AltText))
			}
		}
		coverageSummary = strings.Join(lines, "\n")
	}

	// PROMPT: Identifies the small number of passages in a documentation page where a screenshot is essential — multi-step flows, visually dense moments, or visual recognition the reader cannot reconstruct from prose. Applies a locality rule: a passage is already covered if an image appears in the same section heading or within 3 paragraphs before/after. Default is to flag nothing; over-flagging is worse than missing a gap.
	return fmt.Sprintf(`You are reviewing a documentation page to find the small number of places where a screenshot is ESSENTIAL — not merely helpful. The default is to flag nothing. Be aggressively conservative; over-flagging is worse than missing a gap.

URL: %s

Existing images on this page (if any):
%s

Page content:
%s

A passage is ALREADY COVERED (do not flag it) if an existing image on this page appears in the same section heading as the passage, OR within 3 paragraphs before/after the passage.

Flag a passage ONLY if at least one is true:
1. MULTI-STEP FLOW: It describes a sequence of two or more distinct user actions across changing UI states, and the reader needs to see the intermediate states to stay oriented (e.g., a wizard, an OAuth handshake screen-by-screen, a guided onboarding).
2. VISUALLY DENSE: It describes a moment where prose cannot reasonably enumerate what is on screen — a dashboard with multiple panels, a chart whose shape matters, a configuration page with many interacting fields, a complex error state with specific layout.
3. VISUAL RECOGNITION: The reader is asked to recognize something they cannot reconstruct from text alone — "look for the red banner at the top", "the chart should resemble this shape", "find the icon that looks like…".

Do NOT flag:
- Single-action interactions ("click Save", "press Enter", "fill in the email field").
- Terminal sessions whose output is already shown inline in a code block.
- Reference material (API signatures, option tables, type listings).
- Abstract prose with no specific UI moment.
- Anything covered by the locality rule above.
- Passages where you cannot name, in one sentence, exactly what would be lost if the reader followed prose alone.

Populate "gaps" with one object per REMAINING gap. Each object must have:
- "quoted_passage": the exact verbatim quote from the page. Do not paraphrase.
- "should_show": specific description of the screenshot — name visible elements, values, states, panels. Not "a screenshot of the feature".
- "suggested_alt": alt text / caption, under 100 characters.
- "insertion_hint": where to paste the image, referencing existing prose. Example: "after the paragraph ending '…click Save.'" Do not use line numbers.

Return an empty "gaps" array if nothing meets the bar. This is the expected outcome for most pages.`, pageURL, coverageSummary, content)
}

// fitContentToBudget returns content sized so that the assembled
// screenshot-gap prompt fits inside budget tokens (using the local cl100k_base
// estimator). The returned bool is false when the prompt overhead alone — URL,
// instructions, coverage map — already exceeds the budget; callers should skip
// the page in that case.
func fitContentToBudget(pageURL, content string, coverage map[string][]imageRef, budget int) (string, bool) {
	// Margin absorbs (a) drift between cl100k_base and the provider's exact
	// tokenizer and (b) the char-ratio truncation overshooting a token boundary
	// on repetitive content.
	const margin = 1_000
	overhead := countTokens(buildScreenshotPrompt(pageURL, "", coverage))
	available := budget - overhead - margin
	if available < 100 {
		return "", false
	}
	contentTokens := countTokens(content)
	if contentTokens <= available {
		return content, true
	}
	keepChars := min(int(float64(len(content))*float64(available)/float64(contentTokens)), len(content))
	log.Warnf("screenshot-gaps: truncating %s (%d → ~%d tokens) to fit %d budget",
		pageURL, contentTokens, available, budget)
	return content[:keepChars], true
}

// screenshotResponseItem is one raw item in the LLM's response for a
// screenshot-gap detection call.
type screenshotResponseItem struct {
	QuotedPassage string `json:"quoted_passage"`
	ShouldShow    string `json:"should_show"`
	SuggestedAlt  string `json:"suggested_alt"`
	InsertionHint string `json:"insertion_hint"`
}

// screenshotGapsResponse wraps the gap array because provider tool-call
// input_schemas must be JSON objects at the root.
type screenshotGapsResponse struct {
	Gaps []screenshotResponseItem `json:"gaps"`
}

// PROMPT SCHEMA: output shape for DetectScreenshotGaps.
var screenshotGapsSchema = JSONSchema{
	Name: "screenshot_gaps_response",
	Doc: json.RawMessage(`{
      "type": "object",
      "properties": {
        "gaps": {
          "type": "array",
          "items": {
            "type": "object",
            "properties": {
              "quoted_passage": {"type": "string"},
              "should_show":    {"type": "string"},
              "suggested_alt":  {"type": "string"},
              "insertion_hint": {"type": "string"}
            },
            "required": ["quoted_passage", "should_show", "suggested_alt", "insertion_hint"],
            "additionalProperties": false
          }
        }
      },
      "required": ["gaps"],
      "additionalProperties": false
    }`),
}

// DocPage is one fetched documentation page.
type DocPage struct {
	URL     string
	Path    string
	Content string
}

// ScreenshotProgressFunc is called after each page completes. done/total express
// progress counts. currentPage is the URL of the page just processed.
type ScreenshotProgressFunc func(done, total int, currentPage string)

// DetectScreenshotGaps iterates pages sequentially, issues one LLM call per page,
// and returns all findings. Per-page parse failures are logged and skipped
// (fail-open); context / network errors are returned immediately.
func DetectScreenshotGaps(
	ctx context.Context,
	client LLMClient,
	pages []DocPage,
	progress ScreenshotProgressFunc,
) ([]ScreenshotGap, error) {
	var gaps []ScreenshotGap
	total := len(pages)
	for i, page := range pages {
		refs := extractImages(page.Content)
		coverage := buildCoverageMap(refs)
		content, ok := fitContentToBudget(page.URL, page.Content, coverage, ScreenshotPromptBudget)
		if !ok {
			log.Warnf("screenshot-gaps: skipping %s: prompt overhead exceeds budget", page.URL)
			if progress != nil {
				progress(i+1, total, page.URL)
			}
			continue
		}
		prompt := buildScreenshotPrompt(page.URL, content, coverage)
		raw, err := client.CompleteJSON(ctx, prompt, screenshotGapsSchema)
		if err != nil {
			return nil, fmt.Errorf("DetectScreenshotGaps %s: %w", page.URL, err)
		}
		var resp screenshotGapsResponse
		if err := json.Unmarshal(raw, &resp); err != nil {
			log.Warnf("screenshot-gaps: skipping %s: invalid JSON response: %v", page.URL, err)
			if progress != nil {
				progress(i+1, total, page.URL)
			}
			continue
		}
		for _, it := range resp.Gaps {
			gaps = append(gaps, ScreenshotGap{
				PageURL:       page.URL,
				PagePath:      page.Path,
				QuotedPassage: it.QuotedPassage,
				ShouldShow:    it.ShouldShow,
				SuggestedAlt:  it.SuggestedAlt,
				InsertionHint: it.InsertionHint,
			})
		}
		if progress != nil {
			progress(i+1, total, page.URL)
		}
	}
	return gaps, nil
}
