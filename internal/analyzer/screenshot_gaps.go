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

// buildDetectionPromptWithVerdicts assembles a verdict-annotated detection
// prompt for one docs page. When verdicts is nil or empty, it delegates to
// buildScreenshotPrompt for byte-for-byte backward compatibility (the
// non-vision path). When verdicts is non-empty, each image in the coverage
// list is annotated with "verdict: matches" or "verdict: does not match" using
// the same global "img-N" numbering scheme as the relevance pass (1-indexed,
// no gaps), and the prompt instructs the model to (a) suppress findings when
// a matches image already covers the moment and (b) report those suppressed
// moments under "suppressed_by_image" so the audit stats can count them
// without a second LLM call.
func buildDetectionPromptWithVerdicts(pageURL, content string, refs []imageRef, verdicts []ImageVerdict) string {
	if len(verdicts) == 0 {
		return buildScreenshotPrompt(pageURL, content, buildCoverageMap(refs))
	}

	verdictByIndex := make(map[string]bool, len(verdicts))
	for _, v := range verdicts {
		verdictByIndex[v.Index] = v.Matches
	}

	annotation := func(idx int) string {
		key := fmt.Sprintf("img-%d", idx)
		matches, ok := verdictByIndex[key]
		if !ok {
			return "verdict: unknown"
		}
		if matches {
			return "verdict: matches"
		}
		return "verdict: does not match"
	}

	var coverageSummary string
	if len(refs) == 0 {
		coverageSummary = "No existing images on this page."
	} else {
		// Group by section heading for stable, locality-aware listing while
		// preserving the global img-N numbering (1-based, derived from the
		// position of each ref in the input slice — same scheme used by the
		// relevance pass).
		indexByRef := make(map[*imageRef]int, len(refs))
		for i := range refs {
			indexByRef[&refs[i]] = i + 1
		}
		bySection := make(map[string][]*imageRef)
		for i := range refs {
			r := &refs[i]
			bySection[r.SectionHeading] = append(bySection[r.SectionHeading], r)
		}
		sections := make([]string, 0, len(bySection))
		for s := range bySection {
			sections = append(sections, s)
		}
		sort.Strings(sections)
		var lines []string
		for _, s := range sections {
			heading := s
			if heading == "" {
				heading = "(no heading)"
			}
			for _, r := range bySection[s] {
				lines = append(lines, fmt.Sprintf("- img-%d (%s), section %q, paragraph %d: src=%q alt=%q",
					indexByRef[r], annotation(indexByRef[r]), heading, r.ParagraphIndex, r.Src, r.AltText))
			}
		}
		coverageSummary = strings.Join(lines, "\n")
	}

	// PROMPT: Verdict-enriched screenshot-gap detection. Same locality + bar-of-essentiality rules as the legacy prompt, plus: when an image marked "verdict: matches" already covers a passage, do NOT flag it as a missing screenshot. Instead, emit it under "suppressed_by_image" so audit stats can count what was suppressed without a second LLM round-trip. Images marked "verdict: does not match" do NOT cover their surrounding prose — treat them as if they were absent.
	return fmt.Sprintf(`You are reviewing a documentation page to find the small number of places where a screenshot is ESSENTIAL — not merely helpful. The default is to flag nothing. Be aggressively conservative; over-flagging is worse than missing a gap.

URL: %s

Existing images on this page, each annotated with the relevance-pass verdict:
%s

Page content:
%s

Verdict semantics:
- "verdict: matches" — the image's actual contents accurately reflect the surrounding prose. Treat the moment as ALREADY COVERED.
- "verdict: does not match" — the image's actual contents do NOT reflect the surrounding prose. Treat the moment as NOT covered (as if no image were present at all).
- "verdict: unknown" — fall back to the locality rule (same section heading, or within 3 paragraphs).

A passage is ALREADY COVERED (do not flag it) if a "verdict: matches" image appears in the same section heading as the passage, OR within 3 paragraphs before/after the passage. The locality rule alone (without a matching verdict) is NOT sufficient when a verdict is present for the relevant image.

Flag a passage ONLY if at least one is true:
1. MULTI-STEP FLOW: It describes a sequence of two or more distinct user actions across changing UI states, and the reader needs to see the intermediate states to stay oriented (e.g., a wizard, an OAuth handshake screen-by-screen, a guided onboarding).
2. VISUALLY DENSE: It describes a moment where prose cannot reasonably enumerate what is on screen — a dashboard with multiple panels, a chart whose shape matters, a configuration page with many interacting fields, a complex error state with specific layout.
3. VISUAL RECOGNITION: The reader is asked to recognize something they cannot reconstruct from text alone — "look for the red banner at the top", "the chart should resemble this shape", "find the icon that looks like…".

Do NOT flag:
- Single-action interactions ("click Save", "press Enter", "fill in the email field").
- Terminal sessions whose output is already shown inline in a code block.
- Reference material (API signatures, option tables, type listings).
- Abstract prose with no specific UI moment.
- Anything covered by the locality rule above when paired with a "verdict: matches" image.
- Passages where you cannot name, in one sentence, exactly what would be lost if the reader followed prose alone.

Populate "gaps" with one object per REMAINING gap (one that would be flagged AND is not already covered by a matching image). Each object must have:
- "quoted_passage": the exact verbatim quote from the page. Do not paraphrase.
- "should_show": specific description of the screenshot — name visible elements, values, states, panels. Not "a screenshot of the feature".
- "suggested_alt": alt text / caption, under 100 characters.
- "insertion_hint": where to paste the image, referencing existing prose. Example: "after the paragraph ending '…click Save.'" Do not use line numbers.

Populate "suppressed_by_image" with one object per moment that you WOULD have flagged under the rules above EXCEPT that a "verdict: matches" image already covers it. Same four fields as "gaps". This list is for audit stats only; it is NOT rendered to users.

Return empty arrays for both "gaps" and "suppressed_by_image" when nothing meets the bar. This is the expected outcome for most pages.`, pageURL, coverageSummary, content)
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
// input_schemas must be JSON objects at the root. SuppressedByImage carries
// moments the model would have flagged as missing screenshots if not for an
// existing image whose verdict was matches=true; counted into audit stats but
// not rendered to screenshots.md.
type screenshotGapsResponse struct {
	Gaps              []screenshotResponseItem `json:"gaps"`
	SuppressedByImage []screenshotResponseItem `json:"suppressed_by_image"`
}

// PROMPT SCHEMA: output shape for DetectScreenshotGaps. The suppressed_by_image
// array mirrors the gaps array so the audit pipeline can count suppressed
// moments without issuing a second detection call.
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
        },
        "suppressed_by_image": {
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

// ImageIssue is one image on a docs page that the vision relevance pass
// flagged as misleading: the image's actual contents do not match the prose
// describing it. Index is a stable per-page identifier ("img-1", "img-2", …)
// numbered globally across all batches sent for the page so verdicts and
// issues from different batches can be merged without collision.
type ImageIssue struct {
	PageURL         string `json:"page_url"`
	Index           string `json:"index"`
	Src             string `json:"src"`
	Reason          string `json:"reason"`
	SuggestedAction string `json:"suggested_action"`
}

// ImageVerdict is the per-image relevance verdict from the vision pass.
// Matches=true means the surrounding prose accurately describes what the
// image actually shows. Used downstream to suppress redundant
// missing-screenshot suggestions when an existing image already covers the
// passage.
type ImageVerdict struct {
	Index   string `json:"index"`
	Matches bool   `json:"matches"`
}

// relevancePassResponse is the wire shape for one batch of vision relevance
// findings. Wraps the two arrays so the JSON Schema root can be an object,
// which all provider structured-output paths require.
type relevancePassResponse struct {
	ImageIssues []ImageIssue   `json:"image_issues"`
	Verdicts    []ImageVerdict `json:"verdicts"`
}

// PROMPT SCHEMA: output shape for the vision relevance pass.
var relevancePassSchema = JSONSchema{
	Name: "screenshot_image_relevance",
	Doc: json.RawMessage(`{
      "type": "object",
      "properties": {
        "image_issues": {
          "type": "array",
          "items": {
            "type": "object",
            "properties": {
              "index":            {"type": "string"},
              "src":              {"type": "string"},
              "reason":           {"type": "string"},
              "suggested_action": {"type": "string"}
            },
            "required": ["index", "src", "reason", "suggested_action"],
            "additionalProperties": false
          }
        },
        "verdicts": {
          "type": "array",
          "items": {
            "type": "object",
            "properties": {
              "index":   {"type": "string"},
              "matches": {"type": "boolean"}
            },
            "required": ["index", "matches"],
            "additionalProperties": false
          }
        }
      },
      "required": ["image_issues", "verdicts"],
      "additionalProperties": false
    }`),
}

// buildRelevancePrompt assembles the multimodal prompt for one batch in the
// vision relevance pass. startIdx is the 0-based offset of the first image in
// this batch within the page's full image list, so the model emits indices
// numbered globally across batches (img-1, img-2, …) and downstream merging
// stays collision-free.
func buildRelevancePrompt(page DocPage, batch []imageRef, startIdx int) string {
	first := startIdx + 1
	last := startIdx + len(batch)

	var refsList []string
	for i, r := range batch {
		idx := startIdx + i + 1
		section := r.SectionHeading
		if section == "" {
			section = "(no heading)"
		}
		refsList = append(refsList, fmt.Sprintf("- img-%d: src=%q alt=%q section=%q paragraph=%d",
			idx, r.Src, r.AltText, section, r.ParagraphIndex))
	}
	refsBlock := strings.Join(refsList, "\n")

	// PROMPT: Vision relevance pass — for each image in this batch, decide
	// whether the surrounding prose accurately describes what the image
	// actually shows. Flag mismatches in image_issues; emit a verdict for
	// every image. Indices are numbered globally across batches so a single
	// page-level merge stays collision-free.
	return fmt.Sprintf(`You are reviewing images on a documentation page. For EACH image attached to this message, decide whether the page's prose accurately describes what the image actually shows.

URL: %s

Image index naming convention: each image is referenced as "img-N", numbered globally across the page. The first image attached to THIS message is img-%d; the last is img-%d. Use these exact indices in your response so verdicts from different batches merge cleanly.

Images in this batch (in order, paired with the attached image content):
%s

Page content:
%s

For EACH image in this batch, you MUST emit one entry in "verdicts":
- "index": the img-N identifier from the list above.
- "matches": true if the surrounding prose accurately describes what the image actually shows; false otherwise.

ONLY when matches=false, ALSO emit a corresponding entry in "image_issues":
- "index": the same img-N identifier.
- "src": the image's src attribute (copy verbatim from the list above).
- "reason": one short sentence naming what the image actually shows AND what the prose describes (the mismatch).
- "suggested_action": one of "replace", "recapture", or "remove" — pick the action that best resolves the mismatch.

Do not flag stylistic mismatches (cropping, theme, resolution). Only flag a substantive mismatch: the image depicts a different feature, a different page, a different state, or otherwise does not show what the prose claims.

If every image matches its prose, return "image_issues": [] and one matches=true verdict per image.`, page.URL, first, last, refsBlock, page.Content)
}

// relevancePass walks the page's images in batches of <=5 (Groq cap), issues
// one CompleteJSONMultimodal call per batch, and merges issues + verdicts
// across batches. Indices are numbered globally so merging is collision-free
// regardless of batch boundaries. Per-batch JSON parse errors are logged and
// skipped (fail-open) so one bad batch doesn't poison the page.
func relevancePass(ctx context.Context, client LLMClient, page DocPage, refs []imageRef) ([]ImageIssue, []ImageVerdict, error) {
	var issues []ImageIssue
	var verdicts []ImageVerdict
	startIdx := 0
	for batchN, batch := range splitImageBatches(refs, 5) {
		prompt := buildRelevancePrompt(page, batch, startIdx)
		blocks := make([]ContentBlock, 0, len(batch)+1)
		blocks = append(blocks, ContentBlock{Type: ContentBlockText, Text: prompt})
		for _, r := range batch {
			blocks = append(blocks, ContentBlock{Type: ContentBlockImageURL, ImageURL: r.Src})
		}
		msg := ChatMessage{Role: "user", ContentBlocks: blocks, Content: prompt}
		raw, err := client.CompleteJSONMultimodal(ctx, []ChatMessage{msg}, relevancePassSchema)
		if err != nil {
			return nil, nil, fmt.Errorf("relevancePass batch %d: %w", batchN, err)
		}
		var resp relevancePassResponse
		if err := json.Unmarshal(raw, &resp); err != nil {
			log.Warnf("relevancePass: invalid JSON for %s batch %d: %v", page.URL, batchN, err)
			startIdx += len(batch)
			continue
		}
		for i := range resp.ImageIssues {
			resp.ImageIssues[i].PageURL = page.URL
		}
		issues = append(issues, resp.ImageIssues...)
		verdicts = append(verdicts, resp.Verdicts...)
		startIdx += len(batch)
	}
	return issues, verdicts, nil
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

// ScreenshotResult bundles the outputs of one DetectScreenshotGaps run:
// the missing-screenshot findings rendered in screenshots.md, the image-issue
// findings from the vision relevance pass, and per-page audit stats used by
// the audit log line and the reporter.
type ScreenshotResult struct {
	MissingGaps []ScreenshotGap
	ImageIssues []ImageIssue
	AuditStats  []ScreenshotPageStats
}

// ScreenshotPageStats records what each per-page screenshot pass did. Emitted
// once per page after analysis completes; consumed by the audit log line in
// the CLI and by the reporter when deciding whether to render the
// `## Image Issues` section. VisionEnabled=false means the model lacked vision
// or the page had zero images, so RelevanceBatches and ImageIssues will be 0.
type ScreenshotPageStats struct {
	PageURL            string
	VisionEnabled      bool
	RelevanceBatches   int
	ImagesSeen         int
	ImageIssues        int
	MissingScreenshots int
	MissingSuppressed  int
}

// detectionPass runs the text-only screenshot-gap detection LLM call for one
// page. When verdicts is non-empty, the prompt is verdict-enriched and the
// response carries a suppressed_by_image array; when verdicts is nil it
// delegates to the legacy prompt and only the gaps array is populated. The
// returned `suppressed` count is for audit stats only — suppressed_by_image
// items are NOT rendered to the user. Per-page parse failures are logged and
// the function returns empty results with err=nil so one bad page doesn't
// poison the whole run; context / network errors propagate.
func detectionPass(
	ctx context.Context,
	client LLMClient,
	page DocPage,
	refs []imageRef,
	verdicts []ImageVerdict,
) (gaps []ScreenshotGap, suppressed int, err error) {
	coverage := buildCoverageMap(refs)
	content, ok := fitContentToBudget(page.URL, page.Content, coverage, ScreenshotPromptBudget)
	if !ok {
		log.Warnf("screenshot-gaps: skipping %s: prompt overhead exceeds budget", page.URL)
		return nil, 0, nil
	}
	prompt := buildDetectionPromptWithVerdicts(page.URL, content, refs, verdicts)
	raw, err := client.CompleteJSON(ctx, prompt, screenshotGapsSchema)
	if err != nil {
		return nil, 0, fmt.Errorf("DetectScreenshotGaps %s: %w", page.URL, err)
	}
	var resp screenshotGapsResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		log.Warnf("screenshot-gaps: skipping %s: invalid JSON response: %v", page.URL, err)
		return nil, 0, nil
	}
	gaps = make([]ScreenshotGap, 0, len(resp.Gaps))
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
	return gaps, len(resp.SuppressedByImage), nil
}

// DetectScreenshotGaps iterates pages sequentially. For each page, when the
// model has Vision capability and the page has images, the relevance pass
// runs first and its verdicts feed the verdict-enriched detection prompt;
// otherwise the detection pass runs against the legacy prompt. Per-page parse
// failures are logged and skipped (fail-open); context / network errors are
// returned immediately. Returns a ScreenshotResult bundling missing-screenshot
// gaps, vision image-issues, and per-page audit stats.
func DetectScreenshotGaps(
	ctx context.Context,
	client LLMClient,
	pages []DocPage,
	progress ScreenshotProgressFunc,
) (ScreenshotResult, error) {
	var result ScreenshotResult
	total := len(pages)
	for i, page := range pages {
		refs := extractImages(page.Content)
		stats := ScreenshotPageStats{
			PageURL:    page.URL,
			ImagesSeen: len(refs),
		}
		var verdicts []ImageVerdict
		if client.Capabilities().Vision && len(refs) > 0 {
			stats.VisionEnabled = true
			stats.RelevanceBatches = len(splitImageBatches(refs, 5))
			issues, vs, err := relevancePass(ctx, client, page, refs)
			if err != nil {
				return result, err
			}
			result.ImageIssues = append(result.ImageIssues, issues...)
			stats.ImageIssues = len(issues)
			verdicts = vs
		}
		gaps, suppressed, err := detectionPass(ctx, client, page, refs, verdicts)
		if err != nil {
			return result, err
		}
		stats.MissingScreenshots = len(gaps)
		stats.MissingSuppressed = suppressed
		result.MissingGaps = append(result.MissingGaps, gaps...)
		result.AuditStats = append(result.AuditStats, stats)
		if progress != nil {
			progress(i+1, total, page.URL)
		}
	}
	return result, nil
}
