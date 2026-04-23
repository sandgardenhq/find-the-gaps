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
		prompt := buildScreenshotPrompt(page.URL, page.Content, coverage)
		raw, err := client.Complete(ctx, prompt)
		if err != nil {
			return nil, fmt.Errorf("DetectScreenshotGaps %s: %w", page.URL, err)
		}
		items, err := parseScreenshotResponse(raw)
		if err != nil {
			log.Warnf("screenshot-gaps: skipping %s: %v", page.URL, err)
			if progress != nil {
				progress(i+1, total, page.URL)
			}
			continue
		}
		for _, it := range items {
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
