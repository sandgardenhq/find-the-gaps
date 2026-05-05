package analyzer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

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

// SuppressionMinDimension is the minimum max(width, height) in pixels of
// a declared HTML attr that suggests an image is plausibly a screenshot
// rather than an inline icon or thumbnail. 400px is the inflection point
// between "decoration" and "deliberate page real estate" on docs sites.
const SuppressionMinDimension = 400

// SuppressionMinBytes is the minimum HEAD Content-Length (or inline data
// URI byte length) below which an image is assumed to be too small to be
// a screenshot. 30KB sits between "decoration" (icons, small logos) and
// "content" (UI screenshots typically 50-300KB).
const SuppressionMinBytes = 30 * 1024

// imageRef is one image occurrence on a docs page.
type imageRef struct {
	AltText        string
	Src            string
	SectionHeading string // most recent "# ..." or "## ..." heading above this image; "" if none
	ParagraphIndex int    // 0-based index of the paragraph block containing this image
	// OriginalIndex is the 1-based position of this ref in the page's
	// unfiltered image list (set by extractImages). The vision relevance
	// pass uses it to label images as "img-N" so verdicts stay aligned with
	// the unfiltered refs that the detection prompt iterates — even after
	// resolveVisionRefs / filterVisionSupportedImages drop unsupported or
	// unresolvable srcs.
	OriginalIndex int
	// DeclaredWidth and DeclaredHeight are the integer values of the HTML
	// width / height attrs, if present and parseable; zero otherwise.
	// Markdown ![]() syntax cannot carry dimensions, so refs from that
	// syntax always have zero values here.
	DeclaredWidth  int
	DeclaredHeight int
}

var markdownImageRe = regexp.MustCompile(`!\[([^\]]*)\]\(([^)]+)\)`)
var htmlImgRe = regexp.MustCompile(`(?i)<img\s+([^>]+?)>`)
var htmlAttrSrcRe = regexp.MustCompile(`(?i)\bsrc\s*=\s*(?:"([^"]*)"|'([^']*)')`)
var htmlAttrAltRe = regexp.MustCompile(`(?i)\balt\s*=\s*(?:"([^"]*)"|'([^']*)')`)
var htmlAttrWidthRe = regexp.MustCompile(`(?i)\bwidth\s*=\s*(?:"([^"]*)"|'([^']*)')`)
var htmlAttrHeightRe = regexp.MustCompile(`(?i)\bheight\s*=\s*(?:"([^"]*)"|'([^']*)')`)
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
			w, h := 0, 0
			if mm := htmlAttrWidthRe.FindStringSubmatch(attrs); mm != nil {
				v := mm[1]
				if v == "" {
					v = mm[2]
				}
				if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
					w = n
				}
			}
			if mm := htmlAttrHeightRe.FindStringSubmatch(attrs); mm != nil {
				v := mm[1]
				if v == "" {
					v = mm[2]
				}
				if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
					h = n
				}
			}
			refs = append(refs, imageRef{
				AltText:        alt,
				Src:            src,
				DeclaredWidth:  w,
				DeclaredHeight: h,
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

	for i := range refs {
		refs[i].OriginalIndex = i + 1
	}
	return refs
}

// visionUnsupportedExts is the set of image extensions Anthropic's vision API
// rejects with "The file format is invalid or unsupported". Anthropic accepts
// only jpeg, png, gif, and webp; everything else (vector, next-gen, legacy)
// errors out and aborts the relevance batch. Filtering by extension is the
// minimal, no-network defense.
var visionUnsupportedExts = map[string]struct{}{
	".svg":  {},
	".avif": {},
	".ico":  {},
	".bmp":  {},
	".tif":  {},
	".tiff": {},
	".heic": {},
	".heif": {},
}

// visionUnsupportedDataMimes is the set of data: URI mime types Anthropic
// rejects. Mirrors visionUnsupportedExts for the inline-image path.
var visionUnsupportedDataMimes = map[string]struct{}{
	"image/svg+xml": {},
	"image/avif":    {},
	"image/x-icon":  {},
	"image/bmp":     {},
	"image/tiff":    {},
	"image/heic":    {},
	"image/heif":    {},
}

// suppressionEligible reports whether a given imageRef should be routed
// through the unanalyzable-image suppression layer instead of the vision
// relevance pass. Eligible images are: vision-unsupported formats (per
// visionUnsupportedExts / visionUnsupportedDataMimes) and ALL GIFs.
// GIFs are eligible because every vision provider treats them as a single
// still (typically the first frame), which silently misleads on animated
// demos; the suppression layer's bytes-and-dimensions heuristic is more
// honest than a first-frame relevance verdict.
func suppressionEligible(r imageRef) bool {
	src := strings.TrimSpace(r.Src)
	if src == "" {
		return false
	}
	if strings.HasPrefix(src, "data:") {
		mimeEnd := strings.IndexAny(src[len("data:"):], ";,")
		if mimeEnd < 0 {
			return false
		}
		mime := strings.ToLower(src[len("data:") : len("data:")+mimeEnd])
		if mime == "image/gif" {
			return true
		}
		_, bad := visionUnsupportedDataMimes[mime]
		return bad
	}
	u, err := url.Parse(src)
	if err != nil {
		return false
	}
	ext := strings.ToLower(path.Ext(u.Path))
	if ext == ".gif" {
		return true
	}
	_, bad := visionUnsupportedExts[ext]
	return bad
}

// htmlAttrsSuggestScreenshot reports whether an imageRef's declared
// width / height attrs cross the screenshot-shaped threshold. Either
// dimension alone is sufficient.
func htmlAttrsSuggestScreenshot(r imageRef) bool {
	larger := r.DeclaredWidth
	if r.DeclaredHeight > larger {
		larger = r.DeclaredHeight
	}
	return larger >= SuppressionMinDimension
}

// headSuggestsScreenshot probes a single image URL with HEAD and reports
// whether its Content-Length crosses SuppressionMinBytes. Data URIs short-
// circuit and use the inline byte length without issuing a request.
//
// Failure semantics (matches the design's "no signal -> no suppression"
// rule): missing Content-Length on a 2xx response returns (false, nil) so
// the caller treats it as no signal. Transport errors and non-2xx responses
// return (false, err) so the caller can log them but still falls through
// to no-suppression — the orchestrator does not propagate the error.
func headSuggestsScreenshot(ctx context.Context, client *http.Client, src string) (bool, error) {
	src = strings.TrimSpace(src)
	if strings.HasPrefix(src, "data:") {
		// Inline byte length is a strict lower bound (base64 inflates by
		// ~33%, so the real payload is even smaller); for the suppression
		// threshold this is fine.
		return len(src) >= SuppressionMinBytes, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, src, nil)
	if err != nil {
		return false, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, fmt.Errorf("HEAD %s: status %d", src, resp.StatusCode)
	}
	cl := resp.Header.Get("Content-Length")
	if cl == "" {
		return false, nil
	}
	n, err := strconv.ParseInt(cl, 10, 64)
	if err != nil {
		return false, nil
	}
	return n >= SuppressionMinBytes, nil
}

// decisionForImageRef applies the design's signal precedence: HTML attrs
// win, HEAD as fallback, no signal -> no suppression. HEAD errors are
// swallowed (logged at debug) because the design's "no signal -> no
// suppression" rule means failure is operationally identical to absence.
func decisionForImageRef(ctx context.Context, client *http.Client, r imageRef) bool {
	if htmlAttrsSuggestScreenshot(r) {
		return true
	}
	ok, err := headSuggestsScreenshot(ctx, client, r.Src)
	if err != nil {
		log.Debugf("suppression: HEAD failed for %s: %v", r.Src, err)
		return false
	}
	return ok
}

// SuppressionConcurrencyCap is the maximum number of in-flight HEAD
// requests for the suppression decider. Image-heavy pages can have
// dozens of unanalyzable images; a small cap prevents fan-out storms.
const SuppressionConcurrencyCap = 8

// decideAllSuppressions runs decisionForImageRef for every input ref in
// parallel, deduplicating by absolute Src so one image referenced from
// N pages produces a single HEAD. The returned slice is index-aligned
// with refs. concurrencyCap is the maximum in-flight HEAD count
// (0 -> default).
func decideAllSuppressions(ctx context.Context, client *http.Client, refs []imageRef, concurrencyCap int) []bool {
	if concurrencyCap <= 0 {
		concurrencyCap = SuppressionConcurrencyCap
	}
	out := make([]bool, len(refs))
	if len(refs) == 0 {
		return out
	}
	type cached struct {
		done <-chan struct{}
		val  bool
	}
	cache := make(map[string]*cached)
	var mu sync.Mutex
	sem := make(chan struct{}, concurrencyCap)
	var wg sync.WaitGroup
	for i, r := range refs {
		i, r := i, r
		mu.Lock()
		c, ok := cache[r.Src]
		if !ok {
			ch := make(chan struct{})
			c = &cached{done: ch}
			cache[r.Src] = c
			mu.Unlock()
			wg.Add(1)
			go func() {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				v := decisionForImageRef(ctx, client, r)
				mu.Lock()
				c.val = v
				mu.Unlock()
				close(ch)
			}()
		} else {
			mu.Unlock()
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-c.done
			mu.Lock()
			out[i] = c.val
			mu.Unlock()
		}()
	}
	wg.Wait()
	return out
}

// partitionRefsForVision splits image refs by whether they should go through
// the vision relevance pass (returned first) or the unanalyzable-image
// suppression layer (returned second). Order is preserved within each
// returned slice; OriginalIndex values are inherited unchanged from the
// input refs so synthetic verdicts emitted from the suppression path align
// with the same global "img-N" numbering the detection prompt iterates.
func partitionRefsForVision(refs []imageRef) (visionPath, suppressionPath []imageRef) {
	for _, r := range refs {
		if suppressionEligible(r) {
			suppressionPath = append(suppressionPath, r)
		} else {
			visionPath = append(visionPath, r)
		}
	}
	return visionPath, suppressionPath
}

// resolveImageSrc converts a possibly-relative image src into an absolute URL
// usable by Bifrost / Anthropic's vision API. Returns ok=false when the src is
// empty/whitespace/fragment-only or when a relative src cannot be resolved
// against pageURL (e.g. unparseable page URL, or page URL itself has no host).
// Data URIs and already-absolute URLs are returned verbatim.
func resolveImageSrc(pageURL, src string) (string, bool) {
	src = strings.TrimSpace(src)
	if src == "" || strings.HasPrefix(src, "#") {
		return "", false
	}
	if strings.HasPrefix(src, "data:") {
		return src, true
	}
	ref, err := url.Parse(src)
	if err != nil {
		return "", false
	}
	if ref.IsAbs() {
		return ref.String(), true
	}
	base, err := url.Parse(pageURL)
	if err != nil || !base.IsAbs() || base.Host == "" {
		return "", false
	}
	return base.ResolveReference(ref).String(), true
}

// resolveVisionRefs returns a copy of refs with each Src resolved against
// pageURL. Refs whose src cannot be resolved (empty, fragment-only, or
// relative against an unparseable page URL) are dropped — sending an
// unresolvable src to the vision API guarantees an "invalid image" error.
func resolveVisionRefs(pageURL string, refs []imageRef) []imageRef {
	if len(refs) == 0 {
		return refs
	}
	out := make([]imageRef, 0, len(refs))
	for _, r := range refs {
		abs, ok := resolveImageSrc(pageURL, r.Src)
		if !ok {
			continue
		}
		r.Src = abs
		out = append(out, r)
	}
	return out
}

// filterVisionSupportedImages drops imageRefs whose Src is a known-unsupported
// format for Anthropic's vision API, preventing a single bad image from
// erroring out the whole relevance batch. Unknown extensions and extensionless
// URLs are kept (inclusive-by-default): the upstream API can still fetch them,
// and dropping them would silently shrink coverage.
func filterVisionSupportedImages(refs []imageRef) []imageRef {
	if len(refs) == 0 {
		return refs
	}
	out := make([]imageRef, 0, len(refs))
	for _, r := range refs {
		if visionSupported(r.Src) {
			out = append(out, r)
		}
	}
	return out
}

// visionSupported reports whether a single src URL/data-URI is acceptable to
// Anthropic's vision API. Returns false only for KNOWN-unsupported formats so
// we don't drop legitimate images served from extensionless CDN URLs.
func visionSupported(src string) bool {
	if rest, ok := strings.CutPrefix(src, "data:"); ok {
		// data:<mime>[;params],<payload>
		mime := rest
		if i := strings.IndexAny(rest, ";,"); i >= 0 {
			mime = rest[:i]
		}
		_, bad := visionUnsupportedDataMimes[strings.ToLower(mime)]
		return !bad
	}
	// Strip query and fragment before extension check.
	path := src
	if i := strings.IndexAny(path, "?#"); i >= 0 {
		path = path[:i]
	}
	dot := strings.LastIndex(path, ".")
	if dot < 0 {
		return true
	}
	ext := strings.ToLower(path[dot:])
	_, bad := visionUnsupportedExts[ext]
	return !bad
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

	// PROMPT: Identifies passages in a documentation page where a screenshot earns its place — multi-step flows, non-obvious UI layouts, visual-recognition asks, first-run confirmations whose target state is hard to describe in words. Applies a locality rule: a passage may be covered when an existing image's alt text plausibly matches the topic AND the image appears in the same section heading or within 3 paragraphs before/after. Selective by design: most pages should produce zero gaps. When in doubt, do not flag.
	return fmt.Sprintf(`You are reviewing a documentation page to find the small number of places where a screenshot would meaningfully help the reader — places where prose alone leaves a real gap. Be selective. Most pages should produce zero gaps.

URL: %s

Existing images on this page (if any):
%s

Page content:
%s

A passage may already be visually covered by an existing image, but ONLY when the image's alt text and src plausibly describe the same UI moment as the passage AND the image appears in the same section heading or within 3 paragraphs before/after. An off-topic nearby image (e.g., an architecture diagram next to UI-walkthrough prose) does NOT cover the passage.

Flag a passage ONLY when at least one is true AND the prose by itself leaves a competent reader unable to picture the result:
1. MULTI-STEP FLOW: a sequence of two or more user actions across changing UI states where the reader needs to see intermediate states to stay oriented (a wizard, an OAuth handshake, guided onboarding).
2. NON-OBVIOUS UI LAYOUT: a screen, dashboard, panel, or form whose layout, structure, or visual relationships cannot be reasonably reconstructed from prose — multiple panels, charts whose shape matters, complex error/success states with specific structure.
3. VISUAL RECOGNITION: the reader is asked to recognize, locate, or identify something they cannot reconstruct from text alone ("look for the red banner", "find the gear icon", "the chart should resemble this shape").
4. FIRST-RUN CONFIRMATION: the prose's payoff is recognizing a specific screen or visual state that confirms setup succeeded — and that state is non-trivial to describe in words.

Do NOT flag:
- Single-action interactions ("click Save", "press Enter", "fill in the email field").
- Terminal sessions whose output is already shown inline in a code block.
- Reference material (API signatures, option tables, type listings).
- Pure conceptual prose with no UI moment.
- Generic "you'll see the result" sentences where the result is already described in prose or shown in a code block.
- Any UI moment a competent reader can picture from the prose alone.
- Passages already covered by a topically-matching image under the locality rule above.

Populate "gaps" with one object per gap. Each object must have:
- "quoted_passage": the exact verbatim quote from the page. Do not paraphrase.
- "should_show": specific description of the screenshot — name visible elements, values, states, panels. Not "a screenshot of the feature".
- "suggested_alt": alt text / caption, under 100 characters.
- "insertion_hint": where to paste the image, referencing existing prose. Example: "after the paragraph ending '…click Save.'" Do not use line numbers.

When in doubt, do not flag.`, pageURL, coverageSummary, content)
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

	// PROMPT: Verdict-enriched screenshot-gap detection. A vision model has already inspected each existing image and emitted an authoritative verdict. The first question for every UI moment in the prose is: is there an existing image on this page whose verdict is "matches" and that sits near the passage? If yes, suppress (record under "suppressed_by_image"). If no, only THEN consider whether a screenshot would earn its place under the selective triggers below. "verdict: does not match" images do NOT cover their surrounding prose — treat them as if absent. Selective by design: most pages should produce zero gaps. When in doubt, do not flag.
	return fmt.Sprintf(`You are reviewing a documentation page to find the small number of places where a screenshot would meaningfully help the reader — places where prose alone leaves a real gap. Be selective. Most pages should produce zero gaps.

URL: %s

Existing images on this page, each annotated with the relevance-pass verdict (a vision model already inspected the actual image contents):
%s

Page content:
%s

The verdicts above are AUTHORITATIVE. Do not second-guess them based on filename, alt text, or location:
- "verdict: matches" — the image's actual contents accurately depict what the surrounding prose describes. The passage IS visually covered by this image.
- "verdict: does not match" — the image's actual contents do NOT depict what the surrounding prose describes. Treat the passage as uncovered, exactly as if the image were absent.
- "verdict: unknown" — fall back to the locality rule: a passage is covered only when an image's alt text plausibly matches the topic AND the image appears in the same section heading or within 3 paragraphs before/after.

KEY QUESTION for every UI moment in the prose: is there an existing image on this page whose verdict is "matches" and that sits in the same section heading or within 3 paragraphs of the passage? If yes, the passage is already covered — do NOT add it to "gaps"; record it in "suppressed_by_image" instead. If no, only THEN consider whether a screenshot would earn its place under the selective triggers below.

Flag a passage ONLY when at least one is true AND the prose by itself leaves a competent reader unable to picture the result:
1. MULTI-STEP FLOW: a sequence of two or more user actions across changing UI states where the reader needs to see intermediate states to stay oriented (a wizard, an OAuth handshake, guided onboarding).
2. NON-OBVIOUS UI LAYOUT: a screen, dashboard, panel, or form whose layout, structure, or visual relationships cannot be reasonably reconstructed from prose — multiple panels, charts whose shape matters, complex error/success states with specific structure.
3. VISUAL RECOGNITION: the reader is asked to recognize, locate, or identify something they cannot reconstruct from text alone ("look for the red banner", "find the gear icon", "the chart should resemble this shape").
4. FIRST-RUN CONFIRMATION: the prose's payoff is recognizing a specific screen or visual state that confirms setup succeeded — and that state is non-trivial to describe in words.

Do NOT flag:
- Single-action interactions ("click Save", "press Enter", "fill in the email field").
- Terminal sessions whose output is already shown inline in a code block.
- Reference material (API signatures, option tables, type listings).
- Pure conceptual prose with no UI moment.
- Generic "you'll see the result" sentences where the result is already described in prose or shown in a code block.
- Any UI moment a competent reader can picture from the prose alone.
- Passages where a "verdict: matches" image already sits in the same section heading or within 3 paragraphs (record these in "suppressed_by_image" instead of "gaps").

Populate "gaps" with one object per gap (a passage that should have a screenshot AND no "verdict: matches" image already covers it). Each object must have:
- "quoted_passage": the exact verbatim quote from the page. Do not paraphrase.
- "should_show": specific description of the screenshot — name visible elements, values, states, panels. Not "a screenshot of the feature".
- "suggested_alt": alt text / caption, under 100 characters.
- "insertion_hint": where to paste the image, referencing existing prose. Example: "after the paragraph ending '…click Save.'" Do not use line numbers.

Populate "suppressed_by_image" with one object per moment that you WOULD have flagged under the rules above EXCEPT that a "verdict: matches" image already covers it. Same four fields as "gaps". This list is for audit stats only; it is NOT rendered to users.

When in doubt, do not flag.`, pageURL, coverageSummary, content)
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
// vision relevance pass. Each image's "img-N" label comes from its
// OriginalIndex (its 1-based position in the page's unfiltered image list),
// so verdicts emitted by the model stay aligned with the indices the
// detection prompt uses — even when filtering has dropped images that sit
// between two surviving ones (a sparse batch like img-1, img-3 is normal).
func buildRelevancePrompt(page DocPage, batch []imageRef) string {
	first, last := 0, 0
	var refsList []string
	for i, r := range batch {
		idx := r.OriginalIndex
		if i == 0 || idx < first {
			first = idx
		}
		if idx > last {
			last = idx
		}
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
// across batches. Each image's img-N label is its OriginalIndex in the
// unfiltered refs list, so verdicts merge cleanly across batches and align
// with the indices buildDetectionPromptWithVerdicts uses downstream — even
// when filtering has dropped some refs before this pass. Per-batch JSON parse
// errors and per-batch LLM/transport errors (e.g. Bifrost cannot download an
// image URL) are logged and skipped (fail-open) so one bad batch — or one
// bad image — doesn't poison the page or abort the whole run.
func relevancePass(ctx context.Context, client LLMClient, page DocPage, refs []imageRef) ([]ImageIssue, []ImageVerdict, error) {
	var issues []ImageIssue
	var verdicts []ImageVerdict
	for batchN, batch := range splitImageBatches(refs, 5) {
		prompt := buildRelevancePrompt(page, batch)
		blocks := make([]ContentBlock, 0, len(batch)+1)
		blocks = append(blocks, ContentBlock{Type: ContentBlockText, Text: prompt})
		for _, r := range batch {
			blocks = append(blocks, ContentBlock{Type: ContentBlockImageURL, ImageURL: r.Src})
		}
		msg := ChatMessage{Role: "user", ContentBlocks: blocks}
		raw, err := client.CompleteJSONMultimodal(ctx, []ChatMessage{msg}, relevancePassSchema)
		if err != nil {
			// Context cancellation/deadline must still abort — the caller
			// is shutting down, not asking us to retry. Everything else
			// (Bifrost transport errors, "Unable to download the file",
			// upstream 5xx, etc.) is per-batch and fail-open: log and
			// move on so one bad image src cannot kill the whole run.
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, nil, fmt.Errorf("relevancePass batch %d: %w", batchN, err)
			}
			log.Warnf("relevancePass: skipping %s batch %d: %v", page.URL, batchN, err)
			continue
		}
		var resp relevancePassResponse
		if err := json.Unmarshal(raw, &resp); err != nil {
			log.Warnf("relevancePass: invalid JSON for %s batch %d: %v", page.URL, batchN, err)
			continue
		}
		for i := range resp.ImageIssues {
			resp.ImageIssues[i].PageURL = page.URL
			resp.ImageIssues[i].Reason = unescapeLiteralWhitespace(resp.ImageIssues[i].Reason)
			resp.ImageIssues[i].SuggestedAction = unescapeLiteralWhitespace(resp.ImageIssues[i].SuggestedAction)
		}
		issues = append(issues, resp.ImageIssues...)
		verdicts = append(verdicts, resp.Verdicts...)
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
	MissingGaps     []ScreenshotGap
	PossiblyCovered []ScreenshotGap
	ImageIssues     []ImageIssue
	AuditStats      []ScreenshotPageStats
}

// ScreenshotPageStats records what each per-page screenshot pass did. Emitted
// once per page after analysis completes; consumed by the audit log line in
// the CLI and by the reporter when deciding whether to render the
// `## Image Issues` section. VisionEnabled=false means the model lacked vision
// or the page had zero images, so RelevanceBatches and ImageIssues will be 0.
// DetectionSkipped=true means the page's prompt overhead exceeded
// ScreenshotPromptBudget so the detection LLM call was never issued; this
// distinguishes a budget skip from a clean run with zero findings.
type ScreenshotPageStats struct {
	PageURL            string
	VisionEnabled      bool
	RelevanceBatches   int
	ImagesSeen         int
	ImageIssues        int
	MissingScreenshots int
	PossiblyCovered    int
	DetectionSkipped   bool
}

// detectionPass runs the text-only screenshot-gap detection LLM call for one
// page. When verdicts is non-empty, the prompt is verdict-enriched and the
// response carries a suppressed_by_image array; when verdicts is nil it
// delegates to the legacy prompt and only the gaps array is populated. The
// returned `suppressed` slice carries the suppressed_by_image items the
// model would have flagged as missing screenshots if not for an existing
// image whose verdict was matches=true. Items are unescaped with the same
// treatment as `gaps` and have PageURL / PagePath set; whether they are
// rendered to the user is the caller's decision. The returned `skipped` is
// true when the page's prompt overhead exceeded ScreenshotPromptBudget and
// the LLM call was not issued; the caller surfaces this in audit stats so
// the audit log line can distinguish a budget skip from a clean
// zero-findings result. Per-page parse failures are logged and the function
// returns empty results with err=nil so one bad page doesn't poison the
// whole run; context / network errors propagate.
func detectionPass(
	ctx context.Context,
	client LLMClient,
	page DocPage,
	refs []imageRef,
	verdicts []ImageVerdict,
) (gaps []ScreenshotGap, suppressed []ScreenshotGap, skipped bool, err error) {
	coverage := buildCoverageMap(refs)
	content, ok := fitContentToBudget(page.URL, page.Content, coverage, ScreenshotPromptBudget)
	if !ok {
		log.Warnf("screenshot-gaps: skipping %s: prompt overhead exceeds budget", page.URL)
		return nil, nil, true, nil
	}
	prompt := buildDetectionPromptWithVerdicts(page.URL, content, refs, verdicts)
	raw, err := client.CompleteJSON(ctx, prompt, screenshotGapsSchema)
	if err != nil {
		return nil, nil, false, fmt.Errorf("DetectScreenshotGaps %s: %w", page.URL, err)
	}
	var resp screenshotGapsResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		log.Warnf("screenshot-gaps: skipping %s: invalid JSON response: %v", page.URL, err)
		return nil, nil, false, nil
	}
	gaps = make([]ScreenshotGap, 0, len(resp.Gaps))
	for _, it := range resp.Gaps {
		gaps = append(gaps, ScreenshotGap{
			PageURL:       page.URL,
			PagePath:      page.Path,
			QuotedPassage: unescapeLiteralWhitespace(it.QuotedPassage),
			ShouldShow:    unescapeLiteralWhitespace(it.ShouldShow),
			SuggestedAlt:  unescapeLiteralWhitespace(it.SuggestedAlt),
			InsertionHint: unescapeLiteralWhitespace(it.InsertionHint),
		})
	}
	suppressed = make([]ScreenshotGap, 0, len(resp.SuppressedByImage))
	for _, it := range resp.SuppressedByImage {
		suppressed = append(suppressed, ScreenshotGap{
			PageURL:       page.URL,
			PagePath:      page.Path,
			QuotedPassage: unescapeLiteralWhitespace(it.QuotedPassage),
			ShouldShow:    unescapeLiteralWhitespace(it.ShouldShow),
			SuggestedAlt:  unescapeLiteralWhitespace(it.SuggestedAlt),
			InsertionHint: unescapeLiteralWhitespace(it.InsertionHint),
		})
	}
	return gaps, suppressed, false, nil
}

// unescapeLiteralWhitespace converts the two-character escape sequences \n,
// \r, and \t (backslash + letter) into their real whitespace counterparts.
// Models occasionally emit these sequences as text inside a JSON string value
// instead of producing the actual character; without this normalization, the
// literal `\n` text leaks into screenshots.md and the rendered Hugo page,
// where it shows up as a backslash-n instead of a paragraph break.
func unescapeLiteralWhitespace(s string) string {
	r := strings.NewReplacer(`\n`, "\n", `\r`, "\r", `\t`, "\t")
	return r.Replace(s)
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

		// Partition refs: vision-supported (and non-GIF) take the relevance
		// pass; GIFs and vision-unsupported formats take the suppression
		// path so we don't ask a vision provider to judge a frame it cannot
		// reliably render.
		visionPathRefs, suppressionPathRefs := partitionRefsForVision(refs)

		var verdicts []ImageVerdict
		if client.Capabilities().Vision && len(visionPathRefs) > 0 {
			stats.VisionEnabled = true
			// Two-step prep before the vision call:
			//   1. Resolve relative srcs against page.URL — Bifrost can't
			//      fetch "/static/foo.png" without a base; it ends up
			//      base64-encoding an HTML 404 which Anthropic rejects.
			//   2. Drop formats Anthropic's vision API rejects (SVG, AVIF,
			//      ICO, etc.) — one bad image otherwise errors the whole
			//      batch with "image.source.base64.data: The file format is
			//      invalid or unsupported".
			// Detection still sees the unfiltered refs, since the text-only
			// pass doesn't ship pixels.
			visionRefs := resolveVisionRefs(page.URL, visionPathRefs)
			visionRefs = filterVisionSupportedImages(visionRefs)
			stats.RelevanceBatches = len(splitImageBatches(visionRefs, 5))
			issues, vs, err := relevancePass(ctx, client, page, visionRefs)
			if err != nil {
				return result, err
			}
			result.ImageIssues = append(result.ImageIssues, issues...)
			stats.ImageIssues = len(issues)
			verdicts = vs
		}

		// Suppression decisions for unanalyzable images. Runs even when the
		// model lacks vision capability — the heuristic is text/HEAD only.
		// Only decided=true refs emit a synthetic matches=true verdict;
		// decided=false means "no signal" (NOT "definitely not a screenshot")
		// so we omit the verdict and let the locality rule apply.
		if len(suppressionPathRefs) > 0 {
			headCtx, cancel := context.WithTimeout(ctx, 5*time.Second*time.Duration(len(suppressionPathRefs)))
			decisions := decideAllSuppressions(headCtx, http.DefaultClient, suppressionPathRefs, SuppressionConcurrencyCap)
			for j, r := range suppressionPathRefs {
				if decisions[j] {
					verdicts = append(verdicts, ImageVerdict{
						Index:   fmt.Sprintf("img-%d", r.OriginalIndex),
						Matches: true,
					})
				}
			}
			cancel()
		}

		gaps, suppressed, skipped, err := detectionPass(ctx, client, page, refs, verdicts)
		if err != nil {
			return result, err
		}
		stats.MissingScreenshots = len(gaps)
		stats.PossiblyCovered = len(suppressed)
		stats.DetectionSkipped = skipped
		result.MissingGaps = append(result.MissingGaps, gaps...)
		result.PossiblyCovered = append(result.PossiblyCovered, suppressed...)
		result.AuditStats = append(result.AuditStats, stats)
		if progress != nil {
			progress(i+1, total, page.URL)
		}
	}
	return result, nil
}
