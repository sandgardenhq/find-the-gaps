package pdf

import "strings"

// sanitize rewrites a UTF-8 input so it survives fpdf's WinAnsi-encoded
// core fonts. Anything outside the printable ASCII range either gets a
// known ASCII substitution (em-dash → "-", curly quotes → straight quote,
// etc.) or is dropped. Core fonts have no glyph for em-dash and render
// the raw multi-byte UTF-8 as mojibake; this layer keeps the output
// readable without embedding a Unicode-capable font.
//
// Long-term we may switch to an embedded TTF font; for now ASCII-only
// keeps the PDF small and dependency-free.
func sanitize(s string) string {
	if s == "" {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '–', '—': // en-dash, em-dash
			b.WriteByte('-')
		case '‘', '’': // curly single quotes
			b.WriteByte('\'')
		case '“', '”': // curly double quotes
			b.WriteByte('"')
		case '…': // ellipsis
			b.WriteString("...")
		case '→', '←', '↔': // arrows
			b.WriteString("->")
		case ' ': // non-breaking space
			b.WriteByte(' ')
		case '•': // bullet
			b.WriteByte('*')
		default:
			if r >= 0x20 && r < 0x7F {
				b.WriteRune(r)
				continue
			}
			// Latin-1 supplement (0xA0-0xFF) is covered by WinAnsi; pass
			// through.
			if r >= 0xA1 && r <= 0xFF {
				b.WriteRune(r)
				continue
			}
			// Anything else: drop.
		}
	}
	return b.String()
}
