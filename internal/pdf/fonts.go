package pdf

import (
	_ "embed"

	"github.com/go-pdf/fpdf"
)

// Inter font faces. Embedded as TTFs so the binary is self-contained:
// `report.pdf` always renders with the same typography Hextra uses on
// the site, regardless of what fonts are installed on the user's
// machine. Sourced from rsms/inter v4.1; license retained at
// assets/fonts/LICENSE-Inter.txt (SIL Open Font License 1.1).

//go:embed assets/fonts/Inter-Regular.ttf
var interRegular []byte

//go:embed assets/fonts/Inter-Bold.ttf
var interBold []byte

//go:embed assets/fonts/Inter-Italic.ttf
var interItalic []byte

// registerFonts installs the Inter family on doc as the "Inter" font.
// Three faces are registered to match the styles fpdf understands: "",
// "B", and "I". Bold-italic falls back to italic; report content does
// not currently mix the two.
//
// AddUTF8FontFromBytes uses subsetting, so each PDF carries only the
// glyphs actually referenced — a single-page report adds ~30KB rather
// than the full ~400KB face. The binary itself grows by ~1.25MB to host
// the three embedded TTFs.
func registerFonts(doc *fpdf.Fpdf) {
	doc.AddUTF8FontFromBytes("Inter", "", interRegular)
	doc.AddUTF8FontFromBytes("Inter", "B", interBold)
	doc.AddUTF8FontFromBytes("Inter", "I", interItalic)
}

// bodyFont is the family name used by every text-emitting call in this
// package. Centralised so a future swap (e.g. embedding a different
// type family) lives in one place.
const bodyFont = "Inter"
