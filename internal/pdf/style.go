package pdf

// Color tokens. Values are sourced from the Hextra theme override at
// internal/site/templates/hextra-custom.css so the PDF reads as the same
// document as the rendered site. We hard-code rather than parse CSS at
// runtime; the cross-reference comments below keep future sync easy.
const (
	// Brand / accent. Used for top-level section headings and link text.
	// Matches --primary-hue in hextra-custom.css.
	colorBrandR, colorBrandG, colorBrandB = 0x1f, 0x6f, 0xeb

	// Body text. Near-black, slightly warm.
	colorBodyR, colorBodyG, colorBodyB = 0x1f, 0x23, 0x28

	// Muted gray. Used for metadata (timestamps, URLs).
	colorMutedR, colorMutedG, colorMutedB = 0x6a, 0x73, 0x7d

	// Priority pill backgrounds. Hand-tuned to read clearly on a white page.
	colorLargeR, colorLargeG, colorLargeB    = 0xd1, 0x39, 0x39 // red
	colorMediumR, colorMediumG, colorMediumB = 0xc4, 0x8a, 0x16 // amber
	colorSmallR, colorSmallG, colorSmallB    = 0x6a, 0x73, 0x7d // neutral gray
)

// Margins, in inches.
const (
	marginLeft   = 0.75
	marginRight  = 0.75
	marginTop    = 1.0
	marginBottom = 1.0
)

// Font sizes, in points.
const (
	fontSizeTitle    = 28
	fontSizeH1       = 18
	fontSizeH2       = 14
	fontSizeH3       = 12
	fontSizeBody     = 11
	fontSizeMeta     = 10
	fontSizeFooter   = 9
)
