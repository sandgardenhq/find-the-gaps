package analyzer

import (
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

func TestParseScreenshotResponse_Valid(t *testing.T) {
	raw := `[{"quoted_passage":"Click Save.","should_show":"Save button highlighted.","suggested_alt":"Save button","insertion_hint":"after the sentence 'Click Save.'"}]`
	got, err := parseScreenshotResponse(raw)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "Click Save.", got[0].QuotedPassage)
	assert.Equal(t, "Save button highlighted.", got[0].ShouldShow)
	assert.Equal(t, "Save button", got[0].SuggestedAlt)
	assert.Equal(t, "after the sentence 'Click Save.'", got[0].InsertionHint)
}

func TestParseScreenshotResponse_EmptyArray(t *testing.T) {
	got, err := parseScreenshotResponse("[]")
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestParseScreenshotResponse_WithPreamble(t *testing.T) {
	raw := "Here is the JSON: []"
	got, err := parseScreenshotResponse(raw)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestParseScreenshotResponse_Malformed(t *testing.T) {
	_, err := parseScreenshotResponse("not json")
	require.Error(t, err)
}
