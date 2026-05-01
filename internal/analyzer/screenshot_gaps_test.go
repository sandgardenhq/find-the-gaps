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
	gaps, err := DetectScreenshotGaps(context.Background(), client, nil, nil)
	require.NoError(t, err)
	assert.Empty(t, gaps)
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
	gaps, err := DetectScreenshotGaps(context.Background(), client, pages, nil)
	require.NoError(t, err)
	require.Len(t, gaps, 1)
	assert.Equal(t, "https://example.com/a", gaps[0].PageURL)
	assert.Equal(t, "/tmp/a.md", gaps[0].PagePath)
	assert.Equal(t, "Run the command.", gaps[0].QuotedPassage)
}

func TestDetectScreenshotGaps_ParseErrorIsolatesPage(t *testing.T) {
	client := &fakeLLMClient{
		responses: []string{"not json", `{"gaps":[]}`},
	}
	pages := []DocPage{
		{URL: "https://example.com/a", Path: "/tmp/a.md", Content: "# A\n"},
		{URL: "https://example.com/b", Path: "/tmp/b.md", Content: "# B\n"},
	}
	gaps, err := DetectScreenshotGaps(context.Background(), client, pages, nil)
	require.NoError(t, err) // parse errors log-and-continue
	assert.Empty(t, gaps)
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
