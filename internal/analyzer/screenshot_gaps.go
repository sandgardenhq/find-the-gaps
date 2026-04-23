package analyzer

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

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

// extractImages returns all image references in the markdown, annotated with their
// containing section heading and paragraph index. Paragraphs are separated by blank lines.
func extractImages(md string) []imageRef {
	var refs []imageRef
	paragraphs := strings.Split(md, "\n\n")
	currentHeading := ""
	for pIdx, block := range paragraphs {
		// Track the most recent heading.
		for _, line := range strings.Split(block, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "#") {
				h := strings.TrimLeft(trimmed, "#")
				currentHeading = strings.TrimSpace(h)
			}
		}
		// Find markdown images in this block.
		for _, m := range markdownImageRe.FindAllStringSubmatch(block, -1) {
			refs = append(refs, imageRef{
				AltText:        m[1],
				Src:            m[2],
				SectionHeading: currentHeading,
				ParagraphIndex: pIdx,
			})
		}
		// Find HTML <img> tags in this block.
		for _, m := range htmlImgRe.FindAllStringSubmatch(block, -1) {
			attrs := m[1]
			src := ""
			alt := ""
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
	}
	return refs
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

	// PROMPT: Identifies passages in a documentation page that describe user-facing UI moments (web, app, terminal) and should have a screenshot nearby but do not. Applies a locality rule: a passage is already covered if an image appears in the same section heading or within 3 paragraphs before/after. Returns a JSON array; empty if nothing needs a screenshot.
	return fmt.Sprintf(`You are reviewing a documentation page to identify places where a screenshot would materially help the reader, but none is present nearby.

URL: %s

Existing images on this page (if any):
%s

Page content:
%s

A passage is ALREADY COVERED (do not flag it) if an existing image on this page appears in the same section heading as the passage, OR within 3 paragraphs before/after the passage.

Only flag passages that describe a concrete user-facing moment the reader would benefit from seeing: a web UI, an app screen, a terminal session with visible output, a dialog, a dashboard, a button or form the user interacts with.

Do NOT flag:
- Pure reference material (API signatures, type tables, option lists).
- Abstract prose with no concrete UI moment.
- Passages already covered by a nearby image per the locality rule above.

For each remaining gap, return an object with these fields:
- "quoted_passage": the exact verbatim quote from the page that describes the UI moment. Do not paraphrase.
- "should_show": a concrete description of what the screenshot should depict. Be specific: name the visible elements, values, buttons, states. Not "a screenshot of the feature".
- "suggested_alt": alt text / caption for the screenshot, under 100 characters.
- "insertion_hint": where to paste the image, referencing existing prose. Example: "after the paragraph ending '…click Save.'" Do not use line numbers.

Return a JSON array of these objects. Return [] if nothing needs a screenshot.
Respond with only the JSON array. No markdown code fences. No prose.`, pageURL, coverageSummary, content)
}

// screenshotResponseItem is one raw item in the LLM's JSON-array response for a
// screenshot-gap detection call.
type screenshotResponseItem struct {
	QuotedPassage string `json:"quoted_passage"`
	ShouldShow    string `json:"should_show"`
	SuggestedAlt  string `json:"suggested_alt"`
	InsertionHint string `json:"insertion_hint"`
}

// parseScreenshotResponse extracts a JSON array from raw LLM output and returns
// parsed items. Reuses extractJSONArray (from drift.go) for preamble tolerance.
func parseScreenshotResponse(raw string) ([]screenshotResponseItem, error) {
	arr := extractJSONArray(raw)
	var items []screenshotResponseItem
	if err := json.Unmarshal([]byte(arr), &items); err != nil {
		return nil, fmt.Errorf("invalid screenshot-gap JSON: %w (raw: %q)", err, raw)
	}
	return items, nil
}
