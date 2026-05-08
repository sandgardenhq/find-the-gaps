package analyzer

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// TestClipObservationQuotes_PreservesUTF8Boundary pins that cuts in the
// middle of a multi-byte rune do not produce invalid UTF-8. Real docs
// pages routinely contain em-dashes, ellipses, and smart quotes — each
// 3 bytes — and a naive byte-cut at exactly max bytes can split a rune.
// Go's encoding/json silently substitutes U+FFFD for invalid bytes when
// marshaling, so the symptom is degraded LLM input rather than a hard
// error.
func TestClipObservationQuotes_PreservesUTF8Boundary(t *testing.T) {
	// Build a string whose byte length, with the multi-byte rune at the
	// boundary, forces clipObservationQuotes to cut inside the rune.
	prefix := strings.Repeat("a", 1499) // 1499 ASCII bytes
	emDash := "—"                  // 3 bytes (E2 80 94)
	suffix := "tail"
	input := prefix + emDash + suffix
	// max = 1500 lands inside the em-dash rune (between E2 and 80).
	o := driftObservation{DocQuote: input, CodeQuote: input}
	clipped := clipObservationQuotes(o, 1500)

	if !utf8.ValidString(clipped.DocQuote) {
		t.Fatalf("DocQuote is not valid UTF-8: %q", clipped.DocQuote)
	}
	if !utf8.ValidString(clipped.CodeQuote) {
		t.Fatalf("CodeQuote is not valid UTF-8: %q", clipped.CodeQuote)
	}
	// Sanity-check we actually exercised the truncation path (the input
	// was bigger than max).
	if !strings.HasSuffix(clipped.DocQuote, " […]") {
		t.Fatalf("expected truncation marker on DocQuote, got %q", clipped.DocQuote)
	}
}

// TestClipObservationQuotes_NoOpWhenUnderMax pins that strings within
// the byte budget are returned unchanged, regardless of UTF-8 content.
func TestClipObservationQuotes_NoOpWhenUnderMax(t *testing.T) {
	in := "short — string"
	o := driftObservation{DocQuote: in, CodeQuote: in}
	got := clipObservationQuotes(o, 1500)
	if got.DocQuote != in || got.CodeQuote != in {
		t.Fatalf("expected unchanged, got %+v", got)
	}
}

// TestClipToolResult_PreservesUTF8Boundary pins that the same byte-cut
// risk in clipToolResult is handled. Tool results commonly contain
// non-ASCII content (file reads from source files with comments in
// CJK, JSON with emoji, etc.).
func TestClipToolResult_PreservesUTF8Boundary(t *testing.T) {
	// Pure em-dashes (3 bytes each). Both the initial cut at max*4 and
	// every power-of-two halving land at a byte position that's not a
	// multiple of 3, so a naive byte slice will fall mid-rune.
	body := strings.Repeat("—", 5000)
	out := clipToolResult(body, 100)

	if !utf8.ValidString(out) {
		t.Fatalf("clipToolResult produced invalid UTF-8 near end: %q", out[max(0, len(out)-20):])
	}
	if !strings.Contains(out, "[truncated") {
		t.Fatalf("expected truncation marker, got %q", out)
	}
}
