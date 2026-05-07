package reporter

import (
	"net/url"
	"strings"
	"unicode"
)

// PageLabelFromURL derives a short human-readable label for a docs page URL.
// Used as the heading text above per-page card groups in screenshots.md so a
// reader sees a page name rather than a repeated URL — the URL remains
// accessible as a link inside each card body.
//
// The label is the last meaningful path segment with separators swapped for
// spaces and the first letter upper-cased. File extensions like `.html` and
// `.md` are stripped. A root or empty path falls back to the host. An
// unparseable input is returned unchanged so the caller can render something
// rather than an empty heading.
func PageLabelFromURL(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme == "" && u.Host == "" && !strings.Contains(raw, "/")) {
		return raw
	}
	path := strings.Trim(u.Path, "/")
	if path == "" {
		if u.Host != "" {
			return u.Host
		}
		return raw
	}
	parts := strings.Split(path, "/")
	last := parts[len(parts)-1]
	if dot := strings.LastIndex(last, "."); dot > 0 {
		ext := strings.ToLower(last[dot+1:])
		switch ext {
		case "html", "htm", "md", "markdown":
			last = last[:dot]
		}
	}
	last = strings.ReplaceAll(last, "-", " ")
	last = strings.ReplaceAll(last, "_", " ")
	last = strings.TrimSpace(last)
	if last == "" {
		if u.Host != "" {
			return u.Host
		}
		return raw
	}
	runes := []rune(last)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}
