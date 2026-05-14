package pdf

// Visual tokens mirror the brand block in
// internal/site/assets/theme/hextra/assets/css/custom.css (lines
// 780-823, html.light + --ftg-font-* declarations).
//
// SYNC WITH custom.css:
//   --ftg-paper-warm     -> colorPaperWarm  (body / page background)
//   --ftg-surface        -> colorSurface    (card fill)
//   --ftg-rule           -> colorRule       (card / divider stroke)
//   --ftg-rule-soft      -> colorRuleSoft   (very light dividers)
//   --ftg-ink            -> colorInk        (body text)
//   --ftg-ink-soft       -> colorInkSoft    (secondary text)
//   --ftg-ink-mute       -> colorInkMute    (muted text)
//   --ftg-ink-faint      -> colorInkFaint
//   --ftg-magenta        -> colorMagenta    (link / brand accent)
//   --ftg-magenta-deep   -> colorMagentaDeep
//   --ftg-sev-large      -> colorSevLarge   (Large bucket / stripe)
//   --ftg-sev-medium     -> colorSevMedium  (Medium bucket / stripe)
//   --ftg-sev-small      -> colorSevSmall   (Small bucket / stripe)
//   --ftg-sev-large-tint -> colorSevLargeTint (pill fill, large)
//   --ftg-sev-medium-tint -> colorSevMediumTint
//   --ftg-sev-small-tint  -> colorSevSmallTint
//   --ftg-badge-layer-fg/bg / -user-fg/bg / -internal-fg/bg /
//     -doc-fg/bg / (doc-undocumented = sev-large)
//
// When custom.css changes a token, update the matching block here and
// regenerate the sample PDF to verify the new colour reads as the site.
const (
	// Paper / surfaces.
	colorPaperWarm = 0xf6f1e6 // #f6f1e6 — body background
	colorSurface   = 0xffffff // card / pill fill
	colorRule      = 0xe8e4dc // card / divider stroke
	colorRuleSoft  = 0xf1ede4 // very light dividers

	// Ink (text colours).
	colorInk      = 0x15131a
	colorInkSoft  = 0x2a2730
	colorInkMute  = 0x5a5663
	colorInkFaint = 0x8b8794

	// Brand accents.
	colorMagenta     = 0xff0096 // links / active nav
	colorMagentaDeep = 0xc40076 // hover + sev-large foreground

	// Severity foregrounds (used for stripes + pill text).
	colorSevLarge  = 0xc40076 // magenta-deep
	colorSevMedium = 0xb86d0a // amber-brown
	colorSevSmall  = 0x4ea033 // picnic-deep (green)

	// Severity tints (~ 10% alpha over paper). Pre-blended so we can
	// pass solid hex into fpdf which has no alpha.
	colorSevLargeTint  = 0xfae5f1 // ≈ rgba(196,0,118,0.10) on paper-warm
	colorSevMediumTint = 0xf6e7d3 // ≈ rgba(184,109,10,0.10) on paper-warm
	colorSevSmallTint  = 0xe5efdc // ≈ rgba(78,160,51,0.10) on paper-warm

	// Badge palettes (foreground / background).
	colorBadgeLayerFg    = 0x5a5663
	colorBadgeLayerBg    = 0xf1ede4
	colorBadgeUserFg     = 0x0e8aa0
	colorBadgeUserBg     = 0xe2f4f8 // ≈ rgba(27,183,209,0.12) on paper-warm
	colorBadgeInternalFg = 0x6c5e7a
	colorBadgeInternalBg = 0xece6ee // ≈ rgba(108,94,122,0.10) on paper-warm
	colorBadgeDocFg      = 0x4ea033
	colorBadgeDocBg      = 0xe6f1de // ≈ rgba(111,195,79,0.12) on paper-warm
)

// rgb breaks a packed 0xRRGGBB hex constant into the (r, g, b) tuple
// fpdf expects.
func rgb(hex int) (int, int, int) {
	return (hex >> 16) & 0xff, (hex >> 8) & 0xff, hex & 0xff
}

// setTextColor / setFillColor / setDrawColor wrap fpdf's three-arg
// setters so callers can pass a single packed hex constant.
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
	marginTop    = 0.85
	marginBottom = 0.85
)

// Font sizes, in points. Sized down from the previous round to read
// closer to the site's body rhythm: site body is 16px ≈ 12pt, but PDF
// readers tend to scale up so we step everything one notch smaller.
const (
	fontSizeTitle  = 24 // cover "Find the Gaps"
	fontSizeH1     = 16 // section heading (Features / Gaps / Screenshots)
	fontSizeH2     = 13 // sub-section heading + feature card name
	fontSizeBody   = 10 // card body, descriptions
	fontSizeMeta   = 9  // metadata / muted labels
	fontSizeFooter = 8  // page footer
	fontSizePill   = 8  // priority pill
	fontSizeBadge  = 7  // metadata badge
	fontSizeStat   = 22 // hero stat number
)
