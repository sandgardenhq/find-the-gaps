package analyzer

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractImages_MarkdownSyntax(t *testing.T) {
	md := "# Title\n\nIntro paragraph.\n\n![Dashboard](dashboard.png)\n\nNext paragraph.\n"
	got := extractImages(md)
	assert.Equal(t, []imageRef{
		{AltText: "Dashboard", Src: "dashboard.png", SectionHeading: "Title", ParagraphIndex: 2},
	}, got)
}
