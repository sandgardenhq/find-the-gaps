package pdf

import (
	_ "embed"

	"github.com/go-pdf/fpdf"
)

// Inter (sans body) and Poppins (display headings) embedded as TTFs.
// Both faces are licensed under the SIL Open Font License 1.1 (license
// texts retained at assets/fonts/LICENSE-{Inter,Poppins}.txt). fpdf's
// AddUTF8FontFromBytes does subsetting at output time, so the per-PDF
// cost stays in the tens of KB even though the binary embeds ~2.5MB
// of font data.

//go:embed assets/fonts/Inter-Regular.ttf
var interRegular []byte

//go:embed assets/fonts/Inter-Bold.ttf
var interBold []byte

//go:embed assets/fonts/Inter-Italic.ttf
var interItalic []byte

//go:embed assets/fonts/Poppins-Regular.ttf
var poppinsRegular []byte

//go:embed assets/fonts/Poppins-Bold.ttf
var poppinsBold []byte

//go:embed assets/fonts/Poppins-Italic.ttf
var poppinsItalic []byte

// registerFonts installs both font families on doc. Three faces each:
// "" (regular), "B" (bold), "I" (italic). Bold-italic falls back to
// italic; report content does not currently mix the two.
func registerFonts(doc *fpdf.Fpdf) {
	doc.AddUTF8FontFromBytes("Inter", "", interRegular)
	doc.AddUTF8FontFromBytes("Inter", "B", interBold)
	doc.AddUTF8FontFromBytes("Inter", "I", interItalic)

	doc.AddUTF8FontFromBytes("Poppins", "", poppinsRegular)
	doc.AddUTF8FontFromBytes("Poppins", "B", poppinsBold)
	doc.AddUTF8FontFromBytes("Poppins", "I", poppinsItalic)
}

// bodyFont is the sans-serif used for body text, pills, badges, and
// the footer. Mirrors --ftg-font-body in custom.css.
const bodyFont = "Inter"

// titleFont is the geometric sans display face used for the cover
// title, section headings, sub-section headings, feature-card
// headings, and the TOC heading. Mirrors --ftg-font-display:
// 'Poppins' in custom.css.
const titleFont = "Poppins"
