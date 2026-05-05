package analyzer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractImages_MarkdownSyntax(t *testing.T) {
	md := "# Title\n\nIntro paragraph.\n\n![Dashboard](dashboard.png)\n\nNext paragraph.\n"
	got := extractImages(md)
	assert.Equal(t, []imageRef{
		{AltText: "Dashboard", Src: "dashboard.png", SectionHeading: "Title", ParagraphIndex: 2, OriginalIndex: 1},
	}, got)
}

func TestExtractImages_HTMLSyntax(t *testing.T) {
	md := "# Title\n\n<img src=\"dashboard.png\" alt=\"Dashboard\">\n\nNext paragraph.\n"
	got := extractImages(md)
	assert.Equal(t, []imageRef{
		{AltText: "Dashboard", Src: "dashboard.png", SectionHeading: "Title", ParagraphIndex: 1, OriginalIndex: 1},
	}, got)
}

func TestExtractImages_HTMLSyntax_SingleQuotes(t *testing.T) {
	md := "# Title\n\n<img src='dashboard.png' alt='Dashboard'>\n\nNext paragraph.\n"
	got := extractImages(md)
	assert.Equal(t, []imageRef{
		{AltText: "Dashboard", Src: "dashboard.png", SectionHeading: "Title", ParagraphIndex: 1, OriginalIndex: 1},
	}, got)
}

func TestExtractImages_HTMLSyntax_AttrsReversed(t *testing.T) {
	md := "# Title\n\n<img alt=\"Dashboard\" src=\"dashboard.png\">\n"
	got := extractImages(md)
	assert.Equal(t, []imageRef{
		{AltText: "Dashboard", Src: "dashboard.png", SectionHeading: "Title", ParagraphIndex: 1, OriginalIndex: 1},
	}, got)
}

func TestExtractImages_MixedSyntaxes(t *testing.T) {
	md := "# Title\n\n![One](a.png)\n\n<img src=\"b.png\" alt=\"Two\">\n"
	got := extractImages(md)
	assert.Len(t, got, 2)
	assert.Equal(t, "a.png", got[0].Src)
	assert.Equal(t, "b.png", got[1].Src)
}

func TestExtractImages_IgnoresBashCommentsInFencedCodeBlocks(t *testing.T) {
	md := "# Real Heading\n\n" +
		"```bash\n" +
		"# install the cli\n" +
		"#!/bin/sh\n" +
		"brew install foo\n" +
		"```\n\n" +
		"![Dashboard](dashboard.png)\n"
	got := extractImages(md)
	require.Len(t, got, 1)
	assert.Equal(t, "Real Heading", got[0].SectionHeading,
		"bash comment inside fenced code block must not overwrite the section heading")
}

func TestExtractImages_IgnoresTildeFencedCodeBlocks(t *testing.T) {
	md := "# Real Heading\n\n" +
		"~~~python\n" +
		"# python comment that looks like a heading\n" +
		"print('hello')\n" +
		"~~~\n\n" +
		"![Dashboard](dashboard.png)\n"
	got := extractImages(md)
	require.Len(t, got, 1)
	assert.Equal(t, "Real Heading", got[0].SectionHeading)
}

func TestExtractImages_RequiresATXHeadingSyntax(t *testing.T) {
	// "#no-space" and "#tag" and "#!/bin/sh" are NOT valid ATX headings.
	// Only "#" (1-6 repeats) followed by whitespace counts.
	md := "# Real Heading\n\n" +
		"#notaheading because no space\n\n" +
		"#!/bin/sh\n\n" +
		"![Dashboard](dashboard.png)\n"
	got := extractImages(md)
	require.Len(t, got, 1)
	assert.Equal(t, "Real Heading", got[0].SectionHeading)
}

func TestExtractImages_AcceptsAllATXLevels(t *testing.T) {
	for _, prefix := range []string{"#", "##", "###", "####", "#####", "######"} {
		md := prefix + " Heading\n\n![x](x.png)\n"
		got := extractImages(md)
		require.Len(t, got, 1, "prefix=%q", prefix)
		assert.Equal(t, "Heading", got[0].SectionHeading, "prefix=%q", prefix)
	}
}

func TestExtractImages_RejectsSevenOrMoreHashes(t *testing.T) {
	// 7+ `#` chars is not a valid ATX heading per CommonMark.
	md := "# Real\n\n####### Not a heading\n\n![x](x.png)\n"
	got := extractImages(md)
	require.Len(t, got, 1)
	assert.Equal(t, "Real", got[0].SectionHeading)
}

func TestBuildCoverageMap_GroupsBySection(t *testing.T) {
	refs := []imageRef{
		{Src: "a.png", SectionHeading: "Intro", ParagraphIndex: 1},
		{Src: "b.png", SectionHeading: "Intro", ParagraphIndex: 3},
		{Src: "c.png", SectionHeading: "Setup", ParagraphIndex: 7},
	}
	m := buildCoverageMap(refs)
	assert.Equal(t, []imageRef{refs[0], refs[1]}, m["Intro"])
	assert.Equal(t, []imageRef{refs[2]}, m["Setup"])
}

func TestBuildCoverageMap_EmptyInput(t *testing.T) {
	assert.Empty(t, buildCoverageMap(nil))
}

func TestBuildScreenshotPrompt_IncludesPageContentAndCoverageMap(t *testing.T) {
	pageURL := "https://example.com/quickstart"
	content := "# Quickstart\n\nRun the command and see the output.\n"
	coverage := map[string][]imageRef{
		"Quickstart": {{Src: "hero.png", AltText: "Hero", SectionHeading: "Quickstart", ParagraphIndex: 0}},
	}
	got := buildScreenshotPrompt(pageURL, content, coverage)
	assert.Contains(t, got, pageURL)
	assert.Contains(t, got, content)
	assert.Contains(t, got, "hero.png")
	assert.Contains(t, got, "quoted_passage")
	assert.Contains(t, got, "should_show")
	assert.Contains(t, got, "suggested_alt")
	assert.Contains(t, got, "insertion_hint")
	assert.Contains(t, got, "same section")
	assert.Contains(t, got, "3 paragraphs")
}

func TestBuildScreenshotPrompt_EmptyCoverage(t *testing.T) {
	got := buildScreenshotPrompt("https://example.com/x", "# X\n\nHello.\n", nil)
	assert.Contains(t, got, "No existing images")
}

// fakeLLMClient collects calls and returns canned responses per call index.
// Responses are in the wrapped {"gaps":[...]} format; use `{"gaps":[]}` for
// the empty case. When responses is shorter than the call count, `{"gaps":[]}`
// is returned.
type fakeLLMClient struct {
	responses []string
	errs      []error
	prompts   []string
}

func (f *fakeLLMClient) Complete(_ context.Context, prompt string) (string, error) {
	i := len(f.prompts)
	f.prompts = append(f.prompts, prompt)
	if i < len(f.errs) && f.errs[i] != nil {
		return "", f.errs[i]
	}
	if i < len(f.responses) {
		return f.responses[i], nil
	}
	return `{"gaps":[]}`, nil
}

func (f *fakeLLMClient) CompleteJSON(ctx context.Context, prompt string, _ JSONSchema) (json.RawMessage, error) {
	raw, err := f.Complete(ctx, prompt)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(raw), nil
}

// CompleteJSONMultimodal mirrors CompleteJSON but reads the prompt off the
// first text content block. Tests that exercise the vision path should use a
// dedicated fake (see fakeJSONClient in screenshot_gaps_relevance_test.go);
// this stub exists so fakeLLMClient still satisfies LLMClient.
func (f *fakeLLMClient) CompleteJSONMultimodal(ctx context.Context, msgs []ChatMessage, _ JSONSchema) (json.RawMessage, error) {
	prompt := ""
	if len(msgs) > 0 {
		prompt = msgs[0].Content
	}
	raw, err := f.Complete(ctx, prompt)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(raw), nil
}

func (f *fakeLLMClient) Capabilities() ModelCapabilities { return ModelCapabilities{} }

func TestDetectScreenshotGaps_NoPages(t *testing.T) {
	client := &fakeLLMClient{}
	res, err := DetectScreenshotGaps(context.Background(), client, nil, nil)
	require.NoError(t, err)
	assert.Empty(t, res.MissingGaps)
	assert.Empty(t, res.AuditStats)
	assert.Empty(t, client.prompts)
}

func TestDetectScreenshotGaps_SinglePage_Findings(t *testing.T) {
	client := &fakeLLMClient{
		responses: []string{
			`{"gaps":[{"quoted_passage":"Run the command.","should_show":"Terminal showing output.","suggested_alt":"Terminal","insertion_hint":"after the command block"}]}`,
		},
	}
	pages := []DocPage{
		{URL: "https://example.com/a", Path: "/tmp/a.md", Content: "# A\n\nRun the command.\n"},
	}
	res, err := DetectScreenshotGaps(context.Background(), client, pages, nil)
	require.NoError(t, err)
	require.Len(t, res.MissingGaps, 1)
	assert.Equal(t, "https://example.com/a", res.MissingGaps[0].PageURL)
	assert.Equal(t, "/tmp/a.md", res.MissingGaps[0].PagePath)
	assert.Equal(t, "Run the command.", res.MissingGaps[0].QuotedPassage)
}

// TestDetectScreenshotGaps_NormalizesLiteralEscapeSequences guards against an
// LLM quirk: models sometimes write `\n` (the two-character escape sequence)
// as text inside a JSON string value rather than emitting an actual newline.
// When that happens, the rendered Hugo page shows "step 1.\n\nstep 2." as a
// single literal line instead of paragraphs. The analyzer must normalize these
// before passing the gap downstream so reporters and templates render newlines
// correctly.
func TestDetectScreenshotGaps_NormalizesLiteralEscapeSequences(t *testing.T) {
	client := &fakeLLMClient{
		responses: []string{
			`{"gaps":[{"quoted_passage":"2. Click Add API Key.\\n \\n3. Enter a Name.","should_show":"x","suggested_alt":"x","insertion_hint":"x"}]}`,
		},
	}
	pages := []DocPage{
		{URL: "https://example.com/a", Path: "/tmp/a.md", Content: "# A\n"},
	}
	res, err := DetectScreenshotGaps(context.Background(), client, pages, nil)
	require.NoError(t, err)
	require.Len(t, res.MissingGaps, 1)
	assert.Equal(t, "2. Click Add API Key.\n \n3. Enter a Name.", res.MissingGaps[0].QuotedPassage,
		"literal `\\n` from the model should be converted to real newlines")
}

func TestDetectScreenshotGaps_ParseErrorIsolatesPage(t *testing.T) {
	client := &fakeLLMClient{
		responses: []string{"not json", `{"gaps":[]}`},
	}
	pages := []DocPage{
		{URL: "https://example.com/a", Path: "/tmp/a.md", Content: "# A\n"},
		{URL: "https://example.com/b", Path: "/tmp/b.md", Content: "# B\n"},
	}
	res, err := DetectScreenshotGaps(context.Background(), client, pages, nil)
	require.NoError(t, err) // parse errors log-and-continue
	assert.Empty(t, res.MissingGaps)
	assert.Len(t, client.prompts, 2) // both pages were attempted
}

func TestDetectScreenshotGaps_Progress(t *testing.T) {
	client := &fakeLLMClient{}
	pages := []DocPage{
		{URL: "https://example.com/a", Path: "/tmp/a.md", Content: "# A\n"},
		{URL: "https://example.com/b", Path: "/tmp/b.md", Content: "# B\n"},
	}
	var calls []struct {
		done, total int
		page        string
	}
	progress := func(done, total int, page string) {
		calls = append(calls, struct {
			done, total int
			page        string
		}{done, total, page})
	}
	_, err := DetectScreenshotGaps(context.Background(), client, pages, progress)
	require.NoError(t, err)
	require.Len(t, calls, 2)
	assert.Equal(t, 1, calls[0].done)
	assert.Equal(t, 2, calls[0].total)
	assert.Equal(t, "https://example.com/a", calls[0].page)
	assert.Equal(t, 2, calls[1].done)
}

func TestScreenshotPromptBudget_LeavesRoomForTokenizerDriftAndToolOverhead(t *testing.T) {
	// The local estimator (cl100k_base) undercounts vs Claude's tokenizer on
	// code-heavy reference docs by ~13% (observed: bun.com/reference/node/crypto
	// produced 201,871 actual tokens at ~179K cl100k). Plus, the Anthropic
	// CompleteJSON path forces a "respond" tool whose JSON Schema parameters
	// the estimator does not see. The budget must absorb both.
	const claudeInputWindow = 200_000
	const expectedClaudeDrift = 1.20 // 13% drift + slack for tool/schema input
	worstCaseClaudeTokens := int(float64(ScreenshotPromptBudget) * expectedClaudeDrift)
	assert.LessOrEqual(t, worstCaseClaudeTokens, claudeInputWindow,
		"ScreenshotPromptBudget=%d * %.2fx drift = %d must fit in Claude's %d input window",
		ScreenshotPromptBudget, expectedClaudeDrift, worstCaseClaudeTokens, claudeInputWindow)
}

func TestDetectScreenshotGaps_TruncatesOversizedPage(t *testing.T) {
	// Build content well above ScreenshotPromptBudget so truncation must fire.
	// "lorem ipsum dolor sit amet " ≈ 6 tokens for cl100k_base; repeat enough
	// to exceed the budget by a wide margin.
	chunk := "lorem ipsum dolor sit amet consectetur adipiscing elit "
	repeats := (ScreenshotPromptBudget / 6) * 3
	big := strings.Repeat(chunk, repeats)

	client := &fakeLLMClient{}
	pages := []DocPage{{URL: "https://example.com/huge", Path: "/tmp/huge.md", Content: big}}

	_, err := DetectScreenshotGaps(context.Background(), client, pages, nil)
	require.NoError(t, err)
	require.Len(t, client.prompts, 1, "page should still be sent, just truncated")

	sent := client.prompts[0]
	got := countTokens(sent)
	assert.Less(t, got, ScreenshotPromptBudget,
		"prompt token count must fit inside ScreenshotPromptBudget after truncation")
}

func TestSplitImageBatches(t *testing.T) {
	ref := func(i int) imageRef { return imageRef{Src: fmt.Sprintf("img-%d.png", i)} }
	for _, tc := range []struct {
		name string
		n    int
		want []int // batch sizes
	}{
		{"empty", 0, nil},
		{"one", 1, []int{1}},
		{"exactly five", 5, []int{5}},
		{"six", 6, []int{5, 1}},
		{"twelve", 12, []int{5, 5, 2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			refs := make([]imageRef, tc.n)
			for i := range refs {
				refs[i] = ref(i)
			}
			got := splitImageBatches(refs, 5)
			var gotSizes []int
			for _, b := range got {
				gotSizes = append(gotSizes, len(b))
			}
			assert.Equal(t, tc.want, gotSizes)
		})
	}
}

func TestFilterVisionSupportedImages(t *testing.T) {
	in := []imageRef{
		{Src: "logo.svg"},
		{Src: "diagram.SVG"},
		{Src: "https://example.com/img/dashboard.png"},
		{Src: "https://example.com/img/photo.JPG"},
		{Src: "photo.jpeg"},
		{Src: "anim.gif"},
		{Src: "modern.webp"},
		{Src: "next-gen.avif"},
		{Src: "favicon.ico"},
		{Src: "scan.tiff"},
		{Src: "scan.tif"},
		{Src: "shot.bmp"},
		{Src: "iphone.heic"},
		{Src: "iphone.heif"},
		{Src: "logo.svg?v=2"},
		{Src: "https://cdn.example.com/asset?id=42"}, // no extension, keep
		{Src: "https://example.com/page.png#frag"},
		{Src: "data:image/png;base64,AAAA"},
		{Src: "data:image/svg+xml;utf8,<svg/>"},
	}
	got := filterVisionSupportedImages(in)
	var gotSrcs []string
	for _, r := range got {
		gotSrcs = append(gotSrcs, r.Src)
	}
	assert.Equal(t, []string{
		"https://example.com/img/dashboard.png",
		"https://example.com/img/photo.JPG",
		"photo.jpeg",
		"anim.gif",
		"modern.webp",
		"https://cdn.example.com/asset?id=42",
		"https://example.com/page.png#frag",
		"data:image/png;base64,AAAA",
	}, gotSrcs)
}

func TestFilterVisionSupportedImages_EmptyAndNil(t *testing.T) {
	assert.Nil(t, filterVisionSupportedImages(nil))
	assert.Empty(t, filterVisionSupportedImages([]imageRef{}))
}

func TestResolveImageSrc(t *testing.T) {
	const pageURL = "https://docs.example.com/guide/intro/"
	for _, tc := range []struct {
		name    string
		src     string
		want    string
		wantOK  bool
	}{
		{"absolute https", "https://cdn.example.com/img/dash.png", "https://cdn.example.com/img/dash.png", true},
		{"absolute http", "http://other.test/x.png", "http://other.test/x.png", true},
		{"protocol relative", "//cdn.example.com/img/dash.png", "https://cdn.example.com/img/dash.png", true},
		{"root relative", "/static/img/dash.png", "https://docs.example.com/static/img/dash.png", true},
		{"dot-slash relative", "./img/dash.png", "https://docs.example.com/guide/intro/img/dash.png", true},
		{"bare relative", "img/dash.png", "https://docs.example.com/guide/intro/img/dash.png", true},
		{"parent relative", "../img/dash.png", "https://docs.example.com/guide/img/dash.png", true},
		{"data uri passthrough", "data:image/png;base64,AAAA", "data:image/png;base64,AAAA", true},
		{"empty src", "", "", false},
		{"whitespace src", "   ", "", false},
		{"fragment-only src", "#frag", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := resolveImageSrc(pageURL, tc.src)
			assert.Equal(t, tc.wantOK, ok, "ok flag")
			assert.Equal(t, tc.want, got, "resolved url")
		})
	}
}

func TestResolveImageSrc_BadPageURL(t *testing.T) {
	// Relative src + unparseable page URL → cannot resolve, drop.
	got, ok := resolveImageSrc("::not-a-url", "img/dash.png")
	assert.False(t, ok)
	assert.Empty(t, got)

	// Absolute src + bad page URL → still resolvable.
	got, ok = resolveImageSrc("::not-a-url", "https://cdn.example.com/x.png")
	assert.True(t, ok)
	assert.Equal(t, "https://cdn.example.com/x.png", got)
}

func TestResolveImageSrc_RelativeButPageHasNoHost(t *testing.T) {
	// Page URL is itself relative — root-relative src can't be resolved against it.
	got, ok := resolveImageSrc("guide/intro", "/static/x.png")
	assert.False(t, ok)
	assert.Empty(t, got)
}

func TestDetectScreenshotGaps_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := &fakeLLMClient{errs: []error{context.Canceled}}
	pages := []DocPage{{URL: "https://x", Path: "/x", Content: "# x\n"}}
	_, err := DetectScreenshotGaps(ctx, client, pages, nil)
	require.Error(t, err)
}

func TestBuildDetectionPromptWithVerdicts_AnnotatesImages(t *testing.T) {
	verdicts := []ImageVerdict{{Index: "img-1", Matches: true}, {Index: "img-2", Matches: false}}
	refs := []imageRef{{Src: "a.png", AltText: "Settings"}, {Src: "b.png", AltText: "Logs"}}
	prompt := buildDetectionPromptWithVerdicts("https://x/p", "content...", refs, verdicts)
	assert.Contains(t, prompt, "img-1")
	assert.Contains(t, prompt, "verdict: matches")
	assert.Contains(t, prompt, "verdict: does not match")
}

func TestBuildDetectionPromptWithVerdicts_NilVerdictsDelegateToLegacy(t *testing.T) {
	refs := []imageRef{{Src: "a.png"}}
	got := buildDetectionPromptWithVerdicts("https://x/p", "content...", refs, nil)
	want := buildScreenshotPrompt("https://x/p", "content...", buildCoverageMap(refs))
	assert.Equal(t, want, got)
}

// Both prompts must be selective: not the prior "aggressively conservative /
// flag nothing" extreme, but not the "lean toward flagging / when in doubt
// flag" extreme either (that produced 113 findings on a single docs site in
// practice). The intended posture is "flag a screenshot only when it earns
// its place" — when in doubt, omit.
func TestBuildScreenshotPrompt_IsSelective(t *testing.T) {
	got := buildScreenshotPrompt("https://example.com/x", "# X\n\nHello.\n", nil)
	lower := strings.ToLower(got)
	assert.NotContains(t, lower, "aggressively conservative",
		"legacy prompt should no longer instruct the model to be aggressively conservative")
	assert.NotContains(t, lower, "default is to flag nothing",
		"legacy prompt should no longer set 'flag nothing' as the default")
	assert.NotContains(t, lower, "lean toward flagging",
		"legacy prompt should no longer bias the model toward over-flagging")
	assert.NotContains(t, lower, "when in doubt, flag",
		"legacy prompt should not tell the model to flag when uncertain")
	assert.Contains(t, lower, "when in doubt, do not flag",
		"legacy prompt should bias the model away from flagging when uncertain")
}

func TestBuildDetectionPromptWithVerdicts_IsSelective(t *testing.T) {
	verdicts := []ImageVerdict{{Index: "img-1", Matches: true}}
	refs := []imageRef{{Src: "a.png", AltText: "Settings"}}
	got := buildDetectionPromptWithVerdicts("https://x/p", "content...", refs, verdicts)
	lower := strings.ToLower(got)
	assert.NotContains(t, lower, "aggressively conservative",
		"verdict prompt should no longer instruct the model to be aggressively conservative")
	assert.NotContains(t, lower, "default is to flag nothing",
		"verdict prompt should no longer set 'flag nothing' as the default")
	assert.NotContains(t, lower, "lean toward flagging",
		"verdict prompt should no longer bias the model toward over-flagging")
	assert.NotContains(t, lower, "when in doubt, flag",
		"verdict prompt should not tell the model to flag when uncertain")
	assert.Contains(t, lower, "when in doubt, do not flag",
		"verdict prompt should bias the model away from flagging when uncertain")
}

// Both prompts must continue to exclude three known-false-positive patterns
// that produced noise before: trivial single-action interactions, terminal
// sessions already shown as code blocks, and reference material like API
// signatures and option tables.
func TestPrompts_ExcludeKnownFalsePositivePatterns(t *testing.T) {
	cases := map[string]string{
		"legacy": buildScreenshotPrompt("https://x", "content", nil),
		"verdict": buildDetectionPromptWithVerdicts("https://x", "content",
			[]imageRef{{Src: "a.png"}},
			[]ImageVerdict{{Index: "img-1", Matches: true}}),
	}
	for name, prompt := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Contains(t, prompt, `Single-action interactions`,
				"prompt should still tell the model not to flag single-action UI moments")
			assert.Contains(t, prompt, `click Save`,
				"prompt should keep the canonical 'click Save' example")
			assert.Contains(t, prompt, `Terminal sessions whose output is already shown inline in a code block`,
				"prompt should still exclude terminal sessions already shown inline as code")
			assert.Contains(t, prompt, `API signatures, option tables, type listings`,
				"prompt should keep the reference-material exclusion")
		})
	}
}

// In the vision case we want the prompt to explicitly orient the model around
// the question "is a relevant screenshot already on the page?" rather than
// burying that check inside the locality rule.
func TestBuildDetectionPromptWithVerdicts_AsksWhetherScreenshotIsAlreadyOnPage(t *testing.T) {
	verdicts := []ImageVerdict{{Index: "img-1", Matches: true}}
	refs := []imageRef{{Src: "a.png", AltText: "Settings"}}
	got := buildDetectionPromptWithVerdicts("https://x/p", "content...", refs, verdicts)
	lower := strings.ToLower(got)
	assert.Contains(t, lower, "existing image",
		"verdict prompt should direct the model to check for an existing image on the page")
	// The instruction should treat the verdict (not just locality / alt text)
	// as the authoritative signal that the screenshot is already present.
	assert.Contains(t, got, "AUTHORITATIVE",
		"verdict prompt should mark the relevance verdicts as the authoritative coverage signal")
}

func TestExtractImagesParsesWidthAndHeightAttrs(t *testing.T) {
	cases := []struct {
		name  string
		md    string
		wantW int
		wantH int
	}{
		{
			name:  "double-quoted width and height",
			md:    `<img src="a.png" width="800" height="600">`,
			wantW: 800, wantH: 600,
		},
		{
			name:  "single-quoted width only",
			md:    `<img src='a.png' width='400'>`,
			wantW: 400, wantH: 0,
		},
		{
			name:  "absent attrs",
			md:    `<img src="a.png">`,
			wantW: 0, wantH: 0,
		},
		{
			name:  "non-numeric width is ignored",
			md:    `<img src="a.png" width="auto" height="100">`,
			wantW: 0, wantH: 100,
		},
		{
			name:  "markdown image carries no dimensions",
			md:    `![alt](a.png)`,
			wantW: 0, wantH: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			refs := extractImages(tc.md)
			if len(refs) != 1 {
				t.Fatalf("expected 1 ref, got %d", len(refs))
			}
			if refs[0].DeclaredWidth != tc.wantW {
				t.Errorf("DeclaredWidth: got %d, want %d", refs[0].DeclaredWidth, tc.wantW)
			}
			if refs[0].DeclaredHeight != tc.wantH {
				t.Errorf("DeclaredHeight: got %d, want %d", refs[0].DeclaredHeight, tc.wantH)
			}
		})
	}
}

func TestSuppressionEligible(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want bool
	}{
		{"png is not eligible", "https://x.com/a.png", false},
		{"jpeg is not eligible", "https://x.com/a.jpg", false},
		{"webp is not eligible", "https://x.com/a.webp", false},
		{"gif is eligible", "https://x.com/a.gif", true},
		{"GIF uppercase is eligible", "https://x.com/a.GIF", true},
		{"svg is eligible", "https://x.com/a.svg", true},
		{"avif is eligible", "https://x.com/a.avif", true},
		{"image/gif data URI is eligible", "data:image/gif;base64,abc", true},
		{"image/svg+xml data URI is eligible", "data:image/svg+xml;base64,abc", true},
		{"image/png data URI is not eligible", "data:image/png;base64,abc", false},
		{"extensionless URL is not eligible (vision-supported by default)", "https://x.com/img/abc", false},
		{"empty src is not eligible", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := suppressionEligible(imageRef{Src: tc.src})
			if got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestHTMLAttrsSuggestScreenshot(t *testing.T) {
	cases := []struct {
		name string
		w, h int
		want bool
	}{
		{"both zero", 0, 0, false},
		{"width below threshold", 399, 0, false},
		{"width at threshold", 400, 0, true},
		{"width above threshold", 800, 100, true},
		{"height at threshold, width below", 100, 400, true},
		{"both well below threshold (typical icon)", 24, 24, false},
		{"only width set, above threshold", 800, 0, true},
		{"only height set, above threshold", 0, 1200, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := imageRef{DeclaredWidth: tc.w, DeclaredHeight: tc.h}
			if got := htmlAttrsSuggestScreenshot(r); got != tc.want {
				t.Errorf("got %v, want %v (w=%d h=%d)", got, tc.want, tc.w, tc.h)
			}
		})
	}
}

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestHeadSuggestsScreenshot(t *testing.T) {
	t.Run("data URI uses inline length and short-circuits HEAD", func(t *testing.T) {
		// "image/png;base64," + 30KB of base64 padding = > 30720 bytes
		big := "data:image/png;base64," + strings.Repeat("A", 40000)
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("HEAD should not be issued for data URI")
			return nil, nil
		})}
		got, err := headSuggestsScreenshot(context.Background(), client, big)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !got {
			t.Errorf("expected true for >30KB data URI, got false")
		}
	})

	t.Run("small data URI returns false", func(t *testing.T) {
		small := "data:image/svg+xml,<svg></svg>"
		got, err := headSuggestsScreenshot(context.Background(), &http.Client{}, small)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got {
			t.Errorf("expected false for small data URI, got true")
		}
	})

	t.Run("HEAD 200 with Content-Length above threshold returns true", func(t *testing.T) {
		client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.Method != http.MethodHead {
				t.Errorf("expected HEAD, got %s", req.Method)
			}
			return &http.Response{
				StatusCode: 200,
				Header:     http.Header{"Content-Length": []string{"50000"}},
				Body:       http.NoBody,
			}, nil
		})}
		got, err := headSuggestsScreenshot(context.Background(), client, "https://x.com/a.svg")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !got {
			t.Error("expected true for 50KB Content-Length")
		}
	})

	t.Run("HEAD 200 with Content-Length below threshold returns false", func(t *testing.T) {
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 200,
				Header:     http.Header{"Content-Length": []string{"5000"}},
				Body:       http.NoBody,
			}, nil
		})}
		got, err := headSuggestsScreenshot(context.Background(), client, "https://x.com/a.gif")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got {
			t.Error("expected false for 5KB Content-Length")
		}
	})

	t.Run("missing Content-Length returns false with no error (no signal)", func(t *testing.T) {
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: http.NoBody}, nil
		})}
		got, err := headSuggestsScreenshot(context.Background(), client, "https://x.com/a.gif")
		if err != nil {
			t.Errorf("missing Content-Length should not be an error: %v", err)
		}
		if got {
			t.Error("expected false when no Content-Length header")
		}
	})

	t.Run("non-2xx returns false with error", func(t *testing.T) {
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 404, Header: http.Header{}, Body: http.NoBody}, nil
		})}
		got, err := headSuggestsScreenshot(context.Background(), client, "https://x.com/missing.svg")
		if err == nil {
			t.Error("expected error on 404")
		}
		if got {
			t.Error("expected false on 404")
		}
	})

	t.Run("transport error propagates", func(t *testing.T) {
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("network down")
		})}
		_, err := headSuggestsScreenshot(context.Background(), client, "https://x.com/a.gif")
		if err == nil {
			t.Error("expected error from transport")
		}
	})
}

func TestDecisionForImageRef(t *testing.T) {
	t.Run("HTML attrs sufficient: HEAD is not issued", func(t *testing.T) {
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("HEAD must not be issued when HTML attrs already cross threshold")
			return nil, nil
		})}
		ref := imageRef{Src: "https://x.com/a.gif", DeclaredWidth: 800, DeclaredHeight: 0}
		if !decisionForImageRef(context.Background(), client, ref) {
			t.Error("expected true: width=800 attr alone should suppress")
		}
	})

	t.Run("HTML attrs absent: falls through to HEAD", func(t *testing.T) {
		called := false
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			called = true
			return &http.Response{
				StatusCode: 200,
				Header:     http.Header{"Content-Length": []string{"50000"}},
				Body:       http.NoBody,
			}, nil
		})}
		ref := imageRef{Src: "https://x.com/a.gif"}
		if !decisionForImageRef(context.Background(), client, ref) {
			t.Error("expected true via HEAD")
		}
		if !called {
			t.Error("expected HEAD to be issued when HTML attrs absent")
		}
	})

	t.Run("HTML attrs below threshold and HEAD says small: false", func(t *testing.T) {
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 200,
				Header:     http.Header{"Content-Length": []string{"5000"}},
				Body:       http.NoBody,
			}, nil
		})}
		ref := imageRef{Src: "https://x.com/a.gif", DeclaredWidth: 100}
		if decisionForImageRef(context.Background(), client, ref) {
			t.Error("expected false: small attr + small bytes")
		}
	})

	t.Run("HEAD failure produces false (no signal)", func(t *testing.T) {
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("network down")
		})}
		ref := imageRef{Src: "https://x.com/a.gif"}
		if decisionForImageRef(context.Background(), client, ref) {
			t.Error("expected false: HEAD failure means no signal -> no suppression")
		}
	})
}

func TestDecideAllSuppressionsDedupesByURL(t *testing.T) {
	var heads int32
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&heads, 1)
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Length": []string{"50000"}},
			Body:       http.NoBody,
		}, nil
	})}
	refs := []imageRef{
		{Src: "https://x.com/a.gif"},
		{Src: "https://x.com/a.gif"},
		{Src: "https://x.com/a.gif"},
	}
	decisions := decideAllSuppressions(context.Background(), client, refs, 8)
	if len(decisions) != 3 {
		t.Fatalf("expected 3 decisions, got %d", len(decisions))
	}
	for i, d := range decisions {
		if !d {
			t.Errorf("decision[%d] = false, want true", i)
		}
	}
	if atomic.LoadInt32(&heads) != 1 {
		t.Errorf("expected 1 HEAD (deduped), got %d", heads)
	}
}

func TestDecideAllSuppressionsRespectsConcurrencyCap(t *testing.T) {
	var inflight int32
	var maxInflight int32
	gate := make(chan struct{})
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		cur := atomic.AddInt32(&inflight, 1)
		for {
			peak := atomic.LoadInt32(&maxInflight)
			if cur <= peak || atomic.CompareAndSwapInt32(&maxInflight, peak, cur) {
				break
			}
		}
		<-gate
		atomic.AddInt32(&inflight, -1)
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Length": []string{"100"}},
			Body:       http.NoBody,
		}, nil
	})}
	refs := make([]imageRef, 20)
	for i := range refs {
		refs[i] = imageRef{Src: fmt.Sprintf("https://x.com/img-%d.gif", i)}
	}
	done := make(chan struct{})
	go func() {
		decideAllSuppressions(context.Background(), client, refs, 4)
		close(done)
	}()
	// Give worker pool time to ramp up to its cap.
	time.Sleep(50 * time.Millisecond)
	close(gate)
	<-done
	if maxInflight > 4 {
		t.Errorf("max in-flight HEADs = %d, want <= 4", maxInflight)
	}
}

func TestDetectionPassReturnsSuppressedItems(t *testing.T) {
	// Hand-rolled fake LLM client that returns a fixed JSON response
	// containing both gaps and suppressed_by_image. We use the existing
	// fakeLLMClient (responses []string, one per call); detectionPass makes
	// exactly one CompleteJSON call per page.
	client := &fakeLLMClient{
		responses: []string{`{
			"gaps": [{
				"quoted_passage": "Click Save to continue.",
				"should_show": "save dialog",
				"suggested_alt": "save dialog",
				"insertion_hint": "after the click-save paragraph"
			}],
			"suppressed_by_image": [{
				"quoted_passage": "Watch the demo gif of the upload flow.\\nIt shows the steps.",
				"should_show": "upload flow",
				"suggested_alt": "upload demo",
				"insertion_hint": "after the demo-gif paragraph"
			}]
		}`},
	}
	page := DocPage{URL: "https://x.com/p", Path: "p.md", Content: "# Hello\n\nClick Save."}
	gaps, suppressed, skipped, err := detectionPass(context.Background(), client, page, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if skipped {
		t.Fatal("did not expect skipped=true")
	}
	if len(gaps) != 1 {
		t.Errorf("expected 1 gap, got %d", len(gaps))
	}
	if len(suppressed) != 1 {
		t.Fatalf("expected 1 suppressed item, got %d", len(suppressed))
	}
	// Literal escape sequences must be unescaped, matching the gaps treatment.
	wantPassage := "Watch the demo gif of the upload flow.\nIt shows the steps."
	if suppressed[0].QuotedPassage != wantPassage {
		t.Errorf("suppressed passage mismatch:\n got: %q\nwant: %q", suppressed[0].QuotedPassage, wantPassage)
	}
	if suppressed[0].PageURL != page.URL {
		t.Errorf("suppressed PageURL = %q, want %q", suppressed[0].PageURL, page.URL)
	}
	if suppressed[0].PagePath != page.Path {
		t.Errorf("suppressed PagePath = %q, want %q", suppressed[0].PagePath, page.Path)
	}
	if suppressed[0].ShouldShow != "upload flow" {
		t.Errorf("suppressed ShouldShow = %q", suppressed[0].ShouldShow)
	}
	if suppressed[0].SuggestedAlt != "upload demo" {
		t.Errorf("suppressed SuggestedAlt = %q", suppressed[0].SuggestedAlt)
	}
	if suppressed[0].InsertionHint != "after the demo-gif paragraph" {
		t.Errorf("suppressed InsertionHint = %q", suppressed[0].InsertionHint)
	}
}

func TestScreenshotResultHasPossiblyCovered(t *testing.T) {
	// Compile-time guarantee that the field exists with the expected type.
	var r ScreenshotResult
	r.PossiblyCovered = []ScreenshotGap{{PageURL: "https://x.com"}}
	if len(r.PossiblyCovered) != 1 {
		t.Fatal("PossiblyCovered field unusable")
	}
	if r.PossiblyCovered[0].PageURL != "https://x.com" {
		t.Fatal("ScreenshotGap shape on PossiblyCovered does not match")
	}
}

func TestScreenshotPageStatsHasPossiblyCovered(t *testing.T) {
	var s ScreenshotPageStats
	s.PossiblyCovered = 3
	if s.PossiblyCovered != 3 {
		t.Fatal("PossiblyCovered field unusable")
	}
}
