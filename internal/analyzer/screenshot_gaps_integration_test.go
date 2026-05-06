package analyzer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scriptedClient is a minimal LLMClient that returns canned JSON payloads
// from CompleteJSONMultimodal (vision relevance pass) and CompleteJSON
// (text-only detection pass). The vision and detection counts are tracked
// independently so tests can assert that DetectScreenshotGaps issues the
// expected number of calls per branch.
//
// multimodalResps drives per-call responses for the relevance pass: call N
// returns multimodalResps[N]. When N exceeds the slice length the last entry
// is returned (so a one-element slice is broadcast across all batches). This
// lets a test thread one issue through one specific batch and empty results
// through the rest.
type scriptedClient struct {
	caps            ModelCapabilities
	multimodalResps []json.RawMessage
	detectionResp   json.RawMessage
	multimodalCalls atomic.Int64
	detectionCalls  atomic.Int64
	lastSchemaName  string
	multimodalMsgs  [][]ChatMessage
}

func (s *scriptedClient) Complete(_ context.Context, _ string) (string, error) {
	return "", nil
}

func (s *scriptedClient) CompleteJSON(_ context.Context, _ string, schema JSONSchema) (json.RawMessage, error) {
	s.detectionCalls.Add(1)
	s.lastSchemaName = schema.Name
	return s.detectionResp, nil
}

func (s *scriptedClient) CompleteJSONMultimodal(ctx context.Context, msgs []ChatMessage, schema JSONSchema) (json.RawMessage, error) {
	// Honor ctx cancellation so tests can pin the propagation contract: a
	// cancelled context inside the vision relevance pass must surface as an
	// error from DetectScreenshotGaps without partial state.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	idx := int(s.multimodalCalls.Add(1) - 1)
	s.lastSchemaName = schema.Name
	dup := make([]ChatMessage, len(msgs))
	copy(dup, msgs)
	s.multimodalMsgs = append(s.multimodalMsgs, dup)
	if idx >= len(s.multimodalResps) {
		idx = len(s.multimodalResps) - 1
	}
	return s.multimodalResps[idx], nil
}

func (s *scriptedClient) Capabilities() ModelCapabilities { return s.caps }

func TestDetectScreenshotGaps_VisionBranchEmitsImageIssuesAndAuditStats(t *testing.T) {
	// First batch (img-1..img-5) carries one mismatch on img-2; second batch
	// (img-6) is clean. Verdicts cover every image so the detection pass can
	// reason about coverage globally.
	batch1Resp := json.RawMessage(`{
	  "image_issues": [
	    {"index":"img-2","src":"img-1.png","reason":"shows dashboard, prose describes settings","suggested_action":"replace"}
	  ],
	  "verdicts": [
	    {"index":"img-1","matches":true},
	    {"index":"img-2","matches":false},
	    {"index":"img-3","matches":true},
	    {"index":"img-4","matches":true},
	    {"index":"img-5","matches":true}
	  ]
	}`)
	batch2Resp := json.RawMessage(`{
	  "image_issues": [],
	  "verdicts": [
	    {"index":"img-6","matches":true}
	  ]
	}`)
	detectionResp := json.RawMessage(`{
	  "gaps": [
	    {"quoted_passage":"Click Save.","should_show":"the Save modal","suggested_alt":"Save modal","insertion_hint":"after the Save paragraph","priority":"medium","priority_reason":"test stub"}
	  ],
	  "suppressed_by_image": [
	    {"quoted_passage":"View the dashboard.","should_show":"dashboard","suggested_alt":"Dashboard","insertion_hint":"after the dashboard paragraph","priority":"small","priority_reason":"test stub"}
	  ]
	}`)
	client := &scriptedClient{
		caps:            ModelCapabilities{Vision: true},
		multimodalResps: []json.RawMessage{batch1Resp, batch2Resp},
		detectionResp:   detectionResp,
	}

	// Build a single page with 6 markdown images, distinct paragraphs to
	// keep ParagraphIndex stable.
	var b strings.Builder
	b.WriteString("# Page\n\n")
	for i := 0; i < 6; i++ {
		fmt.Fprintf(&b, "Paragraph %d.\n\n![alt-%d](img-%d.png)\n\n", i, i, i)
	}
	pages := []DocPage{{URL: "https://x/p", Path: "/tmp/p.md", Content: b.String()}}

	res, err := DetectScreenshotGaps(context.Background(), client, pages, nil)
	require.NoError(t, err)

	// Image issues from relevance pass were collected and tagged with the
	// page URL so the reporter can attribute them.
	require.Len(t, res.ImageIssues, 1)
	assert.Equal(t, "img-2", res.ImageIssues[0].Index)
	assert.Equal(t, "https://x/p", res.ImageIssues[0].PageURL)

	// Detection pass produced one missing gap.
	require.Len(t, res.MissingGaps, 1)
	assert.Equal(t, "https://x/p", res.MissingGaps[0].PageURL)
	assert.Equal(t, "Click Save.", res.MissingGaps[0].QuotedPassage)

	// Audit stats reflect the vision branch.
	require.Len(t, res.AuditStats, 1)
	assert.True(t, res.AuditStats[0].VisionEnabled)
	// 6 images at max=5 → 2 batches.
	assert.Equal(t, 2, res.AuditStats[0].RelevanceBatches)
	assert.Equal(t, 6, res.AuditStats[0].ImagesSeen)
	assert.Equal(t, 1, res.AuditStats[0].ImageIssues)
	assert.Equal(t, 1, res.AuditStats[0].MissingScreenshots)
	assert.Equal(t, 1, res.AuditStats[0].PossiblyCovered)
	assert.Equal(t, "https://x/p", res.AuditStats[0].PageURL)

	// Call count: 2 vision calls + 1 detection call.
	assert.Equal(t, int64(2), client.multimodalCalls.Load())
	assert.Equal(t, int64(1), client.detectionCalls.Load())
}

// TestDetectScreenshotGaps_BudgetSkippedPageMarkedSkipped pins the contract
// that a page whose prompt overhead exceeds ScreenshotPromptBudget is recorded
// in AuditStats with DetectionSkipped=true (and zero MissingScreenshots), so
// Task 12's audit log line can distinguish "skipped" from "clean". We also
// assert the detection-pass LLM call was NOT issued for the skipped page —
// proving the budget-skip path was taken rather than the model returning an
// empty result.
func TestDetectScreenshotGaps_BudgetSkippedPageMarkedSkipped(t *testing.T) {
	// Build a page whose content is well over the budget. The estimator counts
	// every character; ~4 chars/token in cl100k_base, so 4M chars ≈ 1M tokens,
	// which dwarfs the 150K budget and forces fitContentToBudget to return
	// ok=false (the overhead alone is fine; available > 100, but contentTokens
	// vastly exceeds available — wait: let me re-read the budget logic).
	//
	// fitContentToBudget returns ok=true and truncates if available >= 100;
	// it returns ok=false ONLY when overhead+margin already exceeds budget.
	// To force overhead > budget, we must inflate the prompt overhead — which
	// is driven by the coverage map and the URL. Easiest: a coverage map with
	// thousands of entries so the listed-images section blows past 150K tokens.
	var b strings.Builder
	b.WriteString("# Page\n\n")
	// Each image line in the coverage section is ~60 tokens; 5,000 images
	// produce ~300K tokens of overhead, well past the 150K budget.
	for i := 0; i < 5000; i++ {
		fmt.Fprintf(&b, "Paragraph %d.\n\n![alt-%d](https://example.com/very-long-image-path-to-bloat-overhead-%d.png)\n\n", i, i, i)
	}
	pages := []DocPage{{URL: "https://x/skipped", Path: "/tmp/skipped.md", Content: b.String()}}

	// Use the scriptedClient with no vision and a detection response that, if
	// erroneously called, would produce a finding. If the budget-skip path is
	// taken correctly, CompleteJSON is never invoked.
	client := &scriptedClient{
		caps:          ModelCapabilities{},
		detectionResp: json.RawMessage(`{"gaps":[{"quoted_passage":"X","should_show":"Y","suggested_alt":"Z","insertion_hint":"W","priority":"medium","priority_reason":"test stub"}]}`),
	}

	res, err := DetectScreenshotGaps(context.Background(), client, pages, nil)
	require.NoError(t, err)

	require.Len(t, res.AuditStats, 1)
	assert.True(t, res.AuditStats[0].DetectionSkipped, "budget-skipped page must be marked DetectionSkipped=true")
	assert.Equal(t, 0, res.AuditStats[0].MissingScreenshots)
	assert.Empty(t, res.MissingGaps, "no findings should be produced for a budget-skipped page")
	assert.Equal(t, int64(0), client.detectionCalls.Load(), "CompleteJSON must not be invoked for a budget-skipped page")
}

func TestDetectScreenshotGaps_NonVisionBranchUnchanged(t *testing.T) {
	client := &fakeLLMClient{
		responses: []string{
			`{"gaps":[{"quoted_passage":"Run the command.","should_show":"Terminal showing output.","suggested_alt":"Terminal","insertion_hint":"after the command block","priority":"medium","priority_reason":"test stub"}]}`,
		},
	}
	pages := []DocPage{
		{URL: "https://example.com/a", Path: "/tmp/a.md", Content: "# A\n\nRun the command.\n"},
	}
	res, err := DetectScreenshotGaps(context.Background(), client, pages, nil)
	require.NoError(t, err)

	// Non-vision branch: no image issues, no relevance batches.
	assert.Empty(t, res.ImageIssues)
	require.Len(t, res.AuditStats, 1)
	assert.False(t, res.AuditStats[0].VisionEnabled)
	assert.Equal(t, 0, res.AuditStats[0].RelevanceBatches)
	assert.Equal(t, 0, res.AuditStats[0].ImageIssues)

	// Missing gaps populated as before.
	require.Len(t, res.MissingGaps, 1)
	assert.Equal(t, "https://example.com/a", res.MissingGaps[0].PageURL)
	assert.Equal(t, "Run the command.", res.MissingGaps[0].QuotedPassage)
	assert.Equal(t, 1, res.AuditStats[0].MissingScreenshots)
}

// TestDetectScreenshotGaps_VisionPathContextCanceled pins the contract that a
// cancelled context inside the vision relevance pass propagates back through
// DetectScreenshotGaps as an error, with no partial state leaked. All prior
// context-cancellation tests use fakeLLMClient (Capabilities() == zero value)
// and never enter the vision branch; this test runs against a vision-capable
// client so the relevance pass is exercised.
//
// Note on TDD honesty: this test pins existing behavior — the current
// implementation already returns errors from CompleteJSONMultimodal — rather
// than driving new code. The "RED" here is the absence of a vision-branch
// cancellation test, which would silently regress if relevancePass were ever
// changed to swallow errors. Pinning the contract is the correct response.
func TestDetectScreenshotGaps_VisionPathContextCanceled(t *testing.T) {
	client := &scriptedClient{
		caps: ModelCapabilities{Vision: true},
		// Unused: ctx is cancelled before the call lands, so the multimodal
		// path returns ctx.Err() before reading multimodalResps. Provide a
		// non-nil entry to satisfy the slice-bounds invariant if it ever does.
		multimodalResps: []json.RawMessage{json.RawMessage(`{"image_issues":[],"verdicts":[]}`)},
		detectionResp:   json.RawMessage(`{"gaps":[]}`),
	}
	pages := []DocPage{{
		URL:     "https://x/p",
		Path:    "/tmp/p.md",
		Content: "# Page\n\n![alt](img.png)\n",
	}}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel BEFORE the call so the vision pass sees ctx.Err() immediately

	res, err := DetectScreenshotGaps(ctx, client, pages, nil)
	require.Error(t, err, "cancelled context inside vision pass must surface as error")
	assert.True(t, errors.Is(err, context.Canceled), "wrapped error must satisfy errors.Is(context.Canceled)")
	assert.Empty(t, res.ImageIssues, "no partial image-issue state on cancellation")
	assert.Empty(t, res.MissingGaps, "no partial missing-gap state on cancellation")
}

// TestDetectScreenshotGaps_VisionPathFiltersUnsupportedImageFormats pins the
// fix for the Anthropic "image.source.base64.data: The file format is invalid
// or unsupported" failure: SVG/AVIF/etc must be filtered out before the vision
// relevance call so one bad image cannot abort the whole batch. Detection-pass
// stats still see the full image count (ImagesSeen) — only the vision branch
// is filtered, since that's the only code path that actually sends pixels.
func TestDetectScreenshotGaps_VisionPathFiltersUnsupportedImageFormats(t *testing.T) {
	client := &scriptedClient{
		caps: ModelCapabilities{Vision: true},
		multimodalResps: []json.RawMessage{
			json.RawMessage(`{"image_issues":[],"verdicts":[{"index":"img-0","matches":true},{"index":"img-1","matches":true}]}`),
		},
		detectionResp: json.RawMessage(`{"gaps":[]}`),
	}
	pages := []DocPage{{
		URL:  "https://x/p",
		Path: "/tmp/p.md",
		Content: "# Page\n\nIntro.\n\n![dash](dashboard.png)\n\n" +
			"![logo](logo.svg)\n\n" +
			"![photo](photo.jpg)\n\n" +
			"![icon](favicon.ico)\n",
	}}

	res, err := DetectScreenshotGaps(context.Background(), client, pages, nil)
	require.NoError(t, err)

	// Exactly one vision call (the 2 PNG/JPG fit in one batch of 5 after SVG/ICO are filtered).
	require.Equal(t, int64(1), client.multimodalCalls.Load(), "should issue exactly one vision call after filtering")
	require.Len(t, client.multimodalMsgs, 1)
	require.Len(t, client.multimodalMsgs[0], 1, "one user message per call")

	var sentImageSrcs []string
	for _, b := range client.multimodalMsgs[0][0].ContentBlocks {
		if b.Type == ContentBlockImageURL {
			sentImageSrcs = append(sentImageSrcs, b.ImageURL)
		}
	}
	assert.Equal(t, []string{"https://x/dashboard.png", "https://x/photo.jpg"}, sentImageSrcs,
		"vision call must NOT receive SVG or ICO sources — Anthropic rejects them")

	// Audit: ImagesSeen counts the raw extracted images (4); RelevanceBatches
	// reflects the filtered count (2 → 1 batch).
	require.Len(t, res.AuditStats, 1)
	assert.Equal(t, 4, res.AuditStats[0].ImagesSeen, "raw image extraction should still see all 4 images")
	assert.Equal(t, 1, res.AuditStats[0].RelevanceBatches, "post-filter, 2 supported images fit in one batch")
}

// TestDetectScreenshotGaps_VisionPathResolvesRelativeURLs pins the second-half
// of the bug fix: relative srcs (root-relative, dot-slash, bare, parent) must
// be resolved against the docs page URL before being shipped to the vision
// API. Otherwise Bifrost can't fetch them, base64-encodes an HTML 404 page,
// and Anthropic rejects it with the same "invalid or unsupported" error.
func TestDetectScreenshotGaps_VisionPathResolvesRelativeURLs(t *testing.T) {
	client := &scriptedClient{
		caps: ModelCapabilities{Vision: true},
		multimodalResps: []json.RawMessage{
			json.RawMessage(`{"image_issues":[],"verdicts":[{"index":"img-0","matches":true},{"index":"img-1","matches":true},{"index":"img-2","matches":true},{"index":"img-3","matches":true}]}`),
		},
		detectionResp: json.RawMessage(`{"gaps":[]}`),
	}
	pages := []DocPage{{
		URL:  "https://docs.example.com/guide/intro/",
		Path: "/tmp/p.md",
		Content: "# Guide\n\nIntro.\n\n" +
			"![root](/static/img/root.png)\n\n" +
			"![dot](./img/dot.png)\n\n" +
			"![bare](img/bare.png)\n\n" +
			"![parent](../img/parent.png)\n",
	}}

	_, err := DetectScreenshotGaps(context.Background(), client, pages, nil)
	require.NoError(t, err)

	require.Len(t, client.multimodalMsgs, 1)
	var sentImageSrcs []string
	for _, b := range client.multimodalMsgs[0][0].ContentBlocks {
		if b.Type == ContentBlockImageURL {
			sentImageSrcs = append(sentImageSrcs, b.ImageURL)
		}
	}
	assert.Equal(t, []string{
		"https://docs.example.com/static/img/root.png",
		"https://docs.example.com/guide/intro/img/dot.png",
		"https://docs.example.com/guide/intro/img/bare.png",
		"https://docs.example.com/guide/img/parent.png",
	}, sentImageSrcs, "vision call must receive absolute URLs resolved against the page URL")
}

// TestDetectScreenshotGaps_VisionVerdictIndicesMatchUnfilteredRefs pins the
// fix for the verdict/refs index drift bug. When filtering drops images from
// the vision pass, the surviving images must keep the indices they had in the
// unfiltered refs list — otherwise verdicts emitted by the vision pass land
// on the wrong images in buildDetectionPromptWithVerdicts (which keys lookups
// off refs's positional 1-based index). On a page with PNG, SVG, JPG, ICO,
// the JPG sits at position 3 and must be labelled "img-3" in the relevance
// prompt, even though it's the second supported image to ship to vision.
func TestDetectScreenshotGaps_VisionVerdictIndicesMatchUnfilteredRefs(t *testing.T) {
	client := &scriptedClient{
		caps: ModelCapabilities{Vision: true},
		multimodalResps: []json.RawMessage{
			json.RawMessage(`{"image_issues":[],"verdicts":[{"index":"img-1","matches":true},{"index":"img-3","matches":true}]}`),
		},
		detectionResp: json.RawMessage(`{"gaps":[]}`),
	}
	pages := []DocPage{{
		URL:  "https://x/p",
		Path: "/tmp/p.md",
		Content: "# Page\n\nIntro.\n\n![dash](dashboard.png)\n\n" +
			"![logo](logo.svg)\n\n" +
			"![photo](photo.jpg)\n\n" +
			"![icon](favicon.ico)\n",
	}}

	_, err := DetectScreenshotGaps(context.Background(), client, pages, nil)
	require.NoError(t, err)

	require.Len(t, client.multimodalMsgs, 1)
	require.Len(t, client.multimodalMsgs[0], 1)

	var promptText string
	for _, b := range client.multimodalMsgs[0][0].ContentBlocks {
		if b.Type == ContentBlockText {
			promptText = b.Text
			break
		}
	}
	require.NotEmpty(t, promptText)

	assert.Contains(t, promptText, `img-1: src="https://x/dashboard.png"`,
		"PNG sits at refs[0] in the unfiltered list — must be labelled img-1")
	assert.Contains(t, promptText, `img-3: src="https://x/photo.jpg"`,
		"JPG sits at refs[2] in the unfiltered list — must be labelled img-3, not img-2")
	assert.NotContains(t, promptText, `img-2: src="https://x/photo.jpg"`,
		"JPG must NOT inherit the SVG's position — that's the bug being fixed")
}
