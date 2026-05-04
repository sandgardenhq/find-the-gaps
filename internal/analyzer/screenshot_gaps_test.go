package analyzer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractImages_MarkdownSyntax(t *testing.T) {
	md := "# Title\n\nIntro paragraph.\n\n![Dashboard](dashboard.png)\n\nNext paragraph.\n"
	got := extractImages(md)
	assert.Equal(t, []imageRef{
		{AltText: "Dashboard", Src: "dashboard.png", SectionHeading: "Title", ParagraphIndex: 2},
	}, got)
}

func TestExtractImages_HTMLSyntax(t *testing.T) {
	md := "# Title\n\n<img src=\"dashboard.png\" alt=\"Dashboard\">\n\nNext paragraph.\n"
	got := extractImages(md)
	assert.Equal(t, []imageRef{
		{AltText: "Dashboard", Src: "dashboard.png", SectionHeading: "Title", ParagraphIndex: 1},
	}, got)
}

func TestExtractImages_HTMLSyntax_SingleQuotes(t *testing.T) {
	md := "# Title\n\n<img src='dashboard.png' alt='Dashboard'>\n\nNext paragraph.\n"
	got := extractImages(md)
	assert.Equal(t, []imageRef{
		{AltText: "Dashboard", Src: "dashboard.png", SectionHeading: "Title", ParagraphIndex: 1},
	}, got)
}

func TestExtractImages_HTMLSyntax_AttrsReversed(t *testing.T) {
	md := "# Title\n\n<img alt=\"Dashboard\" src=\"dashboard.png\">\n"
	got := extractImages(md)
	assert.Equal(t, []imageRef{
		{AltText: "Dashboard", Src: "dashboard.png", SectionHeading: "Title", ParagraphIndex: 1},
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
