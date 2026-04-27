// internal/site/slug.go
package site

import (
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// featureSlug converts a human-readable feature name into a deterministic
// kebab-case identifier suitable for a URL path segment.
// Empty or all-non-alphanumeric inputs return "".
func featureSlug(s string) string {
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	stripped, _, _ := transform.String(t, s)

	var b strings.Builder
	prevDash := true
	for _, r := range stripped {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
			prevDash = false
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// resolveSlugs returns a name → slug map for the given names. Collisions are
// resolved by appending -2, -3, ... in input order so that the first appearance
// of a slug keeps the unsuffixed form. The check is against the set of slugs
// already emitted (not just bases), so a literal name that slugifies to a
// previously-emitted suffix bumps to the next free counter rather than
// colliding. Names that produce empty slugs map to the empty string and are
// not deduplicated.
func resolveSlugs(names []string) map[string]string {
	out := make(map[string]string, len(names))
	taken := make(map[string]bool, len(names))
	for _, n := range names {
		base := featureSlug(n)
		if base == "" {
			out[n] = ""
			continue
		}
		candidate := base
		for i := 2; taken[candidate]; i++ {
			candidate = base + "-" + strconv.Itoa(i)
		}
		taken[candidate] = true
		out[n] = candidate
	}
	return out
}
