package pdf

// Visual tokens mirror the --ftg-* design tokens defined in
// internal/site/assets/theme/hextra/assets/css/custom.css.
//
// SYNC WITH custom.css:
//   --ftg-good            -> colorGoodFg
//   --ftg-good-bg         -> colorGoodBg
//   --ftg-good-border     -> colorGoodBorder
//   --ftg-bad             -> colorBadFg
//   --ftg-bad-bg          -> colorBadBg
//   --ftg-bad-border      -> colorBadBorder
//   --ftg-warn            -> colorWarnFg
//   --ftg-warn-bg         -> colorWarnBg
//   --ftg-warn-border     -> colorWarnBorder
//   --ftg-neutral         -> colorNeutralFg
//   --ftg-neutral-bg      -> colorNeutralBg
//   --ftg-neutral-border  -> colorNeutralBorder
//   --ftg-card-bg         -> colorCardBg
//   --ftg-card-border     -> colorCardBorder
//   --ftg-muted           -> colorMutedFg
//   body (Tailwind slate-900)    -> colorBodyFg
//   link (Tailwind blue-600)     -> colorLinkFg
//
// When custom.css changes a token, update the matching block here and
// regenerate the sample PDF to verify the new colour reads as the site.
const (
	colorGoodFg     = 0x16a34a // #16a34a
	colorGoodBg     = 0xdcfce7 // #dcfce7
	colorGoodBorder = 0x86efac // #86efac

	colorBadFg     = 0xdc2626 // #dc2626
	colorBadBg     = 0xfee2e2 // #fee2e2
	colorBadBorder = 0xfca5a5 // #fca5a5

	colorWarnFg     = 0xd97706 // #d97706
	colorWarnBg     = 0xfef3c7 // #fef3c7
	colorWarnBorder = 0xfcd34d // #fcd34d

	colorNeutralFg     = 0x475569 // #475569
	colorNeutralBg     = 0xf1f5f9 // #f1f5f9
	colorNeutralBorder = 0xcbd5e1 // #cbd5e1

	colorCardBg     = 0xffffff // #ffffff
	colorCardBorder = 0xe2e8f0 // #e2e8f0

	colorBodyFg  = 0x0f172a // Tailwind slate-900
	colorMutedFg = 0x64748b // --ftg-muted
	colorLinkFg  = 0x2563eb // Tailwind blue-600
)

// rgb breaks a packed 0xRRGGBB hex constant into the (r, g, b) tuple fpdf
// expects. Keeping the colour table as packed hex keeps the SYNC block
// above readable; this helper does the unpacking at the call site.
func rgb(hex int) (int, int, int) {
	return (hex >> 16) & 0xff, (hex >> 8) & 0xff, hex & 0xff
}

// setTextColor / setFillColor / setDrawColor wrap fpdf's three-arg
// setters so callers can pass a single packed hex constant. Keeps
// the SYNC table above as the only place colours are written down.
func setTextColor(doc fpdfDoc, hex int) {
	r, g, b := rgb(hex)
	doc.SetTextColor(r, g, b)
}

func setFillColor(doc fpdfDoc, hex int) {
	r, g, b := rgb(hex)
	doc.SetFillColor(r, g, b)
}

func setDrawColor(doc fpdfDoc, hex int) {
	r, g, b := rgb(hex)
	doc.SetDrawColor(r, g, b)
}

// fpdfDoc is the slice of *fpdf.Fpdf the colour setters need. Declared as
// a tiny interface so style.go does not import fpdf directly.
type fpdfDoc interface {
	SetTextColor(r, g, b int)
	SetFillColor(r, g, b int)
	SetDrawColor(r, g, b int)
}

// Margins, in inches.
const (
	marginLeft   = 0.75
	marginRight  = 0.75
	marginTop    = 1.0
	marginBottom = 1.0
)

// Font sizes, in points.
const (
	fontSizeTitle  = 28
	fontSizeH1     = 18
	fontSizeH2     = 14
	fontSizeH3     = 12
	fontSizeBody   = 11
	fontSizeMeta   = 10
	fontSizeFooter = 9
	fontSizePill   = 9
	fontSizeBadge  = 8
	fontSizeStat   = 24
)
