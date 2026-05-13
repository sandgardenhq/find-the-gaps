package pdf

import (
	"regexp"
	"strings"

	"github.com/go-pdf/fpdf"
)

// anchorTable maps stable anchor names (e.g. "features", "gaps",
// "feat-foo-bar") to fpdf link IDs. IDs are allocated lazily on first Get
// so callers can reference an anchor before the corresponding content has
// been rendered. Mark calls SetLink to bind the current page+y to the
// previously-allocated ID, completing the link target.
type anchorTable struct {
	doc   *fpdf.Fpdf
	links map[string]int
}

func newAnchorTable(doc *fpdf.Fpdf) *anchorTable {
	return &anchorTable{doc: doc, links: map[string]int{}}
}

// Get returns the link ID for name, allocating one if it does not yet
// exist. The link ID is stable across calls.
func (a *anchorTable) Get(name string) int {
	if id, ok := a.links[name]; ok {
		return id
	}
	id := a.doc.AddLink()
	a.links[name] = id
	return id
}

// Mark binds the link ID for name to the current page + y position. Must be
// called after the anchored content has been emitted; subsequent clicks on
// that link from elsewhere in the document will jump here.
func (a *anchorTable) Mark(name string) {
	a.doc.SetLink(a.Get(name), -1, -1)
}

var slugifyNonWord = regexp.MustCompile(`[^a-z0-9]+`)

// slugify normalizes a feature name into a stable anchor segment. Mirrors
// the kebab-case slug used by internal/site so cross-references stay
// recognisable across the two outputs.
func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = slugifyNonWord.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}
