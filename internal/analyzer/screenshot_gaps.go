package analyzer

import (
	"regexp"
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
var htmlAttrSrcRe = regexp.MustCompile(`(?i)\bsrc\s*=\s*"([^"]*)"`)
var htmlAttrAltRe = regexp.MustCompile(`(?i)\balt\s*=\s*"([^"]*)"`)

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
			}
			if mm := htmlAttrAltRe.FindStringSubmatch(attrs); mm != nil {
				alt = mm[1]
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
