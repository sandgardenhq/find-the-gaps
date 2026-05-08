package analyzer

import "unicode/utf8"

// truncateAtRuneBoundary returns s truncated to at most n bytes, backing
// up so the result ends on a rune boundary (no partial multi-byte UTF-8
// sequence at the tail). Used by the clipping helpers in drift.go and
// agent_loop.go: a naive s[:n] cut on prose containing em-dashes, smart
// quotes, ellipses, or any non-ASCII content can split a rune; encoding/json
// then substitutes U+FFFD when the prompt is marshaled for the LLM,
// degrading the model's input.
func truncateAtRuneBoundary(s string, n int) string {
	if n >= len(s) {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
